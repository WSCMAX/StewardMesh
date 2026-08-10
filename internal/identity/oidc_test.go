package identity

// Requirements: SEC-GUARD-001, SEC-HTTP-001.

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

type fakeOIDCAuthenticator struct {
	principal OIDCPrincipal
	err       error
	state     string
	nonce     string
	verifier  string
}

func (f *fakeOIDCAuthenticator) AuthorizationURL(state, nonce, verifier string) (string, error) {
	f.state, f.nonce, f.verifier = state, nonce, verifier
	return "https://identity.example.test/authorize?state=" + url.QueryEscape(state), nil
}

func (f *fakeOIDCAuthenticator) Authenticate(_ context.Context, code, verifier, nonce string) (OIDCPrincipal, error) {
	if code != "authorization-code" || verifier != f.verifier || nonce != f.nonce {
		return OIDCPrincipal{}, errors.New("unexpected flow values")
	}
	return f.principal, f.err
}

func TestOIDCFlowBindsStateNonceAndPKCEToEncryptedTransaction(t *testing.T) {
	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	authenticator := &fakeOIDCAuthenticator{principal: OIDCPrincipal{
		Issuer: "https://identity.example.test", Subject: "subject-1", Email: "person@example.test",
	}}
	flow, err := NewOIDCFlow(authenticator, strings.Repeat("t", 32), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	authorizationURL, transaction, expiresAt, err := flow.Start()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(authorizationURL, "https://identity.example.test/authorize?") ||
		transaction == "" || !expiresAt.Equal(now.Add(10*time.Minute)) {
		t.Fatalf("unexpected flow start url=%q transaction=%q expires=%v", authorizationURL, transaction, expiresAt)
	}
	if strings.Contains(transaction, authenticator.state) || strings.Contains(transaction, authenticator.nonce) ||
		strings.Contains(transaction, authenticator.verifier) {
		t.Fatal("encrypted transaction exposed state, nonce, or verifier")
	}
	principal, err := flow.Complete(context.Background(), transaction, authenticator.state, "authorization-code")
	if err != nil || principal.Subject != "subject-1" {
		t.Fatalf("unexpected completed flow %#v err=%v", principal, err)
	}
}

func TestOIDCAuthorizationURLUsesCodeFlowNonceAndS256PKCE(t *testing.T) {
	client := &OIDCClient{oauth: oauth2.Config{
		ClientID: "stewardmesh-web", RedirectURL: "https://stewardmesh.example.test/api/v1/auth/oidc/callback",
		Endpoint: oauth2.Endpoint{AuthURL: "https://identity.example.test/authorize", TokenURL: "https://identity.example.test/token"},
		Scopes:   []string{"openid", "profile", "email"},
	}}
	authorizationURL, err := client.AuthorizationURL("state-value", "nonce-value", strings.Repeat("v", 43))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(authorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if query.Get("response_type") != "code" || query.Get("state") != "state-value" ||
		query.Get("nonce") != "nonce-value" || query.Get("code_challenge_method") != "S256" ||
		query.Get("code_challenge") == "" || query.Get("scope") != "openid profile email" {
		t.Fatalf("unexpected authorization parameters %v", query)
	}
}

func TestOIDCFlowRejectsTamperingMismatchExpiryAndProviderFailure(t *testing.T) {
	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	authenticator := &fakeOIDCAuthenticator{principal: OIDCPrincipal{Issuer: "https://identity.example.test", Subject: "subject-1", Email: "person@example.test"}}
	flow, err := NewOIDCFlow(authenticator, strings.Repeat("t", 32), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	_, transaction, _, err := flow.Start()
	if err != nil {
		t.Fatal(err)
	}
	tamperedSuffix := "A"
	if strings.HasSuffix(transaction, tamperedSuffix) {
		tamperedSuffix = "B"
	}
	tampered := transaction[:len(transaction)-1] + tamperedSuffix
	for name, values := range map[string][2]string{
		"tampering":      {tampered, authenticator.state},
		"state mismatch": {transaction, "different-state"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := flow.Complete(context.Background(), values[0], values[1], "authorization-code"); !errors.Is(err, ErrOIDCAuthentication) {
				t.Fatalf("expected authentication failure, got %v", err)
			}
		})
	}
	now = now.Add(11 * time.Minute)
	if _, err := flow.Complete(context.Background(), transaction, authenticator.state, "authorization-code"); !errors.Is(err, ErrOIDCAuthentication) {
		t.Fatalf("expected expired transaction failure, got %v", err)
	}
	now = now.Add(-11 * time.Minute)
	authenticator.err = errors.New("provider rejected code")
	if _, err := flow.Complete(context.Background(), transaction, authenticator.state, "authorization-code"); !errors.Is(err, ErrOIDCAuthentication) {
		t.Fatalf("expected provider failure, got %v", err)
	}
}

func TestPrincipalFromClaimsUsesStableIdentityAndExactAdministratorMapping(t *testing.T) {
	claims := map[string]json.RawMessage{
		"email":              json.RawMessage(`"Person@Example.test"`),
		"email_verified":     json.RawMessage(`true`),
		"name":               json.RawMessage(`"Example Person"`),
		"preferred_username": json.RawMessage(`"person"`),
		"groups":             json.RawMessage(`["staff","stewardmesh-admins"]`),
	}
	principal, err := principalFromClaims(
		"https://identity.example.test",
		"provider-subject",
		claims,
		"groups",
		map[string]struct{}{"stewardmesh-admins": {}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if principal.Subject != "provider-subject" || principal.Email != "person@example.test" ||
		principal.PreferredUsername != "person" || !principal.EmailVerified || !principal.Administrator {
		t.Fatalf("unexpected principal %#v", principal)
	}
	claims["groups"] = json.RawMessage(`["stewardmesh-admins-extra"]`)
	principal, err = principalFromClaims("https://identity.example.test", "provider-subject", claims, "groups", map[string]struct{}{"stewardmesh-admins": {}})
	if err != nil || principal.Administrator {
		t.Fatalf("administrator mapping must use exact values, principal=%#v err=%v", principal, err)
	}
}

func TestOIDCConfigurationRejectsUnsafeEndpointsAndShortTransactionSecrets(t *testing.T) {
	for _, endpoint := range []string{
		"http://identity.example.test",
		"https://user:secret@identity.example.test",
		"https://identity.example.test?tenant=one",
		"ftp://identity.example.test",
	} {
		if err := validateOIDCEndpoint(endpoint); err == nil {
			t.Fatalf("expected unsafe endpoint %q to fail", endpoint)
		}
	}
	if err := validateOIDCEndpoint("http://127.0.0.1:5556/dex"); err != nil {
		t.Fatal(err)
	}
	if _, err := NewOIDCFlow(&fakeOIDCAuthenticator{}, "short", nil); err == nil {
		t.Fatal("expected a short transaction secret to fail")
	}
}
