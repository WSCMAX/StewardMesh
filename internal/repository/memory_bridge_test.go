package repository_test

// Requirements: REQ-API-001, SEC-MCP-001. Feature: integrations.protocols. GitHub: #14.

import (
	"testing"

	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/repository/contracttest"
)

func TestMemoryBridgeStoreContract(t *testing.T) {
	contracttest.BridgeStore(t, repository.NewMemoryBridgeStore(), "bridge-memory")
}
