// Package guard implements StewardMesh authentication and authorization.
// Requirements: SEC-GUARD-001, SEC-HTTP-001.
package guard

import (
	"context"
	"errors"
	"strings"
	"time"
)

const (
	RequirementID     = "SEC-GUARD-001"
	HTTPRequirementID = "SEC-HTTP-001"
	FeatureID         = "authorization.security"
)

var (
	ErrNotFound          = errors.New("guard record not found")
	ErrBootstrapComplete = errors.New("administrator bootstrap is already complete")
	ErrBootstrapDenied   = errors.New("administrator bootstrap is not authorized")
	ErrInvalidCredential = errors.New("invalid username or password")
	ErrInvalidInput      = errors.New("invalid guard input")
	ErrRateLimited       = errors.New("authentication attempt rate limited")
	ErrInvalidSession    = errors.New("invalid or expired session")
	ErrInvalidCSRF       = errors.New("invalid csrf token")
	ErrPermissionDenied  = errors.New("permission denied")
)

const MinimumPasswordCharacters = 15

type Permission string

const (
	PermissionOrganizationRead Permission = "organization.read"
	PermissionAssetsRead       Permission = "assets.read"
	PermissionAssetsWrite      Permission = "assets.write"
	PermissionDirectoryRead    Permission = "directory.read"
	PermissionDirectoryWrite   Permission = "directory.write"
	PermissionGoalsRead        Permission = "goals.read"
	PermissionGuardManage      Permission = "guard.manage"
)

func AdministratorBundlePermissions() []Permission {
	return []Permission{
		PermissionOrganizationRead,
		PermissionAssetsRead,
		PermissionAssetsWrite,
		PermissionDirectoryRead,
		PermissionDirectoryWrite,
		PermissionGoalsRead,
	}
}

type ScopeKind string

const (
	ScopeOrganization ScopeKind = "organization"
	ScopeSite         ScopeKind = "site"
	ScopeDepartment   ScopeKind = "department"
	ScopeResource     ScopeKind = "resource"
)

type Scope struct {
	Kind           ScopeKind
	OrganizationID string
	ResourceID     string
}

func (s Scope) Validate() error {
	if strings.TrimSpace(s.OrganizationID) == "" {
		return errors.New("organization scope is required")
	}
	switch s.Kind {
	case ScopeOrganization:
		if s.ResourceID != "" && s.ResourceID != s.OrganizationID {
			return errors.New("organization scope resource must match the organization")
		}
	case ScopeSite, ScopeDepartment, ScopeResource:
		if strings.TrimSpace(s.ResourceID) == "" {
			return errors.New("scoped resource id is required")
		}
	default:
		return errors.New("valid scope kind is required")
	}
	return nil
}

type Account struct {
	ID                 string
	OrganizationID     string
	Username           string
	NormalizedUsername string
	Email              string
	DisplayName        string
	PasswordHash       string
	Status             string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type PolicyBundle struct {
	ID             string
	OrganizationID string
	Name           string
	Description    string
	Permissions    []Permission
}

type Role struct {
	ID              string
	OrganizationID  string
	Name            string
	Description     string
	Permissions     []Permission
	PolicyBundleIDs []string
}

type RoleAssignment struct {
	ID             string
	OrganizationID string
	AccountID      string
	RoleID         string
	Scope          Scope
	CreatedAt      time.Time
}

type AdministratorBootstrap struct {
	Account    Account
	Bundle     PolicyBundle
	Role       Role
	Assignment RoleAssignment
}

func (b AdministratorBootstrap) Validate() error {
	account := b.Account
	if account.ID == "" || account.OrganizationID == "" || account.Username == "" ||
		account.NormalizedUsername == "" || account.Email == "" || account.DisplayName == "" ||
		account.PasswordHash == "" || account.Status != "active" || account.CreatedAt.IsZero() || account.UpdatedAt.IsZero() {
		return errors.New("complete administrator account is required")
	}
	if b.Bundle.ID == "" || b.Bundle.OrganizationID != account.OrganizationID || len(b.Bundle.Permissions) == 0 {
		return errors.New("administrator policy bundle is required")
	}
	if b.Role.ID == "" || b.Role.OrganizationID != account.OrganizationID ||
		len(b.Role.Permissions) == 0 || len(b.Role.PolicyBundleIDs) == 0 || b.Role.PolicyBundleIDs[0] != b.Bundle.ID {
		return errors.New("administrator role is required")
	}
	assignment := b.Assignment
	if assignment.ID == "" || assignment.OrganizationID != account.OrganizationID || assignment.AccountID != account.ID ||
		assignment.RoleID != b.Role.ID || assignment.CreatedAt.IsZero() || assignment.Scope.Validate() != nil {
		return errors.New("administrator role assignment is required")
	}
	return nil
}

type Grant struct {
	Permission Permission
	Scope      Scope
}

type Access struct {
	Account Account
	Roles   []Role
	Grants  []Grant
}

type Session struct {
	ID             string
	OrganizationID string
	AccountID      string
	TokenHash      []byte
	CSRFHash       []byte
	CreatedAt      time.Time
	ExpiresAt      time.Time
	RevokedAt      *time.Time
}

type Principal struct {
	Subject        string   `json:"subject"`
	OrganizationID string   `json:"organizationId"`
	Username       string   `json:"username"`
	Email          string   `json:"email"`
	DisplayName    string   `json:"displayName"`
	Roles          []string `json:"roles"`
}

type Authentication struct {
	Session   Session
	Principal Principal
	Grants    []Grant
}

type SessionCredentials struct {
	Authentication Authentication
	Token          string
	CSRFToken      string
}

type BootstrapInput struct {
	Username       string
	Email          string
	DisplayName    string
	Password       string
	BootstrapToken string
}

type LoginInput struct {
	Username string
	Password string
	RateKey  string
}

// Store is the provider-neutral Guard persistence contract. PostgreSQL is the
// first durable implementation; a DynamoDB adapter must conform to the same
// behavior without changing domain services.
type Store interface {
	BootstrapRequired(ctx context.Context, organizationID string) (bool, error)
	BootstrapAdministrator(ctx context.Context, bootstrap AdministratorBootstrap) (Account, error)
	FindAccountByUsername(ctx context.Context, organizationID, normalizedUsername string) (Account, error)
	UpdatePasswordHash(ctx context.Context, accountID, passwordHash string, updatedAt time.Time) error
	AccessForAccount(ctx context.Context, organizationID, accountID string) (Access, error)
	CreateSession(ctx context.Context, session Session) error
	FindSessionByTokenHash(ctx context.Context, organizationID string, tokenHash []byte, now time.Time) (Session, Access, error)
	UpdateSessionCSRF(ctx context.Context, sessionID string, csrfHash []byte) error
	RevokeSession(ctx context.Context, sessionID string, revokedAt time.Time) error
}

type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(password, encodedHash string) (matches bool, needsRehash bool, err error)
}
