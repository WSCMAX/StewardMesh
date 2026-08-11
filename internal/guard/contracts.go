// Package guard implements StewardMesh authentication and authorization.
// Requirements: REQ-PLATFORM-VALKEY-001, SEC-GUARD-001, SEC-HTTP-001.
package guard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	RequirementID     = "SEC-GUARD-001"
	HTTPRequirementID = "SEC-HTTP-001"
	FeatureID         = "authorization.security"
)

var (
	ErrNotFound                   = errors.New("guard record not found")
	ErrBootstrapComplete          = errors.New("administrator bootstrap is already complete")
	ErrBootstrapDenied            = errors.New("administrator bootstrap is not authorized")
	ErrInvalidCredential          = errors.New("invalid username or password")
	ErrInvalidInput               = errors.New("invalid guard input")
	ErrRateLimited                = errors.New("authentication attempt rate limited")
	ErrLoginProtectionUnavailable = errors.New("login protection is unavailable")
	ErrInvalidSession             = errors.New("invalid or expired session")
	ErrInvalidCSRF                = errors.New("invalid csrf token")
	ErrPermissionDenied           = errors.New("permission denied")
	ErrConflict                   = errors.New("guard record conflicts with existing data")
	ErrBuiltInRole                = errors.New("built-in roles cannot be changed")
	ErrManagedAssignment          = errors.New("provider-managed role assignment cannot be changed locally")
	ErrLastAdministrator          = errors.New("the last organization administrator cannot be removed")
	ErrResourceWriteLocked        = errors.New("resource is write-locked until ownership is claimed")
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
	PermissionGoalsWrite       Permission = "goals.write"
	PermissionStorageRead      Permission = "storage.read"
	PermissionStorageWrite     Permission = "storage.write"
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
		PermissionGoalsWrite,
		PermissionStorageRead,
		PermissionStorageWrite,
	}
}

// SupportedPermissions returns the stable permission catalog exposed to
// organization administrators when building custom roles.
func SupportedPermissions() []Permission {
	return []Permission{
		PermissionOrganizationRead,
		PermissionAssetsRead,
		PermissionAssetsWrite,
		PermissionDirectoryRead,
		PermissionDirectoryWrite,
		PermissionGoalsRead,
		PermissionGoalsWrite,
		PermissionStorageRead,
		PermissionStorageWrite,
		PermissionGuardManage,
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
	Source          string
}

const (
	BuiltInRoleSource = "builtin"
	LocalRoleSource   = "local"
)

func (r Role) Validate() error {
	name := strings.TrimSpace(r.Name)
	description := strings.TrimSpace(r.Description)
	if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.OrganizationID) == "" || name == "" {
		return errors.New("complete role identity and name are required")
	}
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > 120 ||
		!utf8.ValidString(description) || utf8.RuneCountInString(description) > 1000 {
		return errors.New("valid role name and description lengths are required")
	}
	if r.Source != BuiltInRoleSource && r.Source != LocalRoleSource {
		return errors.New("valid role source is required")
	}
	if len(r.Permissions) == 0 && len(r.PolicyBundleIDs) == 0 {
		return errors.New("at least one role permission or policy bundle is required")
	}
	return nil
}

type RoleAssignment struct {
	ID             string
	OrganizationID string
	AccountID      string
	RoleID         string
	Scope          Scope
	Source         string
	CreatedAt      time.Time
}

const LocalAssignmentSource = "local"

func (a RoleAssignment) Validate() error {
	if strings.TrimSpace(a.ID) == "" || strings.TrimSpace(a.OrganizationID) == "" ||
		strings.TrimSpace(a.AccountID) == "" || strings.TrimSpace(a.RoleID) == "" || a.CreatedAt.IsZero() {
		return errors.New("complete role assignment identity and creation time are required")
	}
	if a.Scope.OrganizationID != a.OrganizationID {
		return errors.New("role assignment scope must match the organization")
	}
	if err := a.Scope.Validate(); err != nil {
		return err
	}
	if a.Source != LocalAssignmentSource && !validExternalAssignmentSource(a.Source) {
		return errors.New("valid role assignment source is required")
	}
	return nil
}

func validExternalAssignmentSource(source string) bool {
	protocol, encoded, found := strings.Cut(source, ":")
	if !found || protocol != "oidc" && protocol != "saml" {
		return false
	}
	decoded, err := hex.DecodeString(encoded)
	return err == nil && len(decoded) == 16
}

type AuthorizationDirectory struct {
	Accounts             []Account
	Roles                []Role
	PolicyBundles        []PolicyBundle
	AvailablePermissions []Permission
	Assignments          []RoleAssignment
}

type CreateRoleInput struct {
	Name            string
	Description     string
	Permissions     []Permission
	PolicyBundleIDs []string
}

type RoleAssignmentInput struct {
	AccountID  string
	RoleID     string
	ScopeKind  ScopeKind
	ResourceID string
}

// ResourceOwnership preserves the source identity of an externally managed
// record. Imported records remain readable but write-locked until an
// organization administrator explicitly claims local ownership.
type ResourceOwnership struct {
	OrganizationID string
	ResourceType   string
	ResourceID     string
	SourceSystemID string
	SourceRecordID string
	WriteLocked    bool
	RegisteredAt   time.Time
	ClaimedBy      string
	ClaimedAt      *time.Time
}

func (o ResourceOwnership) Validate() error {
	if strings.TrimSpace(o.OrganizationID) == "" || strings.TrimSpace(o.ResourceType) == "" ||
		strings.TrimSpace(o.ResourceID) == "" || strings.TrimSpace(o.SourceSystemID) == "" ||
		strings.TrimSpace(o.SourceRecordID) == "" || o.RegisteredAt.IsZero() {
		return errors.New("complete resource ownership identity, source, and registration time are required")
	}
	if o.WriteLocked {
		if o.ClaimedBy != "" || o.ClaimedAt != nil {
			return errors.New("write-locked ownership cannot be claimed")
		}
		return nil
	}
	if strings.TrimSpace(o.ClaimedBy) == "" || o.ClaimedAt == nil || o.ClaimedAt.IsZero() || o.ClaimedAt.Before(o.RegisteredAt) {
		return errors.New("claimed ownership requires an actor and claim time")
	}
	return nil
}

type ResourceOwnershipInput struct {
	ResourceType   string
	ResourceID     string
	SourceSystemID string
	SourceRecordID string
}

type ExternalIdentity struct {
	OrganizationID string
	Issuer         string
	Subject        string
	AccountID      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type ExternalAccountProvisioning struct {
	Account                   Account
	Identity                  ExternalIdentity
	Administrator             bool
	AdministratorAssignmentID string
	AssignmentSource          string
}

func (p ExternalAccountProvisioning) Validate() error {
	account := p.Account
	identity := p.Identity
	if account.ID == "" || account.OrganizationID == "" || account.Username == "" ||
		account.NormalizedUsername == "" || account.Email == "" || account.DisplayName == "" ||
		account.PasswordHash != "" || account.Status != "active" || account.CreatedAt.IsZero() || account.UpdatedAt.IsZero() {
		return errors.New("complete external account is required")
	}
	if identity.OrganizationID != account.OrganizationID || identity.Issuer == "" || identity.Subject == "" ||
		identity.AccountID != account.ID || identity.CreatedAt.IsZero() || identity.UpdatedAt.IsZero() {
		return errors.New("complete external identity is required")
	}
	if !validExternalAssignmentSource(p.AssignmentSource) {
		return errors.New("valid external assignment source is required")
	}
	if p.Administrator && p.AdministratorAssignmentID == "" {
		return errors.New("administrator mapping identity is required")
	}
	return nil
}

// SAMLRequest binds a short-lived, opaque RelayState hash to one SP-initiated
// authentication request. Assertions and profile attributes are never stored.
type SAMLRequest struct {
	OrganizationID string
	StateHash      []byte
	RequestID      string
	CreatedAt      time.Time
	ExpiresAt      time.Time
}

func (r SAMLRequest) Validate() error {
	if strings.TrimSpace(r.OrganizationID) == "" || len(r.StateHash) != sha256.Size ||
		strings.TrimSpace(r.RequestID) == "" || len(r.RequestID) > 512 || r.CreatedAt.IsZero() ||
		r.ExpiresAt.IsZero() || !r.ExpiresAt.After(r.CreatedAt) {
		return errors.New("complete SAML request tracking data is required")
	}
	return nil
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
		b.Role.Source != BuiltInRoleSource || len(b.Role.Permissions) == 0 ||
		len(b.Role.PolicyBundleIDs) == 0 || b.Role.PolicyBundleIDs[0] != b.Bundle.ID {
		return errors.New("administrator role is required")
	}
	assignment := b.Assignment
	if assignment.OrganizationID != account.OrganizationID || assignment.AccountID != account.ID ||
		assignment.RoleID != b.Role.ID || assignment.Source != LocalAssignmentSource || assignment.Validate() != nil {
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
	ProvisionExternalAccount(ctx context.Context, provisioning ExternalAccountProvisioning) (account Account, created bool, err error)
	CreateSAMLRequest(ctx context.Context, request SAMLRequest) error
	ConsumeSAMLRequest(ctx context.Context, organizationID string, stateHash []byte, now time.Time) (SAMLRequest, error)
	AccessForAccount(ctx context.Context, organizationID, accountID string) (Access, error)
	ListAuthorization(ctx context.Context, organizationID string) (AuthorizationDirectory, error)
	CreateRole(ctx context.Context, role Role) error
	DeleteRole(ctx context.Context, organizationID, roleID string) error
	CreateRoleAssignment(ctx context.Context, assignment RoleAssignment) error
	DeleteRoleAssignment(ctx context.Context, organizationID, assignmentID string) (RoleAssignment, error)
	RegisterResourceOwnership(ctx context.Context, ownership ResourceOwnership) (ResourceOwnership, bool, error)
	ListResourceOwnership(ctx context.Context, organizationID string) ([]ResourceOwnership, error)
	GetResourceOwnership(ctx context.Context, organizationID, resourceType, resourceID string) (ResourceOwnership, error)
	ClaimResourceOwnership(ctx context.Context, organizationID, resourceType, resourceID, claimedBy string, claimedAt time.Time) (ResourceOwnership, error)
	DeleteResourceOwnership(ctx context.Context, ownership ResourceOwnership) error
	RestoreResourceOwnershipLock(ctx context.Context, claimed ResourceOwnership) error
	CreateSession(ctx context.Context, session Session) error
	FindSessionByTokenHash(ctx context.Context, organizationID string, tokenHash []byte, now time.Time) (Session, Access, error)
	UpdateSessionCSRF(ctx context.Context, sessionID string, csrfHash []byte) error
	RevokeSession(ctx context.Context, sessionID string, revokedAt time.Time) error
}

type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(password, encodedHash string) (matches bool, needsRehash bool, err error)
}
