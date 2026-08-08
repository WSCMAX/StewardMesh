// Package contracttest defines adapter-neutral repository conformance tests.
// Future DynamoDB adapters must pass the same contracts as memory and PostgreSQL.
// Requirement: REQ-FOUNDATION-001.
package contracttest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/repository"
)

func OrganizationRepository(t testing.TB, subject repository.OrganizationRepository, id string) {
	t.Helper()
	ctx := context.Background()
	if _, err := subject.GetOrganization(ctx, id+"-missing"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected missing organization to return ErrNotFound, got %v", err)
	}
	createdAt := time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC)
	created, wasCreated, err := subject.BootstrapOrganization(ctx, domain.Organization{
		ID:        id,
		Name:      "Contract Organization",
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !wasCreated || created.ID != id {
		t.Fatalf("expected %q to be created, got %#v", id, created)
	}
	updatedAt := createdAt.Add(time.Hour)
	updated, wasCreated, err := subject.BootstrapOrganization(ctx, domain.Organization{
		ID:        id,
		Name:      "Updated Contract Organization",
		CreatedAt: updatedAt,
		UpdatedAt: updatedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if wasCreated || updated.Name != "Updated Contract Organization" {
		t.Fatalf("expected idempotent update, got %#v", updated)
	}
	if !updated.CreatedAt.Equal(created.CreatedAt) {
		t.Fatal("adapter changed the durable creation time")
	}
	loaded, err := subject.GetOrganization(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID != id || loaded.Name != updated.Name {
		t.Fatalf("unexpected persisted organization %#v", loaded)
	}
}
