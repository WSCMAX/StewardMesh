// Package bridge exposes StewardMesh through bounded, authenticated integration
// protocols. It deliberately keeps OAuth state and MCP confirmations behind a
// provider-neutral store so neither transport owns authorization truth.
// Requirements: REQ-API-001, SEC-MCP-001. Feature: integrations.protocols. GitHub: #14.
package bridge

import (
	"context"
	"crypto/sha256"
	"errors"
	"time"
)

const (
	RequirementID    = "SEC-MCP-001"
	APIRequirementID = "REQ-API-001"
	FeatureID        = "integrations.protocols"

	ProtocolVersion = "2026-07-28"
	SDKVersion      = "github.com/modelcontextprotocol/go-sdk v1.7.0"

	MaximumClients                  = 50
	MaximumRedirectURIs             = 10
	MaximumScopes                   = 5
	MaximumMCPResults               = 25
	MaximumMCPMessageBytes    int64 = 64 << 10
	MaximumConcurrentMCPCalls       = 8
)

var (
	ErrInvalidInput       = errors.New("invalid Bridge input")
	ErrNotFound           = errors.New("Bridge record not found")
	ErrConflict           = errors.New("Bridge record conflicts with existing data")
	ErrUnauthorized       = errors.New("Bridge authentication failed")
	ErrPermissionDenied   = errors.New("Bridge permission denied")
	ErrExpired            = errors.New("Bridge credential expired")
	ErrReplay             = errors.New("Bridge credential was already used")
	ErrRateLimited        = errors.New("Bridge request rate limited")
	ErrConfirmationNeeded = errors.New("Bridge write confirmation is required")
)

type Scope string

const (
	ScopeMCPResources       Scope = "mcp:resources"
	ScopeAssetsRead         Scope = "assets:read"
	ScopeDirectoryRead      Scope = "directory:read"
	ScopeSignalsRead        Scope = "signals:read"
	ScopeSignalsAcknowledge Scope = "signals:acknowledge"
)

func SupportedScopes() []Scope {
	return []Scope{ScopeMCPResources, ScopeAssetsRead, ScopeDirectoryRead, ScopeSignalsRead, ScopeSignalsAcknowledge}
}

// Client is a pre-registered public OAuth client. Bridge v1 intentionally
// does not issue or retain symmetric client secrets; S256 PKCE is mandatory.
type Client struct {
	ID             string     `json:"id"`
	OrganizationID string     `json:"organizationId"`
	Name           string     `json:"name"`
	RedirectURIs   []string   `json:"redirectUris"`
	AllowedScopes  []Scope    `json:"allowedScopes"`
	CreatedBy      string     `json:"createdBy"`
	CreatedAt      time.Time  `json:"createdAt"`
	RevokedAt      *time.Time `json:"revokedAt,omitempty"`
}

type CreateClientInput struct {
	Name          string   `json:"name"`
	RedirectURIs  []string `json:"redirectUris"`
	AllowedScopes []Scope  `json:"allowedScopes"`
}

// AuthorizationRequest is the short-lived, server-owned consent transaction.
// OAuth state is persisted only long enough to echo it to the exact redirect.
type AuthorizationRequest struct {
	ID             string
	OrganizationID string
	ClientID       string
	ActorID        string
	RedirectURI    string
	ResourceURI    string
	Scopes         []Scope
	State          string
	CodeChallenge  string
	CreatedAt      time.Time
	ExpiresAt      time.Time
	DecidedAt      *time.Time
	Approved       bool
}

type Consent struct {
	ID         string    `json:"id"`
	ClientID   string    `json:"clientId"`
	ClientName string    `json:"clientName"`
	Scopes     []Scope   `json:"scopes"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

type AuthorizationCode struct {
	ID             string
	OrganizationID string
	RequestID      string
	ClientID       string
	ActorID        string
	RedirectURI    string
	ResourceURI    string
	Scopes         []Scope
	CodeHash       []byte
	CodeChallenge  string
	CreatedAt      time.Time
	ExpiresAt      time.Time
	ConsumedAt     *time.Time
}

// Grant is a revocable OAuth session. Only hashes of access and refresh
// credentials are persisted.
type Grant struct {
	ID               string     `json:"id"`
	OrganizationID   string     `json:"organizationId"`
	ClientID         string     `json:"clientId"`
	ClientName       string     `json:"clientName,omitempty"`
	ActorID          string     `json:"actorId"`
	ResourceURI      string     `json:"resourceUri"`
	Scopes           []Scope    `json:"scopes"`
	AccessTokenHash  []byte     `json:"-"`
	RefreshTokenHash []byte     `json:"-"`
	AccessExpiresAt  time.Time  `json:"accessExpiresAt"`
	RefreshExpiresAt time.Time  `json:"refreshExpiresAt"`
	CreatedAt        time.Time  `json:"createdAt"`
	LastUsedAt       *time.Time `json:"lastUsedAt,omitempty"`
	RevokedAt        *time.Time `json:"revokedAt,omitempty"`
}

type TokenCredentials struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	Grant        Grant  `json:"-"`
}

type Confirmation struct {
	ID             string
	OrganizationID string
	ActorID        string
	Action         string
	ArgumentsHash  []byte
	TokenHash      []byte
	CreatedAt      time.Time
	ExpiresAt      time.Time
	ConsumedAt     *time.Time
}

type ConfirmationChallenge struct {
	ConfirmationToken string    `json:"confirmationToken"`
	Action            string    `json:"action"`
	Summary           string    `json:"summary"`
	ExpiresAt         time.Time `json:"expiresAt"`
}

type RateWindow struct {
	KeyHash     [sha256.Size]byte
	WindowStart time.Time
	Count       int
}

// Store defines the durable OAuth, confirmation, and abuse-control boundary.
// Exchange and rotation methods are atomic so codes and refresh credentials
// cannot be replayed under concurrency.
type Store interface {
	CreateClient(ctx context.Context, client Client) (Client, error)
	ListClients(ctx context.Context, organizationID string) ([]Client, error)
	GetClient(ctx context.Context, organizationID, clientID string) (Client, error)
	RevokeClient(ctx context.Context, organizationID, clientID string, revokedAt time.Time) (Client, error)

	CreateAuthorizationRequest(ctx context.Context, request AuthorizationRequest) error
	GetAuthorizationRequest(ctx context.Context, organizationID, requestID string) (AuthorizationRequest, error)
	DecideAuthorizationRequest(ctx context.Context, request AuthorizationRequest, code *AuthorizationCode) error
	ExchangeAuthorizationCode(ctx context.Context, organizationID string, codeHash []byte, clientID, redirectURI, resourceURI, codeChallenge string, now time.Time, grant Grant) (Grant, error)

	RotateRefreshToken(ctx context.Context, organizationID string, refreshHash []byte, clientID, resourceURI string, now time.Time, replacement Grant) (Grant, error)
	AuthenticateAccessToken(ctx context.Context, organizationID string, accessHash []byte, resourceURI string, now time.Time) (Grant, error)
	ListGrants(ctx context.Context, organizationID string) ([]Grant, error)
	RevokeGrant(ctx context.Context, organizationID, grantID string, revokedAt time.Time) (Grant, error)
	RevokeToken(ctx context.Context, organizationID string, tokenHash []byte, revokedAt time.Time) error

	CreateConfirmation(ctx context.Context, confirmation Confirmation) error
	ConsumeConfirmation(ctx context.Context, organizationID, actorID, action string, argumentsHash, tokenHash []byte, now time.Time) (Confirmation, error)

	AllowRate(ctx context.Context, organizationID string, keyHash [sha256.Size]byte, windowStart time.Time, limit int) (bool, error)
}
