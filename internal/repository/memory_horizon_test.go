package repository_test

// Requirement: REQ-HORIZON-001. Feature: lifecycle.planning.

import (
	"testing"

	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/repository/contracttest"
)

func TestMemoryHorizonStoreContract(t *testing.T) {
	contracttest.HorizonStore(t, repository.NewMemoryHorizonStore(), "memory-horizon", "asset-memory", "memory")
}
