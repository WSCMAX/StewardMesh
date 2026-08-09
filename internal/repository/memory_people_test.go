package repository_test

// Requirement: REQ-PEOPLE-001. Feature: identity.directory.

import (
	"testing"

	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/repository/contracttest"
)

func TestMemoryPeopleStoreContract(t *testing.T) {
	contracttest.PeopleStore(t, repository.NewMemoryPeopleStore(), "memory-people-organization")
}
