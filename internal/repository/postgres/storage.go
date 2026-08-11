package postgres

// Requirement: REQ-STORAGE-001. Feature: storage.blobs.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxlemke/stewardmesh/internal/storage"
)

type StorageStore struct{ database *sql.DB }

var _ storage.MetadataStore = (*StorageStore)(nil)

func NewStorageStore(database *sql.DB) (*StorageStore, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	return &StorageStore{database: database}, nil
}

func (s *StorageStore) ListBlobs(ctx context.Context, organizationID string) ([]storage.Blob, error) {
	rows, err := s.database.QueryContext(ctx, `
		SELECT organization_id, id, name, media_type, size_bytes, sha256, provider, object_key,
			source_system_id, source_record_id, resource_type, resource_id, created_by, created_at
		FROM vault_blobs WHERE organization_id = $1 ORDER BY created_at DESC, id
	`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list Vault blobs: %w", err)
	}
	defer rows.Close()
	items := make([]storage.Blob, 0)
	for rows.Next() {
		blob, err := scanVaultBlob(rows)
		if err != nil {
			return nil, fmt.Errorf("scan Vault blob: %w", err)
		}
		items = append(items, blob)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Vault blobs: %w", err)
	}
	return items, nil
}

func (s *StorageStore) GetBlob(ctx context.Context, organizationID, id string) (storage.Blob, error) {
	blob, err := scanVaultBlob(s.database.QueryRowContext(ctx, `
		SELECT organization_id, id, name, media_type, size_bytes, sha256, provider, object_key,
			source_system_id, source_record_id, resource_type, resource_id, created_by, created_at
		FROM vault_blobs WHERE organization_id = $1 AND id = $2
	`, organizationID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return storage.Blob{}, storage.ErrNotFound
	}
	if err != nil {
		return storage.Blob{}, fmt.Errorf("get Vault blob: %w", err)
	}
	return blob, nil
}

func (s *StorageStore) CreateBlob(ctx context.Context, blob storage.Blob) (storage.Blob, error) {
	created, err := scanVaultBlob(s.database.QueryRowContext(ctx, `
		INSERT INTO vault_blobs (
			organization_id, id, name, media_type, size_bytes, sha256, provider, object_key,
			source_system_id, source_record_id, resource_type, resource_id, created_by, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, ''), NULLIF($10, ''), NULLIF($11, ''), NULLIF($12, ''), $13, $14)
		RETURNING organization_id, id, name, media_type, size_bytes, sha256, provider, object_key,
			source_system_id, source_record_id, resource_type, resource_id, created_by, created_at
	`, blob.OrganizationID, blob.ID, blob.Name, blob.MediaType, blob.SizeBytes, blob.SHA256, blob.Provider, blob.ObjectKey(),
		blob.SourceSystemID, blob.SourceRecordID, blob.ResourceType, blob.ResourceID, blob.CreatedBy, blob.CreatedAt))
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return storage.Blob{}, storage.ErrConflict
		}
		return storage.Blob{}, fmt.Errorf("create Vault blob: %w", err)
	}
	return created, nil
}

type vaultScanner interface{ Scan(...any) error }

func scanVaultBlob(scanner vaultScanner) (storage.Blob, error) {
	var blob storage.Blob
	var objectKey string
	var sourceSystemID, sourceRecordID, resourceType, resourceID sql.NullString
	err := scanner.Scan(
		&blob.OrganizationID, &blob.ID, &blob.Name, &blob.MediaType, &blob.SizeBytes, &blob.SHA256,
		&blob.Provider, &objectKey, &sourceSystemID, &sourceRecordID, &resourceType, &resourceID,
		&blob.CreatedBy, &blob.CreatedAt,
	)
	if err != nil {
		return storage.Blob{}, err
	}
	blob.SetObjectKey(objectKey)
	blob.SourceSystemID = sourceSystemID.String
	blob.SourceRecordID = sourceRecordID.String
	blob.ResourceType = resourceType.String
	blob.ResourceID = resourceID.String
	return blob, nil
}
