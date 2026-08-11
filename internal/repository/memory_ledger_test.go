package repository_test

import (
	"testing"

	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/repository/contracttest"
)

// Requirement: REQ-LEDGER-001.
func TestMemoryLedgerStoreContract(t *testing.T) {
	contracttest.LedgerStore(t, repository.NewMemoryLedgerStore(), "memory-ledger", "memory")
}
