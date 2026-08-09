package repository_test

// Requirement: SEC-GUARD-001.

import (
	"testing"

	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/repository/contracttest"
)

func TestMemoryGuardStoreContract(t *testing.T) {
	contracttest.GuardStore(t, repository.NewMemoryGuardStore(), "memory-guard-organization")
}
