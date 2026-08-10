package contracttest

// Requirement: SEC-GUARD-001.

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/guard"
)

func GuardStore(t *testing.T, store guard.Store, organizationID string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	accountID := randomID(t)
	bundleID := randomID(t)
	roleID := randomID(t)
	assignmentID := randomID(t)
	required, err := store.BootstrapRequired(ctx, organizationID)
	if err != nil {
		t.Fatal(err)
	}
	if !required {
		t.Fatal("expected a new organization to require administrator bootstrap")
	}
	bootstrap := guard.AdministratorBootstrap{
		Account: guard.Account{
			ID:                 accountID,
			OrganizationID:     organizationID,
			Username:           "Administrator",
			NormalizedUsername: "administrator",
			Email:              "administrator@example.test",
			DisplayName:        "Administrator",
			PasswordHash:       "$argon2id$v=19$m=19456,t=2,p=1$c2FsdHNhbHRzYWx0c2FsdA$aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaA",
			Status:             "active",
			CreatedAt:          now,
			UpdatedAt:          now,
		},
		Bundle: guard.PolicyBundle{
			ID:             bundleID,
			OrganizationID: organizationID,
			Name:           "Core administration",
			Permissions:    guard.AdministratorBundlePermissions(),
		},
		Role: guard.Role{
			ID:              roleID,
			OrganizationID:  organizationID,
			Name:            "Administrator",
			Permissions:     []guard.Permission{guard.PermissionGuardManage},
			PolicyBundleIDs: []string{bundleID},
		},
		Assignment: guard.RoleAssignment{
			ID:             assignmentID,
			OrganizationID: organizationID,
			AccountID:      accountID,
			RoleID:         roleID,
			Scope: guard.Scope{
				Kind:           guard.ScopeOrganization,
				OrganizationID: organizationID,
				ResourceID:     organizationID,
			},
			CreatedAt: now,
		},
	}
	created, err := store.BootstrapAdministrator(ctx, bootstrap)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != accountID || created.PasswordHash == "" {
		t.Fatalf("unexpected administrator account %#v", created)
	}
	required, err = store.BootstrapRequired(ctx, organizationID)
	if err != nil {
		t.Fatal(err)
	}
	if required {
		t.Fatal("expected bootstrap to become unavailable after administrator creation")
	}
	if _, err := store.BootstrapAdministrator(ctx, bootstrap); !errors.Is(err, guard.ErrBootstrapComplete) {
		t.Fatalf("expected duplicate bootstrap to fail closed, got %v", err)
	}
	loaded, err := store.FindAccountByUsername(ctx, organizationID, "administrator")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID != accountID || loaded.PasswordHash != bootstrap.Account.PasswordHash {
		t.Fatalf("unexpected account %#v", loaded)
	}
	updatedHash := bootstrap.Account.PasswordHash + "updated"
	if err := store.UpdatePasswordHash(ctx, accountID, updatedHash, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	loaded, err = store.FindAccountByUsername(ctx, organizationID, "administrator")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PasswordHash != updatedHash {
		t.Fatal("expected password hash update")
	}
	access, err := store.AccessForAccount(ctx, organizationID, accountID)
	if err != nil {
		t.Fatal(err)
	}
	if len(access.Roles) != 1 || access.Roles[0].Name != "Administrator" {
		t.Fatalf("unexpected roles %#v", access.Roles)
	}
	if access.Account.PasswordHash != "" {
		t.Fatal("access resolution must not return a password hash")
	}
	assertGrant(t, access.Grants, guard.PermissionGuardManage)
	assertGrant(t, access.Grants, guard.PermissionAssetsWrite)

	externalAccountID := randomID(t)
	externalAssignmentID := randomID(t)
	externalProvisioning := guard.ExternalAccountProvisioning{
		Account: guard.Account{
			ID:                 externalAccountID,
			OrganizationID:     organizationID,
			Username:           "oidc-" + externalAccountID[:12],
			NormalizedUsername: "oidc-" + externalAccountID[:12],
			Email:              "external@example.test",
			DisplayName:        "External Administrator",
			Status:             "active",
			CreatedAt:          now,
			UpdatedAt:          now,
		},
		Identity: guard.ExternalIdentity{
			OrganizationID: organizationID,
			Issuer:         "https://identity.example.test/tenant",
			Subject:        "external-subject",
			AccountID:      externalAccountID,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
		Administrator:             true,
		AdministratorAssignmentID: externalAssignmentID,
		AssignmentSource:          "oidc:0123456789abcdef0123456789abcdef",
	}
	externalAccount, externalCreated, err := store.ProvisionExternalAccount(ctx, externalProvisioning)
	if err != nil || !externalCreated || externalAccount.ID != externalAccountID || externalAccount.PasswordHash != "" {
		t.Fatalf("unexpected external account %#v created=%t err=%v", externalAccount, externalCreated, err)
	}
	externalAccess, err := store.AccessForAccount(ctx, organizationID, externalAccount.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertGrant(t, externalAccess.Grants, guard.PermissionGuardManage)
	externalProvisioning.Account.ID = randomID(t)
	externalProvisioning.Account.Email = "refreshed@example.test"
	externalProvisioning.Account.DisplayName = "Refreshed Person"
	externalProvisioning.Account.UpdatedAt = now.Add(time.Minute)
	externalProvisioning.Identity.AccountID = externalProvisioning.Account.ID
	externalProvisioning.Identity.UpdatedAt = now.Add(time.Minute)
	externalProvisioning.Administrator = false
	externalProvisioning.AdministratorAssignmentID = ""
	externalAccount, externalCreated, err = store.ProvisionExternalAccount(ctx, externalProvisioning)
	if err != nil || externalCreated || externalAccount.ID != externalAccountID || externalAccount.Email != "refreshed@example.test" {
		t.Fatalf("unexpected refreshed external account %#v created=%t err=%v", externalAccount, externalCreated, err)
	}
	externalAccess, err = store.AccessForAccount(ctx, organizationID, externalAccount.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(externalAccess.Grants) != 0 || len(externalAccess.Roles) != 0 {
		t.Fatalf("expected claim removal to remove only the external administrator mapping, access=%#v", externalAccess)
	}

	sessionID := randomID(t)
	tokenHash := sha256.Sum256([]byte("session-token-" + sessionID))
	csrfHash := sha256.Sum256([]byte("csrf-token-" + sessionID))
	session := guard.Session{
		ID:             sessionID,
		OrganizationID: organizationID,
		AccountID:      accountID,
		TokenHash:      tokenHash[:],
		CSRFHash:       csrfHash[:],
		CreatedAt:      now,
		ExpiresAt:      now.Add(time.Hour),
	}
	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	foundSession, foundAccess, err := store.FindSessionByTokenHash(ctx, organizationID, tokenHash[:], now)
	if err != nil {
		t.Fatal(err)
	}
	if foundSession.ID != session.ID || foundAccess.Account.ID != accountID {
		t.Fatalf("unexpected session access %#v %#v", foundSession, foundAccess)
	}
	rotatedHash := sha256.Sum256([]byte("rotated-csrf-token-" + sessionID))
	if err := store.UpdateSessionCSRF(ctx, session.ID, rotatedHash[:]); err != nil {
		t.Fatal(err)
	}
	foundSession, _, err = store.FindSessionByTokenHash(ctx, organizationID, tokenHash[:], now)
	if err != nil {
		t.Fatal(err)
	}
	if string(foundSession.CSRFHash) != string(rotatedHash[:]) {
		t.Fatal("expected csrf hash rotation")
	}
	if err := store.RevokeSession(ctx, session.ID, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.FindSessionByTokenHash(ctx, organizationID, tokenHash[:], now.Add(2*time.Minute)); !errors.Is(err, guard.ErrNotFound) {
		t.Fatalf("expected revoked session to be unavailable, got %v", err)
	}
}

func randomID(t *testing.T) string {
	t.Helper()
	id, err := foundation.NewCorrelationID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func assertGrant(t *testing.T, grants []guard.Grant, permission guard.Permission) {
	t.Helper()
	for _, grant := range grants {
		if grant.Permission == permission && grant.Scope.Kind == guard.ScopeOrganization {
			return
		}
	}
	t.Fatalf("expected organization-scoped permission %q in %#v", permission, grants)
}
