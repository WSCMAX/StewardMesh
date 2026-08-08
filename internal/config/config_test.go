package config

// Requirements: SEC-GUARD-001, SEC-HTTP-001.

import (
	"strings"
	"testing"
	"time"
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

func TestLocalGuardDefaultsAllowExplicitHTTPDevelopment(t *testing.T) {
	t.Setenv("STEWARDMESH_REPOSITORY_DRIVER", "memory")
	t.Setenv("STEWARDMESH_ALLOWED_ORIGIN", "http://localhost:5173")
	t.Setenv("STEWARDMESH_SESSION_COOKIE_SECURE", "false")
	t.Setenv("STEWARDMESH_SESSION_TTL", "12h")
	configuration, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if configuration.SessionCookieSecure || configuration.SessionTTL != 12*time.Hour {
		t.Fatalf("unexpected local Guard configuration %#v", configuration)
	}
}

func TestSharedListenerRequiresHTTPSCookiesAndBootstrapToken(t *testing.T) {
	configuration := FromEnv()
	configuration.RepositoryDriver = RepositoryDriverMemory
	configuration.Addr = "0.0.0.0:8080"
	configuration.AllowedOrigin = "https://inventory.example.test"
	configuration.SessionCookieSecure = true
	configuration.BootstrapToken = ""
	if err := configuration.Validate(); err == nil || !strings.Contains(err.Error(), "BOOTSTRAP_TOKEN") {
		t.Fatalf("expected shared listener without a bootstrap token to fail, got %v", err)
	}
	configuration.BootstrapToken = strings.Repeat("a", 32)
	if err := configuration.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadRejectsInvalidGuardSecurityConfiguration(t *testing.T) {
	t.Setenv("STEWARDMESH_REPOSITORY_DRIVER", "memory")
	t.Setenv("STEWARDMESH_SESSION_COOKIE_SECURE", "sometimes")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "SESSION_COOKIE_SECURE") {
		t.Fatalf("expected invalid secure-cookie setting to fail, got %v", err)
	}
}
