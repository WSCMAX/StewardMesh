package repository_test

// Requirements: REQ-DIRECTORY-EXPANSION-002, REQ-DIRECTORY-EXPANSION-003.
// Features: integrations.protocols, identity.directory.

import (
	"testing"

	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/repository/contracttest"
)

func TestMemoryDirectoryImportStoreContract(t *testing.T) {
	contracttest.DirectoryImportStore(t, repository.NewMemoryDirectoryImportStore(), "memory-directory-import", "memory")
}
