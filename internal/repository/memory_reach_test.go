package repository_test

// Requirement: REQ-REACH-001. Feature: messaging.delivery. GitHub: #12.

import (
	"context"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/reach"
	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/repository/contracttest"
)

func TestMemoryReachStoreContract(t *testing.T) {
	contracttest.ReachStore(t, repository.NewMemoryReachStore(), "reach-memory-organization", "memory")
}

func TestMemoryReachStoreReturnsEmptyCollectionsInsteadOfNull(t *testing.T) {
	store := repository.NewMemoryReachStore()
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	provider, err := store.CreateProvider(context.Background(), reach.Provider{ID: "hook", OrganizationID: "organization-one", Name: "Hook", Kind: reach.ProviderWebhook, EndpointID: "hook", SecretRef: "external:hook", SecretConfigured: true, Enabled: true, Revision: 1, CreatedBy: "admin", UpdatedBy: "admin", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	tests, err := store.ListProviderTests(context.Background(), provider.OrganizationID, provider.ID)
	if err != nil || tests == nil {
		t.Fatalf("provider test collection %#v: %v", tests, err)
	}
}
