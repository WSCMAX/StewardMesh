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
			Source:          guard.BuiltInRoleSource,
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
			Source:    guard.LocalAssignmentSource,
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
	directory, err := store.ListAuthorization(ctx, organizationID)
	if err != nil {
		t.Fatal(err)
	}
	if len(directory.Accounts) != 1 || directory.Accounts[0].PasswordHash != "" ||
		len(directory.Roles) != 1 || len(directory.Assignments) != 1 ||
		len(directory.PolicyBundles) != 1 || directory.Roles[0].Source != guard.BuiltInRoleSource ||
		directory.Assignments[0].Source != guard.LocalAssignmentSource {
		t.Fatalf("unexpected authorization directory %#v", directory)
	}
	if err := store.DeleteRole(ctx, organizationID, roleID); !errors.Is(err, guard.ErrBuiltInRole) {
		t.Fatalf("expected built-in role to be protected, got %v", err)
	}
	customRole := guard.Role{
		ID: randomID(t), OrganizationID: organizationID, Name: "Asset steward",
		Description: "Manages inventory records.", Permissions: []guard.Permission{guard.PermissionAssetsWrite},
		PolicyBundleIDs: []string{bundleID}, Source: guard.LocalRoleSource,
	}
	if err := store.CreateRole(ctx, customRole); err != nil {
		t.Fatalf("create custom role: %v", err)
	}
	duplicateName := customRole
	duplicateName.ID = randomID(t)
	duplicateName.Name = "  ASSET STEWARD  "
	if err := store.CreateRole(ctx, duplicateName); !errors.Is(err, guard.ErrConflict) {
		t.Fatalf("expected normalized role name conflict, got %v", err)
	}
	missingBundle := customRole
	missingBundle.ID = randomID(t)
	missingBundle.Name = "Missing bundle"
	missingBundle.PolicyBundleIDs = []string{randomID(t)}
	if err := store.CreateRole(ctx, missingBundle); !errors.Is(err, guard.ErrNotFound) {
		t.Fatalf("expected unknown policy bundle rejection, got %v", err)
	}
	customAssignment := guard.RoleAssignment{
		ID: randomID(t), OrganizationID: organizationID, AccountID: accountID, RoleID: customRole.ID,
		Scope:  guard.Scope{Kind: guard.ScopeSite, OrganizationID: organizationID, ResourceID: "custom-role-site"},
		Source: guard.LocalAssignmentSource, CreatedAt: now.Add(30 * time.Second),
	}
	if err := store.CreateRoleAssignment(ctx, customAssignment); err != nil {
		t.Fatalf("assign custom role: %v", err)
	}
	if err := store.DeleteRole(ctx, organizationID, customRole.ID); !errors.Is(err, guard.ErrConflict) {
		t.Fatalf("expected assigned custom role to be protected, got %v", err)
	}
	if _, err := store.DeleteRoleAssignment(ctx, organizationID, customAssignment.ID); err != nil {
		t.Fatalf("remove custom role assignment: %v", err)
	}
	if err := store.DeleteRole(ctx, organizationID, customRole.ID); err != nil {
		t.Fatalf("delete unassigned custom role: %v", err)
	}
	if _, err := store.DeleteRoleAssignment(ctx, organizationID, assignmentID); !errors.Is(err, guard.ErrLastAdministrator) {
		t.Fatalf("expected the only organization administrator to be protected, got %v", err)
	}
	siteAssignment := guard.RoleAssignment{
		ID:             randomID(t),
		OrganizationID: organizationID,
		AccountID:      accountID,
		RoleID:         roleID,
		Scope: guard.Scope{
			Kind:           guard.ScopeSite,
			OrganizationID: organizationID,
			ResourceID:     "site-one",
		},
		Source:    guard.LocalAssignmentSource,
		CreatedAt: now.Add(time.Second),
	}
	if err := store.CreateRoleAssignment(ctx, siteAssignment); err != nil {
		t.Fatal(err)
	}
	duplicate := siteAssignment
	duplicate.ID = randomID(t)
	if err := store.CreateRoleAssignment(ctx, duplicate); !errors.Is(err, guard.ErrConflict) {
		t.Fatalf("expected duplicate scoped assignment conflict, got %v", err)
	}
	deletedSite, err := store.DeleteRoleAssignment(ctx, organizationID, siteAssignment.ID)
	if err != nil || deletedSite.ID != siteAssignment.ID {
		t.Fatalf("unexpected scoped assignment deletion %#v err=%v", deletedSite, err)
	}

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
	directory, err = store.ListAuthorization(ctx, organizationID)
	if err != nil {
		t.Fatal(err)
	}
	foundManagedAssignment := false
	for _, assignment := range directory.Assignments {
		if assignment.ID == externalAssignmentID && assignment.Source == externalProvisioning.AssignmentSource {
			foundManagedAssignment = true
		}
	}
	if !foundManagedAssignment {
		t.Fatalf("expected provider-managed assignment in directory %#v", directory.Assignments)
	}
	if _, err := store.DeleteRoleAssignment(ctx, organizationID, externalAssignmentID); !errors.Is(err, guard.ErrManagedAssignment) {
		t.Fatalf("expected provider-managed assignment deletion to fail, got %v", err)
	}
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
	localExternalAssignment := guard.RoleAssignment{
		ID:             randomID(t),
		OrganizationID: organizationID,
		AccountID:      externalAccountID,
		RoleID:         roleID,
		Scope: guard.Scope{
			Kind:           guard.ScopeOrganization,
			OrganizationID: organizationID,
			ResourceID:     organizationID,
		},
		Source:    guard.LocalAssignmentSource,
		CreatedAt: now.Add(2 * time.Minute),
	}
	if err := store.CreateRoleAssignment(ctx, localExternalAssignment); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeleteRoleAssignment(ctx, organizationID, assignmentID); err != nil {
		t.Fatalf("expected a manager assignment to be removable when another remains: %v", err)
	}
	if _, err := store.DeleteRoleAssignment(ctx, organizationID, localExternalAssignment.ID); !errors.Is(err, guard.ErrLastAdministrator) {
		t.Fatalf("expected replacement organization administrator to be protected, got %v", err)
	}
	if err := store.CreateRoleAssignment(ctx, bootstrap.Assignment); err != nil {
		t.Fatalf("restore bootstrap administrator assignment: %v", err)
	}
	if _, err := store.DeleteRoleAssignment(ctx, organizationID, localExternalAssignment.ID); err != nil {
		t.Fatalf("remove replacement administrator assignment: %v", err)
	}

	registeredAt := now.Add(3 * time.Minute)
	ownership := guard.ResourceOwnership{
		OrganizationID: organizationID,
		ResourceType:   "asset",
		ResourceID:     "asset-imported-one",
		SourceSystemID: "source-system-one",
		SourceRecordID: "external-record-one",
		WriteLocked:    true,
		RegisteredAt:   registeredAt,
	}
	registered, ownershipCreated, err := store.RegisterResourceOwnership(ctx, ownership)
	if err != nil || !ownershipCreated || registered.ResourceID != ownership.ResourceID || !registered.WriteLocked {
		t.Fatalf("unexpected resource ownership registration %#v created=%t err=%v", registered, ownershipCreated, err)
	}
	registered, ownershipCreated, err = store.RegisterResourceOwnership(ctx, ownership)
	if err != nil || ownershipCreated || registered.ResourceID != ownership.ResourceID {
		t.Fatalf("expected idempotent ownership registration %#v created=%t err=%v", registered, ownershipCreated, err)
	}
	conflictingOwnership := ownership
	conflictingOwnership.ResourceID = "asset-imported-two"
	if _, _, err := store.RegisterResourceOwnership(ctx, conflictingOwnership); !errors.Is(err, guard.ErrConflict) {
		t.Fatalf("expected duplicate source ownership conflict, got %v", err)
	}
	listedOwnership, err := store.ListResourceOwnership(ctx, organizationID)
	if err != nil || len(listedOwnership) != 1 || listedOwnership[0].ResourceID != ownership.ResourceID {
		t.Fatalf("unexpected resource ownership list %#v err=%v", listedOwnership, err)
	}
	loadedOwnership, err := store.GetResourceOwnership(ctx, organizationID, ownership.ResourceType, ownership.ResourceID)
	if err != nil || !loadedOwnership.WriteLocked {
		t.Fatalf("unexpected loaded resource ownership %#v err=%v", loadedOwnership, err)
	}
	claimedAt := registeredAt.Add(time.Minute)
	claimed, err := store.ClaimResourceOwnership(ctx, organizationID, ownership.ResourceType, ownership.ResourceID, accountID, claimedAt)
	if err != nil || claimed.WriteLocked || claimed.ClaimedBy != accountID || claimed.ClaimedAt == nil || !claimed.ClaimedAt.Equal(claimedAt) {
		t.Fatalf("unexpected ownership claim %#v err=%v", claimed, err)
	}
	if err := store.RestoreResourceOwnershipLock(ctx, claimed); err != nil {
		t.Fatalf("restore resource ownership lock: %v", err)
	}
	restored, err := store.GetResourceOwnership(ctx, organizationID, ownership.ResourceType, ownership.ResourceID)
	if err != nil || !restored.WriteLocked || restored.ClaimedAt != nil || restored.ClaimedBy != "" {
		t.Fatalf("unexpected restored ownership lock %#v err=%v", restored, err)
	}
	claimed, err = store.ClaimResourceOwnership(ctx, organizationID, ownership.ResourceType, ownership.ResourceID, accountID, claimedAt)
	if err != nil || claimed.WriteLocked {
		t.Fatalf("unexpected ownership re-claim %#v err=%v", claimed, err)
	}
	if _, err := store.ClaimResourceOwnership(ctx, organizationID, ownership.ResourceType, ownership.ResourceID, accountID, claimedAt); !errors.Is(err, guard.ErrConflict) {
		t.Fatalf("expected repeated ownership claim conflict, got %v", err)
	}
	deletable := guard.ResourceOwnership{
		OrganizationID: organizationID,
		ResourceType:   "asset",
		ResourceID:     "asset-audit-rollback",
		SourceSystemID: "source-system-one",
		SourceRecordID: "external-record-audit-rollback",
		WriteLocked:    true,
		RegisteredAt:   registeredAt.Add(2 * time.Minute),
	}
	if _, created, err := store.RegisterResourceOwnership(ctx, deletable); err != nil || !created {
		t.Fatalf("register deletable ownership created=%t err=%v", created, err)
	}
	if err := store.DeleteResourceOwnership(ctx, deletable); err != nil {
		t.Fatalf("delete resource ownership: %v", err)
	}
	if _, err := store.GetResourceOwnership(ctx, organizationID, deletable.ResourceType, deletable.ResourceID); !errors.Is(err, guard.ErrNotFound) {
		t.Fatalf("expected deleted ownership to be unavailable, got %v", err)
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
