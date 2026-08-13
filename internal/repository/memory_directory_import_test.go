package repository_test

// Requirements: REQ-DIRECTORY-EXPANSION-002, REQ-DIRECTORY-EXPANSION-003, REQ-DIRECTORY-EXPANSION-005, REQ-DIRECTORY-EXPANSION-006.
// Features: integrations.protocols, identity.directory.

import (
	"testing"

	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/repository/contracttest"
)

func TestMemoryDirectoryImportStoreContract(t *testing.T) {
	store := repository.NewMemoryDirectoryImportStore()
	contracttest.DirectoryImportStore(t, store, "memory-directory-import", "memory")
	contracttest.DirectoryGroupMappingStore(t, store, "memory-directory-import", "memory-group-mapping")
	contracttest.DirectoryGroupTargetStore(t, store, "memory-directory-groups", "memory-groups")
}
