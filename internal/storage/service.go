package storage

// Requirements: REQ-STORAGE-001, REQ-EXCHANGE-001. Features: storage.blobs, migration.packages.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/maxlemke/stewardmesh/internal/foundation"
)

var (
	recordIDPattern  = regexp.MustCompile(`^[a-f0-9]{32}$`)
	sha256Pattern    = regexp.MustCompile(`^[a-f0-9]{64}$`)
	referencePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	resourcePattern  = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)
)

type ServiceConfig struct {
	OrganizationID string
	DownloadTTL    time.Duration
	Now            func() time.Time
}

type Service struct {
	metadata       MetadataStore
	objects        ObjectStore
	auditor        foundation.Auditor
	organizationID string
	downloadTTL    time.Duration
	now            func() time.Time
}

func NewService(metadata MetadataStore, objects ObjectStore, auditor foundation.Auditor, configuration ServiceConfig) (*Service, error) {
	if metadata == nil || objects == nil || auditor == nil {
		return nil, errors.New("Vault metadata store, object store, and auditor are required")
	}
	configuration.OrganizationID = strings.TrimSpace(configuration.OrganizationID)
	if configuration.OrganizationID == "" {
		return nil, errors.New("Vault organization id is required")
	}
	if objects.MaximumBytes() <= 0 {
		return nil, errors.New("Vault object store must enforce a positive size limit")
	}
	if configuration.DownloadTTL == 0 {
		configuration.DownloadTTL = 5 * time.Minute
	}
	if configuration.DownloadTTL < time.Minute || configuration.DownloadTTL > 15*time.Minute {
		return nil, errors.New("Vault download authorization TTL must be between 1m and 15m")
	}
	if configuration.Now == nil {
		configuration.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{
		metadata: metadata, objects: objects, auditor: auditor,
		organizationID: configuration.OrganizationID, downloadTTL: configuration.DownloadTTL, now: configuration.Now,
	}, nil
}

func (s *Service) MaximumUploadBytes() int64 { return s.objects.MaximumBytes() }

func (s *Service) ListBlobs(ctx context.Context) ([]Blob, error) {
	return s.metadata.ListBlobs(ctx, s.organizationID)
}

func (s *Service) GetBlob(ctx context.Context, id string) (Blob, error) {
	id = strings.TrimSpace(id)
	if !recordIDPattern.MatchString(id) {
		return Blob{}, ErrInvalidInput
	}
	return s.metadata.GetBlob(ctx, s.organizationID, id)
}

func (s *Service) CreateBlob(ctx context.Context, input CreateBlobInput) (Blob, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.MediaType = strings.TrimSpace(input.MediaType)
	input.SourceSystemID = strings.TrimSpace(input.SourceSystemID)
	input.SourceRecordID = strings.TrimSpace(input.SourceRecordID)
	input.ResourceType = strings.TrimSpace(input.ResourceType)
	input.ResourceID = strings.TrimSpace(input.ResourceID)
	if err := validateCreateBlobInput(input); err != nil {
		return Blob{}, err
	}
	id, err := foundation.NewCorrelationID()
	if err != nil {
		return Blob{}, fmt.Errorf("create Vault blob id: %w", err)
	}
	organizationHash := sha256.Sum256([]byte(s.organizationID))
	key := hex.EncodeToString(organizationHash[:16]) + "/" + id
	stored, err := s.objects.Put(ctx, key, input.MediaType, input.Content)
	if err != nil {
		return Blob{}, err
	}
	createdAt := s.now().UTC()
	blob := Blob{
		ID: id, OrganizationID: s.organizationID, Name: input.Name, MediaType: input.MediaType,
		SizeBytes: stored.SizeBytes, SHA256: stored.SHA256, Provider: s.objects.Provider(),
		SourceSystemID: input.SourceSystemID, SourceRecordID: input.SourceRecordID,
		ResourceType: input.ResourceType, ResourceID: input.ResourceID,
		CreatedBy: actorFromContext(ctx), CreatedAt: createdAt, objectKey: key,
	}
	created, err := s.metadata.CreateBlob(ctx, blob)
	if err != nil {
		if cleanupErr := s.objects.Delete(ctx, key); cleanupErr != nil {
			return Blob{}, errors.Join(err, fmt.Errorf("roll back Vault object: %w", cleanupErr))
		}
		return Blob{}, err
	}
	if err := s.audit(ctx, "vault.blob.created", created, map[string]string{
		"provider": created.Provider, "mediaType": created.MediaType,
		"sizeBytes": fmt.Sprint(created.SizeBytes), "sha256": created.SHA256,
		"sourceSystemId": created.SourceSystemID, "sourceRecordId": created.SourceRecordID,
		"resourceType": created.ResourceType, "resourceId": created.ResourceID,
	}); err != nil {
		return Blob{}, fmt.Errorf("audit Vault blob creation: %w", err)
	}
	return created, nil
}

// ImportBlob creates a blob under its portable ID after independently
// verifying the package checksum. Exact replays are unchanged; conflicting
// bytes or metadata never overwrite an existing object.
func (s *Service) ImportBlob(ctx context.Context, input ImportBlobInput) (Blob, bool, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.SHA256 = strings.TrimSpace(input.SHA256)
	createInput := CreateBlobInput{
		Name: strings.TrimSpace(input.Name), MediaType: strings.TrimSpace(input.MediaType),
		SourceSystemID: strings.TrimSpace(input.SourceSystemID), SourceRecordID: strings.TrimSpace(input.SourceRecordID),
		ResourceType: strings.TrimSpace(input.ResourceType), ResourceID: strings.TrimSpace(input.ResourceID), Content: input.Content,
	}
	if !recordIDPattern.MatchString(input.ID) || !sha256Pattern.MatchString(input.SHA256) ||
		input.SizeBytes < 0 || input.SizeBytes > s.objects.MaximumBytes() || validateCreateBlobInput(createInput) != nil {
		return Blob{}, false, ErrInvalidInput
	}
	if existing, err := s.metadata.GetBlob(ctx, s.organizationID, input.ID); err == nil {
		if sameImportedBlob(existing, input) {
			return existing, false, nil
		}
		return Blob{}, false, ErrConflict
	} else if !errors.Is(err, ErrNotFound) {
		return Blob{}, false, err
	}
	organizationHash := sha256.Sum256([]byte(s.organizationID))
	key := hex.EncodeToString(organizationHash[:16]) + "/" + input.ID
	stored, err := s.objects.Put(ctx, key, createInput.MediaType, createInput.Content)
	objectCreated := err == nil
	if errors.Is(err, ErrConflict) {
		stored, err = s.verifyStoredObject(ctx, key)
	}
	if err != nil {
		return Blob{}, false, err
	}
	if stored.SizeBytes != input.SizeBytes || stored.SHA256 != input.SHA256 {
		if objectCreated {
			_ = s.objects.Delete(ctx, key)
		}
		return Blob{}, false, ErrIntegrity
	}
	createdAt := s.now().UTC()
	blob := Blob{
		ID: input.ID, OrganizationID: s.organizationID, Name: createInput.Name, MediaType: createInput.MediaType,
		SizeBytes: stored.SizeBytes, SHA256: stored.SHA256, Provider: s.objects.Provider(),
		SourceSystemID: createInput.SourceSystemID, SourceRecordID: createInput.SourceRecordID,
		ResourceType: createInput.ResourceType, ResourceID: createInput.ResourceID,
		CreatedBy: actorFromContext(ctx), CreatedAt: createdAt, objectKey: key,
	}
	created, err := s.metadata.CreateBlob(ctx, blob)
	if err != nil {
		if existing, getErr := s.metadata.GetBlob(ctx, s.organizationID, input.ID); getErr == nil && sameImportedBlob(existing, input) {
			return existing, false, nil
		}
		if objectCreated {
			_ = s.objects.Delete(ctx, key)
		}
		return Blob{}, false, err
	}
	if err := s.audit(ctx, "vault.blob.imported", created, map[string]string{
		"provider": created.Provider, "mediaType": created.MediaType, "sizeBytes": fmt.Sprint(created.SizeBytes),
		"sha256": created.SHA256, "sourceSystemId": created.SourceSystemID,
		"resourceType": created.ResourceType, "resourceId": created.ResourceID,
	}); err != nil {
		return Blob{}, false, fmt.Errorf("audit Vault blob import: %w", err)
	}
	return created, true, nil
}

func (s *Service) verifyStoredObject(ctx context.Context, key string) (StoredObject, error) {
	content, err := s.objects.Open(ctx, key)
	if err != nil {
		return StoredObject{}, err
	}
	defer content.Close()
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(content, s.objects.MaximumBytes()+1))
	if err != nil {
		return StoredObject{}, err
	}
	if written > s.objects.MaximumBytes() {
		return StoredObject{}, ErrTooLarge
	}
	return StoredObject{SizeBytes: written, SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

func sameImportedBlob(existing Blob, input ImportBlobInput) bool {
	return existing.ID == input.ID && existing.Name == strings.TrimSpace(input.Name) && existing.MediaType == strings.TrimSpace(input.MediaType) &&
		existing.SizeBytes == input.SizeBytes && existing.SHA256 == strings.TrimSpace(input.SHA256) &&
		existing.SourceSystemID == strings.TrimSpace(input.SourceSystemID) && existing.SourceRecordID == strings.TrimSpace(input.SourceRecordID) &&
		existing.ResourceType == strings.TrimSpace(input.ResourceType) && existing.ResourceID == strings.TrimSpace(input.ResourceID)
}

func (s *Service) OpenBlob(ctx context.Context, id string) (Blob, io.ReadCloser, error) {
	blob, err := s.GetBlob(ctx, id)
	if err != nil {
		return Blob{}, nil, err
	}
	content, err := s.objects.Open(ctx, blob.objectKey)
	if err != nil {
		return Blob{}, nil, err
	}
	return blob, &integrityReader{reader: content, expected: blob.SHA256, hash: sha256.New()}, nil
}

func (s *Service) OpenAuthorizedBlob(ctx context.Context, id, token string) (Blob, io.ReadCloser, error) {
	blob, err := s.GetBlob(ctx, id)
	if err != nil {
		return Blob{}, nil, err
	}
	if err := s.objects.ValidateDownload(ctx, blob.objectKey, token); err != nil {
		return Blob{}, nil, err
	}
	content, err := s.objects.Open(ctx, blob.objectKey)
	if err != nil {
		return Blob{}, nil, err
	}
	return blob, &integrityReader{reader: content, expected: blob.SHA256, hash: sha256.New()}, nil
}

func (s *Service) AuthorizeDownload(ctx context.Context, id string) (DownloadAuthorization, error) {
	blob, err := s.GetBlob(ctx, id)
	if err != nil {
		return DownloadAuthorization{}, err
	}
	objectAuthorization, err := s.objects.AuthorizeDownload(ctx, blob.objectKey, blob.Name, s.downloadTTL)
	if err != nil {
		return DownloadAuthorization{}, err
	}
	authorization := DownloadAuthorization{URL: objectAuthorization.URL, ExpiresAt: s.now().UTC().Add(s.downloadTTL)}
	if objectAuthorization.Token != "" {
		authorization.URL = "?token=" + objectAuthorization.Token
	}
	if err := s.audit(ctx, "vault.blob.download_authorized", blob, map[string]string{
		"provider": blob.Provider, "expiresAt": authorization.ExpiresAt.Format(time.RFC3339),
	}); err != nil {
		return DownloadAuthorization{}, fmt.Errorf("audit Vault download authorization: %w", err)
	}
	return authorization, nil
}

func validateCreateBlobInput(input CreateBlobInput) error {
	if input.Content == nil || input.Name == "" || utf8.RuneCountInString(input.Name) > 255 || strings.ContainsAny(input.Name, "/\\\x00\r\n") {
		return ErrInvalidInput
	}
	for _, character := range input.Name {
		if character < 0x20 || character == 0x7f {
			return ErrInvalidInput
		}
	}
	parsedType, _, err := mime.ParseMediaType(input.MediaType)
	if err != nil || parsedType != input.MediaType || len(input.MediaType) > 127 {
		return ErrInvalidInput
	}
	if (input.SourceSystemID == "") != (input.SourceRecordID == "") ||
		(input.SourceSystemID != "" && (!referencePattern.MatchString(input.SourceSystemID) || !referencePattern.MatchString(input.SourceRecordID))) {
		return ErrInvalidInput
	}
	if (input.ResourceType == "") != (input.ResourceID == "") ||
		(input.ResourceType != "" && (!resourcePattern.MatchString(input.ResourceType) || !referencePattern.MatchString(input.ResourceID))) {
		return ErrInvalidInput
	}
	return nil
}

func actorFromContext(ctx context.Context) string {
	if scope, ok := foundation.ScopeFromContext(ctx); ok && strings.TrimSpace(scope.ActorID) != "" {
		return scope.ActorID
	}
	return "system:unknown"
}

func (s *Service) audit(ctx context.Context, action string, blob Blob, metadata map[string]string) error {
	scope, ok := foundation.ScopeFromContext(ctx)
	if !ok || strings.TrimSpace(scope.CorrelationID) == "" {
		correlationID, err := foundation.NewCorrelationID()
		if err != nil {
			return err
		}
		scope = foundation.Scope{OrganizationID: s.organizationID, ActorID: actorFromContext(ctx), CorrelationID: correlationID}
		ctx = foundation.WithScope(ctx, scope)
	}
	eventID, err := foundation.NewCorrelationID()
	if err != nil {
		return err
	}
	metadata["requirementId"] = RequirementID
	metadata["featureId"] = FeatureID
	return s.auditor.Record(ctx, foundation.AuditEvent{
		ID: eventID, OrganizationID: s.organizationID, ActorID: actorFromContext(ctx),
		CorrelationID: scope.CorrelationID, Action: action, ResourceType: "blob", ResourceID: blob.ID,
		OccurredAt: s.now().UTC(), Metadata: metadata,
	})
}

type integrityReader struct {
	reader   io.ReadCloser
	expected string
	hash     interface {
		io.Writer
		Sum([]byte) []byte
	}
	verified bool
}

func (r *integrityReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		_, _ = r.hash.Write(p[:n])
	}
	if errors.Is(err, io.EOF) && !r.verified {
		r.verified = true
		if hex.EncodeToString(r.hash.Sum(nil)) != r.expected {
			return n, ErrIntegrity
		}
	}
	return n, err
}

func (r *integrityReader) Close() error { return r.reader.Close() }
