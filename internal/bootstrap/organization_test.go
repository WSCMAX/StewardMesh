package bootstrap

// Requirement: REQ-FOUNDATION-001.

import (
	"context"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/repository"
)

func TestEnsureOrganizationIsIdempotent(t *testing.T) {
	repo := repository.NewMemoryOrganizationRepository()
	service, err := NewOrganizationService(repo)
	if err != nil {
		t.Fatal(err)
	}
	fixedTime := time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return fixedTime }

	createdOrganization, created, err := service.EnsureOrganization(context.Background(), "local-org", "Local Organization")
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected organization to be created")
	}

	service.now = func() time.Time { return fixedTime.Add(time.Hour) }
	updatedOrganization, created, err := service.EnsureOrganization(context.Background(), "local-org", "Renamed Organization")
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("expected existing organization to be reused")
	}
	if updatedOrganization.Name != "Renamed Organization" {
		t.Fatalf("expected updated display name, got %q", updatedOrganization.Name)
	}
	if !updatedOrganization.CreatedAt.Equal(createdOrganization.CreatedAt) {
		t.Fatal("bootstrap changed the durable creation time")
	}
}

func TestNewOrganizationValidatesIdentity(t *testing.T) {
	for _, id := range []string{"", "contains spaces", "/absolute"} {
		if _, err := NewOrganization(id, "Example"); err == nil {
			t.Fatalf("expected %q to be rejected", id)
		}
	}
	if _, err := NewOrganization("valid.organization-1", "Example"); err != nil {
		t.Fatalf("expected valid organization: %v", err)
	}
}
