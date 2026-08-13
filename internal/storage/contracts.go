// Package storage implements Vault's provider-neutral blob storage boundary.
// Requirement: REQ-STORAGE-001. Feature: storage.blobs.
package storage

import (
	"context"
	"errors"
	"io"
	"time"
)

const (
	RequirementID = "REQ-STORAGE-001"
	FeatureID     = "storage.blobs"
)

var (
	ErrInvalidInput = errors.New("invalid Vault input")
	ErrNotFound     = errors.New("Vault blob not found")
	ErrConflict     = errors.New("Vault blob conflicts with existing data")
	ErrTooLarge     = errors.New("Vault blob exceeds the configured size limit")
	ErrIntegrity    = errors.New("Vault blob failed integrity verification")
)

// Blob is durable metadata. Object keys, credentials, and signed URLs are
// intentionally absent from its JSON representation.
type Blob struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organizationId"`
	Name           string    `json:"name"`
	MediaType      string    `json:"mediaType"`
	SizeBytes      int64     `json:"sizeBytes"`
	SHA256         string    `json:"sha256"`
	Provider       string    `json:"provider"`
	SourceSystemID string    `json:"sourceSystemId,omitempty"`
	SourceRecordID string    `json:"sourceRecordId,omitempty"`
	ResourceType   string    `json:"resourceType,omitempty"`
	ResourceID     string    `json:"resourceId,omitempty"`
	CreatedBy      string    `json:"createdBy"`
	CreatedAt      time.Time `json:"createdAt"`
	objectKey      string
}

// ObjectKey is available only to trusted adapters and services. It is not
// serialized into API responses or exports.
func (b Blob) ObjectKey() string { return b.objectKey }

// SetObjectKey reconstructs a trusted persisted blob without exposing the key
// as a public record field.
func (b *Blob) SetObjectKey(key string) { b.objectKey = key }

type CreateBlobInput struct {
	Name           string
	MediaType      string
	SourceSystemID string
	SourceRecordID string
	ResourceType   string
	ResourceID     string
	Content        io.Reader
}

// ImportBlobInput is Exchange's narrow integrity-preserving seam. IDs and
// checksums come from a verified package; object keys and credentials never do.
type ImportBlobInput struct {
	ID             string
	Name           string
	MediaType      string
	SizeBytes      int64
	SHA256         string
	SourceSystemID string
	SourceRecordID string
	ResourceType   string
	ResourceID     string
	Content        io.Reader
}

type DownloadAuthorization struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type StoredObject struct {
	SizeBytes int64
	SHA256    string
}

type ObjectDownloadAuthorization struct {
	URL   string
	Token string
}

// ObjectStore is implemented by local filesystem and S3-compatible adapters.
// Put must enforce the adapter's maximum object size and verify its checksum.
type ObjectStore interface {
	Provider() string
	MaximumBytes() int64
	Put(ctx context.Context, key, mediaType string, content io.Reader) (StoredObject, error)
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	AuthorizeDownload(ctx context.Context, key, name string, ttl time.Duration) (ObjectDownloadAuthorization, error)
	ValidateDownload(ctx context.Context, key, token string) error
}

type MetadataStore interface {
	ListBlobs(ctx context.Context, organizationID string) ([]Blob, error)
	GetBlob(ctx context.Context, organizationID, id string) (Blob, error)
	CreateBlob(ctx context.Context, blob Blob) (Blob, error)
}
