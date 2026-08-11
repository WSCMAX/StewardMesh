package repository

// Requirement: REQ-STORAGE-001. Feature: storage.blobs.

import (
	"context"
	"sort"
	"sync"

	"github.com/maxlemke/stewardmesh/internal/storage"
)

type MemoryStorageStore struct {
	mu    sync.RWMutex
	blobs map[string]storage.Blob
}

var _ storage.MetadataStore = (*MemoryStorageStore)(nil)

func NewMemoryStorageStore() *MemoryStorageStore {
	return &MemoryStorageStore{blobs: make(map[string]storage.Blob)}
}

func vaultRecordKey(organizationID, id string) string { return organizationID + "\x00" + id }

func (s *MemoryStorageStore) ListBlobs(_ context.Context, organizationID string) ([]storage.Blob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]storage.Blob, 0)
	for _, blob := range s.blobs {
		if blob.OrganizationID == organizationID {
			items = append(items, blob)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items, nil
}

func (s *MemoryStorageStore) GetBlob(_ context.Context, organizationID, id string) (storage.Blob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	blob, exists := s.blobs[vaultRecordKey(organizationID, id)]
	if !exists {
		return storage.Blob{}, storage.ErrNotFound
	}
	return blob, nil
}

func (s *MemoryStorageStore) CreateBlob(_ context.Context, blob storage.Blob) (storage.Blob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := vaultRecordKey(blob.OrganizationID, blob.ID)
	if _, exists := s.blobs[key]; exists {
		return storage.Blob{}, storage.ErrConflict
	}
	for _, existing := range s.blobs {
		if existing.OrganizationID == blob.OrganizationID && existing.ObjectKey() == blob.ObjectKey() {
			return storage.Blob{}, storage.ErrConflict
		}
	}
	s.blobs[key] = blob
	return blob, nil
}
