package guard

// Requirements: SEC-GUARD-001, SEC-HTTP-001.

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
)

const (
	maximumPasswordBytes = 1024
	defaultSessionTTL    = 12 * time.Hour
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,63}$`)

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
			Description:    "Common organization, inventory, directory, and goals permissions.",
			Permissions:    AdministratorBundlePermissions(),
		},
		Role: Role{
			ID:              roleID,
			OrganizationID:  s.organizationID,
			Name:            "Administrator",
			Description:     "Full organization administrator for the initial StewardMesh deployment.",
			Permissions:     []Permission{PermissionGuardManage},
			PolicyBundleIDs: []string{bundleID},
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
	if !s.limiter.Allow(clientRateKey, now) || !s.limiter.Allow(accountRateKey, now) {
		s.recordSecurityEvent(ctx, "anonymous", "guard.login.rate_limited", "account", "unknown", nil)
		return SessionCredentials{}, ErrRateLimited
	}
	account, err := s.store.FindAccountByUsername(ctx, s.organizationID, normalizedUsername)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return SessionCredentials{}, fmt.Errorf("load account: %w", err)
	}
	encodedHash := s.dummyPasswordHash
	if err == nil {
		encodedHash = account.PasswordHash
	}
	matches, needsRehash, verifyErr := s.hasher.Verify(input.Password, encodedHash)
	if verifyErr != nil {
		return SessionCredentials{}, fmt.Errorf("verify stored credential: %w", verifyErr)
	}
	if err != nil || !matches || account.Status != "active" || !usernamePattern.MatchString(normalizedUsername) {
		s.limiter.Failure(clientRateKey, now)
		s.limiter.Failure(accountRateKey, now)
		resourceID := "unknown"
		if account.ID != "" {
			resourceID = account.ID
		}
		s.recordSecurityEvent(ctx, "anonymous", "guard.login.failed", "account", resourceID, nil)
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
	s.limiter.Reset(clientRateKey)
	s.limiter.Reset(accountRateKey)
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

func normalizeUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
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
