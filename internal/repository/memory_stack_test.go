package repository_test

// Requirement: REQ-STACK-001. Feature: software.licenses. GitHub: #7.

import (
	"testing"

	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/repository/contracttest"
)

func TestMemoryStackStoreContract(t *testing.T) {
	contracttest.StackStore(t, repository.NewMemoryStackStore(), "memory-stack", "memory")
}
