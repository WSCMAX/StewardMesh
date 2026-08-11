package contracttest

// Requirement: REQ-STORAGE-001. Feature: storage.blobs.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/storage"
)

func StorageStore(t *testing.T, store storage.MetadataStore, organizationID, suffix string) {
	t.Helper()
	ctx := context.Background()
	createdAt := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	blob := storage.Blob{
		ID: "0123456789abcdef0123456789abcdef", OrganizationID: organizationID,
		Name: "evidence.txt", MediaType: "text/plain", SizeBytes: 8,
		SHA256:   "c2cc8e71a926b30b67bdb504ee9218d727d0bb9bc3b1d714736e198df3867d21",
		Provider: "local", CreatedBy: "account-" + suffix, CreatedAt: createdAt,
		SourceSystemID: "contract", SourceRecordID: "row-" + suffix,
		ResourceType: "asset", ResourceID: "asset-" + suffix,
	}
	blob.SetObjectKey("example-org/0123456789abcdef0123456789abcdef")
	created, err := store.CreateBlob(ctx, blob)
	if err != nil {
		t.Fatal(err)
	}
	if created.ObjectKey() != blob.ObjectKey() || created.Name != blob.Name {
		t.Fatalf("unexpected created blob %#v", created)
	}
	if _, err := store.CreateBlob(ctx, blob); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	loaded, err := store.GetBlob(ctx, organizationID, blob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SHA256 != blob.SHA256 || loaded.SourceRecordID != blob.SourceRecordID {
		t.Fatalf("unexpected loaded blob %#v", loaded)
	}
	items, err := store.ListBlobs(ctx, organizationID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != blob.ID {
		t.Fatalf("unexpected blobs %#v", items)
	}
	if _, err := store.GetBlob(ctx, "other-org", blob.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("expected tenant isolation, got %v", err)
	}
}
