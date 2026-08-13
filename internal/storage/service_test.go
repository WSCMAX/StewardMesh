package storage

// Requirement: REQ-STORAGE-001. Feature: storage.blobs.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/foundation"
)

type serviceMetadataStore struct{ blobs map[string]Blob }

func newServiceMetadataStore() *serviceMetadataStore {
	return &serviceMetadataStore{blobs: make(map[string]Blob)}
}
func (s *serviceMetadataStore) ListBlobs(context.Context, string) ([]Blob, error) {
	items := make([]Blob, 0, len(s.blobs))
	for _, blob := range s.blobs {
		items = append(items, blob)
	}
	return items, nil
}
func (s *serviceMetadataStore) GetBlob(_ context.Context, organizationID, id string) (Blob, error) {
	blob, ok := s.blobs[organizationID+id]
	if !ok {
		return Blob{}, ErrNotFound
	}
	return blob, nil
}
func (s *serviceMetadataStore) CreateBlob(_ context.Context, blob Blob) (Blob, error) {
	key := blob.OrganizationID + blob.ID
	if _, ok := s.blobs[key]; ok {
		return Blob{}, ErrConflict
	}
	s.blobs[key] = blob
	return blob, nil
}

func TestServiceCreatesMetadataAndVerifiesContent(t *testing.T) {
	metadata := newServiceMetadataStore()
	objects, err := NewLocalBlobStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	service, err := NewService(metadata, objects, foundation.NopAuditor{}, ServiceConfig{OrganizationID: "example-org", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	ctx := foundation.WithScope(context.Background(), foundation.Scope{OrganizationID: "example-org", ActorID: "account-1", CorrelationID: "request-1"})
	created, err := service.CreateBlob(ctx, CreateBlobInput{
		Name: "evidence.txt", MediaType: "text/plain", Content: strings.NewReader("verified"),
		SourceSystemID: "importer", SourceRecordID: "row-1", ResourceType: "asset", ResourceID: "asset-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.OrganizationID != "example-org" || created.CreatedBy != "account-1" || created.Provider != "local" ||
		created.SizeBytes != 8 || created.SHA256 == "" || created.ObjectKey() == "" || created.CreatedAt != now {
		t.Fatalf("unexpected blob %#v", created)
	}
	_, content, err := service.OpenBlob(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := io.ReadAll(content)
	if err != nil {
		t.Fatal(err)
	}
	_ = content.Close()
	if !bytes.Equal(loaded, []byte("verified")) {
		t.Fatalf("unexpected content %q", loaded)
	}
	authorization, err := service.AuthorizeDownload(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(authorization.URL, "?token=") || authorization.ExpiresAt != now.Add(5*time.Minute) {
		t.Fatalf("unexpected authorization %#v", authorization)
	}
}

func TestServiceRejectsUnsafeMetadata(t *testing.T) {
	objects, err := NewLocalBlobStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(newServiceMetadataStore(), objects, foundation.NopAuditor{}, ServiceConfig{OrganizationID: "example-org"})
	if err != nil {
		t.Fatal(err)
	}
	inputs := []CreateBlobInput{
		{Name: "../secret", MediaType: "text/plain", Content: strings.NewReader("x")},
		{Name: "file.txt", MediaType: "text/plain; charset=utf-8", Content: strings.NewReader("x")},
		{Name: "file.txt", MediaType: "text/plain", SourceSystemID: "source", Content: strings.NewReader("x")},
		{Name: "file.txt", MediaType: "text/plain", ResourceType: "Asset", ResourceID: "one", Content: strings.NewReader("x")},
	}
	for _, input := range inputs {
		if _, err := service.CreateBlob(context.Background(), input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("expected invalid input for %#v, got %v", input, err)
		}
	}
}

func TestImportBlobVerifiesStoredBytesBeforeReturningUnchanged(t *testing.T) {
	original := []byte("original")
	digest := sha256.Sum256(original)
	checksum := hex.EncodeToString(digest[:])
	for _, metadataOnly := range []bool{false, true} {
		mode := "included"
		if metadataOnly {
			mode = "metadata-only"
		}
		for _, failure := range []string{"missing", "same-size-tamper"} {
			t.Run(mode+"/"+failure, func(t *testing.T) {
				objects, err := NewLocalBlobStore(t.TempDir(), 1024)
				if err != nil {
					t.Fatal(err)
				}
				service, err := NewService(newServiceMetadataStore(), objects, foundation.NopAuditor{}, ServiceConfig{OrganizationID: "replay-org"})
				if err != nil {
					t.Fatal(err)
				}
				base := ImportBlobInput{
					ID: strings.Repeat("a", 32), Name: "replay.txt", MediaType: "text/plain", SizeBytes: int64(len(original)), SHA256: checksum,
					SourceSystemID: "archive", SourceRecordID: "archive:replay", Content: bytes.NewReader(original),
				}
				created, wasCreated, err := service.ImportBlob(context.Background(), base)
				if err != nil || !wasCreated {
					t.Fatalf("seed imported blob: created=%t blob=%#v err=%v", wasCreated, created, err)
				}
				if err := objects.Delete(context.Background(), created.objectKey); err != nil {
					t.Fatal(err)
				}
				if failure == "same-size-tamper" {
					if _, err := objects.Put(context.Background(), created.objectKey, "text/plain", strings.NewReader("tampered")); err != nil {
						t.Fatal(err)
					}
				}
				replay := base
				replay.MetadataOnly = metadataOnly
				if metadataOnly {
					replay.Content = nil
				} else {
					replay.Content = bytes.NewReader(original)
				}
				_, replayCreated, replayErr := service.ImportBlob(context.Background(), replay)
				want := ErrNotFound
				if failure == "same-size-tamper" {
					want = ErrIntegrity
				}
				if !errors.Is(replayErr, want) || replayCreated {
					t.Fatalf("unsafe unchanged replay: created=%t err=%v want=%v", replayCreated, replayErr, want)
				}
				exact, inspectErr := service.ImportedBlobExists(context.Background(), replay)
				if inspectErr != nil || exact {
					t.Fatalf("read-only replay inspection trusted stale metadata: exact=%t err=%v", exact, inspectErr)
				}
			})
		}
	}
}
