package config

// Requirement: REQ-DIRECTORY-EXPANSION-007. Feature: platform.foundation.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadReadsYAMLConfigAndLetsEnvironmentOverride(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.yaml")
	content := []byte(`
listen:
  addr: "0.0.0.0:8080"
  allowed_origin: "http://127.0.0.1:8080"
  insecure_bind: true
organization:
  id: file-organization
  name: "File Organization"
repository:
  driver: memory
storage:
  driver: local
  blob_dir: /tmp/stewardmesh-blobs
web:
  dir: /web
seed:
  synthetic: false
  campus: false
session:
  cookie_secure: false
  ttl: 12h
`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("STEWARDMESH_CONFIG", path)
	t.Setenv("STEWARDMESH_REPOSITORY_DRIVER", "")
	t.Setenv("STEWARDMESH_ORGANIZATION_ID", "")
	t.Setenv("STEWARDMESH_ORGANIZATION_NAME", "")
	t.Setenv("STEWARDMESH_ADDR", "")
	t.Setenv("STEWARDMESH_ALLOWED_ORIGIN", "")
	t.Setenv("STEWARDMESH_INSECURE_BIND", "")
	t.Setenv("STEWARDMESH_WEB_DIR", "")
	configuration, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if configuration.Addr != "0.0.0.0:8080" || configuration.OrganizationID != "file-organization" ||
		!configuration.InsecureBind || configuration.WebDir != "/web" || configuration.RepositoryDriver != RepositoryDriverMemory {
		t.Fatalf("unexpected file-backed configuration %#v", configuration)
	}

	t.Setenv("STEWARDMESH_ORGANIZATION_ID", "env-organization")
	t.Setenv("STEWARDMESH_ORGANIZATION_NAME", "Env Organization")
	overridden, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if overridden.OrganizationID != "env-organization" || overridden.Addr != "0.0.0.0:8080" {
		t.Fatalf("environment should override the config file, got %#v", overridden)
	}
}

func TestLoadRejectsMissingExplicitConfigFile(t *testing.T) {
	t.Setenv("STEWARDMESH_CONFIG", filepath.Join(t.TempDir(), "missing.yaml"))
	t.Setenv("STEWARDMESH_REPOSITORY_DRIVER", "memory")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "STEWARDMESH_CONFIG") {
		t.Fatalf("expected missing config file to fail, got %v", err)
	}
}

func TestInsecureBindAllowsUnspecifiedAddressWithLoopbackOrigin(t *testing.T) {
	configuration := FromEnv()
	configuration.RepositoryDriver = RepositoryDriverMemory
	configuration.Addr = "0.0.0.0:8080"
	configuration.AllowedOrigin = "http://127.0.0.1:8080"
	configuration.SessionCookieSecure = false
	configuration.InsecureBind = true
	if err := configuration.Validate(); err != nil {
		t.Fatal(err)
	}
	configuration.AllowedOrigin = "https://inventory.example.test"
	configuration.SessionCookieSecure = true
	if err := configuration.Validate(); err == nil || !strings.Contains(err.Error(), "INSECURE_BIND") {
		t.Fatalf("expected insecure bind to require a loopback HTTP origin, got %v", err)
	}
}

func TestSharedListenerStillRequiresHTTPSWithoutInsecureBind(t *testing.T) {
	configuration := FromEnv()
	configuration.RepositoryDriver = RepositoryDriverMemory
	configuration.Addr = "0.0.0.0:8080"
	configuration.AllowedOrigin = "http://127.0.0.1:8080"
	configuration.SessionCookieSecure = false
	configuration.InsecureBind = false
	if err := configuration.Validate(); err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("expected shared listener without insecure bind to require HTTPS, got %v", err)
	}
}
