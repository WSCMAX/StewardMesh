package contracttest

// Provider-neutral Bridge adapter contract.
// Requirements: REQ-API-001, SEC-MCP-001. Feature: integrations.protocols. GitHub: #14.

import (
	"context"
	"crypto/sha256"
	"errors"
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
	if isolated, err := subject.ListClients(ctx, organizationID+"-other"); err != nil || len(isolated) != 0 {
		t.Fatalf("Bridge organization isolation failed %#v err=%v", isolated, err)
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
