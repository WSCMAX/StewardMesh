package reach_test

// Requirement: REQ-REACH-001. Feature: messaging.delivery. GitHub: #12.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maxlemke/stewardmesh/internal/reach"
)

func TestLoadEndpointsFileAcceptsCredentialFreeRoutesAndRedactsCatalog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reach-endpoints.json")
	contents := []byte(`[{"id":"hook-primary","label":"Operations webhook","kind":"webhook","url":"https://hooks.example.test/reach","testUrl":"https://hooks.example.test/health"}]`)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	endpoints, err := reach.LoadEndpointsFile(path)
	if err != nil || len(endpoints) != 1 || endpoints[0].URL == "" {
		t.Fatalf("load endpoints: %#v %v", endpoints, err)
	}
	catalog, err := reach.NewEndpointCatalog(endpoints)
	if err != nil {
		t.Fatal(err)
	}
	serialized, err := json.Marshal(catalog.List())
	if err != nil || strings.Contains(string(serialized), "hooks.example.test") {
		t.Fatalf("catalog exposed route metadata: %s %v", serialized, err)
	}
	if _, err := catalog.Get("hook-primary", reach.ProviderWebhook); err != nil {
		t.Fatalf("resolve deployment route: %v", err)
	}
}

func TestLoadEndpointsFileRejectsUnknownFieldsAndUnsafeDestinations(t *testing.T) {
	for name, contents := range map[string]string{
		"unknown":     `[{"id":"hook","label":"Hook","kind":"webhook","url":"https://example.test","secret":"must-not-be-here"}]`,
		"remote-http": `[{"id":"hook","label":"Hook","kind":"webhook","url":"http://example.test/reach","allowLocalHttp":true}]`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "reach-endpoints.json")
			if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := reach.LoadEndpointsFile(path); err == nil {
				t.Fatal("expected deployment endpoint rejection")
			}
		})
	}
}

func TestEnvironmentSecretResolverRequiresExplicitReferenceScheme(t *testing.T) {
	resolver, err := reach.NewEnvironmentSecretResolver("STEWARDMESH_REACH_SECRET_")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("STEWARDMESH_REACH_SECRET_HOOK_PRIMARY", "01234567890123456789012345678901")
	secret, err := resolver.Resolve(context.Background(), "env:hook-primary")
	if err != nil || string(secret) != "01234567890123456789012345678901" {
		t.Fatalf("resolve environment reference: %q %v", secret, err)
	}
	if _, err := resolver.Resolve(context.Background(), "01234567890123456789012345678901"); err == nil {
		t.Fatal("raw-looking value was accepted as a secret reference")
	}
}
