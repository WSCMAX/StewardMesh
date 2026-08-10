package guard_test

// Requirements: REQ-PLATFORM-VALKEY-001, SEC-GUARD-001.

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/cache"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	. "github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/identity"
	"github.com/maxlemke/stewardmesh/internal/repository"
)

type fastTestHasher struct{}

func (fastTestHasher) Hash(password string) (string, error) {
	digest := sha256.Sum256([]byte(password))
	return "test$" + base64.RawStdEncoding.EncodeToString(digest[:]), nil
}

func (fastTestHasher) Verify(password, encodedHash string) (bool, bool, error) {
	expected, err := (fastTestHasher{}).Hash(password)
	if err != nil {
		return false, false, err
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(encodedHash)) == 1, false, nil
}

type recordingAuditor struct {
	events []foundation.AuditEvent
}

func (a *recordingAuditor) Record(_ context.Context, event foundation.AuditEvent) error {
	a.events = append(a.events, event)
	return nil
}

type disabledSessionStore struct {
	Store
}

type controlledAttemptLimiter struct {
	allowErr   error
	failureErr error
	resetErr   error
}

func (l controlledAttemptLimiter) Allow(context.Context, string, time.Time) (bool, error) {
	return l.allowErr == nil, l.allowErr
}

func (l controlledAttemptLimiter) Failure(context.Context, string, time.Time) error {
	return l.failureErr
}

func (l controlledAttemptLimiter) Reset(context.Context, string) error {
	return l.resetErr
}

func (s disabledSessionStore) FindSessionByTokenHash(ctx context.Context, organizationID string, tokenHash []byte, now time.Time) (Session, Access, error) {
	session, access, err := s.Store.FindSessionByTokenHash(ctx, organizationID, tokenHash, now)
	access.Account.Status = "disabled"
	return session, access, err
}

func TestServiceBootstrapLoginAuthorizeAndRevoke(t *testing.T) {
	store := repository.NewMemoryGuardStore()
	auditor := &recordingAuditor{}
	now := time.Now().UTC().Truncate(time.Second)
	service, err := NewService(store, fastTestHasher{}, auditor, nil, ServiceConfig{
		OrganizationID: "service-organization",
		SessionTTL:     time.Hour,
		Now:            func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := foundation.WithScope(context.Background(), foundation.Scope{
		OrganizationID: "service-organization",
		ActorID:        "anonymous",
		CorrelationID:  "service-test",
	})
	required, tokenRequired, err := service.BootstrapStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !required || tokenRequired {
		t.Fatalf("unexpected bootstrap state required=%t tokenRequired=%t", required, tokenRequired)
	}
	password := "correct horse battery staple"
	credentials, err := service.Bootstrap(ctx, BootstrapInput{
		Username:    "Administrator",
		Email:       "administrator@example.test",
		DisplayName: "Example Administrator",
		Password:    password,
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if credentials.Token == "" || credentials.CSRFToken == "" || credentials.Authentication.Principal.Subject == "" {
		t.Fatalf("unexpected credentials %#v", credentials)
	}
	account, err := store.FindAccountByUsername(ctx, "service-organization", "administrator")
	if err != nil {
		t.Fatal(err)
	}
	if account.PasswordHash == "" || strings.Contains(account.PasswordHash, password) {
		t.Fatal("password must be stored only as a non-plaintext hash")
	}
	if err := service.CheckPermission(ctx, credentials.Authentication, PermissionAssetsWrite, Scope{
		Kind:           ScopeOrganization,
		OrganizationID: "service-organization",
		ResourceID:     "service-organization",
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.ValidateCSRF(credentials.Authentication, credentials.CSRFToken); err != nil {
		t.Fatal(err)
	}
	rotatedToken, err := service.RefreshCSRF(ctx, &credentials.Authentication)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ValidateCSRF(credentials.Authentication, credentials.CSRFToken); !errors.Is(err, ErrInvalidCSRF) {
		t.Fatalf("expected prior csrf token to expire, got %v", err)
	}
	if err := service.ValidateCSRF(credentials.Authentication, rotatedToken); err != nil {
		t.Fatal(err)
	}
	authenticated, err := service.AuthenticateSession(ctx, credentials.Token)
	if err != nil || authenticated.Principal.Subject != account.ID {
		t.Fatalf("unexpected authenticated session %#v err=%v", authenticated, err)
	}
	login, err := service.Login(ctx, LoginInput{Username: "ADMINISTRATOR", Password: password, RateKey: "127.0.0.1"})
	if err != nil || login.Authentication.Principal.Subject != account.ID {
		t.Fatalf("unexpected login %#v err=%v", login, err)
	}
	if err := service.Logout(ctx, login.Authentication); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AuthenticateSession(ctx, login.Token); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("expected revoked session to fail, got %v", err)
	}
	for _, event := range auditor.events {
		if strings.Contains(event.Action, password) || strings.Contains(event.ResourceID, password) {
			t.Fatal("audit event leaked a password")
		}
		for _, value := range event.Metadata {
			if strings.Contains(value, password) {
				t.Fatal("audit metadata leaked a password")
			}
		}
	}
}

func TestServiceManagesScopedRoleAssignmentsWithoutAllowingAdministratorLockout(t *testing.T) {
	store := repository.NewMemoryGuardStore()
	auditor := &recordingAuditor{}
	now := time.Now().UTC().Truncate(time.Second)
	service, err := NewService(store, fastTestHasher{}, auditor, nil, ServiceConfig{
		OrganizationID: "assignment-organization",
		SessionTTL:     time.Hour,
		Now:            func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := service.Bootstrap(context.Background(), BootstrapInput{
		Username: "administrator", Email: "administrator@example.test", DisplayName: "Administrator",
		Password: "correct horse battery staple",
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	directory, err := service.ListAuthorization(context.Background(), credentials.Authentication)
	if err != nil {
		t.Fatal(err)
	}
	if len(directory.Accounts) != 1 || len(directory.Roles) != 1 || len(directory.Assignments) != 1 {
		t.Fatalf("unexpected initial authorization directory %#v", directory)
	}
	bootstrapAssignmentID := directory.Assignments[0].ID
	if _, err := service.RevokeRoleAssignment(context.Background(), credentials.Authentication, bootstrapAssignmentID); !errors.Is(err, ErrLastAdministrator) {
		t.Fatalf("expected last organization administrator protection, got %v", err)
	}
	now = now.Add(time.Minute)
	assignment, err := service.AssignRole(context.Background(), credentials.Authentication, RoleAssignmentInput{
		AccountID:  directory.Accounts[0].ID,
		RoleID:     directory.Roles[0].ID,
		ScopeKind:  ScopeSite,
		ResourceID: "site-one",
	})
	if err != nil {
		t.Fatal(err)
	}
	if assignment.Scope.Kind != ScopeSite || assignment.Scope.ResourceID != "site-one" || assignment.Source != LocalAssignmentSource {
		t.Fatalf("unexpected scoped assignment %#v", assignment)
	}
	if _, err := service.AssignRole(context.Background(), credentials.Authentication, RoleAssignmentInput{
		AccountID:  directory.Accounts[0].ID,
		RoleID:     directory.Roles[0].ID,
		ScopeKind:  ScopeSite,
		ResourceID: "site-one",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected duplicate assignment conflict, got %v", err)
	}
	if _, err := service.RevokeRoleAssignment(context.Background(), credentials.Authentication, assignment.ID); err != nil {
		t.Fatal(err)
	}
	unauthorized := credentials.Authentication
	unauthorized.Grants = nil
	if _, err := service.ListAuthorization(context.Background(), unauthorized); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expected organization-level management permission, got %v", err)
	}
	foundCreated, foundRevoked := false, false
	for _, event := range auditor.events {
		foundCreated = foundCreated || event.Action == "guard.role_assignment.created" && event.ResourceID == assignment.ID
		foundRevoked = foundRevoked || event.Action == "guard.role_assignment.revoked" && event.ResourceID == assignment.ID
	}
	if !foundCreated || !foundRevoked {
		t.Fatalf("expected role assignment audit events, got %#v", auditor.events)
	}
}

func TestServiceJITProvisionsOIDCAccountAndSynchronizesAdministratorMapping(t *testing.T) {
	store := repository.NewMemoryGuardStore()
	auditor := &recordingAuditor{}
	now := time.Now().UTC().Truncate(time.Second)
	service, err := NewService(store, fastTestHasher{}, auditor, nil, ServiceConfig{
		OrganizationID: "oidc-organization",
		SessionTTL:     time.Hour,
		Now:            func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Bootstrap(context.Background(), BootstrapInput{
		Username: "administrator", Email: "administrator@example.test", DisplayName: "Administrator",
		Password: "correct horse battery staple",
	}, true); err != nil {
		t.Fatal(err)
	}
	principal := identity.OIDCPrincipal{
		Issuer: "https://identity.example.test/tenant", Subject: "provider-subject",
		Email: "person@example.test", EmailVerified: true, DisplayName: "Example Person", Administrator: true,
	}
	credentials, err := service.LoginOIDC(context.Background(), principal)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(credentials.Authentication.Principal.Username, "oidc-") ||
		credentials.Authentication.Principal.Email != "person@example.test" {
		t.Fatalf("unexpected external principal %#v", credentials.Authentication.Principal)
	}
	if err := service.CheckPermission(context.Background(), credentials.Authentication, PermissionGuardManage, Scope{
		Kind: ScopeOrganization, OrganizationID: "oidc-organization", ResourceID: "oidc-organization",
	}); err != nil {
		t.Fatal(err)
	}
	accountID := credentials.Authentication.Principal.Subject
	if _, err := service.Login(context.Background(), LoginInput{
		Username: credentials.Authentication.Principal.Username, Password: "not a local password", RateKey: "client",
	}); !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("expected external account to reject local password login, got %v", err)
	}
	now = now.Add(time.Minute)
	principal.Email = "refreshed@example.test"
	principal.DisplayName = "Refreshed Person"
	principal.Administrator = false
	refreshed, err := service.LoginOIDC(context.Background(), principal)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Authentication.Principal.Subject != accountID || refreshed.Authentication.Principal.Email != "refreshed@example.test" ||
		len(refreshed.Authentication.Principal.Roles) != 0 || len(refreshed.Authentication.Grants) != 0 {
		t.Fatalf("unexpected refreshed external principal %#v", refreshed.Authentication)
	}
	foundSuccess := false
	for _, event := range auditor.events {
		if event.Action == "guard.oidc.login.succeeded" {
			foundSuccess = true
		}
		if strings.Contains(event.ResourceID, principal.Issuer) || strings.Contains(event.ResourceID, principal.Subject) {
			t.Fatal("audit event exposed provider assertion values")
		}
	}
	if !foundSuccess {
		t.Fatal("expected OpenID Connect login audit event")
	}
}

func TestServiceRejectsInvalidOIDCPrincipal(t *testing.T) {
	service, err := NewService(repository.NewMemoryGuardStore(), fastTestHasher{}, foundation.NopAuditor{}, nil, ServiceConfig{
		OrganizationID: "oidc-invalid-organization", SessionTTL: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.LoginOIDC(context.Background(), identity.OIDCPrincipal{
		Issuer: "https://identity.example.test", Subject: "subject", Email: "not-an-email", DisplayName: "Person",
	}); !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("expected invalid external principal to fail, got %v", err)
	}
}

func TestServiceRequiresBootstrapTokenWhenConfigured(t *testing.T) {
	store := repository.NewMemoryGuardStore()
	service, err := NewService(store, fastTestHasher{}, foundation.NopAuditor{}, nil, ServiceConfig{
		OrganizationID: "token-organization",
		BootstrapToken: strings.Repeat("a", 32),
		SessionTTL:     time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	input := BootstrapInput{
		Username:       "administrator",
		Email:          "administrator@example.test",
		DisplayName:    "Administrator",
		Password:       "correct horse battery staple",
		BootstrapToken: "incorrect",
	}
	if _, err := service.Bootstrap(context.Background(), input, true); !errors.Is(err, ErrBootstrapDenied) {
		t.Fatalf("expected incorrect bootstrap token to fail, got %v", err)
	}
	input.BootstrapToken = strings.Repeat("a", 32)
	if _, err := service.Bootstrap(context.Background(), input, false); err != nil {
		t.Fatal(err)
	}
}

func TestServiceRejectsAnExistingSessionForDisabledAccount(t *testing.T) {
	store := repository.NewMemoryGuardStore()
	service, err := NewService(store, fastTestHasher{}, foundation.NopAuditor{}, nil, ServiceConfig{
		OrganizationID: "disabled-account-organization",
		SessionTTL:     time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := service.Bootstrap(context.Background(), BootstrapInput{
		Username:    "administrator",
		Email:       "administrator@example.test",
		DisplayName: "Administrator",
		Password:    "correct horse battery staple",
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	disabledService, err := NewService(disabledSessionStore{Store: store}, fastTestHasher{}, foundation.NopAuditor{}, nil, ServiceConfig{
		OrganizationID: "disabled-account-organization",
		SessionTTL:     time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := disabledService.AuthenticateSession(context.Background(), credentials.Token); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("expected disabled account session to be rejected, got %v", err)
	}
}

func TestServiceRateLimitsInvalidCredentials(t *testing.T) {
	store := repository.NewMemoryGuardStore()
	auditor := &recordingAuditor{}
	limiter, err := NewMemoryAttemptLimiter(2, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(store, fastTestHasher{}, auditor, limiter, ServiceConfig{
		OrganizationID: "rate-organization",
		SessionTTL:     time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Bootstrap(context.Background(), BootstrapInput{
		Username:    "administrator",
		Email:       "administrator@example.test",
		DisplayName: "Administrator",
		Password:    "correct horse battery staple",
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	input := LoginInput{Username: "administrator", Password: "wrong password value", RateKey: "client"}
	for range 2 {
		if _, err := service.Login(context.Background(), input); !errors.Is(err, ErrInvalidCredential) {
			t.Fatalf("expected invalid credentials, got %v", err)
		}
	}
	if _, err := service.Login(context.Background(), input); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected rate limit, got %v", err)
	}
}

func TestServiceRateLimitsUsernameSprayingByDirectClient(t *testing.T) {
	store := repository.NewMemoryGuardStore()
	limiter, err := NewMemoryAttemptLimiter(2, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(store, fastTestHasher{}, foundation.NopAuditor{}, limiter, ServiceConfig{
		OrganizationID: "client-rate-organization",
		SessionTTL:     time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, username := range []string{"unknown-one", "unknown-two"} {
		_, err := service.Login(context.Background(), LoginInput{
			Username: username,
			Password: "wrong password value",
			RateKey:  "shared-client",
		})
		if !errors.Is(err, ErrInvalidCredential) {
			t.Fatalf("expected invalid credentials for %q, got %v", username, err)
		}
	}
	_, err = service.Login(context.Background(), LoginInput{
		Username: "unknown-three",
		Password: "wrong password value",
		RateKey:  "shared-client",
	})
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected client rate limit, got %v", err)
	}
}

func TestServiceRateLimitsAccountAcrossDirectClients(t *testing.T) {
	store := repository.NewMemoryGuardStore()
	limiter, err := NewMemoryAttemptLimiter(2, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(store, fastTestHasher{}, foundation.NopAuditor{}, limiter, ServiceConfig{
		OrganizationID: "account-rate-organization",
		SessionTTL:     time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Bootstrap(context.Background(), BootstrapInput{
		Username:    "administrator",
		Email:       "administrator@example.test",
		DisplayName: "Administrator",
		Password:    "correct horse battery staple",
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, client := range []string{"client-one", "client-two"} {
		_, err := service.Login(context.Background(), LoginInput{
			Username: "administrator",
			Password: "wrong password value",
			RateKey:  client,
		})
		if !errors.Is(err, ErrInvalidCredential) {
			t.Fatalf("expected invalid credentials for %q, got %v", client, err)
		}
	}
	_, err = service.Login(context.Background(), LoginInput{
		Username: "administrator",
		Password: "wrong password value",
		RateKey:  "client-three",
	})
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected account rate limit, got %v", err)
	}
}

func TestGuardServicesShareCacheBackedLoginFailuresAcrossReplicas(t *testing.T) {
	guardStore := repository.NewMemoryGuardStore()
	sharedCache := cache.NewDefaultMemoryStore()
	namespace, err := cache.NewNamespace("stewardmesh", "v1", "replica-organization")
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte(strings.Repeat("s", 32))
	firstLimiter, err := NewCacheAttemptLimiter(sharedCache, namespace, secret, 2, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	secondLimiter, err := NewCacheAttemptLimiter(sharedCache, namespace, secret, 2, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	newReplica := func(limiter AttemptLimiter) *Service {
		service, err := NewService(guardStore, fastTestHasher{}, foundation.NopAuditor{}, limiter, ServiceConfig{
			OrganizationID: "replica-organization",
			SessionTTL:     time.Hour,
		})
		if err != nil {
			t.Fatal(err)
		}
		return service
	}
	first := newReplica(firstLimiter)
	second := newReplica(secondLimiter)
	_, err = first.Bootstrap(context.Background(), BootstrapInput{
		Username:    "administrator",
		Email:       "administrator@example.test",
		DisplayName: "Administrator",
		Password:    "correct horse battery staple",
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	input := LoginInput{
		Username: "administrator",
		Password: "wrong password value",
		RateKey:  "shared-client",
	}
	for _, service := range []*Service{first, second} {
		if _, err := service.Login(context.Background(), input); !errors.Is(err, ErrInvalidCredential) {
			t.Fatalf("expected invalid credential from replica, got %v", err)
		}
	}
	if _, err := first.Login(context.Background(), input); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected shared rate limit across replicas, got %v", err)
	}
}

func TestServiceFailsClosedWhenConfiguredLoginProtectionIsUnavailable(t *testing.T) {
	unavailable := errors.New("shared cache unavailable")
	tests := []struct {
		name     string
		limiter  controlledAttemptLimiter
		password string
	}{
		{name: "check", limiter: controlledAttemptLimiter{allowErr: unavailable}, password: "correct horse battery staple"},
		{name: "record failure", limiter: controlledAttemptLimiter{failureErr: unavailable}, password: "wrong password value"},
		{name: "reset success", limiter: controlledAttemptLimiter{resetErr: unavailable}, password: "correct horse battery staple"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := repository.NewMemoryGuardStore()
			auditor := &recordingAuditor{}
			service, err := NewService(store, fastTestHasher{}, auditor, test.limiter, ServiceConfig{
				OrganizationID: "outage-organization",
				SessionTTL:     time.Hour,
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = service.Bootstrap(context.Background(), BootstrapInput{
				Username:    "administrator",
				Email:       "administrator@example.test",
				DisplayName: "Administrator",
				Password:    "correct horse battery staple",
			}, true)
			if err != nil {
				t.Fatal(err)
			}
			_, err = service.Login(context.Background(), LoginInput{
				Username: "administrator",
				Password: test.password,
				RateKey:  "127.0.0.1",
			})
			if !errors.Is(err, ErrLoginProtectionUnavailable) {
				t.Fatalf("expected login protection outage, got %v", err)
			}
			foundUnavailableEvent := false
			for _, event := range auditor.events {
				if event.Action == "guard.login.protection_unavailable" {
					foundUnavailableEvent = true
				}
			}
			if !foundUnavailableEvent {
				t.Fatal("expected login protection outage audit event")
			}
		})
	}
}

func TestPasswordPolicyUsesCurrentSingleFactorMinimum(t *testing.T) {
	service, err := NewService(repository.NewMemoryGuardStore(), fastTestHasher{}, foundation.NopAuditor{}, nil, ServiceConfig{
		OrganizationID: "password-policy-organization",
		SessionTTL:     time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Bootstrap(context.Background(), BootstrapInput{
		Username:    "administrator",
		Email:       "administrator@example.test",
		DisplayName: "Administrator",
		Password:    "short-password",
	}, true)
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected short password to fail, got %v", err)
	}
}
