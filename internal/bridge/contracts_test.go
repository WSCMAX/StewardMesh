package bridge

// Requirements: REQ-API-001, SEC-MCP-001. Feature: integrations.protocols. GitHub: #14.

import (
	"os"
	"strings"
	"testing"
)

func TestRESTAndGRPCBridgeAdministrationParity(t *testing.T) {
	openAPI, err := os.ReadFile("../../api/openapi/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	protobuf, err := os.ReadFile("../../api/proto/stewardmesh.proto")
	if err != nil {
		t.Fatal(err)
	}
	goModule, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(goModule), SDKVersion) || !strings.Contains(string(openAPI), "const: "+ProtocolVersion) {
		t.Fatalf("Bridge protocol pin drift: protocol=%q sdk=%q", ProtocolVersion, SDKVersion)
	}
	pairs := []struct{ rest, rpc string }{
		{"operationId: listBridgeClients", "rpc ListClients("}, {"operationId: createBridgeClient", "rpc CreateClient("},
		{"operationId: revokeBridgeClient", "rpc RevokeClient("}, {"operationId: listBridgeGrants", "rpc ListGrants("},
		{"operationId: revokeBridgeGrant", "rpc RevokeGrant("},
	}
	for _, pair := range pairs {
		if !strings.Contains(string(openAPI), pair.rest) || !strings.Contains(string(protobuf), pair.rpc) {
			t.Fatalf("missing Bridge REST/gRPC parity pair %q / %q", pair.rest, pair.rpc)
		}
	}
	for _, transportOnly := range []string{"operationId: authorizeBridgeClient", "operationId: exchangeBridgeToken", "operationId: callBridgeMCP"} {
		if !strings.Contains(string(openAPI), transportOnly) {
			t.Fatalf("missing documented HTTP-only Bridge operation %q", transportOnly)
		}
	}
	if !strings.Contains(string(protobuf), "intentionally not exposed") {
		t.Fatal("protobuf must document intentional OAuth/MCP transport gaps")
	}
}

func TestRedirectAndPublicURLValidationRejectsUnsafeForms(t *testing.T) {
	for _, raw := range []string{"https://example.test/callback#fragment", "https://user:secret@example.test/callback", "http://example.test/callback", "javascript:alert(1)", "//example.test/callback"} {
		if _, err := normalizeRedirectURIs([]string{raw}); err == nil {
			t.Fatalf("unsafe redirect accepted: %q", raw)
		}
	}
	if values, err := normalizeRedirectURIs([]string{"https://example.test/callback", "http://127.0.0.1:7777/callback"}); err != nil || len(values) != 2 {
		t.Fatalf("safe redirects rejected: %#v err=%v", values, err)
	}
	if _, err := normalizeIssuer("https://example.test/path"); err == nil {
		t.Fatal("issuer path was accepted")
	}
	if _, err := normalizeResourceURI("https://example.test/not-mcp"); err == nil {
		t.Fatal("non-MCP audience was accepted")
	}
}

func FuzzBridgeCanonicalArguments(f *testing.F) {
	f.Add("alert-one", int64(1), "confirmation")
	f.Add(strings.Repeat("x", 5000), int64(-1), strings.Repeat("y", 600))
	f.Fuzz(func(t *testing.T, alertID string, revision int64, token string) {
		_, _ = canonicalHash(struct {
			AlertID  string `json:"alertId"`
			Revision int64  `json:"revision"`
			Token    string `json:"token"`
		}{alertID, revision, token})
	})
}
