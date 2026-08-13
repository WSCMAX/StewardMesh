package bridge

// Requirements: REQ-API-001, SEC-MCP-001. Feature: integrations.protocols. GitHub: #14.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/people"
	"github.com/maxlemke/stewardmesh/internal/signals"
)

const (
	defaultAuthorizationRequestTTL = 10 * time.Minute
	defaultAuthorizationCodeTTL    = 2 * time.Minute
	defaultAccessTokenTTL          = 15 * time.Minute
	defaultRefreshTokenTTL         = 8 * time.Hour
	defaultConfirmationTTL         = 2 * time.Minute
)

var (
	bridgeIDPattern           = regexp.MustCompile(`^[a-f0-9]{32}$`)
	pkceVerifierPattern       = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)
	oauthStatePattern         = regexp.MustCompile(`^[\x21-\x7e]{1,512}$`)
	confirmationActionPattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)
)

type ServiceConfig struct {
	OrganizationID          string
	Issuer                  string
	ResourceURI             string
	AuthorizationRequestTTL time.Duration
	AuthorizationCodeTTL    time.Duration
	AccessTokenTTL          time.Duration
	RefreshTokenTTL         time.Duration
	ConfirmationTTL         time.Duration
	Now                     func() time.Time
}

type Service struct {
	store           Store
	guard           *guard.Service
	atlas           *atlas.Service
	people          *people.Service
	signals         *signals.Service
	auditor         foundation.Auditor
	organization    domain.Organization
	issuer          string
	resourceURI     string
	authorizeTTL    time.Duration
	codeTTL         time.Duration
	accessTTL       time.Duration
	refreshTTL      time.Duration
	confirmationTTL time.Duration
	now             func() time.Time
	concurrency     chan struct{}
}

type AuthorizationInput struct {
	ResponseType        string
	ClientID            string
	RedirectURI         string
	ResourceURI         string
	Scopes              string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
}

type TokenInput struct {
	GrantType    string
	Code         string
	RefreshToken string
	ClientID     string
	RedirectURI  string
	ResourceURI  string
	CodeVerifier string
}

type Access struct {
	Grant          Grant
	Authentication guard.Authentication
}

func NewService(store Store, guardService *guard.Service, atlasService *atlas.Service, peopleService *people.Service, signalsService *signals.Service, auditor foundation.Auditor, organization domain.Organization, configuration ServiceConfig) (*Service, error) {
	if store == nil || guardService == nil || atlasService == nil || peopleService == nil || signalsService == nil || auditor == nil {
		return nil, errors.New("Bridge store, Guard, domain services, and auditor are required")
	}
	configuration.OrganizationID = strings.TrimSpace(configuration.OrganizationID)
	if configuration.OrganizationID == "" || organization.ID != configuration.OrganizationID {
		return nil, errors.New("Bridge organization is required and must match the configured organization")
	}
	issuer, err := normalizeIssuer(configuration.Issuer)
	if err != nil {
		return nil, err
	}
	resourceURI, err := normalizeResourceURI(configuration.ResourceURI)
	if err != nil {
		return nil, err
	}
	issuerURL, _ := url.Parse(issuer)
	resourceURL, _ := url.Parse(resourceURI)
	if !strings.EqualFold(issuerURL.Scheme, resourceURL.Scheme) || !strings.EqualFold(issuerURL.Host, resourceURL.Host) {
		return nil, errors.New("Bridge issuer and MCP resource must use the same origin")
	}
	configuration.AuthorizationRequestTTL = durationOr(configuration.AuthorizationRequestTTL, defaultAuthorizationRequestTTL)
	configuration.AuthorizationCodeTTL = durationOr(configuration.AuthorizationCodeTTL, defaultAuthorizationCodeTTL)
	configuration.AccessTokenTTL = durationOr(configuration.AccessTokenTTL, defaultAccessTokenTTL)
	configuration.RefreshTokenTTL = durationOr(configuration.RefreshTokenTTL, defaultRefreshTokenTTL)
	configuration.ConfirmationTTL = durationOr(configuration.ConfirmationTTL, defaultConfirmationTTL)
	if configuration.AuthorizationRequestTTL < time.Minute || configuration.AuthorizationRequestTTL > 15*time.Minute ||
		configuration.AuthorizationCodeTTL < 30*time.Second || configuration.AuthorizationCodeTTL > 5*time.Minute ||
		configuration.AccessTokenTTL < 5*time.Minute || configuration.AccessTokenTTL > time.Hour ||
		configuration.RefreshTokenTTL < time.Hour || configuration.RefreshTokenTTL > 24*time.Hour ||
		configuration.ConfirmationTTL < 30*time.Second || configuration.ConfirmationTTL > 5*time.Minute {
		return nil, errors.New("Bridge credential lifetimes are outside their safe bounds")
	}
	if configuration.Now == nil {
		configuration.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{
		store: store, guard: guardService, atlas: atlasService, people: peopleService, signals: signalsService,
		auditor: auditor, organization: organization, issuer: issuer, resourceURI: resourceURI,
		authorizeTTL: configuration.AuthorizationRequestTTL, codeTTL: configuration.AuthorizationCodeTTL,
		accessTTL: configuration.AccessTokenTTL, refreshTTL: configuration.RefreshTokenTTL,
		confirmationTTL: configuration.ConfirmationTTL, now: configuration.Now,
		concurrency: make(chan struct{}, MaximumConcurrentMCPCalls),
	}, nil
}

func durationOr(value, fallback time.Duration) time.Duration {
	if value == 0 {
		return fallback
	}
	return value
}

func (s *Service) Issuer() string                    { return s.issuer }
func (s *Service) ResourceURI() string               { return s.resourceURI }
func (s *Service) Organization() domain.Organization { return s.organization }
func (s *Service) DomainServices() (*atlas.Service, *people.Service, *signals.Service) {
	return s.atlas, s.people, s.signals
}

func (s *Service) Acquire(ctx context.Context) error {
	select {
	case s.concurrency <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return ErrRateLimited
	}
}

func (s *Service) Release() { <-s.concurrency }

func (s *Service) CreateClient(ctx context.Context, authentication guard.Authentication, input CreateClientInput) (Client, error) {
	if err := s.requirePermission(ctx, authentication, guard.PermissionIntegrationsWrite); err != nil {
		return Client{}, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 120 {
		return Client{}, ErrInvalidInput
	}
	redirects, err := normalizeRedirectURIs(input.RedirectURIs)
	if err != nil {
		return Client{}, err
	}
	scopes, err := normalizeScopes(input.AllowedScopes, true)
	if err != nil {
		return Client{}, err
	}
	id, err := newID()
	if err != nil {
		return Client{}, err
	}
	client := Client{ID: id, OrganizationID: s.organization.ID, Name: name, RedirectURIs: redirects, AllowedScopes: scopes,
		CreatedBy: authentication.Principal.Subject, CreatedAt: s.now().UTC()}
	created, err := s.store.CreateClient(ctx, client)
	if err != nil {
		return Client{}, err
	}
	if err := s.audit(ctx, authentication.Principal.Subject, "bridge.oauth.client.created", "oauth_client", created.ID,
		map[string]string{"redirectCount": fmt.Sprintf("%d", len(created.RedirectURIs)), "scopeCount": fmt.Sprintf("%d", len(created.AllowedScopes))}); err != nil {
		return Client{}, fmt.Errorf("audit OAuth client creation: %w", err)
	}
	return created, nil
}

func (s *Service) ListClients(ctx context.Context, authentication guard.Authentication) ([]Client, error) {
	if err := s.requirePermission(ctx, authentication, guard.PermissionIntegrationsRead); err != nil {
		return nil, err
	}
	return s.store.ListClients(ctx, s.organization.ID)
}

func (s *Service) RevokeClient(ctx context.Context, authentication guard.Authentication, clientID string) (Client, error) {
	if err := s.requirePermission(ctx, authentication, guard.PermissionIntegrationsWrite); err != nil {
		return Client{}, err
	}
	if !bridgeIDPattern.MatchString(strings.TrimSpace(clientID)) {
		return Client{}, ErrInvalidInput
	}
	revoked, err := s.store.RevokeClient(ctx, s.organization.ID, clientID, s.now().UTC())
	if err != nil {
		return Client{}, err
	}
	if err := s.audit(ctx, authentication.Principal.Subject, "bridge.oauth.client.revoked", "oauth_client", revoked.ID, nil); err != nil {
		return Client{}, fmt.Errorf("audit OAuth client revocation: %w", err)
	}
	return revoked, nil
}

func (s *Service) ListGrants(ctx context.Context, authentication guard.Authentication) ([]Grant, error) {
	if err := s.requirePermission(ctx, authentication, guard.PermissionIntegrationsRead); err != nil {
		return nil, err
	}
	return s.store.ListGrants(ctx, s.organization.ID)
}

func (s *Service) RevokeGrant(ctx context.Context, authentication guard.Authentication, grantID string) (Grant, error) {
	if err := s.requirePermission(ctx, authentication, guard.PermissionIntegrationsWrite); err != nil {
		return Grant{}, err
	}
	if !bridgeIDPattern.MatchString(strings.TrimSpace(grantID)) {
		return Grant{}, ErrInvalidInput
	}
	revoked, err := s.store.RevokeGrant(ctx, s.organization.ID, grantID, s.now().UTC())
	if err != nil {
		return Grant{}, err
	}
	if err := s.audit(ctx, authentication.Principal.Subject, "bridge.oauth.grant.revoked", "oauth_grant", revoked.ID, nil); err != nil {
		return Grant{}, fmt.Errorf("audit OAuth grant revocation: %w", err)
	}
	return revoked, nil
}

func (s *Service) BeginAuthorization(ctx context.Context, authentication guard.Authentication, input AuthorizationInput) (AuthorizationRequest, error) {
	if input.ResponseType != "code" || input.CodeChallengeMethod != "S256" || !validCodeChallenge(input.CodeChallenge) ||
		input.State != "" && !oauthStatePattern.MatchString(input.State) || input.ResourceURI != s.resourceURI {
		return AuthorizationRequest{}, ErrInvalidInput
	}
	client, err := s.store.GetClient(ctx, s.organization.ID, strings.TrimSpace(input.ClientID))
	if err != nil || client.RevokedAt != nil {
		return AuthorizationRequest{}, ErrUnauthorized
	}
	if !containsString(client.RedirectURIs, input.RedirectURI) {
		return AuthorizationRequest{}, ErrUnauthorized
	}
	scopes, err := parseScopeString(input.Scopes)
	if err != nil || !scopeSubset(scopes, client.AllowedScopes) {
		return AuthorizationRequest{}, ErrPermissionDenied
	}
	for _, permission := range permissionsForScopes(scopes) {
		if permission == guard.PermissionIntegrationsRead || permission == guard.PermissionSignalsRead || permission == guard.PermissionSignalsWrite {
			if err := s.requirePermission(ctx, authentication, permission); err != nil {
				return AuthorizationRequest{}, err
			}
			continue
		}
		if !hasPermission(authentication, permission, s.organization.ID) {
			return AuthorizationRequest{}, ErrPermissionDenied
		}
	}
	id, err := newID()
	if err != nil {
		return AuthorizationRequest{}, err
	}
	now := s.now().UTC()
	request := AuthorizationRequest{ID: id, OrganizationID: s.organization.ID, ClientID: client.ID, ActorID: authentication.Principal.Subject,
		RedirectURI: input.RedirectURI, ResourceURI: s.resourceURI, Scopes: scopes, State: input.State,
		CodeChallenge: input.CodeChallenge, CreatedAt: now, ExpiresAt: now.Add(s.authorizeTTL)}
	if err := s.store.CreateAuthorizationRequest(ctx, request); err != nil {
		return AuthorizationRequest{}, err
	}
	return request, nil
}

func (s *Service) Consent(ctx context.Context, authentication guard.Authentication, requestID string) (Consent, error) {
	request, client, err := s.loadConsent(ctx, authentication, requestID)
	if err != nil {
		return Consent{}, err
	}
	return Consent{ID: request.ID, ClientID: client.ID, ClientName: client.Name, Scopes: append([]Scope(nil), request.Scopes...), ExpiresAt: request.ExpiresAt}, nil
}

func (s *Service) DecideConsent(ctx context.Context, authentication guard.Authentication, requestID string, approved bool) (string, error) {
	request, _, err := s.loadConsent(ctx, authentication, requestID)
	if err != nil {
		return "", err
	}
	now := s.now().UTC()
	request.DecidedAt, request.Approved = &now, approved
	redirect, _ := url.Parse(request.RedirectURI)
	query := redirect.Query()
	query.Set("iss", s.issuer)
	if request.State != "" {
		query.Set("state", request.State)
	}
	var codeRecord *AuthorizationCode
	if approved {
		rawCode, codeHash, err := newSecret()
		if err != nil {
			return "", err
		}
		codeID, err := newID()
		if err != nil {
			return "", err
		}
		codeRecord = &AuthorizationCode{ID: codeID, OrganizationID: s.organization.ID, RequestID: request.ID, ClientID: request.ClientID,
			ActorID: request.ActorID, RedirectURI: request.RedirectURI, ResourceURI: request.ResourceURI, Scopes: append([]Scope(nil), request.Scopes...),
			CodeHash: codeHash, CodeChallenge: request.CodeChallenge, CreatedAt: now, ExpiresAt: now.Add(s.codeTTL)}
		query.Set("code", rawCode)
	} else {
		query.Set("error", "access_denied")
		query.Set("error_description", "The resource owner declined access.")
	}
	if err := s.store.DecideAuthorizationRequest(ctx, request, codeRecord); err != nil {
		return "", err
	}
	redirect.RawQuery = query.Encode()
	action := "bridge.oauth.consent.denied"
	if approved {
		action = "bridge.oauth.consent.approved"
	}
	if err := s.audit(ctx, authentication.Principal.Subject, action, "oauth_client", request.ClientID,
		map[string]string{"scopeCount": fmt.Sprintf("%d", len(request.Scopes))}); err != nil {
		return "", fmt.Errorf("audit OAuth consent decision: %w", err)
	}
	return redirect.String(), nil
}

func (s *Service) loadConsent(ctx context.Context, authentication guard.Authentication, requestID string) (AuthorizationRequest, Client, error) {
	if !bridgeIDPattern.MatchString(strings.TrimSpace(requestID)) || authentication.Principal.Subject == "" {
		return AuthorizationRequest{}, Client{}, ErrInvalidInput
	}
	request, err := s.store.GetAuthorizationRequest(ctx, s.organization.ID, requestID)
	if err != nil {
		return AuthorizationRequest{}, Client{}, err
	}
	if request.ActorID != authentication.Principal.Subject {
		return AuthorizationRequest{}, Client{}, ErrUnauthorized
	}
	if request.DecidedAt != nil {
		return AuthorizationRequest{}, Client{}, ErrReplay
	}
	if !request.ExpiresAt.After(s.now().UTC()) {
		return AuthorizationRequest{}, Client{}, ErrExpired
	}
	client, err := s.store.GetClient(ctx, s.organization.ID, request.ClientID)
	if err != nil || client.RevokedAt != nil {
		return AuthorizationRequest{}, Client{}, ErrUnauthorized
	}
	return request, client, nil
}

func (s *Service) ExchangeToken(ctx context.Context, input TokenInput) (TokenCredentials, error) {
	if input.ResourceURI != s.resourceURI || !bridgeIDPattern.MatchString(input.ClientID) {
		return TokenCredentials{}, ErrInvalidInput
	}
	switch input.GrantType {
	case "authorization_code":
		if input.Code == "" || !pkceVerifierPattern.MatchString(input.CodeVerifier) || input.RedirectURI == "" {
			return TokenCredentials{}, ErrInvalidInput
		}
		challenge := pkceChallenge(input.CodeVerifier)
		grant, credentials, err := s.newGrant(input.ClientID, "", nil)
		if err != nil {
			return TokenCredentials{}, err
		}
		codeHash := secretHash(input.Code)
		grant, err = s.store.ExchangeAuthorizationCode(ctx, s.organization.ID, codeHash, input.ClientID, input.RedirectURI, input.ResourceURI, challenge, s.now().UTC(), grant)
		if err != nil {
			return TokenCredentials{}, err
		}
		credentials.Grant, credentials.Scope = grant, scopeString(grant.Scopes)
		credentials.ExpiresIn = expiresInSeconds(s.now().UTC(), grant.AccessExpiresAt)
		if err := s.audit(ctx, grant.ActorID, "bridge.oauth.token.issued", "oauth_grant", grant.ID, map[string]string{"clientId": grant.ClientID}); err != nil {
			return TokenCredentials{}, fmt.Errorf("audit OAuth token issuance: %w", err)
		}
		return credentials, nil
	case "refresh_token":
		if input.RefreshToken == "" || input.RedirectURI != "" || input.Code != "" || input.CodeVerifier != "" {
			return TokenCredentials{}, ErrInvalidInput
		}
		grant, credentials, err := s.newGrant(input.ClientID, "", nil)
		if err != nil {
			return TokenCredentials{}, err
		}
		grant, err = s.store.RotateRefreshToken(ctx, s.organization.ID, secretHash(input.RefreshToken), input.ClientID, input.ResourceURI, s.now().UTC(), grant)
		if err != nil {
			return TokenCredentials{}, err
		}
		credentials.Grant, credentials.Scope = grant, scopeString(grant.Scopes)
		credentials.ExpiresIn = expiresInSeconds(s.now().UTC(), grant.AccessExpiresAt)
		if err := s.audit(ctx, grant.ActorID, "bridge.oauth.token.rotated", "oauth_grant", grant.ID, map[string]string{"clientId": grant.ClientID}); err != nil {
			return TokenCredentials{}, fmt.Errorf("audit OAuth token rotation: %w", err)
		}
		return credentials, nil
	default:
		return TokenCredentials{}, ErrInvalidInput
	}
}

func (s *Service) newGrant(clientID, actorID string, scopes []Scope) (Grant, TokenCredentials, error) {
	accessToken, accessHash, err := newSecret()
	if err != nil {
		return Grant{}, TokenCredentials{}, err
	}
	refreshToken, refreshHash, err := newSecret()
	if err != nil {
		return Grant{}, TokenCredentials{}, err
	}
	id, err := newID()
	if err != nil {
		return Grant{}, TokenCredentials{}, err
	}
	now := s.now().UTC()
	grant := Grant{ID: id, OrganizationID: s.organization.ID, ClientID: clientID, ActorID: actorID, ResourceURI: s.resourceURI,
		Scopes: append([]Scope(nil), scopes...), AccessTokenHash: accessHash, RefreshTokenHash: refreshHash,
		AccessExpiresAt: now.Add(s.accessTTL), RefreshExpiresAt: now.Add(s.refreshTTL), CreatedAt: now}
	return grant, TokenCredentials{AccessToken: accessToken, TokenType: "Bearer", ExpiresIn: int64(s.accessTTL.Seconds()), RefreshToken: refreshToken}, nil
}

func (s *Service) AuthenticateAccessToken(ctx context.Context, rawToken string) (Access, error) {
	if rawToken == "" || len(rawToken) > 512 {
		return Access{}, ErrUnauthorized
	}
	grant, err := s.store.AuthenticateAccessToken(ctx, s.organization.ID, secretHash(rawToken), s.resourceURI, s.now().UTC())
	if err != nil {
		return Access{}, ErrUnauthorized
	}
	authentication, err := s.guard.AuthenticateAccount(ctx, grant.ActorID)
	if err != nil || authentication.Principal.OrganizationID != s.organization.ID {
		return Access{}, ErrUnauthorized
	}
	return Access{Grant: grant, Authentication: authentication}, nil
}

// AuthenticateLocalSession binds a stdio server to one current Guard session,
// the configured organization, and an explicit bounded scope list. The session
// token is never converted into an MCP bearer token or persisted by Bridge.
func (s *Service) AuthenticateLocalSession(ctx context.Context, rawSessionToken, requestedScopes string) (Access, error) {
	if rawSessionToken == "" || len(rawSessionToken) > 512 {
		return Access{}, ErrUnauthorized
	}
	authentication, err := s.guard.AuthenticateSession(ctx, rawSessionToken)
	if err != nil || authentication.Principal.OrganizationID != s.organization.ID {
		return Access{}, ErrUnauthorized
	}
	if err := s.requirePermission(ctx, authentication, guard.PermissionIntegrationsRead); err != nil {
		return Access{}, err
	}
	scopes, err := parseScopeString(requestedScopes)
	if err != nil {
		return Access{}, err
	}
	for _, permission := range permissionsForScopes(scopes) {
		if permission == guard.PermissionIntegrationsRead || permission == guard.PermissionSignalsRead || permission == guard.PermissionSignalsWrite {
			if err := s.requirePermission(ctx, authentication, permission); err != nil {
				return Access{}, err
			}
			continue
		}
		if !hasPermission(authentication, permission, s.organization.ID) {
			return Access{}, ErrPermissionDenied
		}
	}
	grant := Grant{ID: "local-stdio", OrganizationID: s.organization.ID, ClientID: "local-stdio",
		ActorID: authentication.Principal.Subject, ResourceURI: s.resourceURI, Scopes: scopes,
		CreatedAt: authentication.Session.CreatedAt, AccessExpiresAt: authentication.Session.ExpiresAt,
		RefreshExpiresAt: authentication.Session.ExpiresAt}
	return Access{Grant: grant, Authentication: authentication}, nil
}

func (s *Service) RevokeToken(ctx context.Context, rawToken string) error {
	if rawToken == "" || len(rawToken) > 512 {
		return nil // RFC 7009-style idempotent response without token probing.
	}
	err := s.store.RevokeToken(ctx, s.organization.ID, secretHash(rawToken), s.now().UTC())
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

func (s *Service) PrepareConfirmation(ctx context.Context, authentication guard.Authentication, action string, arguments any, summary string) (ConfirmationChallenge, error) {
	action = strings.TrimSpace(action)
	if authentication.Principal.OrganizationID != s.organization.ID || authentication.Principal.Subject == "" ||
		!confirmationActionPattern.MatchString(action) || summary == "" || !utf8.ValidString(summary) || utf8.RuneCountInString(summary) > 300 {
		return ConfirmationChallenge{}, ErrInvalidInput
	}
	argumentHash, err := canonicalHash(arguments)
	if err != nil {
		return ConfirmationChallenge{}, ErrInvalidInput
	}
	rawToken, tokenHash, err := newSecret()
	if err != nil {
		return ConfirmationChallenge{}, err
	}
	id, err := newID()
	if err != nil {
		return ConfirmationChallenge{}, err
	}
	now := s.now().UTC()
	confirmation := Confirmation{ID: id, OrganizationID: s.organization.ID, ActorID: authentication.Principal.Subject, Action: action,
		ArgumentsHash: argumentHash, TokenHash: tokenHash, CreatedAt: now, ExpiresAt: now.Add(s.confirmationTTL)}
	if err := s.store.CreateConfirmation(ctx, confirmation); err != nil {
		return ConfirmationChallenge{}, err
	}
	if err := s.audit(ctx, authentication.Principal.Subject, "bridge.mcp.confirmation.prepared", "mcp_confirmation", confirmation.ID,
		map[string]string{"action": action}); err != nil {
		return ConfirmationChallenge{}, fmt.Errorf("audit MCP confirmation preparation: %w", err)
	}
	return ConfirmationChallenge{ConfirmationToken: rawToken, Action: action, Summary: summary, ExpiresAt: confirmation.ExpiresAt}, nil
}

func (s *Service) ConsumeConfirmation(ctx context.Context, authentication guard.Authentication, action string, arguments any, rawToken string) error {
	argumentHash, err := canonicalHash(arguments)
	if err != nil || authentication.Principal.OrganizationID != s.organization.ID || authentication.Principal.Subject == "" ||
		rawToken == "" || len(rawToken) > 512 {
		return ErrInvalidInput
	}
	confirmation, err := s.store.ConsumeConfirmation(ctx, s.organization.ID, authentication.Principal.Subject, action, argumentHash, secretHash(rawToken), s.now().UTC())
	if err != nil {
		return err
	}
	return s.audit(ctx, authentication.Principal.Subject, "bridge.mcp.confirmation.consumed", "mcp_confirmation", confirmation.ID,
		map[string]string{"action": action})
}

func (s *Service) AllowRate(ctx context.Context, dimensions []string, limit int, window time.Duration) error {
	if len(dimensions) == 0 || len(dimensions) > 3 || limit < 1 || limit > 10_000 || window < time.Second || window > time.Hour {
		return ErrInvalidInput
	}
	now := s.now().UTC()
	windowStart := now.Truncate(window)
	for _, dimension := range dimensions {
		if dimension == "" || len(dimension) > 512 {
			return ErrInvalidInput
		}
		key := sha256.Sum256([]byte("bridge-rate-v1\x00" + s.organization.ID + "\x00" + dimension))
		allowed, err := s.store.AllowRate(ctx, s.organization.ID, key, windowStart, limit)
		if err != nil {
			return err
		}
		if !allowed {
			return ErrRateLimited
		}
	}
	return nil
}

func (s *Service) requirePermission(ctx context.Context, authentication guard.Authentication, permission guard.Permission) error {
	if authentication.Principal.OrganizationID != s.organization.ID || authentication.Principal.Subject == "" {
		return ErrUnauthorized
	}
	if err := s.guard.CheckPermission(ctx, authentication, permission, guard.Scope{Kind: guard.ScopeOrganization, OrganizationID: s.organization.ID, ResourceID: s.organization.ID}); err != nil {
		return ErrPermissionDenied
	}
	return nil
}

func (s *Service) RequireScopePermission(ctx context.Context, access Access, scope Scope, permission guard.Permission) error {
	if !hasScope(access.Grant.Scopes, scope) {
		return ErrPermissionDenied
	}
	// Signals currently has no site/department relationship that Bridge can
	// safely project. Keep it organization-scoped, matching the REST surface,
	// until that domain has an explicit visibility model.
	if permission == guard.PermissionSignalsRead || permission == guard.PermissionSignalsWrite {
		return s.requirePermission(ctx, access.Authentication, permission)
	}
	if !hasPermission(access.Authentication, permission, s.organization.ID) {
		return ErrPermissionDenied
	}
	return nil
}

func hasPermission(authentication guard.Authentication, permission guard.Permission, organizationID string) bool {
	for _, grant := range authentication.Grants {
		if grant.Permission == permission && grant.Scope.OrganizationID == organizationID {
			return true
		}
	}
	return false
}

func (s *Service) CheckResourceWrite(ctx context.Context, authentication guard.Authentication, resourceType, resourceID string) error {
	return s.guard.CheckResourceWrite(ctx, authentication, resourceType, resourceID)
}

func (s *Service) ContextForAccess(ctx context.Context, access Access) (context.Context, error) {
	scoped, _, err := s.scopedContext(ctx, access.Authentication.Principal.Subject)
	return scoped, err
}

func permissionsForScopes(scopes []Scope) []guard.Permission {
	seen := map[guard.Permission]bool{}
	result := []guard.Permission{guard.PermissionIntegrationsRead}
	seen[guard.PermissionIntegrationsRead] = true
	for _, scope := range scopes {
		var permission guard.Permission
		switch scope {
		case ScopeMCPResources:
			permission = guard.PermissionIntegrationsRead
		case ScopeAssetsRead:
			permission = guard.PermissionAssetsRead
		case ScopeDirectoryRead:
			permission = guard.PermissionDirectoryRead
		case ScopeSignalsRead:
			permission = guard.PermissionSignalsRead
		case ScopeSignalsAcknowledge:
			permission = guard.PermissionSignalsWrite
		}
		if permission != "" && !seen[permission] {
			seen[permission] = true
			result = append(result, permission)
		}
	}
	return result
}

func hasScope(scopes []Scope, expected Scope) bool {
	for _, scope := range scopes {
		if scope == expected {
			return true
		}
	}
	return false
}

func parseScopeString(raw string) ([]Scope, error) {
	fields := strings.Fields(raw)
	values := make([]Scope, len(fields))
	for index, field := range fields {
		values[index] = Scope(field)
	}
	return normalizeScopes(values, true)
}

func normalizeScopes(values []Scope, requireResources bool) ([]Scope, error) {
	if len(values) == 0 || len(values) > MaximumScopes {
		return nil, ErrInvalidInput
	}
	valid := map[Scope]bool{}
	for _, scope := range SupportedScopes() {
		valid[scope] = true
	}
	seen := map[Scope]bool{}
	result := make([]Scope, 0, len(values))
	for _, value := range values {
		if !valid[value] || seen[value] {
			return nil, ErrInvalidInput
		}
		seen[value] = true
		result = append(result, value)
	}
	if requireResources && !seen[ScopeMCPResources] {
		return nil, ErrInvalidInput
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result, nil
}

func scopeSubset(requested, allowed []Scope) bool {
	set := make(map[Scope]bool, len(allowed))
	for _, scope := range allowed {
		set[scope] = true
	}
	for _, scope := range requested {
		if !set[scope] {
			return false
		}
	}
	return true
}

func scopeString(scopes []Scope) string {
	values := make([]string, len(scopes))
	for index, scope := range scopes {
		values[index] = string(scope)
	}
	return strings.Join(values, " ")
}

func expiresInSeconds(now, expiresAt time.Time) int64 {
	seconds := int64(expiresAt.Sub(now).Seconds())
	if seconds < 0 {
		return 0
	}
	return seconds
}

func normalizeRedirectURIs(values []string) ([]string, error) {
	if len(values) == 0 || len(values) > MaximumRedirectURIs {
		return nil, ErrInvalidInput
	}
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, raw := range values {
		if raw != strings.TrimSpace(raw) || len(raw) > 2048 || seen[raw] {
			return nil, ErrInvalidInput
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed.IsAbs() == false || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
			return nil, ErrInvalidInput
		}
		if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackHostname(parsed.Hostname())) {
			return nil, ErrInvalidInput
		}
		seen[raw] = true
		result = append(result, raw)
	}
	sort.Strings(result)
	return result, nil
}

func normalizeIssuer(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" {
		return "", errors.New("Bridge issuer must be an HTTP origin without credentials, query, fragment, or path")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackHostname(parsed.Hostname())) {
		return "", errors.New("Bridge issuer must use HTTPS except on loopback")
	}
	return raw, nil
}

func normalizeResourceURI(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "/mcp" {
		return "", errors.New("Bridge resource URI must be an absolute /mcp URL without credentials, query, or fragment")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackHostname(parsed.Hostname())) {
		return "", errors.New("Bridge resource URI must use HTTPS except on loopback")
	}
	return raw, nil
}

func isLoopbackHostname(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if subtle.ConstantTimeCompare([]byte(value), []byte(expected)) == 1 {
			return true
		}
	}
	return false
}

func validCodeChallenge(value string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && len(value) == 43
}

func pkceChallenge(verifier string) string {
	digest := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func newID() (string, error) { return foundation.NewCorrelationID() }

func newSecret() (string, []byte, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", nil, err
	}
	value := base64.RawURLEncoding.EncodeToString(buffer)
	clear(buffer)
	return value, secretHash(value), nil
}

func secretHash(value string) []byte {
	digest := sha256.Sum256([]byte(value))
	return append([]byte(nil), digest[:]...)
}

func (s *Service) audit(ctx context.Context, actorID, action, resourceType, resourceID string, metadata map[string]string) error {
	ctx, scope, err := s.scopedContext(ctx, actorID)
	if err != nil {
		return err
	}
	eventID, err := foundation.NewCorrelationID()
	if err != nil {
		return err
	}
	if metadata == nil {
		metadata = map[string]string{}
	}
	metadata["requirementId"] = RequirementID
	return s.auditor.Record(ctx, foundation.AuditEvent{ID: eventID, OrganizationID: s.organization.ID, ActorID: actorID,
		CorrelationID: scope.CorrelationID, Action: action, ResourceType: resourceType, ResourceID: resourceID,
		OccurredAt: s.now().UTC(), Metadata: metadata})
}

func (s *Service) scopedContext(ctx context.Context, actorID string) (context.Context, foundation.Scope, error) {
	if scope, ok := foundation.ScopeFromContext(ctx); ok {
		if scope.OrganizationID != s.organization.ID {
			return nil, foundation.Scope{}, ErrUnauthorized
		}
		scope.ActorID = actorID
		return foundation.WithScope(ctx, scope), scope, nil
	}
	correlationID, err := foundation.NewCorrelationID()
	if err != nil {
		return nil, foundation.Scope{}, err
	}
	scope := foundation.Scope{OrganizationID: s.organization.ID, ActorID: actorID, CorrelationID: correlationID}
	return foundation.WithScope(ctx, scope), scope, nil
}

func (s *Service) ContextForActor(ctx context.Context, actorID string) (context.Context, error) {
	ctx, _, err := s.scopedContext(ctx, actorID)
	return ctx, err
}
