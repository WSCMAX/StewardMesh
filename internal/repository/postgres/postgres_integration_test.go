package postgres

// Requirements: REQ-FOUNDATION-001, SEC-GUARD-001, REQ-PEOPLE-001,
// REQ-DIRECTORY-EXPANSION-001.

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/bootstrap"
	"github.com/maxlemke/stewardmesh/internal/repository/contracttest"
)

func TestOrganizationRepositoryIntegration(t *testing.T) {
	databaseURL := os.Getenv("STEWARDMESH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("STEWARDMESH_TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, database); err != nil {
		t.Fatalf("expected idempotent migrations: %v", err)
	}
	repository, err := NewOrganizationRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	contracttest.OrganizationRepository(t, repository, fmt.Sprintf("postgres-contract-%d", time.Now().UnixNano()))
	service, err := bootstrap.NewOrganizationService(repository)
	if err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("integration-%d", time.Now().UnixNano())
	created, wasCreated, err := service.EnsureOrganization(ctx, id, "Integration Organization")
	if err != nil {
		t.Fatal(err)
	}
	if !wasCreated || created.ID != id {
		t.Fatalf("expected organization %q to be created", id)
	}
	updated, wasCreated, err := service.EnsureOrganization(ctx, id, "Updated Integration Organization")
	if err != nil {
		t.Fatal(err)
	}
	if wasCreated || updated.Name != "Updated Integration Organization" {
		t.Fatalf("expected idempotent update, got %#v", updated)
	}
	loaded, err := repository.GetOrganization(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID != id || loaded.Name != updated.Name {
		t.Fatalf("unexpected persisted organization %#v", loaded)
	}
}

func TestGuardStoreIntegration(t *testing.T) {
	databaseURL := os.Getenv("STEWARDMESH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("STEWARDMESH_TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	organizations, err := NewOrganizationRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	organizationID := fmt.Sprintf("guard-integration-%d", time.Now().UnixNano())
	service, err := bootstrap.NewOrganizationService(organizations)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.EnsureOrganization(ctx, organizationID, "Guard Integration"); err != nil {
		t.Fatal(err)
	}
	store, err := NewGuardStore(database)
	if err != nil {
		t.Fatal(err)
	}
	contracttest.GuardStore(t, store, organizationID)
}

func TestPeopleStoreIntegration(t *testing.T) {
	databaseURL := os.Getenv("STEWARDMESH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("STEWARDMESH_TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	organizations, err := NewOrganizationRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	organizationID := fmt.Sprintf("people-integration-%d", time.Now().UnixNano())
	service, err := bootstrap.NewOrganizationService(organizations)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.EnsureOrganization(ctx, organizationID, "People Integration"); err != nil {
		t.Fatal(err)
	}
	store, err := NewPeopleStore(database)
	if err != nil {
		t.Fatal(err)
	}
	contracttest.PeopleStore(t, store, organizationID)
}
