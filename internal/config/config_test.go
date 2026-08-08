package config

import (
	"strings"
	"testing"
)

func TestLoadSupportsMemoryDevelopmentMode(t *testing.T) {
	t.Setenv("STEWARDMESH_REPOSITORY_DRIVER", "memory")
	t.Setenv("STEWARDMESH_DATABASE_URL", "")
	t.Setenv("STEWARDMESH_ORGANIZATION_ID", "test-organization")
	t.Setenv("STEWARDMESH_ORGANIZATION_NAME", "Test Organization")
	configuration, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if configuration.RepositoryDriver != RepositoryDriverMemory {
		t.Fatalf("expected memory driver, got %q", configuration.RepositoryDriver)
	}
}

func TestValidateRejectsUnknownRepositoryDriver(t *testing.T) {
	configuration := FromEnv()
	configuration.RepositoryDriver = "sqlite"
	if err := configuration.Validate(); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatal("expected unsupported driver to fail validation")
	}
}

func TestValidateRejectsUnsafeAllowedOrigin(t *testing.T) {
	configuration := FromEnv()
	configuration.RepositoryDriver = RepositoryDriverMemory
	configuration.AllowedOrigin = "https://user:password@example.com/path"
	if err := configuration.Validate(); err == nil || !strings.Contains(err.Error(), "ALLOWED_ORIGIN") {
		t.Fatal("expected origin with credentials and a path to fail validation")
	}
}

func TestValidateRequiresDatabaseURLForPostgres(t *testing.T) {
	configuration := FromEnv()
	configuration.RepositoryDriver = RepositoryDriverPostgres
	configuration.DatabaseURL = ""
	if err := configuration.Validate(); err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatal("expected PostgreSQL without a database URL to fail closed")
	}
}
