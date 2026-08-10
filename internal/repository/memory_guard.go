package repository

// Requirements: SEC-GUARD-001, SEC-HTTP-001.

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/maxlemke/stewardmesh/internal/guard"
)

type MemoryGuardStore struct {
	mu                  sync.RWMutex
	accounts            map[string]guard.Account
	accountByUsername   map[string]string
	bundles             map[string]guard.PolicyBundle
	roles               map[string]guard.Role
	assignments         map[string]guard.RoleAssignment
	externalIdentities  map[string]guard.ExternalIdentity
	externalAssignments map[string]string
	sessions            map[string]guard.Session
	sessionByTokenHash  map[string]string
}

var _ guard.Store = (*MemoryGuardStore)(nil)

func NewMemoryGuardStore() *MemoryGuardStore {
	return &MemoryGuardStore{
		accounts:            make(map[string]guard.Account),
		accountByUsername:   make(map[string]string),
		bundles:             make(map[string]guard.PolicyBundle),
		roles:               make(map[string]guard.Role),
		assignments:         make(map[string]guard.RoleAssignment),
		externalIdentities:  make(map[string]guard.ExternalIdentity),
		externalAssignments: make(map[string]string),
		sessions:            make(map[string]guard.Session),
		sessionByTokenHash:  make(map[string]string),
	}
}

func (s *MemoryGuardStore) BootstrapRequired(_ context.Context, organizationID string) (bool, error) {
	if organizationID == "" {
		return false, errors.New("organization id is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, account := range s.accounts {
		if account.OrganizationID == organizationID {
			return false, nil
		}
	}
	return true, nil
}

func (s *MemoryGuardStore) BootstrapAdministrator(_ context.Context, bootstrap guard.AdministratorBootstrap) (guard.Account, error) {
	if err := bootstrap.Validate(); err != nil {
		return guard.Account{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, account := range s.accounts {
		if account.OrganizationID == bootstrap.Account.OrganizationID {
			return guard.Account{}, guard.ErrBootstrapComplete
		}
	}
	usernameKey := guardUsernameKey(bootstrap.Account.OrganizationID, bootstrap.Account.NormalizedUsername)
	if _, exists := s.accountByUsername[usernameKey]; exists {
		return guard.Account{}, guard.ErrBootstrapComplete
	}
	s.accounts[bootstrap.Account.ID] = cloneGuardAccount(bootstrap.Account)
	s.accountByUsername[usernameKey] = bootstrap.Account.ID
	s.bundles[bootstrap.Bundle.ID] = clonePolicyBundle(bootstrap.Bundle)
	s.roles[bootstrap.Role.ID] = cloneGuardRole(bootstrap.Role)
	s.assignments[bootstrap.Assignment.ID] = bootstrap.Assignment
	return cloneGuardAccount(bootstrap.Account), nil
}

func (s *MemoryGuardStore) FindAccountByUsername(_ context.Context, organizationID, normalizedUsername string) (guard.Account, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	accountID, ok := s.accountByUsername[guardUsernameKey(organizationID, normalizedUsername)]
	if !ok {
		return guard.Account{}, guard.ErrNotFound
	}
	return cloneGuardAccount(s.accounts[accountID]), nil
}

func (s *MemoryGuardStore) UpdatePasswordHash(_ context.Context, accountID, passwordHash string, updatedAt time.Time) error {
	if passwordHash == "" || updatedAt.IsZero() {
		return errors.New("password hash and update time are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	account, ok := s.accounts[accountID]
	if !ok {
		return guard.ErrNotFound
	}
	account.PasswordHash = passwordHash
	account.UpdatedAt = updatedAt
	s.accounts[accountID] = account
	return nil
}

func (s *MemoryGuardStore) ProvisionExternalAccount(_ context.Context, provisioning guard.ExternalAccountProvisioning) (guard.Account, bool, error) {
	if err := provisioning.Validate(); err != nil {
		return guard.Account{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var administratorRoleID string
	if provisioning.Administrator {
		for _, role := range s.roles {
			if role.OrganizationID == provisioning.Account.OrganizationID && role.Name == "Administrator" {
				administratorRoleID = role.ID
				break
			}
		}
		if administratorRoleID == "" {
			return guard.Account{}, false, guard.ErrNotFound
		}
	}
	identityKey := guardExternalIdentityKey(
		provisioning.Identity.OrganizationID,
		provisioning.Identity.Issuer,
		provisioning.Identity.Subject,
	)
	externalIdentity, exists := s.externalIdentities[identityKey]
	created := !exists
	account := provisioning.Account
	if exists {
		stored, ok := s.accounts[externalIdentity.AccountID]
		if !ok || stored.OrganizationID != provisioning.Account.OrganizationID {
			return guard.Account{}, false, guard.ErrNotFound
		}
		stored.Email = provisioning.Account.Email
		stored.DisplayName = provisioning.Account.DisplayName
		stored.UpdatedAt = provisioning.Account.UpdatedAt
		account = stored
		externalIdentity.UpdatedAt = provisioning.Identity.UpdatedAt
		s.externalIdentities[identityKey] = externalIdentity
	} else {
		usernameKey := guardUsernameKey(account.OrganizationID, account.NormalizedUsername)
		if _, duplicate := s.accounts[account.ID]; duplicate {
			return guard.Account{}, false, errors.New("external account already exists")
		}
		if _, duplicate := s.accountByUsername[usernameKey]; duplicate {
			return guard.Account{}, false, errors.New("external account username already exists")
		}
		s.accounts[account.ID] = cloneGuardAccount(account)
		s.accountByUsername[usernameKey] = account.ID
		s.externalIdentities[identityKey] = provisioning.Identity
	}
	assignmentKey := guardExternalAssignmentKey(account.OrganizationID, account.ID, provisioning.AssignmentSource)
	if provisioning.Administrator {
		hasAdministratorAssignment := false
		for _, assignment := range s.assignments {
			if assignment.OrganizationID == account.OrganizationID && assignment.AccountID == account.ID &&
				assignment.RoleID == administratorRoleID && assignment.Scope.Kind == guard.ScopeOrganization &&
				assignment.Scope.ResourceID == account.OrganizationID {
				hasAdministratorAssignment = true
				break
			}
		}
		if !hasAdministratorAssignment {
			s.assignments[provisioning.AdministratorAssignmentID] = guard.RoleAssignment{
				ID:             provisioning.AdministratorAssignmentID,
				OrganizationID: account.OrganizationID,
				AccountID:      account.ID,
				RoleID:         administratorRoleID,
				Scope: guard.Scope{
					Kind:           guard.ScopeOrganization,
					OrganizationID: account.OrganizationID,
					ResourceID:     account.OrganizationID,
				},
				Source:    provisioning.AssignmentSource,
				CreatedAt: provisioning.Account.UpdatedAt,
			}
			s.externalAssignments[assignmentKey] = provisioning.AdministratorAssignmentID
		}
	} else if assignmentID := s.externalAssignments[assignmentKey]; assignmentID != "" {
		delete(s.assignments, assignmentID)
		delete(s.externalAssignments, assignmentKey)
	}
	s.accounts[account.ID] = cloneGuardAccount(account)
	return cloneGuardAccount(account), created, nil
}

func (s *MemoryGuardStore) AccessForAccount(_ context.Context, organizationID, accountID string) (guard.Access, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	account, ok := s.accounts[accountID]
	if !ok || account.OrganizationID != organizationID {
		return guard.Access{}, guard.ErrNotFound
	}
	accessAccount := cloneGuardAccount(account)
	accessAccount.PasswordHash = ""
	access := guard.Access{Account: accessAccount}
	seenGrants := make(map[string]struct{})
	for _, assignment := range s.assignments {
		if assignment.OrganizationID != organizationID || assignment.AccountID != accountID {
			continue
		}
		role, exists := s.roles[assignment.RoleID]
		if !exists {
			continue
		}
		access.Roles = append(access.Roles, cloneGuardRole(role))
		permissions := append([]guard.Permission(nil), role.Permissions...)
		for _, bundleID := range role.PolicyBundleIDs {
			permissions = append(permissions, s.bundles[bundleID].Permissions...)
		}
		for _, permission := range permissions {
			key := string(permission) + "\x00" + string(assignment.Scope.Kind) + "\x00" + assignment.Scope.OrganizationID + "\x00" + assignment.Scope.ResourceID
			if _, exists := seenGrants[key]; exists {
				continue
			}
			seenGrants[key] = struct{}{}
			access.Grants = append(access.Grants, guard.Grant{Permission: permission, Scope: assignment.Scope})
		}
	}
	sort.Slice(access.Roles, func(i, j int) bool { return access.Roles[i].Name < access.Roles[j].Name })
	sort.Slice(access.Grants, func(i, j int) bool {
		if access.Grants[i].Permission == access.Grants[j].Permission {
			return access.Grants[i].Scope.ResourceID < access.Grants[j].Scope.ResourceID
		}
		return access.Grants[i].Permission < access.Grants[j].Permission
	})
	return access, nil
}

func (s *MemoryGuardStore) ListAuthorization(_ context.Context, organizationID string) (guard.AuthorizationDirectory, error) {
	if organizationID == "" {
		return guard.AuthorizationDirectory{}, errors.New("organization id is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	directory := guard.AuthorizationDirectory{}
	for _, account := range s.accounts {
		if account.OrganizationID != organizationID {
			continue
		}
		account = cloneGuardAccount(account)
		account.PasswordHash = ""
		directory.Accounts = append(directory.Accounts, account)
	}
	for _, role := range s.roles {
		if role.OrganizationID == organizationID {
			directory.Roles = append(directory.Roles, cloneGuardRole(role))
		}
	}
	for _, assignment := range s.assignments {
		if assignment.OrganizationID == organizationID {
			directory.Assignments = append(directory.Assignments, assignment)
		}
	}
	sort.Slice(directory.Accounts, func(i, j int) bool {
		if directory.Accounts[i].DisplayName == directory.Accounts[j].DisplayName {
			return directory.Accounts[i].ID < directory.Accounts[j].ID
		}
		return directory.Accounts[i].DisplayName < directory.Accounts[j].DisplayName
	})
	sort.Slice(directory.Roles, func(i, j int) bool {
		if directory.Roles[i].Name == directory.Roles[j].Name {
			return directory.Roles[i].ID < directory.Roles[j].ID
		}
		return directory.Roles[i].Name < directory.Roles[j].Name
	})
	sort.Slice(directory.Assignments, func(i, j int) bool {
		if directory.Assignments[i].CreatedAt.Equal(directory.Assignments[j].CreatedAt) {
			return directory.Assignments[i].ID < directory.Assignments[j].ID
		}
		return directory.Assignments[i].CreatedAt.Before(directory.Assignments[j].CreatedAt)
	})
	return directory, nil
}

func (s *MemoryGuardStore) CreateRoleAssignment(_ context.Context, assignment guard.RoleAssignment) error {
	if err := assignment.Validate(); err != nil {
		return err
	}
	if assignment.Source != guard.LocalAssignmentSource {
		return guard.ErrManagedAssignment
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	account, accountExists := s.accounts[assignment.AccountID]
	role, roleExists := s.roles[assignment.RoleID]
	if !accountExists || account.OrganizationID != assignment.OrganizationID ||
		!roleExists || role.OrganizationID != assignment.OrganizationID {
		return guard.ErrNotFound
	}
	if _, exists := s.assignments[assignment.ID]; exists {
		return guard.ErrConflict
	}
	for _, existing := range s.assignments {
		if existing.OrganizationID == assignment.OrganizationID && existing.AccountID == assignment.AccountID &&
			existing.RoleID == assignment.RoleID && existing.Scope.Kind == assignment.Scope.Kind &&
			existing.Scope.ResourceID == assignment.Scope.ResourceID {
			return guard.ErrConflict
		}
	}
	s.assignments[assignment.ID] = assignment
	return nil
}

func (s *MemoryGuardStore) DeleteRoleAssignment(_ context.Context, organizationID, assignmentID string) (guard.RoleAssignment, error) {
	if organizationID == "" || assignmentID == "" {
		return guard.RoleAssignment{}, errors.New("organization id and assignment id are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	assignment, exists := s.assignments[assignmentID]
	if !exists || assignment.OrganizationID != organizationID {
		return guard.RoleAssignment{}, guard.ErrNotFound
	}
	if assignment.Source != guard.LocalAssignmentSource {
		return guard.RoleAssignment{}, guard.ErrManagedAssignment
	}
	if assignment.Scope.Kind == guard.ScopeOrganization && assignment.Scope.ResourceID == organizationID &&
		s.roleGrantsPermission(assignment.RoleID, guard.PermissionGuardManage) {
		remainingManagers := 0
		for _, candidate := range s.assignments {
			if candidate.ID == assignment.ID || candidate.OrganizationID != organizationID ||
				candidate.Scope.Kind != guard.ScopeOrganization || candidate.Scope.ResourceID != organizationID ||
				!s.roleGrantsPermission(candidate.RoleID, guard.PermissionGuardManage) {
				continue
			}
			if account, ok := s.accounts[candidate.AccountID]; ok && account.OrganizationID == organizationID && account.Status == "active" {
				remainingManagers++
			}
		}
		if remainingManagers == 0 {
			return guard.RoleAssignment{}, guard.ErrLastAdministrator
		}
	}
	delete(s.assignments, assignmentID)
	return assignment, nil
}

func (s *MemoryGuardStore) roleGrantsPermission(roleID string, permission guard.Permission) bool {
	role, exists := s.roles[roleID]
	if !exists {
		return false
	}
	for _, candidate := range role.Permissions {
		if candidate == permission {
			return true
		}
	}
	for _, bundleID := range role.PolicyBundleIDs {
		for _, candidate := range s.bundles[bundleID].Permissions {
			if candidate == permission {
				return true
			}
		}
	}
	return false
}

func (s *MemoryGuardStore) CreateSession(_ context.Context, session guard.Session) error {
	if session.ID == "" || session.OrganizationID == "" || session.AccountID == "" ||
		len(session.TokenHash) != 32 || len(session.CSRFHash) != 32 || session.CreatedAt.IsZero() ||
		!session.ExpiresAt.After(session.CreatedAt) {
		return errors.New("complete session identity, hashes, and lifetime are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	account, ok := s.accounts[session.AccountID]
	if !ok || account.OrganizationID != session.OrganizationID {
		return guard.ErrNotFound
	}
	tokenKey := hex.EncodeToString(session.TokenHash)
	if _, exists := s.sessions[session.ID]; exists {
		return errors.New("session already exists")
	}
	if _, exists := s.sessionByTokenHash[tokenKey]; exists {
		return errors.New("session token already exists")
	}
	s.sessions[session.ID] = cloneSession(session)
	s.sessionByTokenHash[tokenKey] = session.ID
	return nil
}

func (s *MemoryGuardStore) FindSessionByTokenHash(ctx context.Context, organizationID string, tokenHash []byte, now time.Time) (guard.Session, guard.Access, error) {
	if len(tokenHash) != 32 {
		return guard.Session{}, guard.Access{}, guard.ErrNotFound
	}
	s.mu.RLock()
	sessionID, ok := s.sessionByTokenHash[hex.EncodeToString(tokenHash)]
	session := cloneSession(s.sessions[sessionID])
	s.mu.RUnlock()
	if !ok || session.OrganizationID != organizationID || session.RevokedAt != nil || !session.ExpiresAt.After(now) || !bytes.Equal(session.TokenHash, tokenHash) {
		return guard.Session{}, guard.Access{}, guard.ErrNotFound
	}
	access, err := s.AccessForAccount(ctx, organizationID, session.AccountID)
	if err != nil {
		return guard.Session{}, guard.Access{}, err
	}
	return session, access, nil
}

func (s *MemoryGuardStore) UpdateSessionCSRF(_ context.Context, sessionID string, csrfHash []byte) error {
	if len(csrfHash) != 32 {
		return errors.New("csrf hash must contain 32 bytes")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[sessionID]
	if !ok || session.RevokedAt != nil {
		return guard.ErrNotFound
	}
	session.CSRFHash = append([]byte(nil), csrfHash...)
	s.sessions[sessionID] = session
	return nil
}

func (s *MemoryGuardStore) RevokeSession(_ context.Context, sessionID string, revokedAt time.Time) error {
	if revokedAt.IsZero() {
		return errors.New("revocation time is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[sessionID]
	if !ok {
		return guard.ErrNotFound
	}
	session.RevokedAt = &revokedAt
	s.sessions[sessionID] = session
	return nil
}

func guardUsernameKey(organizationID, normalizedUsername string) string {
	return organizationID + "\x00" + normalizedUsername
}

func guardExternalIdentityKey(organizationID, issuer, subject string) string {
	return organizationID + "\x00" + issuer + "\x00" + subject
}

func guardExternalAssignmentKey(organizationID, accountID, source string) string {
	return organizationID + "\x00" + accountID + "\x00" + source
}

func cloneGuardAccount(account guard.Account) guard.Account {
	return account
}

func clonePolicyBundle(bundle guard.PolicyBundle) guard.PolicyBundle {
	bundle.Permissions = append([]guard.Permission(nil), bundle.Permissions...)
	return bundle
}

func cloneGuardRole(role guard.Role) guard.Role {
	role.Permissions = append([]guard.Permission(nil), role.Permissions...)
	role.PolicyBundleIDs = append([]string(nil), role.PolicyBundleIDs...)
	return role
}

func cloneSession(session guard.Session) guard.Session {
	session.TokenHash = append([]byte(nil), session.TokenHash...)
	session.CSRFHash = append([]byte(nil), session.CSRFHash...)
	if session.RevokedAt != nil {
		revokedAt := *session.RevokedAt
		session.RevokedAt = &revokedAt
	}
	return session
}
