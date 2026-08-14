package contracttest

// Provider-neutral Bridge adapter contract.
// Requirements: REQ-API-001, SEC-MCP-001, REQ-EXCHANGE-001.
// Features: integrations.protocols, migration.packages. GitHub: #9, #14.

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/bridge"
)

func BridgeStore(t testing.TB, subject bridge.Store, organizationID string) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	client := bridge.Client{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", OrganizationID: organizationID, Name: "Contract client", RedirectURIs: []string{"https://client.example/callback"}, AllowedScopes: []bridge.Scope{bridge.ScopeMCPResources, bridge.ScopeAssetsRead}, CreatedBy: "account-one", CreatedAt: now}
	created, err := subject.CreateClient(ctx, client)
	if err != nil || created.ID != client.ID {
		t.Fatalf("create Bridge client %#v err=%v", created, err)
	}
	client.RedirectURIs[0] = "https://changed.invalid"
	loaded, err := subject.GetClient(ctx, organizationID, created.ID)
	if err != nil || loaded.RedirectURIs[0] != "https://client.example/callback" {
		t.Fatalf("Bridge client was not defensively persisted %#v err=%v", loaded, err)
	}
	if isolated, err := subject.ListClients(ctx, organizationID+"-other", bridge.PageRequest{Limit: 1}); err != nil || len(isolated) != 0 {
		t.Fatalf("Bridge organization isolation failed %#v err=%v", isolated, err)
	}
	for index, name := range []string{"A contract client", "Z contract client"} {
		id := fmt.Sprintf("%032x", index+2)
		if _, err := subject.CreateClient(ctx, bridge.Client{ID: id, OrganizationID: organizationID, Name: name, RedirectURIs: []string{"https://client.example/" + id}, AllowedScopes: []bridge.Scope{bridge.ScopeMCPResources}, CreatedBy: "account-one", CreatedAt: now.Add(time.Duration(index+1) * time.Second)}); err != nil {
			t.Fatalf("create paginated Bridge client: %v", err)
		}
	}
	firstPage, err := subject.ListClients(ctx, organizationID, bridge.PageRequest{Limit: 1})
	if err != nil || len(firstPage) != 2 {
		t.Fatalf("bounded Bridge client first page %#v err=%v", firstPage, err)
	}
	secondPage, err := subject.ListClients(ctx, organizationID, bridge.PageRequest{Cursor: firstPage[0].ID, Limit: 1})
	if err != nil || len(secondPage) != 2 || secondPage[0].ID == firstPage[0].ID {
		t.Fatalf("bounded Bridge client continuation %#v err=%v", secondPage, err)
	}
	if _, err := subject.ListClients(ctx, organizationID, bridge.PageRequest{Cursor: "ffffffffffffffffffffffffffffffff", Limit: 1}); !errors.Is(err, bridge.ErrInvalidInput) {
		t.Fatalf("invalid Bridge client cursor error=%v", err)
	}
	if _, err := subject.ListExchangeClients(ctx, organizationID, 2); !errors.Is(err, bridge.ErrTooLarge) {
		t.Fatalf("unbounded Bridge Exchange snapshot error=%v", err)
	}
	exchangeRevokedAt := now.Add(30 * time.Second)
	exchangeClient := bridge.Client{
		ID: "99999999999999999999999999999999", OrganizationID: organizationID, Name: "Imported contract client",
		RedirectURIs: []string{"https://imported.example.test/callback"}, AllowedScopes: []bridge.Scope{bridge.ScopeMCPResources},
		CreatedBy: "system:exchange", CreatedAt: now, RevokedAt: &exchangeRevokedAt,
	}
	imported, importCreated, err := subject.ImportExchangeClient(ctx, exchangeClient)
	if err != nil || !importCreated || imported.RevokedAt == nil {
		t.Fatalf("import Bridge Exchange client %#v created=%t err=%v", imported, importCreated, err)
	}
	if replayed, replayCreated, err := subject.ImportExchangeClient(ctx, exchangeClient); err != nil || replayCreated || replayed.ID != exchangeClient.ID {
		t.Fatalf("replay Bridge Exchange client %#v created=%t err=%v", replayed, replayCreated, err)
	}
	conflict := exchangeClient
	conflict.Name = "Changed imported client"
	if _, _, err := subject.ImportExchangeClient(ctx, conflict); !errors.Is(err, bridge.ErrConflict) {
		t.Fatalf("conflicting Bridge Exchange client error=%v", err)
	}
	if snapshot, err := subject.ListExchangeClients(ctx, organizationID, 10); err != nil || len(snapshot) != 4 {
		t.Fatalf("Bridge Exchange snapshot %#v err=%v", snapshot, err)
	}

	request := bridge.AuthorizationRequest{ID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", OrganizationID: organizationID, ClientID: loaded.ID, ActorID: "account-one", RedirectURI: loaded.RedirectURIs[0], ResourceURI: "https://steward.example/mcp", Scopes: loaded.AllowedScopes, State: "opaque-state", CodeChallenge: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", CreatedAt: now, ExpiresAt: now.Add(5 * time.Minute)}
	if err := subject.CreateAuthorizationRequest(ctx, request); err != nil {
		t.Fatal(err)
	}
	decided := now.Add(time.Second)
	request.DecidedAt, request.Approved = &decided, true
	codeHash := sha256.Sum256([]byte("code"))
	code := bridge.AuthorizationCode{ID: "cccccccccccccccccccccccccccccccc", OrganizationID: organizationID, RequestID: request.ID, ClientID: request.ClientID, ActorID: request.ActorID, RedirectURI: request.RedirectURI, ResourceURI: request.ResourceURI, Scopes: request.Scopes, CodeHash: codeHash[:], CodeChallenge: request.CodeChallenge, CreatedAt: decided, ExpiresAt: decided.Add(2 * time.Minute)}
	if err := subject.DecideAuthorizationRequest(ctx, request, &code); err != nil {
		t.Fatal(err)
	}
	if err := subject.DecideAuthorizationRequest(ctx, request, &code); !errors.Is(err, bridge.ErrReplay) {
		t.Fatalf("expected consent replay rejection, got %v", err)
	}

	accessHash, refreshHash := sha256.Sum256([]byte("access")), sha256.Sum256([]byte("refresh"))
	grant := bridge.Grant{ID: "dddddddddddddddddddddddddddddddd", AccessTokenHash: accessHash[:], RefreshTokenHash: refreshHash[:], AccessExpiresAt: now.Add(15 * time.Minute), RefreshExpiresAt: now.Add(8 * time.Hour), CreatedAt: now.Add(2 * time.Second)}
	if _, err := subject.ExchangeAuthorizationCode(ctx, organizationID, codeHash[:], loaded.ID, "https://wrong.example/callback", request.ResourceURI, request.CodeChallenge, now.Add(2*time.Second), grant); !errors.Is(err, bridge.ErrUnauthorized) {
		t.Fatalf("exact redirect binding failed: %v", err)
	}
	issued, err := subject.ExchangeAuthorizationCode(ctx, organizationID, codeHash[:], loaded.ID, request.RedirectURI, request.ResourceURI, request.CodeChallenge, now.Add(2*time.Second), grant)
	if err != nil || issued.ActorID != request.ActorID || len(issued.Scopes) != 2 {
		t.Fatalf("exchange Bridge code %#v err=%v", issued, err)
	}
	if _, err := subject.ExchangeAuthorizationCode(ctx, organizationID, codeHash[:], loaded.ID, request.RedirectURI, request.ResourceURI, request.CodeChallenge, now.Add(2*time.Second), grant); err == nil {
		t.Fatal("authorization code replay was accepted")
	}
	if authenticated, err := subject.AuthenticateAccessToken(ctx, organizationID, accessHash[:], request.ResourceURI, now.Add(3*time.Second)); err != nil || authenticated.ID != grant.ID || authenticated.LastUsedAt == nil {
		t.Fatalf("authenticate Bridge access token %#v err=%v", authenticated, err)
	}
	replacementAccessHash, replacementRefreshHash := sha256.Sum256([]byte("replacement access")), sha256.Sum256([]byte("replacement refresh"))
	replacement := bridge.Grant{ID: "ffffffffffffffffffffffffffffffff", AccessTokenHash: replacementAccessHash[:], RefreshTokenHash: replacementRefreshHash[:], AccessExpiresAt: now.Add(20 * time.Minute), RefreshExpiresAt: now.Add(8*time.Hour + time.Minute), CreatedAt: now.Add(4 * time.Second)}
	rotated, err := subject.RotateRefreshToken(ctx, organizationID, refreshHash[:], loaded.ID, request.ResourceURI, now.Add(4*time.Second), replacement)
	if err != nil || rotated.ActorID != request.ActorID || !rotated.RefreshExpiresAt.Equal(issued.RefreshExpiresAt) {
		t.Fatalf("rotate Bridge refresh token %#v err=%v", rotated, err)
	}
	if _, err := subject.RotateRefreshToken(ctx, organizationID, refreshHash[:], loaded.ID, request.ResourceURI, now.Add(5*time.Second), replacement); err == nil {
		t.Fatal("rotated refresh token replay was accepted")
	}
	if authenticated, err := subject.AuthenticateAccessToken(ctx, organizationID, replacementAccessHash[:], request.ResourceURI, now.Add(5*time.Second)); err != nil || authenticated.ID != replacement.ID {
		t.Fatalf("authenticate rotated Bridge access token %#v err=%v", authenticated, err)
	}
	grantPage, err := subject.ListGrants(ctx, organizationID, bridge.PageRequest{Limit: 1})
	if err != nil || len(grantPage) != 2 {
		t.Fatalf("bounded Bridge grant first page %#v err=%v", grantPage, err)
	}
	grantNext, err := subject.ListGrants(ctx, organizationID, bridge.PageRequest{Cursor: grantPage[0].ID, Limit: 1})
	if err != nil || len(grantNext) != 1 || grantNext[0].ID == grantPage[0].ID {
		t.Fatalf("bounded Bridge grant continuation %#v err=%v", grantNext, err)
	}
	if _, err := subject.ListGrants(ctx, organizationID, bridge.PageRequest{Limit: bridge.MaximumAdministrationPageSize + 1}); !errors.Is(err, bridge.ErrInvalidInput) {
		t.Fatalf("unbounded Bridge grant page error=%v", err)
	}

	confirmationHash, tokenHash := sha256.Sum256([]byte("arguments")), sha256.Sum256([]byte("confirmation"))
	confirmation := bridge.Confirmation{ID: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", OrganizationID: organizationID, ActorID: request.ActorID, Action: "signals.alert.acknowledge", ArgumentsHash: confirmationHash[:], TokenHash: tokenHash[:], CreatedAt: now, ExpiresAt: now.Add(2 * time.Minute)}
	if err := subject.CreateConfirmation(ctx, confirmation); err != nil {
		t.Fatal(err)
	}
	wrong := sha256.Sum256([]byte("changed"))
	if _, err := subject.ConsumeConfirmation(ctx, organizationID, request.ActorID, confirmation.Action, wrong[:], tokenHash[:], now.Add(time.Second)); !errors.Is(err, bridge.ErrUnauthorized) {
		t.Fatalf("confirmation argument binding failed: %v", err)
	}
	if _, err := subject.ConsumeConfirmation(ctx, organizationID, "account-two", confirmation.Action, confirmationHash[:], tokenHash[:], now.Add(time.Second)); err == nil {
		t.Fatal("confirmation actor binding failed")
	}
	if _, err := subject.ConsumeConfirmation(ctx, organizationID, request.ActorID, "signals.alert.assign", confirmationHash[:], tokenHash[:], now.Add(time.Second)); err == nil {
		t.Fatal("confirmation action binding failed")
	}
	if _, err := subject.ConsumeConfirmation(ctx, organizationID, request.ActorID, confirmation.Action, confirmationHash[:], tokenHash[:], now.Add(time.Second)); err != nil {
		t.Fatalf("consume confirmation: %v", err)
	}
	if _, err := subject.ConsumeConfirmation(ctx, organizationID, request.ActorID, confirmation.Action, confirmationHash[:], tokenHash[:], now.Add(time.Second)); err == nil {
		t.Fatal("confirmation replay was accepted")
	}

	rateKey := sha256.Sum256([]byte("rate"))
	if allowed, err := subject.AllowRate(ctx, organizationID, rateKey, now.Truncate(time.Minute), 1); err != nil || !allowed {
		t.Fatalf("initial rate use allowed=%v err=%v", allowed, err)
	}
	if allowed, err := subject.AllowRate(ctx, organizationID, rateKey, now.Truncate(time.Minute), 1); err != nil || allowed {
		t.Fatalf("rate limit failed allowed=%v err=%v", allowed, err)
	}

	revoked, err := subject.RevokeClient(ctx, organizationID, loaded.ID, now.Add(time.Minute))
	if err != nil || revoked.RevokedAt == nil {
		t.Fatalf("revoke Bridge client %#v err=%v", revoked, err)
	}
	if _, err := subject.AuthenticateAccessToken(ctx, organizationID, replacementAccessHash[:], request.ResourceURI, now.Add(2*time.Minute)); err == nil {
		t.Fatal("revoked client access token was accepted")
	}
}
