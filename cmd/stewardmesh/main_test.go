package main

// Requirement: REQ-FOUNDATION-001.

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/config"
)

func TestInitializeFoundationWithMemoryAdapter(t *testing.T) {
	configuration := config.FromEnv()
	configuration.RepositoryDriver = config.RepositoryDriverMemory
	configuration.OrganizationID = "clean-install"
	configuration.OrganizationName = "Clean Install"
	runtime, err := initializeFoundation(context.Background(), configuration)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.close()
	if runtime.organization.ID != "clean-install" || runtime.organization.Name != "Clean Install" {
		t.Fatalf("unexpected organization %#v", runtime.organization)
	}
}

func TestInitializeFoundationWithPostgres(t *testing.T) {
	databaseURL := os.Getenv("STEWARDMESH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("STEWARDMESH_TEST_DATABASE_URL is not configured")
	}
	configuration := config.FromEnv()
	configuration.RepositoryDriver = config.RepositoryDriverPostgres
	configuration.DatabaseURL = databaseURL
	configuration.OrganizationID = fmt.Sprintf("clean-install-%d", time.Now().UnixNano())
	configuration.OrganizationName = "PostgreSQL Clean Install"
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	runtime, err := initializeFoundation(ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.close()
	if runtime.organization.ID != configuration.OrganizationID {
		t.Fatalf("unexpected organization %#v", runtime.organization)
	}
}
