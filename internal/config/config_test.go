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
		name      string
		driver    CacheDriver
		url       string
		keySecret string
	}{
		{name: "disabled", driver: CacheDriverNone},
		{name: "memory", driver: CacheDriverMemory},
		{name: "Valkey", driver: CacheDriverValkey, url: "redis://localhost:6379/0", keySecret: strings.Repeat("s", 32)},
		{name: "Valkey TLS", driver: CacheDriverValkey, url: "rediss://cache.example.test:6379/0", keySecret: strings.Repeat("s", 32)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("STEWARDMESH_REPOSITORY_DRIVER", "memory")
			t.Setenv("STEWARDMESH_CACHE_DRIVER", string(test.driver))
			t.Setenv("STEWARDMESH_CACHE_URL", test.url)
			t.Setenv("STEWARDMESH_CACHE_KEY_SECRET", test.keySecret)
			configuration, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if configuration.CacheDriver != test.driver || configuration.CacheURL != test.url ||
				configuration.CacheKeySecret != test.keySecret {
				t.Fatalf("unexpected cache configuration %#v", configuration)
			}
		})
	}
}

func TestValidateRejectsUnsafeCacheConfigurationWithoutLeakingCredentials(t *testing.T) {
	tests := []struct {
		name      string
		driver    CacheDriver
		url       string
		keySecret string
	}{
		{name: "unknown driver", driver: "redis"},
		{name: "missing Valkey URL", driver: CacheDriverValkey},
		{name: "unsupported scheme", driver: CacheDriverValkey, url: "http://cache.example.test", keySecret: strings.Repeat("s", 32)},
		{name: "short key secret", driver: CacheDriverValkey, url: "redis://cache.example.test:6379", keySecret: "super-secret"},
		{name: "ignored secret", driver: CacheDriverNone, url: "redis://user:super-secret@localhost:6379"},
		{name: "ignored key secret", driver: CacheDriverNone, keySecret: "super-secret"},
		{name: "malformed secret", driver: CacheDriverValkey, url: "redis://user:super-secret@", keySecret: strings.Repeat("s", 32)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configuration := FromEnv()
			configuration.RepositoryDriver = RepositoryDriverMemory
			configuration.CacheDriver = test.driver
			configuration.CacheURL = test.url
			configuration.CacheKeySecret = test.keySecret
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

func TestLoadSupportsOIDCWithExactAdministratorClaimValues(t *testing.T) {
	t.Setenv("STEWARDMESH_REPOSITORY_DRIVER", "memory")
	t.Setenv("STEWARDMESH_OIDC_ISSUER_URL", "https://identity.example.test/tenant")
	t.Setenv("STEWARDMESH_OIDC_CLIENT_ID", "stewardmesh-web")
	t.Setenv("STEWARDMESH_OIDC_CLIENT_SECRET", strings.Repeat("c", 32))
	t.Setenv("STEWARDMESH_OIDC_REDIRECT_URL", "http://localhost:5173/api/v1/auth/oidc/callback")
	t.Setenv("STEWARDMESH_OIDC_TRANSACTION_SECRET", strings.Repeat("t", 32))
	t.Setenv("STEWARDMESH_OIDC_ADMINISTRATOR_CLAIM", "groups")
	t.Setenv("STEWARDMESH_OIDC_ADMINISTRATOR_VALUES", "stewardmesh-admins, platform-admins, stewardmesh-admins")
	configuration, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !configuration.OIDCEnabled() || !configuration.OIDCRequireVerifiedEmail ||
		configuration.OIDCAdministratorClaim != "groups" ||
		len(configuration.OIDCAdministratorValues) != 2 || configuration.OIDCAdministratorValues[0] != "stewardmesh-admins" {
		t.Fatalf("unexpected OpenID Connect configuration %#v", configuration)
	}
}

func TestValidateRejectsPartialOrUnsafeOIDCConfigurationWithoutLeakingSecrets(t *testing.T) {
	valid := FromEnv()
	valid.RepositoryDriver = RepositoryDriverMemory
	valid.OIDCIssuerURL = "https://identity.example.test/tenant"
	valid.OIDCClientID = "stewardmesh-web"
	valid.OIDCClientSecret = "super-secret-client-value"
	valid.OIDCRedirectURL = "http://localhost:5173/api/v1/auth/oidc/callback"
	valid.OIDCTransactionSecret = strings.Repeat("t", 32)
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "ignored client secret", mutate: func(configuration *Config) {
			configuration.OIDCIssuerURL = ""
		}},
		{name: "plaintext remote issuer", mutate: func(configuration *Config) {
			configuration.OIDCIssuerURL = "http://identity.example.test/tenant"
		}},
		{name: "credentialed issuer", mutate: func(configuration *Config) {
			configuration.OIDCIssuerURL = "https://user:super-secret@identity.example.test/tenant"
		}},
		{name: "redirect origin mismatch", mutate: func(configuration *Config) {
			configuration.OIDCRedirectURL = "https://other.example.test/api/v1/auth/oidc/callback"
		}},
		{name: "redirect path mismatch", mutate: func(configuration *Config) {
			configuration.OIDCRedirectURL = "http://localhost:5173/callback"
		}},
		{name: "short transaction secret", mutate: func(configuration *Config) {
			configuration.OIDCTransactionSecret = "short"
		}},
		{name: "invalid administrator claim", mutate: func(configuration *Config) {
			configuration.OIDCAdministratorClaim = "groups claim"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configuration := valid
			test.mutate(&configuration)
			err := configuration.Validate()
			if err == nil {
				t.Fatal("expected invalid OpenID Connect configuration")
			}
			if strings.Contains(err.Error(), "super-secret") {
				t.Fatal("configuration error leaked an OpenID Connect secret")
			}
		})
	}
}

func TestLoadSupportsSAMLWithExactAdministratorAttributeValues(t *testing.T) {
	t.Setenv("STEWARDMESH_REPOSITORY_DRIVER", "memory")
	t.Setenv("STEWARDMESH_SAML_IDP_METADATA_URL", "https://identity.example.test/metadata")
	t.Setenv("STEWARDMESH_SAML_SP_CERTIFICATE_FILE", "/run/secrets/stewardmesh-saml.crt")
	t.Setenv("STEWARDMESH_SAML_SP_PRIVATE_KEY_FILE", "/run/secrets/stewardmesh-saml.key")
	t.Setenv("STEWARDMESH_SAML_EMAIL_ATTRIBUTE", "mail")
	t.Setenv("STEWARDMESH_SAML_DISPLAY_NAME_ATTRIBUTE", "displayName")
	t.Setenv("STEWARDMESH_SAML_ADMINISTRATOR_ATTRIBUTE", "groups")
	t.Setenv("STEWARDMESH_SAML_ADMINISTRATOR_VALUES", "stewardmesh-admins, platform-admins, stewardmesh-admins")
	configuration, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !configuration.SAMLEnabled() || configuration.EffectiveSAMLEntityID() != "http://localhost:5173/api/v1/auth/saml/metadata" ||
		configuration.SAMLACSURL() != "http://localhost:5173/api/v1/auth/saml/acs" ||
		len(configuration.SAMLAdministratorValues) != 2 || configuration.SAMLAdministratorValues[0] != "stewardmesh-admins" {
		t.Fatalf("unexpected SAML configuration %#v", configuration)
	}
}

func TestValidateRejectsPartialOrUnsafeSAMLConfigurationWithoutLeakingPaths(t *testing.T) {
	valid := FromEnv()
	valid.RepositoryDriver = RepositoryDriverMemory
	valid.SAMLIDPMetadataURL = "https://identity.example.test/metadata"
	valid.SAMLSPCertificateFile = "/run/secrets/service-provider.crt"
	valid.SAMLSPPrivateKeyFile = "/run/secrets/super-secret-private-key.pem"
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "ignored private key", mutate: func(configuration *Config) {
			configuration.SAMLIDPMetadataURL = ""
		}},
		{name: "plaintext remote metadata", mutate: func(configuration *Config) {
			configuration.SAMLIDPMetadataURL = "http://identity.example.test/metadata"
		}},
		{name: "credentialed metadata", mutate: func(configuration *Config) {
			configuration.SAMLIDPMetadataURL = "https://user:super-secret@identity.example.test/metadata"
		}},
		{name: "missing certificate", mutate: func(configuration *Config) {
			configuration.SAMLSPCertificateFile = ""
		}},
		{name: "invalid private key path", mutate: func(configuration *Config) {
			configuration.SAMLSPPrivateKeyFile = "bad\x00path"
		}},
		{name: "relative entity id", mutate: func(configuration *Config) {
			configuration.SAMLEntityID = "relative-entity"
		}},
		{name: "administrator values without attribute", mutate: func(configuration *Config) {
			configuration.SAMLAdministratorValues = []string{"administrators"}
		}},
		{name: "multiline attribute", mutate: func(configuration *Config) {
			configuration.SAMLEmailAttribute = "mail\nrole"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configuration := valid
			test.mutate(&configuration)
			err := configuration.Validate()
			if err == nil {
				t.Fatal("expected invalid SAML configuration")
			}
			if strings.Contains(err.Error(), "super-secret") {
				t.Fatal("configuration error leaked a SAML secret or credential")
			}
		})
	}
}
