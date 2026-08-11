package repository_test

import (
	"testing"

	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/repository/contracttest"
)

// Requirement: REQ-ATLAS-001.
func TestMemoryAtlasStoreContract(t *testing.T) {
	contracttest.AtlasStore(t, repository.NewMemoryAtlasStore(), "memory-atlas", "memory")
}
