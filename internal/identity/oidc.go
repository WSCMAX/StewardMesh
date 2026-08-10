package identity

// Requirements: SEC-GUARD-001, SEC-HTTP-001.

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	defaultOIDCTransactionTTL = 10 * time.Minute
	maximumAuthorizationCode  = 4096
)

var ErrOIDCAuthentication = errors.New("openid connect authentication failed")

// OIDCConfig describes one provider-neutral OpenID Connect relying party.
// AdministratorValues are matched exactly against string values in the
// configured claim; no substring or case-folded authorization decisions are
// made.
type OIDCConfig struct {
	IssuerURL            string
	ClientID             string
	ClientSecret         string
	RedirectURL          string
	AdministratorClaim   string
	AdministratorValues  []string
	RequireVerifiedEmail bool
}

// OIDCPrincipal is the verified identity assertion passed to Guard. Issuer and
// Subject form the stable external identity key. Profile claims are never used
// as that key.
type OIDCPrincipal struct {
	Issuer            string
	Subject           string
	PreferredUsername string
	Email             string
	EmailVerified     bool
	DisplayName       string
	Administrator     bool
}

// OIDCAuthenticator is the protocol boundary consumed by the encrypted flow
// coordinator and replaced by deterministic fakes in unit tests.
type OIDCAuthenticator interface {
	AuthorizationURL(state, nonce, verifier string) (string, error)
	Authenticate(ctx context.Context, code, verifier, nonce string) (OIDCPrincipal, error)
}

// OIDCClient performs discovery, authorization-code exchange with S256 PKCE,
// and signed ID-token verification through the provider's discovered keys.
type OIDCClient struct {
	oauth                oauth2.Config
	verifier             *oidc.IDTokenVerifier
	administratorClaim   string
	administratorValues  map[string]struct{}
	requireVerifiedEmail bool
}

func NewOIDCClient(ctx context.Context, configuration OIDCConfig) (*OIDCClient, error) {
	if ctx == nil {
		return nil, errors.New("initialize OpenID Connect: context is required")
	}
	configuration.IssuerURL = strings.TrimSpace(configuration.IssuerURL)
	configuration.ClientID = strings.TrimSpace(configuration.ClientID)
	configuration.RedirectURL = strings.TrimSpace(configuration.RedirectURL)
	configuration.AdministratorClaim = strings.TrimSpace(configuration.AdministratorClaim)
	if err := validateOIDCEndpoint(configuration.IssuerURL); err != nil {
		return nil, fmt.Errorf("initialize OpenID Connect: issuer URL is invalid: %w", err)
	}
	if err := validateOIDCEndpoint(configuration.RedirectURL); err != nil {
		return nil, fmt.Errorf("initialize OpenID Connect: redirect URL is invalid: %w", err)
	}
	if configuration.ClientID == "" || configuration.ClientSecret == "" {
		return nil, errors.New("initialize OpenID Connect: client id and client secret are required")
	}
	if configuration.AdministratorClaim == "" {
		configuration.AdministratorClaim = "groups"
	}
	administratorValues := make(map[string]struct{}, len(configuration.AdministratorValues))
	for _, value := range configuration.AdministratorValues {
		value = strings.TrimSpace(value)
		if value != "" {
			administratorValues[value] = struct{}{}
		}
	}
	provider, err := oidc.NewProvider(ctx, configuration.IssuerURL)
	if err != nil {
		return nil, errors.New("initialize OpenID Connect: provider discovery failed")
	}
	oauthConfiguration := oauth2.Config{
		ClientID:     configuration.ClientID,
		ClientSecret: configuration.ClientSecret,
		RedirectURL:  configuration.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, oidc.ScopeProfile, oidc.ScopeEmail},
	}
	return &OIDCClient{
		oauth:                oauthConfiguration,
		verifier:             provider.Verifier(&oidc.Config{ClientID: configuration.ClientID}),
		administratorClaim:   configuration.AdministratorClaim,
		administratorValues:  administratorValues,
		requireVerifiedEmail: configuration.RequireVerifiedEmail,
	}, nil
}

func (c *OIDCClient) AuthorizationURL(state, nonce, verifier string) (string, error) {
	if c == nil || state == "" || nonce == "" || verifier == "" {
		return "", ErrOIDCAuthentication
	}
	return c.oauth.AuthCodeURL(
		state,
		oidc.Nonce(nonce),
		oauth2.S256ChallengeOption(verifier),
	), nil
}

func (c *OIDCClient) Authenticate(ctx context.Context, code, verifier, expectedNonce string) (OIDCPrincipal, error) {
	if c == nil || ctx == nil || code == "" || len(code) > maximumAuthorizationCode || verifier == "" || expectedNonce == "" {
		return OIDCPrincipal{}, ErrOIDCAuthentication
	}
	token, err := c.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return OIDCPrincipal{}, ErrOIDCAuthentication
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return OIDCPrincipal{}, ErrOIDCAuthentication
	}
	idToken, err := c.verifier.Verify(ctx, rawIDToken)
	if err != nil || subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(expectedNonce)) != 1 {
		return OIDCPrincipal{}, ErrOIDCAuthentication
	}
	claims := make(map[string]json.RawMessage)
	if err := idToken.Claims(&claims); err != nil {
		return OIDCPrincipal{}, ErrOIDCAuthentication
	}
	principal, err := principalFromClaims(idToken.Issuer, idToken.Subject, claims, c.administratorClaim, c.administratorValues)
	if err != nil || c.requireVerifiedEmail && !principal.EmailVerified {
		return OIDCPrincipal{}, ErrOIDCAuthentication
	}
	return principal, nil
}

func principalFromClaims(
	issuer string,
	subject string,
	claims map[string]json.RawMessage,
	administratorClaim string,
	administratorValues map[string]struct{},
) (OIDCPrincipal, error) {
	var standard struct {
		Email             string `json:"email"`
		EmailVerified     bool   `json:"email_verified"`
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
	}
	encoded, err := json.Marshal(claims)
	if err != nil || json.Unmarshal(encoded, &standard) != nil {
		return OIDCPrincipal{}, ErrOIDCAuthentication
	}
	issuer = strings.TrimSpace(issuer)
	subject = strings.TrimSpace(subject)
	email := strings.ToLower(strings.TrimSpace(standard.Email))
	displayName := strings.TrimSpace(standard.Name)
	if displayName == "" {
		displayName = strings.TrimSpace(standard.PreferredUsername)
	}
	if displayName == "" {
		displayName = email
	}
	if issuer == "" || subject == "" || email == "" || displayName == "" {
		return OIDCPrincipal{}, ErrOIDCAuthentication
	}
	principal := OIDCPrincipal{
		Issuer:            issuer,
		Subject:           subject,
		PreferredUsername: strings.TrimSpace(standard.PreferredUsername),
		Email:             email,
		EmailVerified:     standard.EmailVerified,
		DisplayName:       displayName,
	}
	for _, value := range claimStrings(claims[administratorClaim]) {
		if _, ok := administratorValues[value]; ok {
			principal.Administrator = true
			break
		}
	}
	return principal, nil
}

func claimStrings(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var single string
	if json.Unmarshal(raw, &single) == nil {
		return []string{single}
	}
	var values []string
	if json.Unmarshal(raw, &values) == nil {
		sort.Strings(values)
		return values
	}
	return nil
}

func validateOIDCEndpoint(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("an absolute URL without credentials, query, or fragment is required")
	}
	if parsed.Scheme == "https" {
		return nil
	}
	if parsed.Scheme != "http" {
		return errors.New("HTTPS is required")
	}
	host := parsed.Hostname()
	if !strings.EqualFold(host, "localhost") {
		address := net.ParseIP(host)
		if address == nil || !address.IsLoopback() {
			return errors.New("HTTP is allowed only for loopback development")
		}
	}
	return nil
}

// OIDCFlow binds state, nonce, and PKCE verifier values to the initiating user
// agent in a short-lived AEAD-protected value. Provider tokens are never stored
// in the transaction or returned to the browser.
type OIDCFlow struct {
	authenticator  OIDCAuthenticator
	aead           cipher.AEAD
	now            func() time.Time
	transactionTTL time.Duration
}

type oidcTransaction struct {
	State     string `json:"state"`
	Nonce     string `json:"nonce"`
	Verifier  string `json:"verifier"`
	ExpiresAt int64  `json:"expiresAt"`
}

func NewOIDCFlow(authenticator OIDCAuthenticator, transactionSecret string, now func() time.Time) (*OIDCFlow, error) {
	if authenticator == nil || len(transactionSecret) < 32 {
		return nil, errors.New("OpenID Connect authenticator and 32-byte transaction secret are required")
	}
	key := sha256.Sum256([]byte(transactionSecret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, errors.New("initialize OpenID Connect transaction protection")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errors.New("initialize OpenID Connect transaction protection")
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &OIDCFlow{authenticator: authenticator, aead: aead, now: now, transactionTTL: defaultOIDCTransactionTTL}, nil
}

func (f *OIDCFlow) Start() (authorizationURL, transactionValue string, expiresAt time.Time, err error) {
	if f == nil {
		return "", "", time.Time{}, ErrOIDCAuthentication
	}
	state, err := randomOIDCValue()
	if err != nil {
		return "", "", time.Time{}, err
	}
	nonce, err := randomOIDCValue()
	if err != nil {
		return "", "", time.Time{}, err
	}
	verifier, err := randomOIDCValue()
	if err != nil {
		return "", "", time.Time{}, err
	}
	expiresAt = f.now().Add(f.transactionTTL)
	transactionValue, err = f.seal(oidcTransaction{State: state, Nonce: nonce, Verifier: verifier, ExpiresAt: expiresAt.Unix()})
	if err != nil {
		return "", "", time.Time{}, err
	}
	authorizationURL, err = f.authenticator.AuthorizationURL(state, nonce, verifier)
	if err != nil {
		return "", "", time.Time{}, ErrOIDCAuthentication
	}
	return authorizationURL, transactionValue, expiresAt, nil
}

func (f *OIDCFlow) Complete(ctx context.Context, transactionValue, returnedState, code string) (OIDCPrincipal, error) {
	if f == nil || ctx == nil || transactionValue == "" || returnedState == "" || code == "" {
		return OIDCPrincipal{}, ErrOIDCAuthentication
	}
	transaction, err := f.validatedTransaction(transactionValue, returnedState)
	if err != nil {
		return OIDCPrincipal{}, ErrOIDCAuthentication
	}
	principal, err := f.authenticator.Authenticate(ctx, code, transaction.Verifier, transaction.Nonce)
	if err != nil {
		return OIDCPrincipal{}, ErrOIDCAuthentication
	}
	return principal, nil
}

func (f *OIDCFlow) Validate(transactionValue, returnedState string) error {
	if f == nil || transactionValue == "" || returnedState == "" {
		return ErrOIDCAuthentication
	}
	_, err := f.validatedTransaction(transactionValue, returnedState)
	return err
}

func (f *OIDCFlow) validatedTransaction(transactionValue, returnedState string) (oidcTransaction, error) {
	transaction, err := f.open(transactionValue)
	if err != nil || !time.Unix(transaction.ExpiresAt, 0).After(f.now()) ||
		subtle.ConstantTimeCompare([]byte(transaction.State), []byte(returnedState)) != 1 {
		return oidcTransaction{}, ErrOIDCAuthentication
	}
	return transaction, nil
}

func (f *OIDCFlow) seal(transaction oidcTransaction) (string, error) {
	plaintext, err := json.Marshal(transaction)
	if err != nil {
		return "", ErrOIDCAuthentication
	}
	nonce := make([]byte, f.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", errors.New("generate OpenID Connect transaction nonce")
	}
	sealed := f.aead.Seal(nonce, nonce, plaintext, []byte("stewardmesh-oidc-transaction-v1"))
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (f *OIDCFlow) open(value string) (oidcTransaction, error) {
	encoded, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil || len(encoded) <= f.aead.NonceSize() {
		return oidcTransaction{}, ErrOIDCAuthentication
	}
	nonce := encoded[:f.aead.NonceSize()]
	plaintext, err := f.aead.Open(nil, nonce, encoded[f.aead.NonceSize():], []byte("stewardmesh-oidc-transaction-v1"))
	if err != nil {
		return oidcTransaction{}, ErrOIDCAuthentication
	}
	var transaction oidcTransaction
	if err := json.Unmarshal(plaintext, &transaction); err != nil || transaction.State == "" ||
		transaction.Nonce == "" || transaction.Verifier == "" || transaction.ExpiresAt == 0 {
		return oidcTransaction{}, ErrOIDCAuthentication
	}
	return transaction, nil
}

func randomOIDCValue() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", errors.New("generate OpenID Connect transaction value")
	}
	encoded := base64.RawURLEncoding.EncodeToString(value)
	clear(value)
	return encoded, nil
}
