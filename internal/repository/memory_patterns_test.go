package repository_test

// Requirement: REQ-PATTERNS-001. Feature: templates.schemas. GitHub: #8.

import (
	"testing"

	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/repository/contracttest"
)

func TestMemoryPatternsStoreContract(t *testing.T) {
	contracttest.PatternsStore(t, repository.NewMemoryPatternsStore(), "memory-patterns", "memory")
}
