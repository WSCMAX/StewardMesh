package repository_test

import (
	"testing"

	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/repository/contracttest"
)

// Requirement: REQ-ATLAS-CODES-001. Feature: inventory.identifiers.
func TestMemoryAtlasCodesStoreContract(t *testing.T) {
	contracttest.AtlasCodesStore(
		t,
		repository.NewMemoryAtlasCodesStore(),
		"memory-atlas-codes", "asset-one", "memory-atlas-codes-other", "asset-two", "memory",
	)
}
