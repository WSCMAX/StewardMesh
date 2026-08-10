package config

// Requirements: REQ-PLATFORM-VALKEY-001, SEC-GUARD-001, SEC-HTTP-001.

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

func TestLoadSupportsDisabledMemoryAndValkeyCacheDrivers(t *testing.T) {
	tests := []struct {
		name   string
		driver CacheDriver
		url    string
	}{
		{name: "disabled", driver: CacheDriverNone},
		{name: "memory", driver: CacheDriverMemory},
		{name: "Valkey", driver: CacheDriverValkey, url: "redis://localhost:6379/0"},
		{name: "Valkey TLS", driver: CacheDriverValkey, url: "rediss://cache.example.test:6379/0"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("STEWARDMESH_REPOSITORY_DRIVER", "memory")
			t.Setenv("STEWARDMESH_CACHE_DRIVER", string(test.driver))
			t.Setenv("STEWARDMESH_CACHE_URL", test.url)
			configuration, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if configuration.CacheDriver != test.driver || configuration.CacheURL != test.url {
				t.Fatalf("unexpected cache configuration %#v", configuration)
			}
		})
	}
}

func TestValidateRejectsUnsafeCacheConfigurationWithoutLeakingCredentials(t *testing.T) {
	tests := []struct {
		name   string
		driver CacheDriver
		url    string
	}{
		{name: "unknown driver", driver: "redis"},
		{name: "missing Valkey URL", driver: CacheDriverValkey},
		{name: "unsupported scheme", driver: CacheDriverValkey, url: "http://cache.example.test"},
		{name: "ignored secret", driver: CacheDriverNone, url: "redis://user:super-secret@localhost:6379"},
		{name: "malformed secret", driver: CacheDriverValkey, url: "redis://user:super-secret@"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configuration := FromEnv()
			configuration.RepositoryDriver = RepositoryDriverMemory
			configuration.CacheDriver = test.driver
			configuration.CacheURL = test.url
			err := configuration.Validate()
			if err == nil {
				t.Fatal("expected invalid cache configuration")
			}
			if strings.Contains(err.Error(), "super-secret") {
				t.Fatal("expected cache configuration error to redact credentials")
			}
		})
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
