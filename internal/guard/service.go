package guard

// Requirements: REQ-PLATFORM-VALKEY-001, SEC-GUARD-001, SEC-HTTP-001.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/identity"
)

const (
	maximumPasswordBytes = 1024
	defaultSessionTTL    = 12 * time.Hour
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,63}$`)
var guardIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var guardResourceIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
var resourceTypePattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)

type ServiceConfig struct {
	OrganizationID string
	BootstrapToken string
	SessionTTL     time.Duration
	Now            func() time.Time
}

type Service struct {
	store                  Store
	hasher                 PasswordHasher
	auditor                foundation.Auditor
	limiter                AttemptLimiter
	organizationID         string
	bootstrapTokenHash     [sha256.Size]byte
	bootstrapTokenRequired bool
	sessionTTL             time.Duration
	now                    func() time.Time
	dummyPasswordHash      string
}

func NewService(store Store, hasher PasswordHasher, auditor foundation.Auditor, limiter AttemptLimiter, configuration ServiceConfig) (*Service, error) {
	if store == nil || hasher == nil || auditor == nil {
		return nil, errors.New("guard store, password hasher, and auditor are required")
	}
	configuration.OrganizationID = strings.TrimSpace(configuration.OrganizationID)
	if configuration.OrganizationID == "" {
		return nil, errors.New("guard organization id is required")
	}
	if configuration.SessionTTL == 0 {
		configuration.SessionTTL = defaultSessionTTL
	}
	if configuration.SessionTTL < 15*time.Minute || configuration.SessionTTL > 24*time.Hour {
		return nil, errors.New("session ttl must be between 15 minutes and 24 hours")
	}
	if configuration.Now == nil {
		configuration.Now = func() time.Time { return time.Now().UTC() }
	}
	if configuration.BootstrapToken != "" && len(configuration.BootstrapToken) < 32 {
		return nil, errors.New("bootstrap token must contain at least 32 bytes")
	}
	if limiter == nil {
		var err error
		limiter, err = NewMemoryAttemptLimiter(5, 15*time.Minute)
		if err != nil {
			return nil, err
		}
	}
	dummyPassword, _, err := newSecret()
	if err != nil {
		return nil, err
	}
	dummyPasswordHash, err := hasher.Hash(dummyPassword)
	if err != nil {
		return nil, fmt.Errorf("initialize credential comparison: %w", err)
	}
	service := &Service{
		store:                  store,
		hasher:                 hasher,
		auditor:                auditor,
		limiter:                limiter,
		organizationID:         configuration.OrganizationID,
		bootstrapTokenRequired: configuration.BootstrapToken != "",
		sessionTTL:             configuration.SessionTTL,
		now:                    configuration.Now,
		dummyPasswordHash:      dummyPasswordHash,
	}
	if service.bootstrapTokenRequired {
		service.bootstrapTokenHash = sha256.Sum256([]byte(configuration.BootstrapToken))
	}
	return service, nil
}

func (s *Service) BootstrapStatus(ctx context.Context) (required, tokenRequired bool, err error) {
	required, err = s.store.BootstrapRequired(ctx, s.organizationID)
	return required, s.bootstrapTokenRequired, err
}

func (s *Service) Bootstrap(ctx context.Context, input BootstrapInput, trustedRequest bool) (SessionCredentials, error) {
	if !s.bootstrapAuthorized(input.BootstrapToken, trustedRequest) {
		s.recordSecurityEvent(ctx, "anonymous", "guard.bootstrap.denied", "organization", s.organizationID, map[string]string{"reason": "access"})
		return SessionCredentials{}, ErrBootstrapDenied
	}
	required, err := s.store.BootstrapRequired(ctx, s.organizationID)
	if err != nil {
		return SessionCredentials{}, fmt.Errorf("read bootstrap state: %w", err)
	}
	if !required {
		return SessionCredentials{}, ErrBootstrapComplete
	}
	username, normalizedUsername, email, displayName, err := validateAccountInput(input)
	if err != nil {
		return SessionCredentials{}, err
	}
	if err := validatePassword(input.Password); err != nil {
		return SessionCredentials{}, err
	}
	passwordHash, err := s.hasher.Hash(input.Password)
	if err != nil {
		return SessionCredentials{}, fmt.Errorf("hash administrator password: %w", err)
	}
	now := s.now()
	accountID, err := newID()
	if err != nil {
		return SessionCredentials{}, err
	}
	bundleID, err := newID()
	if err != nil {
		return SessionCredentials{}, err
	}
	roleID, err := newID()
	if err != nil {
		return SessionCredentials{}, err
	}
	assignmentID, err := newID()
	if err != nil {
		return SessionCredentials{}, err
	}
	account, err := s.store.BootstrapAdministrator(ctx, AdministratorBootstrap{
		Account: Account{
			ID:                 accountID,
			OrganizationID:     s.organizationID,
			Username:           username,
			NormalizedUsername: normalizedUsername,
			Email:              email,
			DisplayName:        displayName,
			PasswordHash:       passwordHash,
			Status:             "active",
			CreatedAt:          now,
			UpdatedAt:          now,
		},
		Bundle: PolicyBundle{
			ID:             bundleID,
			OrganizationID: s.organizationID,
			Name:           "Core administration",
			Description:    "Common organization, inventory, directory, goals, and Vault permissions.",
			Permissions:    AdministratorBundlePermissions(),
		},
		Role: Role{
			ID:              roleID,
			OrganizationID:  s.organizationID,
			Name:            "Administrator",
			Description:     "Full organization administrator for the initial StewardMesh deployment.",
			Permissions:     []Permission{PermissionGuardManage},
			PolicyBundleIDs: []string{bundleID},
			Source:          BuiltInRoleSource,
		},
		Assignment: RoleAssignment{
			ID:             assignmentID,
			OrganizationID: s.organizationID,
			AccountID:      accountID,
			RoleID:         roleID,
			Scope: Scope{
				Kind:           ScopeOrganization,
				OrganizationID: s.organizationID,
				ResourceID:     s.organizationID,
			},
			Source:    LocalAssignmentSource,
			CreatedAt: now,
		},
	})
	if err != nil {
		return SessionCredentials{}, err
	}
	credentials, err := s.issueSession(ctx, account.ID)
	if err != nil {
		return SessionCredentials{}, err
	}
	if err := s.audit(ctx, account.ID, "guard.bootstrap.created", "account", account.ID, map[string]string{"role": "Administrator"}); err != nil {
		_ = s.store.RevokeSession(ctx, credentials.Authentication.Session.ID, s.now())
		return SessionCredentials{}, fmt.Errorf("audit administrator bootstrap: %w", err)
	}
	return credentials, nil
}

func (s *Service) Login(ctx context.Context, input LoginInput) (SessionCredentials, error) {
	normalizedUsername := normalizeUsername(input.Username)
	clientRateKey := "client|" + strings.TrimSpace(input.RateKey)
	accountRateKey := "account|" + normalizedUsername
	now := s.now()
	clientAllowed, clientLimitErr := s.limiter.Allow(ctx, clientRateKey, now)
	accountAllowed, accountLimitErr := s.limiter.Allow(ctx, accountRateKey, now)
	if limitErr := errors.Join(clientLimitErr, accountLimitErr); limitErr != nil {
		s.recordSecurityEvent(ctx, "anonymous", "guard.login.protection_unavailable", "account", "unknown", nil)
		return SessionCredentials{}, fmt.Errorf("%w: check failure counters: %w", ErrLoginProtectionUnavailable, limitErr)
	}
	if !clientAllowed || !accountAllowed {
		s.recordSecurityEvent(ctx, "anonymous", "guard.login.rate_limited", "account", "unknown", nil)
		return SessionCredentials{}, ErrRateLimited
	}
	account, err := s.store.FindAccountByUsername(ctx, s.organizationID, normalizedUsername)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return SessionCredentials{}, fmt.Errorf("load account: %w", err)
	}
	encodedHash := s.dummyPasswordHash
	if err == nil && account.PasswordHash != "" {
		encodedHash = account.PasswordHash
	}
	matches, needsRehash, verifyErr := s.hasher.Verify(input.Password, encodedHash)
	if verifyErr != nil {
		return SessionCredentials{}, fmt.Errorf("verify stored credential: %w", verifyErr)
	}
	if err != nil || account.PasswordHash == "" || !matches || account.Status != "active" || !usernamePattern.MatchString(normalizedUsername) {
		failureErr := errors.Join(
			s.limiter.Failure(ctx, clientRateKey, now),
			s.limiter.Failure(ctx, accountRateKey, now),
		)
		resourceID := "unknown"
		if account.ID != "" {
			resourceID = account.ID
		}
		s.recordSecurityEvent(ctx, "anonymous", "guard.login.failed", "account", resourceID, nil)
		if failureErr != nil {
			s.recordSecurityEvent(ctx, "anonymous", "guard.login.protection_unavailable", "account", "unknown", nil)
			return SessionCredentials{}, fmt.Errorf("%w: record failure counters: %w", ErrLoginProtectionUnavailable, failureErr)
		}
		return SessionCredentials{}, ErrInvalidCredential
	}
	if needsRehash {
		passwordHash, hashErr := s.hasher.Hash(input.Password)
		if hashErr != nil {
			return SessionCredentials{}, fmt.Errorf("refresh password hash: %w", hashErr)
		}
		if updateErr := s.store.UpdatePasswordHash(ctx, account.ID, passwordHash, now); updateErr != nil {
			return SessionCredentials{}, fmt.Errorf("store refreshed password hash: %w", updateErr)
		}
	}
	if resetErr := errors.Join(
		s.limiter.Reset(ctx, clientRateKey),
		s.limiter.Reset(ctx, accountRateKey),
	); resetErr != nil {
		s.recordSecurityEvent(ctx, "anonymous", "guard.login.protection_unavailable", "account", account.ID, nil)
		return SessionCredentials{}, fmt.Errorf("%w: reset failure counters: %w", ErrLoginProtectionUnavailable, resetErr)
	}
	credentials, err := s.issueSession(ctx, account.ID)
	if err != nil {
		return SessionCredentials{}, err
	}
	if err := s.audit(ctx, account.ID, "guard.login.succeeded", "account", account.ID, nil); err != nil {
		_ = s.store.RevokeSession(ctx, credentials.Authentication.Session.ID, s.now())
		return SessionCredentials{}, fmt.Errorf("audit login: %w", err)
	}
	return credentials, nil
}

func (s *Service) LoginOIDC(ctx context.Context, principal identity.OIDCPrincipal) (SessionCredentials, error) {
	return s.loginExternal(ctx, "oidc", externalPrincipal{
		Issuer: principal.Issuer, Subject: principal.Subject, Email: principal.Email,
		DisplayName: principal.DisplayName, Administrator: principal.Administrator,
	})
}

func (s *Service) LoginSAML(ctx context.Context, principal identity.SAMLPrincipal) (SessionCredentials, error) {
	return s.loginExternal(ctx, "saml", externalPrincipal{
		Issuer: principal.Issuer, Subject: principal.Subject, Email: principal.Email,
		DisplayName: principal.DisplayName, Administrator: principal.Administrator,
	})
}

type externalPrincipal struct {
	Issuer        string
	Subject       string
	Email         string
	DisplayName   string
	Administrator bool
}

func (s *Service) loginExternal(ctx context.Context, protocol string, principal externalPrincipal) (SessionCredentials, error) {
	issuer := strings.TrimSpace(principal.Issuer)
	subject := strings.TrimSpace(principal.Subject)
	email := strings.ToLower(strings.TrimSpace(principal.Email))
	displayName := strings.TrimSpace(principal.DisplayName)
	failureEvent := "guard." + protocol + ".login.failed"
	address, emailErr := mail.ParseAddress(email)
	if protocol != "oidc" && protocol != "saml" || issuer == "" || len(issuer) > 2048 || subject == "" || len(subject) > 512 ||
		emailErr != nil || address.Address != email || len(email) > 320 || displayName == "" ||
		utf8.RuneCountInString(displayName) > 200 {
		s.recordSecurityEvent(ctx, "anonymous", failureEvent, "external_identity", "unknown", nil)
		return SessionCredentials{}, ErrInvalidCredential
	}
	now := s.now()
	accountID, err := newID()
	if err != nil {
		return SessionCredentials{}, err
	}
	assignmentID, err := newID()
	if err != nil {
		return SessionCredentials{}, err
	}
	username := externalUsername(protocol, issuer, subject)
	account, created, err := s.store.ProvisionExternalAccount(ctx, ExternalAccountProvisioning{
		Account: Account{
			ID:                 accountID,
			OrganizationID:     s.organizationID,
			Username:           username,
			NormalizedUsername: username,
			Email:              email,
			DisplayName:        displayName,
			Status:             "active",
			CreatedAt:          now,
			UpdatedAt:          now,
		},
		Identity: ExternalIdentity{
			OrganizationID: s.organizationID,
			Issuer:         issuer,
			Subject:        subject,
			AccountID:      accountID,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
		Administrator:             principal.Administrator,
		AdministratorAssignmentID: assignmentID,
		AssignmentSource:          externalAssignmentSource(protocol, issuer),
	})
	if err != nil {
		s.recordSecurityEvent(ctx, "anonymous", failureEvent, "external_identity", "unknown", nil)
		return SessionCredentials{}, fmt.Errorf("provision %s account: %w", protocol, err)
	}
	if account.Status != "active" {
		s.recordSecurityEvent(ctx, account.ID, failureEvent, "account", account.ID, map[string]string{"reason": "disabled"})
		return SessionCredentials{}, ErrInvalidCredential
	}
	credentials, err := s.issueSession(ctx, account.ID)
	if err != nil {
		return SessionCredentials{}, err
	}
	metadata := map[string]string{
		"created":             fmt.Sprintf("%t", created),
		"administratorMapped": fmt.Sprintf("%t", principal.Administrator),
	}
	if err := s.audit(ctx, account.ID, "guard."+protocol+".login.succeeded", "account", account.ID, metadata); err != nil {
		_ = s.store.RevokeSession(ctx, credentials.Authentication.Session.ID, s.now())
		return SessionCredentials{}, fmt.Errorf("audit %s login: %w", protocol, err)
	}
	return credentials, nil
}

func (s *Service) RecordOIDCFailure(ctx context.Context) {
	s.recordSecurityEvent(ctx, "anonymous", "guard.oidc.login.failed", "external_identity", "unknown", nil)
}

func (s *Service) TrackSAMLRequest(ctx context.Context, relayState, requestID string, expiresAt time.Time) error {
	stateHash, err := hashSecret(relayState)
	if err != nil || strings.TrimSpace(requestID) == "" {
		return ErrInvalidCredential
	}
	now := s.now()
	if !expiresAt.After(now) || expiresAt.After(now.Add(15*time.Minute)) {
		return ErrInvalidCredential
	}
	if err := s.store.CreateSAMLRequest(ctx, SAMLRequest{
		OrganizationID: s.organizationID,
		StateHash:      stateHash,
		RequestID:      requestID,
		CreatedAt:      now,
		ExpiresAt:      expiresAt,
	}); err != nil {
		return fmt.Errorf("track SAML authentication request: %w", err)
	}
	return nil
}

func (s *Service) ConsumeSAMLRequest(ctx context.Context, relayState string) (string, error) {
	stateHash, err := hashSecret(relayState)
	if err != nil {
		return "", ErrInvalidCredential
	}
	request, err := s.store.ConsumeSAMLRequest(ctx, s.organizationID, stateHash, s.now())
	if errors.Is(err, ErrNotFound) {
		return "", ErrInvalidCredential
	}
	if err != nil {
		return "", fmt.Errorf("consume SAML authentication request: %w", err)
	}
	return request.RequestID, nil
}

func (s *Service) RecordSAMLFailure(ctx context.Context) {
	s.recordSecurityEvent(ctx, "anonymous", "guard.saml.login.failed", "external_identity", "unknown", nil)
}

func (s *Service) AuthenticateSession(ctx context.Context, rawToken string) (Authentication, error) {
	tokenHash, err := hashSecret(rawToken)
	if err != nil {
		return Authentication{}, ErrInvalidSession
	}
	session, access, err := s.store.FindSessionByTokenHash(ctx, s.organizationID, tokenHash, s.now())
	if err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrInvalidSession) {
			return Authentication{}, ErrInvalidSession
		}
		return Authentication{}, fmt.Errorf("load session: %w", err)
	}
	if access.Account.Status != "active" {
		return Authentication{}, ErrInvalidSession
	}
	return authenticationFrom(session, access), nil
}

func (s *Service) RefreshCSRF(ctx context.Context, authentication *Authentication) (string, error) {
	if authentication == nil || authentication.Session.ID == "" {
		return "", ErrInvalidSession
	}
	token, hash, err := newSecret()
	if err != nil {
		return "", err
	}
	if err := s.store.UpdateSessionCSRF(ctx, authentication.Session.ID, hash); err != nil {
		return "", fmt.Errorf("rotate csrf token: %w", err)
	}
	authentication.Session.CSRFHash = append([]byte(nil), hash...)
	return token, nil
}

func (s *Service) ValidateCSRF(authentication Authentication, rawToken string) error {
	actual, err := hashSecret(rawToken)
	if err != nil || len(authentication.Session.CSRFHash) != sha256.Size ||
		subtle.ConstantTimeCompare(actual, authentication.Session.CSRFHash) != 1 {
		return ErrInvalidCSRF
	}
	return nil
}

func (s *Service) Logout(ctx context.Context, authentication Authentication) error {
	if authentication.Session.ID == "" {
		return ErrInvalidSession
	}
	if err := s.store.RevokeSession(ctx, authentication.Session.ID, s.now()); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return s.audit(ctx, authentication.Principal.Subject, "guard.logout.succeeded", "session", authentication.Session.ID, nil)
}

func (s *Service) CheckPermission(ctx context.Context, authentication Authentication, permission Permission, target Scope) error {
	if err := target.Validate(); err != nil {
		return ErrPermissionDenied
	}
	for _, grant := range authentication.Grants {
		if grant.Permission != permission || grant.Scope.OrganizationID != target.OrganizationID {
			continue
		}
		if grant.Scope.Kind == ScopeOrganization ||
			(grant.Scope.Kind == target.Kind && grant.Scope.ResourceID == target.ResourceID) {
			return nil
		}
	}
	s.recordSecurityEvent(
		ctx,
		authentication.Principal.Subject,
		"guard.authorization.denied",
		string(target.Kind),
		target.ResourceID,
		map[string]string{"permission": string(permission)},
	)
	return ErrPermissionDenied
}

func (s *Service) ListAuthorization(ctx context.Context, authentication Authentication) (AuthorizationDirectory, error) {
	if err := s.requireOrganizationManager(ctx, authentication); err != nil {
		return AuthorizationDirectory{}, err
	}
	directory, err := s.store.ListAuthorization(ctx, s.organizationID)
	if err != nil {
		return AuthorizationDirectory{}, fmt.Errorf("list Guard authorization: %w", err)
	}
	directory.AvailablePermissions = SupportedPermissions()
	return directory, nil
}

func (s *Service) CreateRole(ctx context.Context, authentication Authentication, input CreateRoleInput) (Role, error) {
	if err := s.requireOrganizationManager(ctx, authentication); err != nil {
		return Role{}, err
	}
	name := strings.TrimSpace(input.Name)
	description := strings.TrimSpace(input.Description)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 120 ||
		!utf8.ValidString(description) || utf8.RuneCountInString(description) > 1000 {
		return Role{}, fmt.Errorf("%w: role name must contain 1 to 120 characters and description at most 1000", ErrInvalidInput)
	}
	permissions, err := normalizeRolePermissions(input.Permissions)
	if err != nil {
		return Role{}, err
	}
	bundleIDs, err := normalizeRoleBundleIDs(input.PolicyBundleIDs)
	if err != nil {
		return Role{}, err
	}
	if len(permissions) == 0 && len(bundleIDs) == 0 {
		return Role{}, fmt.Errorf("%w: select at least one permission or policy bundle", ErrInvalidInput)
	}
	roleID, err := newID()
	if err != nil {
		return Role{}, err
	}
	role := Role{
		ID: roleID, OrganizationID: s.organizationID, Name: name, Description: description,
		Permissions: permissions, PolicyBundleIDs: bundleIDs, Source: LocalRoleSource,
	}
	if err := s.store.CreateRole(ctx, role); err != nil {
		return Role{}, fmt.Errorf("create role: %w", err)
	}
	metadata := map[string]string{
		"source": string(LocalRoleSource), "permissionCount": fmt.Sprintf("%d", len(permissions)),
		"policyBundleCount": fmt.Sprintf("%d", len(bundleIDs)),
	}
	if err := s.audit(ctx, authentication.Principal.Subject, "guard.role.created", "role", role.ID, metadata); err != nil {
		rollbackErr := s.store.DeleteRole(ctx, s.organizationID, role.ID)
		return Role{}, fmt.Errorf("audit role creation: %w", errors.Join(err, rollbackErr))
	}
	return role, nil
}

func normalizeRolePermissions(values []Permission) ([]Permission, error) {
	supported := make(map[Permission]struct{})
	for _, permission := range SupportedPermissions() {
		supported[permission] = struct{}{}
	}
	seen := make(map[Permission]struct{})
	result := make([]Permission, 0, len(values))
	for _, permission := range values {
		if _, ok := supported[permission]; !ok {
			return nil, fmt.Errorf("%w: unsupported role permission", ErrInvalidInput)
		}
		if _, duplicate := seen[permission]; duplicate {
			continue
		}
		seen[permission] = struct{}{}
		result = append(result, permission)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result, nil
}

func normalizeRoleBundleIDs(values []string) ([]string, error) {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if !guardIDPattern.MatchString(value) {
			return nil, fmt.Errorf("%w: valid policy bundle ids are required", ErrInvalidInput)
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}

func (s *Service) AssignRole(ctx context.Context, authentication Authentication, input RoleAssignmentInput) (RoleAssignment, error) {
	if err := s.requireOrganizationManager(ctx, authentication); err != nil {
		return RoleAssignment{}, err
	}
	accountID := strings.TrimSpace(input.AccountID)
	roleID := strings.TrimSpace(input.RoleID)
	resourceID := strings.TrimSpace(input.ResourceID)
	if !guardIDPattern.MatchString(accountID) || !guardIDPattern.MatchString(roleID) {
		return RoleAssignment{}, fmt.Errorf("%w: valid account and role ids are required", ErrInvalidInput)
	}
	scope := Scope{Kind: input.ScopeKind, OrganizationID: s.organizationID, ResourceID: resourceID}
	if scope.Kind == ScopeOrganization {
		scope.ResourceID = s.organizationID
	}
	if (scope.Kind != ScopeOrganization && !guardResourceIDPattern.MatchString(scope.ResourceID)) || scope.Validate() != nil {
		return RoleAssignment{}, fmt.Errorf("%w: valid organization, site, department, or resource scope is required", ErrInvalidInput)
	}
	assignmentID, err := newID()
	if err != nil {
		return RoleAssignment{}, err
	}
	assignment := RoleAssignment{
		ID:             assignmentID,
		OrganizationID: s.organizationID,
		AccountID:      accountID,
		RoleID:         roleID,
		Scope:          scope,
		Source:         LocalAssignmentSource,
		CreatedAt:      s.now(),
	}
	if err := s.store.CreateRoleAssignment(ctx, assignment); err != nil {
		return RoleAssignment{}, fmt.Errorf("create role assignment: %w", err)
	}
	metadata := map[string]string{
		"accountId": accountID,
		"roleId":    roleID,
		"scopeKind": string(scope.Kind),
		"scopeId":   scope.ResourceID,
	}
	if err := s.audit(ctx, authentication.Principal.Subject, "guard.role_assignment.created", "role_assignment", assignment.ID, metadata); err != nil {
		_, rollbackErr := s.store.DeleteRoleAssignment(ctx, s.organizationID, assignment.ID)
		return RoleAssignment{}, fmt.Errorf("audit role assignment creation: %w", errors.Join(err, rollbackErr))
	}
	return assignment, nil
}

func (s *Service) RevokeRoleAssignment(ctx context.Context, authentication Authentication, assignmentID string) (RoleAssignment, error) {
	if err := s.requireOrganizationManager(ctx, authentication); err != nil {
		return RoleAssignment{}, err
	}
	assignmentID = strings.TrimSpace(assignmentID)
	if !guardIDPattern.MatchString(assignmentID) {
		return RoleAssignment{}, fmt.Errorf("%w: valid role assignment id is required", ErrInvalidInput)
	}
	assignment, err := s.store.DeleteRoleAssignment(ctx, s.organizationID, assignmentID)
	if err != nil {
		return RoleAssignment{}, fmt.Errorf("delete role assignment: %w", err)
	}
	metadata := map[string]string{
		"accountId": assignment.AccountID,
		"roleId":    assignment.RoleID,
		"scopeKind": string(assignment.Scope.Kind),
		"scopeId":   assignment.Scope.ResourceID,
	}
	if err := s.audit(ctx, authentication.Principal.Subject, "guard.role_assignment.revoked", "role_assignment", assignment.ID, metadata); err != nil {
		rollbackErr := s.store.CreateRoleAssignment(ctx, assignment)
		return RoleAssignment{}, fmt.Errorf("audit role assignment revocation: %w", errors.Join(err, rollbackErr))
	}
	return assignment, nil
}

// RegisterResourceOwnership records externally sourced provenance and applies
// a write lock. Re-registering the same source identity is idempotent and never
// re-locks a resource after local ownership has been claimed.
func (s *Service) RegisterResourceOwnership(ctx context.Context, authentication Authentication, input ResourceOwnershipInput) (ResourceOwnership, bool, error) {
	if err := s.requireOrganizationManager(ctx, authentication); err != nil {
		return ResourceOwnership{}, false, err
	}
	resourceType, resourceID, sourceSystemID, sourceRecordID, err := validateOwnershipInput(input)
	if err != nil {
		return ResourceOwnership{}, false, err
	}
	ownership := ResourceOwnership{
		OrganizationID: s.organizationID,
		ResourceType:   resourceType,
		ResourceID:     resourceID,
		SourceSystemID: sourceSystemID,
		SourceRecordID: sourceRecordID,
		WriteLocked:    true,
		RegisteredAt:   s.now(),
	}
	registered, created, err := s.store.RegisterResourceOwnership(ctx, ownership)
	if err != nil {
		return ResourceOwnership{}, false, fmt.Errorf("register resource ownership: %w", err)
	}
	if !created {
		return registered, false, nil
	}
	metadata := map[string]string{
		"resourceType":   registered.ResourceType,
		"sourceSystemId": registered.SourceSystemID,
	}
	if err := s.audit(ctx, authentication.Principal.Subject, "guard.ownership.locked", registered.ResourceType, registered.ResourceID, metadata); err != nil {
		rollbackErr := s.store.DeleteResourceOwnership(ctx, registered)
		return ResourceOwnership{}, false, fmt.Errorf("audit resource ownership registration: %w", errors.Join(err, rollbackErr))
	}
	return registered, true, nil
}

func (s *Service) ListResourceOwnership(ctx context.Context, authentication Authentication) ([]ResourceOwnership, error) {
	if err := s.requireOrganizationManager(ctx, authentication); err != nil {
		return nil, err
	}
	ownership, err := s.store.ListResourceOwnership(ctx, s.organizationID)
	if err != nil {
		return nil, fmt.Errorf("list resource ownership: %w", err)
	}
	return ownership, nil
}

func (s *Service) ClaimResourceOwnership(ctx context.Context, authentication Authentication, resourceType, resourceID string) (ResourceOwnership, error) {
	if err := s.requireOrganizationManager(ctx, authentication); err != nil {
		return ResourceOwnership{}, err
	}
	resourceType = strings.TrimSpace(resourceType)
	resourceID = strings.TrimSpace(resourceID)
	if !resourceTypePattern.MatchString(resourceType) || !guardResourceIDPattern.MatchString(resourceID) {
		return ResourceOwnership{}, fmt.Errorf("%w: valid resource type and id are required", ErrInvalidInput)
	}
	claimed, err := s.store.ClaimResourceOwnership(
		ctx, s.organizationID, resourceType, resourceID, authentication.Principal.Subject, s.now(),
	)
	if err != nil {
		return ResourceOwnership{}, fmt.Errorf("claim resource ownership: %w", err)
	}
	metadata := map[string]string{
		"resourceType":   claimed.ResourceType,
		"sourceSystemId": claimed.SourceSystemID,
	}
	if err := s.audit(ctx, authentication.Principal.Subject, "guard.ownership.claimed", claimed.ResourceType, claimed.ResourceID, metadata); err != nil {
		rollbackErr := s.store.RestoreResourceOwnershipLock(ctx, claimed)
		return ResourceOwnership{}, fmt.Errorf("audit resource ownership claim: %w", errors.Join(err, rollbackErr))
	}
	return claimed, nil
}

// CheckResourceWrite allows locally owned and unknown records, but denies
// mutation when Guard has an active external ownership lock for the resource.
func (s *Service) CheckResourceWrite(ctx context.Context, authentication Authentication, resourceType, resourceID string) error {
	resourceType = strings.TrimSpace(resourceType)
	resourceID = strings.TrimSpace(resourceID)
	if !resourceTypePattern.MatchString(resourceType) || !guardResourceIDPattern.MatchString(resourceID) {
		return fmt.Errorf("%w: valid resource type and id are required", ErrInvalidInput)
	}
	ownership, err := s.store.GetResourceOwnership(ctx, s.organizationID, resourceType, resourceID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read resource ownership: %w", err)
	}
	if !ownership.WriteLocked {
		return nil
	}
	auditErr := s.audit(
		ctx,
		authentication.Principal.Subject,
		"guard.ownership.write_denied",
		ownership.ResourceType,
		ownership.ResourceID,
		map[string]string{"sourceSystemId": ownership.SourceSystemID},
	)
	if auditErr != nil {
		return fmt.Errorf("%w: audit write denial: %v", ErrResourceWriteLocked, auditErr)
	}
	return ErrResourceWriteLocked
}

func (s *Service) requireOrganizationManager(ctx context.Context, authentication Authentication) error {
	return s.CheckPermission(ctx, authentication, PermissionGuardManage, Scope{
		Kind:           ScopeOrganization,
		OrganizationID: s.organizationID,
		ResourceID:     s.organizationID,
	})
}

func (s *Service) BootstrapTokenRequired() bool {
	return s.bootstrapTokenRequired
}

func (s *Service) bootstrapAuthorized(rawToken string, trustedRequest bool) bool {
	if !s.bootstrapTokenRequired {
		return trustedRequest
	}
	actual := sha256.Sum256([]byte(rawToken))
	return subtle.ConstantTimeCompare(actual[:], s.bootstrapTokenHash[:]) == 1
}

func (s *Service) issueSession(ctx context.Context, accountID string) (SessionCredentials, error) {
	access, err := s.store.AccessForAccount(ctx, s.organizationID, accountID)
	if err != nil {
		return SessionCredentials{}, fmt.Errorf("load account access: %w", err)
	}
	token, tokenHash, err := newSecret()
	if err != nil {
		return SessionCredentials{}, err
	}
	csrfToken, csrfHash, err := newSecret()
	if err != nil {
		return SessionCredentials{}, err
	}
	sessionID, err := newID()
	if err != nil {
		return SessionCredentials{}, err
	}
	now := s.now()
	session := Session{
		ID:             sessionID,
		OrganizationID: s.organizationID,
		AccountID:      accountID,
		TokenHash:      tokenHash,
		CSRFHash:       csrfHash,
		CreatedAt:      now,
		ExpiresAt:      now.Add(s.sessionTTL),
	}
	if err := s.store.CreateSession(ctx, session); err != nil {
		return SessionCredentials{}, fmt.Errorf("create session: %w", err)
	}
	return SessionCredentials{
		Authentication: authenticationFrom(session, access),
		Token:          token,
		CSRFToken:      csrfToken,
	}, nil
}

func authenticationFrom(session Session, access Access) Authentication {
	roleNames := make([]string, 0, len(access.Roles))
	seen := make(map[string]struct{}, len(access.Roles))
	for _, role := range access.Roles {
		if _, exists := seen[role.Name]; exists {
			continue
		}
		seen[role.Name] = struct{}{}
		roleNames = append(roleNames, role.Name)
	}
	sort.Strings(roleNames)
	return Authentication{
		Session: session,
		Principal: Principal{
			Subject:        access.Account.ID,
			OrganizationID: access.Account.OrganizationID,
			Username:       access.Account.Username,
			Email:          access.Account.Email,
			DisplayName:    access.Account.DisplayName,
			Roles:          roleNames,
		},
		Grants: append([]Grant(nil), access.Grants...),
	}
}

func validateAccountInput(input BootstrapInput) (username, normalizedUsername, email, displayName string, err error) {
	username = strings.TrimSpace(input.Username)
	normalizedUsername = normalizeUsername(username)
	if !usernamePattern.MatchString(normalizedUsername) {
		return "", "", "", "", fmt.Errorf("%w: username must be 3 to 64 letters, numbers, periods, underscores, or hyphens", ErrInvalidInput)
	}
	displayName = strings.TrimSpace(input.DisplayName)
	if displayName == "" || utf8.RuneCountInString(displayName) > 200 {
		return "", "", "", "", fmt.Errorf("%w: display name must contain 1 to 200 characters", ErrInvalidInput)
	}
	email = strings.TrimSpace(input.Email)
	address, parseErr := mail.ParseAddress(email)
	if parseErr != nil || address.Address != email || len(email) > 320 {
		return "", "", "", "", fmt.Errorf("%w: a valid email address is required", ErrInvalidInput)
	}
	return username, normalizedUsername, strings.ToLower(email), displayName, nil
}

func validatePassword(password string) error {
	if !utf8.ValidString(password) || utf8.RuneCountInString(password) < MinimumPasswordCharacters || len(password) > maximumPasswordBytes {
		return fmt.Errorf("%w: password must contain at least %d characters and no more than %d bytes", ErrInvalidInput, MinimumPasswordCharacters, maximumPasswordBytes)
	}
	return nil
}

func validateOwnershipInput(input ResourceOwnershipInput) (resourceType, resourceID, sourceSystemID, sourceRecordID string, err error) {
	resourceType = strings.TrimSpace(input.ResourceType)
	resourceID = strings.TrimSpace(input.ResourceID)
	sourceSystemID = strings.TrimSpace(input.SourceSystemID)
	sourceRecordID = strings.TrimSpace(input.SourceRecordID)
	if !resourceTypePattern.MatchString(resourceType) || !guardResourceIDPattern.MatchString(resourceID) ||
		!guardResourceIDPattern.MatchString(sourceSystemID) || sourceRecordID == "" ||
		!utf8.ValidString(sourceRecordID) || utf8.RuneCountInString(sourceRecordID) > 256 {
		return "", "", "", "", fmt.Errorf("%w: valid resource and source identities are required", ErrInvalidInput)
	}
	return resourceType, resourceID, sourceSystemID, sourceRecordID, nil
}

func normalizeUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

func externalUsername(protocol, issuer, subject string) string {
	digest := sha256.Sum256([]byte(issuer + "\x00" + subject))
	return fmt.Sprintf("%s-%x", protocol, digest[:12])
}

func externalAssignmentSource(protocol, issuer string) string {
	digest := sha256.Sum256([]byte(issuer))
	return fmt.Sprintf("%s:%x", protocol, digest[:16])
}

func newID() (string, error) {
	return foundation.NewCorrelationID()
}

func newSecret() (string, []byte, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", nil, errors.New("generate secure token")
	}
	raw := base64.RawURLEncoding.EncodeToString(buffer)
	digest := sha256.Sum256([]byte(raw))
	clear(buffer)
	return raw, append([]byte(nil), digest[:]...), nil
}

func hashSecret(raw string) ([]byte, error) {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	if err != nil || len(decoded) != 32 {
		return nil, errors.New("secure token is invalid")
	}
	clear(decoded)
	digest := sha256.Sum256([]byte(raw))
	return append([]byte(nil), digest[:]...), nil
}

func (s *Service) audit(ctx context.Context, actorID, action, resourceType, resourceID string, metadata map[string]string) error {
	scope, ok := foundation.ScopeFromContext(ctx)
	if !ok || scope.CorrelationID == "" {
		correlationID, err := foundation.NewCorrelationID()
		if err != nil {
			return err
		}
		scope = foundation.Scope{OrganizationID: s.organizationID, ActorID: actorID, CorrelationID: correlationID}
		ctx = foundation.WithScope(ctx, scope)
	}
	if metadata == nil {
		metadata = make(map[string]string)
	}
	metadata["requirementId"] = RequirementID
	eventID, err := foundation.NewCorrelationID()
	if err != nil {
		return err
	}
	return s.auditor.Record(ctx, foundation.AuditEvent{
		ID:             eventID,
		OrganizationID: s.organizationID,
		ActorID:        actorID,
		CorrelationID:  scope.CorrelationID,
		Action:         action,
		ResourceType:   resourceType,
		ResourceID:     resourceID,
		OccurredAt:     s.now(),
		Metadata:       metadata,
	})
}

func (s *Service) recordSecurityEvent(ctx context.Context, actorID, action, resourceType, resourceID string, metadata map[string]string) {
	_ = s.audit(ctx, actorID, action, resourceType, resourceID, metadata)
}
