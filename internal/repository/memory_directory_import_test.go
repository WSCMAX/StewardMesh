package repository_test

// Requirement: REQ-DIRECTORY-EXPANSION-002. Feature: integrations.protocols.

import (
	"testing"

	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/repository/contracttest"
)

func TestMemoryDirectoryImportStoreContract(t *testing.T) {
	contracttest.DirectoryImportStore(t, repository.NewMemoryDirectoryImportStore(), "memory-directory-import", "memory")
}
