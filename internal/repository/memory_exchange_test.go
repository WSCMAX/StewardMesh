package repository_test

// Requirement: REQ-EXCHANGE-001. Feature: migration.packages. GitHub: #9.

import (
	"testing"

	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/repository/contracttest"
)

func TestMemoryExchangeStoreContract(t *testing.T) {
	contracttest.ExchangeStore(t, repository.NewMemoryExchangeStore(), "memory-exchange", "memory")
}
