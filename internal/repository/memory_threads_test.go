package repository_test

import (
	"testing"

	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/repository/contracttest"
)

// Requirement: REQ-THREADS-001.
func TestMemoryThreadsStoreContract(t *testing.T) {
	contracttest.ThreadsStore(t, repository.NewMemoryThreadsStore(), "memory-threads", "memory")
}
