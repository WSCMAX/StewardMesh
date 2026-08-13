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

func TestTeamsEndpointRequiresOneSafeExposedDestinationKey(t *testing.T) {
	for name, endpoint := range map[string]reach.Endpoint{
		"missing Teams destination": {ID: "teams", Label: "Teams", Kind: reach.ProviderTeams, URL: "https://graph.microsoft.com/v1.0/teams/team/channels/channel/messages"},
		"invalid Teams destination": {ID: "teams", Label: "Teams", Kind: reach.ProviderTeams, DestinationKey: "https://graph.microsoft.com/channel", URL: "https://graph.microsoft.com/v1.0/teams/team/channels/channel/messages"},
		"destination on webhook":    {ID: "hook", Label: "Hook", Kind: reach.ProviderWebhook, DestinationKey: "operations", URL: "https://hooks.example.test/reach"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := reach.NewEndpointCatalog([]reach.Endpoint{endpoint}); err == nil {
				t.Fatal("expected destination metadata rejection")
			}
		})
	}
	catalog, err := reach.NewEndpointCatalog([]reach.Endpoint{{
		ID: "teams", Label: "Operations Teams", Kind: reach.ProviderTeams, DestinationKey: "operations-channel",
		URL: "https://graph.microsoft.com/v1.0/teams/team/channels/channel/messages",
	}})
	if err != nil {
		t.Fatal(err)
	}
	serialized, err := json.Marshal(catalog.List())
	if err != nil || !strings.Contains(string(serialized), `"destinationKey":"operations-channel"`) || strings.Contains(string(serialized), "graph.microsoft.com") {
		t.Fatalf("unsafe Teams endpoint response %s: %v", serialized, err)
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
