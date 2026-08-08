package repository_test

// Requirement: REQ-FOUNDATION-001.

import (
	"testing"

	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/repository/contracttest"
)

func TestMemoryOrganizationRepositoryContract(t *testing.T) {
	contracttest.OrganizationRepository(t, repository.NewMemoryOrganizationRepository(), "memory-contract")
}
