package repository_test

// Requirement: REQ-STORAGE-001. Feature: storage.blobs.

import (
	"testing"

	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/repository/contracttest"
)

func TestMemoryStorageStoreContract(t *testing.T) {
	contracttest.StorageStore(t, repository.NewMemoryStorageStore(), "example-org", "memory")
}
