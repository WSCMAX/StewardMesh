package postgres

// Requirements: SEC-GUARD-001, SEC-HTTP-001.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/maxlemke/stewardmesh/internal/guard"
)

type GuardStore struct {
	database *sql.DB
}

var _ guard.Store = (*GuardStore)(nil)

func NewGuardStore(database *sql.DB) (*GuardStore, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	return &GuardStore{database: database}, nil
}

func (s *GuardStore) CreateSAMLRequest(ctx context.Context, request guard.SAMLRequest) error {
	if err := request.Validate(); err != nil {
		return err
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin SAML request tracking: %w", err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, `
		DELETE FROM guard_saml_requests
		WHERE organization_id = $1 AND expires_at <= $2
	`, request.OrganizationID, request.CreatedAt); err != nil {
		return fmt.Errorf("remove expired SAML requests: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO guard_saml_requests (
			organization_id, state_hash, request_id, created_at, expires_at
		) VALUES ($1, $2, $3, $4, $5)
	`, request.OrganizationID, request.StateHash, request.RequestID, request.CreatedAt, request.ExpiresAt); err != nil {
		return fmt.Errorf("track SAML request: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit SAML request tracking: %w", err)
	}
	return nil
}

func (s *GuardStore) ConsumeSAMLRequest(
	ctx context.Context,
	organizationID string,
	stateHash []byte,
	now time.Time,
) (guard.SAMLRequest, error) {
	if organizationID == "" || len(stateHash) != sha256.Size || now.IsZero() {
		return guard.SAMLRequest{}, errors.New("organization, RelayState hash, and current time are required")
	}
	row := s.database.QueryRowContext(ctx, `
		DELETE FROM guard_saml_requests
		WHERE organization_id = $1 AND state_hash = $2 AND expires_at > $3
		RETURNING organization_id, state_hash, request_id, created_at, expires_at
	`, organizationID, stateHash, now)
	var request guard.SAMLRequest
	if err := row.Scan(&request.OrganizationID, &request.StateHash, &request.RequestID, &request.CreatedAt, &request.ExpiresAt); errors.Is(err, sql.ErrNoRows) {
		return guard.SAMLRequest{}, guard.ErrNotFound
	} else if err != nil {
		return guard.SAMLRequest{}, fmt.Errorf("consume SAML request: %w", err)
	}
	return request, nil
}

func (s *GuardStore) BootstrapRequired(ctx context.Context, organizationID string) (bool, error) {
	if organizationID == "" {
		return false, errors.New("organization id is required")
	}
	var exists bool
	if err := s.database.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM guard_accounts WHERE organization_id = $1
		)
	`, organizationID).Scan(&exists); err != nil {
		return false, fmt.Errorf("read guard bootstrap state: %w", err)
	}
	return !exists, nil
}

func (s *GuardStore) BootstrapAdministrator(ctx context.Context, bootstrap guard.AdministratorBootstrap) (guard.Account, error) {
	if err := bootstrap.Validate(); err != nil {
		return guard.Account{}, err
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return guard.Account{}, fmt.Errorf("begin administrator bootstrap: %w", err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", bootstrap.Account.OrganizationID); err != nil {
		return guard.Account{}, fmt.Errorf("lock administrator bootstrap: %w", err)
	}
	var exists bool
	if err := transaction.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM guard_accounts WHERE organization_id = $1)
	`, bootstrap.Account.OrganizationID).Scan(&exists); err != nil {
		return guard.Account{}, fmt.Errorf("verify administrator bootstrap: %w", err)
	}
	if exists {
		return guard.Account{}, guard.ErrBootstrapComplete
	}
	account := bootstrap.Account
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO guard_accounts (
			id, organization_id, username, normalized_username, email,
			display_name, password_hash, status, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, account.ID, account.OrganizationID, account.Username, account.NormalizedUsername, account.Email,
		account.DisplayName, account.PasswordHash, account.Status, account.CreatedAt, account.UpdatedAt); err != nil {
		return guard.Account{}, fmt.Errorf("create administrator account: %w", err)
	}
	bundle := bootstrap.Bundle
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO guard_policy_bundles (id, organization_id, name, description)
		VALUES ($1, $2, $3, $4)
	`, bundle.ID, bundle.OrganizationID, bundle.Name, bundle.Description); err != nil {
		return guard.Account{}, fmt.Errorf("create administrator policy bundle: %w", err)
	}
	for _, permission := range bundle.Permissions {
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO guard_policy_bundle_permissions (organization_id, bundle_id, permission)
			VALUES ($1, $2, $3)
		`, bundle.OrganizationID, bundle.ID, string(permission)); err != nil {
			return guard.Account{}, fmt.Errorf("create policy bundle permission: %w", err)
		}
	}
	role := bootstrap.Role
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO guard_roles (id, organization_id, name, description, source)
		VALUES ($1, $2, $3, $4, $5)
	`, role.ID, role.OrganizationID, role.Name, role.Description, role.Source); err != nil {
		return guard.Account{}, fmt.Errorf("create administrator role: %w", err)
	}
	for _, permission := range role.Permissions {
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO guard_role_permissions (organization_id, role_id, permission)
			VALUES ($1, $2, $3)
		`, role.OrganizationID, role.ID, string(permission)); err != nil {
			return guard.Account{}, fmt.Errorf("create role permission: %w", err)
		}
	}
	for _, bundleID := range role.PolicyBundleIDs {
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO guard_role_policy_bundles (organization_id, role_id, bundle_id)
			VALUES ($1, $2, $3)
		`, role.OrganizationID, role.ID, bundleID); err != nil {
			return guard.Account{}, fmt.Errorf("attach policy bundle to role: %w", err)
		}
	}
	assignment := bootstrap.Assignment
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO guard_role_assignments (
			id, organization_id, account_id, role_id, scope_kind, scope_id, created_at, source
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, assignment.ID, assignment.OrganizationID, assignment.AccountID, assignment.RoleID,
		string(assignment.Scope.Kind), assignment.Scope.ResourceID, assignment.CreatedAt, assignment.Source); err != nil {
		return guard.Account{}, fmt.Errorf("assign administrator role: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return guard.Account{}, fmt.Errorf("commit administrator bootstrap: %w", err)
	}
	return account, nil
}

func (s *GuardStore) FindAccountByUsername(ctx context.Context, organizationID, normalizedUsername string) (guard.Account, error) {
	row := s.database.QueryRowContext(ctx, `
		SELECT id, organization_id, username, normalized_username, email,
		       display_name, COALESCE(password_hash, ''), status, created_at, updated_at
		FROM guard_accounts
		WHERE organization_id = $1 AND normalized_username = $2
	`, organizationID, normalizedUsername)
	account, err := scanGuardAccount(row)
	if errors.Is(err, sql.ErrNoRows) {
		return guard.Account{}, guard.ErrNotFound
	}
	if err != nil {
		return guard.Account{}, fmt.Errorf("find guard account: %w", err)
	}
	return account, nil
}

func (s *GuardStore) ProvisionExternalAccount(ctx context.Context, provisioning guard.ExternalAccountProvisioning) (guard.Account, bool, error) {
	if err := provisioning.Validate(); err != nil {
		return guard.Account{}, false, err
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return guard.Account{}, false, fmt.Errorf("begin external account provisioning: %w", err)
	}
	defer transaction.Rollback()
	identity := provisioning.Identity
	lockDigest := sha256.Sum256([]byte(identity.OrganizationID + "\x00" + identity.Issuer + "\x00" + identity.Subject))
	lockKey := fmt.Sprintf("%x", lockDigest[:])
	if _, err := transaction.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", lockKey); err != nil {
		return guard.Account{}, false, fmt.Errorf("lock external account provisioning: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", identity.OrganizationID); err != nil {
		return guard.Account{}, false, fmt.Errorf("lock organization role assignments: %w", err)
	}
	var administratorRoleID string
	if provisioning.Administrator {
		err := transaction.QueryRowContext(ctx, `
			SELECT id FROM guard_roles WHERE organization_id = $1 AND name = 'Administrator'
		`, identity.OrganizationID).Scan(&administratorRoleID)
		if errors.Is(err, sql.ErrNoRows) {
			return guard.Account{}, false, guard.ErrNotFound
		}
		if err != nil {
			return guard.Account{}, false, fmt.Errorf("load administrator role: %w", err)
		}
	}
	row := transaction.QueryRowContext(ctx, `
		SELECT a.id, a.organization_id, a.username, a.normalized_username, a.email,
		       a.display_name, COALESCE(a.password_hash, ''), a.status, a.created_at, a.updated_at
		FROM guard_external_identities i
		JOIN guard_accounts a
		  ON a.organization_id = i.organization_id AND a.id = i.account_id
		WHERE i.organization_id = $1 AND i.issuer = $2 AND i.subject = $3
	`, identity.OrganizationID, identity.Issuer, identity.Subject)
	account, err := scanGuardAccount(row)
	created := false
	switch {
	case errors.Is(err, sql.ErrNoRows):
		account = provisioning.Account
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO guard_accounts (
				id, organization_id, username, normalized_username, email,
				display_name, password_hash, status, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, NULL, $7, $8, $9)
		`, account.ID, account.OrganizationID, account.Username, account.NormalizedUsername, account.Email,
			account.DisplayName, account.Status, account.CreatedAt, account.UpdatedAt); err != nil {
			return guard.Account{}, false, fmt.Errorf("create external account: %w", err)
		}
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO guard_external_identities (
				organization_id, issuer, subject, account_id, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6)
		`, identity.OrganizationID, identity.Issuer, identity.Subject, account.ID, identity.CreatedAt, identity.UpdatedAt); err != nil {
			return guard.Account{}, false, fmt.Errorf("bind external identity: %w", err)
		}
		created = true
	case err != nil:
		return guard.Account{}, false, fmt.Errorf("find external account: %w", err)
	default:
		account.Email = provisioning.Account.Email
		account.DisplayName = provisioning.Account.DisplayName
		account.UpdatedAt = provisioning.Account.UpdatedAt
		if _, err := transaction.ExecContext(ctx, `
			UPDATE guard_accounts
			SET email = $2, display_name = $3, updated_at = $4
			WHERE id = $1
		`, account.ID, account.Email, account.DisplayName, account.UpdatedAt); err != nil {
			return guard.Account{}, false, fmt.Errorf("refresh external account: %w", err)
		}
		if _, err := transaction.ExecContext(ctx, `
			UPDATE guard_external_identities SET updated_at = $4
			WHERE organization_id = $1 AND issuer = $2 AND subject = $3
		`, identity.OrganizationID, identity.Issuer, identity.Subject, identity.UpdatedAt); err != nil {
			return guard.Account{}, false, fmt.Errorf("refresh external identity: %w", err)
		}
	}
	if provisioning.Administrator {
		var assignmentExists bool
		if err := transaction.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM guard_role_assignments
				WHERE organization_id = $1 AND account_id = $2 AND role_id = $3
				  AND scope_kind = 'organization' AND scope_id = $1
			)
		`, account.OrganizationID, account.ID, administratorRoleID).Scan(&assignmentExists); err != nil {
			return guard.Account{}, false, fmt.Errorf("read external administrator mapping: %w", err)
		}
		if !assignmentExists {
			if _, err := transaction.ExecContext(ctx, `
				INSERT INTO guard_role_assignments (
					id, organization_id, account_id, role_id, scope_kind, scope_id, created_at, source
				) VALUES ($1, $2, $3, $4, 'organization', $2, $5, $6)
			`, provisioning.AdministratorAssignmentID, account.OrganizationID, account.ID, administratorRoleID,
				provisioning.Account.UpdatedAt, provisioning.AssignmentSource); err != nil {
				return guard.Account{}, false, fmt.Errorf("map external administrator: %w", err)
			}
		}
	} else {
		if _, err := transaction.ExecContext(ctx, `
			DELETE FROM guard_role_assignments
			WHERE organization_id = $1 AND account_id = $2 AND source = $3
		`, account.OrganizationID, account.ID, provisioning.AssignmentSource); err != nil {
			return guard.Account{}, false, fmt.Errorf("remove external administrator mapping: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return guard.Account{}, false, fmt.Errorf("commit external account provisioning: %w", err)
	}
	return account, created, nil
}

func (s *GuardStore) UpdatePasswordHash(ctx context.Context, accountID, passwordHash string, updatedAt time.Time) error {
	result, err := s.database.ExecContext(ctx, `
		UPDATE guard_accounts SET password_hash = $2, updated_at = $3 WHERE id = $1
	`, accountID, passwordHash, updatedAt)
	if err != nil {
		return fmt.Errorf("update password hash: %w", err)
	}
	return requireAffected(result)
}

func (s *GuardStore) AccessForAccount(ctx context.Context, organizationID, accountID string) (guard.Access, error) {
	row := s.database.QueryRowContext(ctx, `
		SELECT id, organization_id, username, normalized_username, email,
		       display_name, COALESCE(password_hash, ''), status, created_at, updated_at
		FROM guard_accounts
		WHERE organization_id = $1 AND id = $2
	`, organizationID, accountID)
	account, err := scanGuardAccount(row)
	if errors.Is(err, sql.ErrNoRows) {
		return guard.Access{}, guard.ErrNotFound
	}
	if err != nil {
		return guard.Access{}, fmt.Errorf("load access account: %w", err)
	}
	account.PasswordHash = ""
	rows, err := s.database.QueryContext(ctx, `
		SELECT r.id, r.organization_id, r.name, r.description, r.source,
		       a.scope_kind, a.scope_id,
		       permissions.permission, permissions.source, permissions.bundle_id
		FROM guard_role_assignments a
		JOIN guard_roles r
		  ON r.organization_id = a.organization_id AND r.id = a.role_id
		LEFT JOIN LATERAL (
			SELECT rp.permission, 'role'::TEXT AS source, ''::TEXT AS bundle_id
			FROM guard_role_permissions rp
			WHERE rp.organization_id = r.organization_id AND rp.role_id = r.id
			UNION ALL
			SELECT bp.permission, 'bundle'::TEXT AS source, rb.bundle_id
			FROM guard_role_policy_bundles rb
			JOIN guard_policy_bundle_permissions bp
			  ON bp.organization_id = rb.organization_id AND bp.bundle_id = rb.bundle_id
			WHERE rb.organization_id = r.organization_id AND rb.role_id = r.id
		) permissions ON TRUE
		WHERE a.organization_id = $1 AND a.account_id = $2
		ORDER BY r.name, permissions.permission
	`, organizationID, accountID)
	if err != nil {
		return guard.Access{}, fmt.Errorf("load account grants: %w", err)
	}
	defer rows.Close()
	access := guard.Access{Account: account}
	roles := make(map[string]*guard.Role)
	roleOrder := make([]string, 0)
	seenBundles := make(map[string]map[string]struct{})
	seenGrants := make(map[string]struct{})
	for rows.Next() {
		var (
			roleID, roleOrganizationID, roleName, roleDescription string
			scopeKind, scopeID                                    string
			permission, source, bundleID                          sql.NullString
		)
		var roleSource string
		if err := rows.Scan(&roleID, &roleOrganizationID, &roleName, &roleDescription, &roleSource,
			&scopeKind, &scopeID, &permission, &source, &bundleID); err != nil {
			return guard.Access{}, fmt.Errorf("scan account grant: %w", err)
		}
		role, exists := roles[roleID]
		if !exists {
			role = &guard.Role{ID: roleID, OrganizationID: roleOrganizationID, Name: roleName, Description: roleDescription, Source: roleSource}
			roles[roleID] = role
			roleOrder = append(roleOrder, roleID)
			seenBundles[roleID] = make(map[string]struct{})
		}
		if !permission.Valid {
			continue
		}
		permissionValue := guard.Permission(permission.String)
		if source.String == "role" {
			role.Permissions = appendUniquePermission(role.Permissions, permissionValue)
		} else if source.String == "bundle" && bundleID.String != "" {
			if _, exists := seenBundles[roleID][bundleID.String]; !exists {
				seenBundles[roleID][bundleID.String] = struct{}{}
				role.PolicyBundleIDs = append(role.PolicyBundleIDs, bundleID.String)
			}
		}
		scope := guard.Scope{Kind: guard.ScopeKind(scopeKind), OrganizationID: organizationID, ResourceID: scopeID}
		grantKey := string(permissionValue) + "\x00" + scopeKind + "\x00" + scopeID
		if _, exists := seenGrants[grantKey]; !exists {
			seenGrants[grantKey] = struct{}{}
			access.Grants = append(access.Grants, guard.Grant{Permission: permissionValue, Scope: scope})
		}
	}
	if err := rows.Err(); err != nil {
		return guard.Access{}, fmt.Errorf("iterate account grants: %w", err)
	}
	for _, roleID := range roleOrder {
		role := *roles[roleID]
		sort.Slice(role.Permissions, func(i, j int) bool { return role.Permissions[i] < role.Permissions[j] })
		sort.Strings(role.PolicyBundleIDs)
		access.Roles = append(access.Roles, role)
	}
	sort.Slice(access.Grants, func(i, j int) bool {
		if access.Grants[i].Permission == access.Grants[j].Permission {
			return access.Grants[i].Scope.ResourceID < access.Grants[j].Scope.ResourceID
		}
		return access.Grants[i].Permission < access.Grants[j].Permission
	})
	guard.AugmentAccessGrants(organizationID, &access)
	return access, nil
}

func (s *GuardStore) ListAuthorization(ctx context.Context, organizationID string) (guard.AuthorizationDirectory, error) {
	if organizationID == "" {
		return guard.AuthorizationDirectory{}, errors.New("organization id is required")
	}
	directory := guard.AuthorizationDirectory{}
	accountRows, err := s.database.QueryContext(ctx, `
		SELECT id, organization_id, username, normalized_username, email,
		       display_name, ''::TEXT, status, created_at, updated_at
		FROM guard_accounts
		WHERE organization_id = $1
		ORDER BY display_name, id
	`, organizationID)
	if err != nil {
		return guard.AuthorizationDirectory{}, fmt.Errorf("list guard accounts: %w", err)
	}
	for accountRows.Next() {
		account, scanErr := scanGuardAccount(accountRows)
		if scanErr != nil {
			accountRows.Close()
			return guard.AuthorizationDirectory{}, fmt.Errorf("scan guard account: %w", scanErr)
		}
		directory.Accounts = append(directory.Accounts, account)
	}
	if err := accountRows.Err(); err != nil {
		accountRows.Close()
		return guard.AuthorizationDirectory{}, fmt.Errorf("iterate guard accounts: %w", err)
	}
	accountRows.Close()

	roleRows, err := s.database.QueryContext(ctx, `
		SELECT r.id, r.organization_id, r.name, r.description, r.source,
		       rp.permission, rb.bundle_id
		FROM guard_roles r
		LEFT JOIN guard_role_permissions rp
		  ON rp.organization_id = r.organization_id AND rp.role_id = r.id
		LEFT JOIN guard_role_policy_bundles rb
		  ON rb.organization_id = r.organization_id AND rb.role_id = r.id
		WHERE r.organization_id = $1
		ORDER BY r.name, r.id, rp.permission, rb.bundle_id
	`, organizationID)
	if err != nil {
		return guard.AuthorizationDirectory{}, fmt.Errorf("list guard roles: %w", err)
	}
	roleIndexes := make(map[string]int)
	for roleRows.Next() {
		var role guard.Role
		var permission, bundleID sql.NullString
		if err := roleRows.Scan(&role.ID, &role.OrganizationID, &role.Name, &role.Description, &role.Source, &permission, &bundleID); err != nil {
			roleRows.Close()
			return guard.AuthorizationDirectory{}, fmt.Errorf("scan guard role: %w", err)
		}
		index, exists := roleIndexes[role.ID]
		if !exists {
			index = len(directory.Roles)
			roleIndexes[role.ID] = index
			directory.Roles = append(directory.Roles, role)
		}
		if permission.Valid {
			directory.Roles[index].Permissions = appendUniquePermission(directory.Roles[index].Permissions, guard.Permission(permission.String))
		}
		if bundleID.Valid {
			directory.Roles[index].PolicyBundleIDs = appendUniqueString(directory.Roles[index].PolicyBundleIDs, bundleID.String)
		}
	}
	if err := roleRows.Err(); err != nil {
		roleRows.Close()
		return guard.AuthorizationDirectory{}, fmt.Errorf("iterate guard roles: %w", err)
	}
	roleRows.Close()

	bundleRows, err := s.database.QueryContext(ctx, `
		SELECT b.id, b.organization_id, b.name, b.description, bp.permission
		FROM guard_policy_bundles b
		LEFT JOIN guard_policy_bundle_permissions bp
		  ON bp.organization_id = b.organization_id AND bp.bundle_id = b.id
		WHERE b.organization_id = $1
		ORDER BY b.name, b.id, bp.permission
	`, organizationID)
	if err != nil {
		return guard.AuthorizationDirectory{}, fmt.Errorf("list guard policy bundles: %w", err)
	}
	bundleIndexes := make(map[string]int)
	for bundleRows.Next() {
		var bundle guard.PolicyBundle
		var permission sql.NullString
		if err := bundleRows.Scan(&bundle.ID, &bundle.OrganizationID, &bundle.Name, &bundle.Description, &permission); err != nil {
			bundleRows.Close()
			return guard.AuthorizationDirectory{}, fmt.Errorf("scan guard policy bundle: %w", err)
		}
		index, exists := bundleIndexes[bundle.ID]
		if !exists {
			index = len(directory.PolicyBundles)
			bundleIndexes[bundle.ID] = index
			directory.PolicyBundles = append(directory.PolicyBundles, bundle)
		}
		if permission.Valid {
			directory.PolicyBundles[index].Permissions = appendUniquePermission(
				directory.PolicyBundles[index].Permissions, guard.Permission(permission.String),
			)
		}
	}
	if err := bundleRows.Err(); err != nil {
		bundleRows.Close()
		return guard.AuthorizationDirectory{}, fmt.Errorf("iterate guard policy bundles: %w", err)
	}
	bundleRows.Close()

	assignmentRows, err := s.database.QueryContext(ctx, `
		SELECT id, organization_id, account_id, role_id, scope_kind, scope_id, source, created_at
		FROM guard_role_assignments
		WHERE organization_id = $1
		ORDER BY created_at, id
	`, organizationID)
	if err != nil {
		return guard.AuthorizationDirectory{}, fmt.Errorf("list guard role assignments: %w", err)
	}
	defer assignmentRows.Close()
	for assignmentRows.Next() {
		var assignment guard.RoleAssignment
		var scopeKind string
		if err := assignmentRows.Scan(&assignment.ID, &assignment.OrganizationID, &assignment.AccountID,
			&assignment.RoleID, &scopeKind, &assignment.Scope.ResourceID, &assignment.Source, &assignment.CreatedAt); err != nil {
			return guard.AuthorizationDirectory{}, fmt.Errorf("scan guard role assignment: %w", err)
		}
		assignment.Scope.Kind = guard.ScopeKind(scopeKind)
		assignment.Scope.OrganizationID = assignment.OrganizationID
		directory.Assignments = append(directory.Assignments, assignment)
	}
	if err := assignmentRows.Err(); err != nil {
		return guard.AuthorizationDirectory{}, fmt.Errorf("iterate guard role assignments: %w", err)
	}
	return directory, nil
}

func (s *GuardStore) CreateRole(ctx context.Context, role guard.Role) error {
	if err := role.Validate(); err != nil {
		return err
	}
	if role.Source != guard.LocalRoleSource {
		return guard.ErrBuiltInRole
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin role creation: %w", err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", role.OrganizationID); err != nil {
		return fmt.Errorf("lock organization roles: %w", err)
	}
	for _, bundleID := range role.PolicyBundleIDs {
		var exists bool
		if err := transaction.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM guard_policy_bundles WHERE organization_id = $1 AND id = $2
			)
		`, role.OrganizationID, bundleID).Scan(&exists); err != nil {
			return fmt.Errorf("verify role policy bundle: %w", err)
		}
		if !exists {
			return guard.ErrNotFound
		}
	}
	result, err := transaction.ExecContext(ctx, `
		INSERT INTO guard_roles (id, organization_id, name, description, source)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT DO NOTHING
	`, role.ID, role.OrganizationID, role.Name, role.Description, role.Source)
	if err != nil {
		return fmt.Errorf("create role: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read created role: %w", err)
	}
	if count == 0 {
		return guard.ErrConflict
	}
	for _, permission := range role.Permissions {
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO guard_role_permissions (organization_id, role_id, permission)
			VALUES ($1, $2, $3)
		`, role.OrganizationID, role.ID, string(permission)); err != nil {
			return fmt.Errorf("create role permission: %w", err)
		}
	}
	for _, bundleID := range role.PolicyBundleIDs {
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO guard_role_policy_bundles (organization_id, role_id, bundle_id)
			VALUES ($1, $2, $3)
		`, role.OrganizationID, role.ID, bundleID); err != nil {
			return fmt.Errorf("attach role policy bundle: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit role creation: %w", err)
	}
	return nil
}

func (s *GuardStore) DeleteRole(ctx context.Context, organizationID, roleID string) error {
	if organizationID == "" || roleID == "" {
		return errors.New("organization id and role id are required")
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin role deletion: %w", err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", organizationID); err != nil {
		return fmt.Errorf("lock organization roles: %w", err)
	}
	var source string
	err = transaction.QueryRowContext(ctx, `
		SELECT source FROM guard_roles WHERE organization_id = $1 AND id = $2 FOR UPDATE
	`, organizationID, roleID).Scan(&source)
	if errors.Is(err, sql.ErrNoRows) {
		return guard.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("load role for deletion: %w", err)
	}
	if source != guard.LocalRoleSource {
		return guard.ErrBuiltInRole
	}
	var assigned bool
	if err := transaction.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM guard_role_assignments WHERE organization_id = $1 AND role_id = $2
		)
	`, organizationID, roleID).Scan(&assigned); err != nil {
		return fmt.Errorf("inspect role assignments: %w", err)
	}
	if assigned {
		return guard.ErrConflict
	}
	if _, err := transaction.ExecContext(ctx, `
		DELETE FROM guard_roles WHERE organization_id = $1 AND id = $2
	`, organizationID, roleID); err != nil {
		return fmt.Errorf("delete role: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit role deletion: %w", err)
	}
	return nil
}

func (s *GuardStore) CreateRoleAssignment(ctx context.Context, assignment guard.RoleAssignment) error {
	if err := assignment.Validate(); err != nil {
		return err
	}
	if assignment.Source != guard.LocalAssignmentSource {
		return guard.ErrManagedAssignment
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin role assignment creation: %w", err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", assignment.OrganizationID); err != nil {
		return fmt.Errorf("lock organization role assignments: %w", err)
	}
	var referencesExist bool
	if err := transaction.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM guard_accounts a, guard_roles r
			WHERE a.organization_id = $1 AND a.id = $2
			  AND r.organization_id = $1 AND r.id = $3
		)
	`, assignment.OrganizationID, assignment.AccountID, assignment.RoleID).Scan(&referencesExist); err != nil {
		return fmt.Errorf("verify role assignment references: %w", err)
	}
	if !referencesExist {
		return guard.ErrNotFound
	}
	result, err := transaction.ExecContext(ctx, `
		INSERT INTO guard_role_assignments (
			id, organization_id, account_id, role_id, scope_kind, scope_id, created_at, source
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT DO NOTHING
	`, assignment.ID, assignment.OrganizationID, assignment.AccountID, assignment.RoleID,
		string(assignment.Scope.Kind), assignment.Scope.ResourceID, assignment.CreatedAt, assignment.Source)
	if err != nil {
		return fmt.Errorf("create role assignment: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read created role assignment: %w", err)
	}
	if count == 0 {
		return guard.ErrConflict
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit role assignment creation: %w", err)
	}
	return nil
}

func (s *GuardStore) DeleteRoleAssignment(ctx context.Context, organizationID, assignmentID string) (guard.RoleAssignment, error) {
	if organizationID == "" || assignmentID == "" {
		return guard.RoleAssignment{}, errors.New("organization id and assignment id are required")
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return guard.RoleAssignment{}, fmt.Errorf("begin role assignment deletion: %w", err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", organizationID); err != nil {
		return guard.RoleAssignment{}, fmt.Errorf("lock organization role assignments: %w", err)
	}
	var assignment guard.RoleAssignment
	var scopeKind string
	err = transaction.QueryRowContext(ctx, `
		SELECT id, organization_id, account_id, role_id, scope_kind, scope_id, source, created_at
		FROM guard_role_assignments
		WHERE organization_id = $1 AND id = $2
		FOR UPDATE
	`, organizationID, assignmentID).Scan(&assignment.ID, &assignment.OrganizationID, &assignment.AccountID,
		&assignment.RoleID, &scopeKind, &assignment.Scope.ResourceID, &assignment.Source, &assignment.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return guard.RoleAssignment{}, guard.ErrNotFound
	}
	if err != nil {
		return guard.RoleAssignment{}, fmt.Errorf("load role assignment for deletion: %w", err)
	}
	assignment.Scope = guard.Scope{Kind: guard.ScopeKind(scopeKind), OrganizationID: organizationID, ResourceID: assignment.Scope.ResourceID}
	if assignment.Source != guard.LocalAssignmentSource {
		return guard.RoleAssignment{}, guard.ErrManagedAssignment
	}
	if assignment.Scope.Kind == guard.ScopeOrganization && assignment.Scope.ResourceID == organizationID {
		var grantsManagement bool
		if err := transaction.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM guard_role_permissions
				WHERE organization_id = $1 AND role_id = $2 AND permission = $3
				UNION ALL
				SELECT 1
				FROM guard_role_policy_bundles rb
				JOIN guard_policy_bundle_permissions bp
				  ON bp.organization_id = rb.organization_id AND bp.bundle_id = rb.bundle_id
				WHERE rb.organization_id = $1 AND rb.role_id = $2 AND bp.permission = $3
			)
		`, organizationID, assignment.RoleID, string(guard.PermissionGuardManage)).Scan(&grantsManagement); err != nil {
			return guard.RoleAssignment{}, fmt.Errorf("inspect role assignment permissions: %w", err)
		}
		if grantsManagement {
			var remainingManagers int
			if err := transaction.QueryRowContext(ctx, `
				SELECT COUNT(*)
				FROM guard_role_assignments a
				JOIN guard_accounts account
				  ON account.organization_id = a.organization_id AND account.id = a.account_id
				WHERE a.organization_id = $1 AND a.id <> $2
				  AND a.scope_kind = 'organization' AND a.scope_id = $1
				  AND account.status = 'active'
				  AND EXISTS (
					SELECT 1 FROM guard_role_permissions rp
					WHERE rp.organization_id = a.organization_id AND rp.role_id = a.role_id AND rp.permission = $3
					UNION ALL
					SELECT 1
					FROM guard_role_policy_bundles rb
					JOIN guard_policy_bundle_permissions bp
					  ON bp.organization_id = rb.organization_id AND bp.bundle_id = rb.bundle_id
					WHERE rb.organization_id = a.organization_id AND rb.role_id = a.role_id AND bp.permission = $3
				  )
			`, organizationID, assignment.ID, string(guard.PermissionGuardManage)).Scan(&remainingManagers); err != nil {
				return guard.RoleAssignment{}, fmt.Errorf("count organization administrators: %w", err)
			}
			if remainingManagers == 0 {
				return guard.RoleAssignment{}, guard.ErrLastAdministrator
			}
		}
	}
	if _, err := transaction.ExecContext(ctx, `
		DELETE FROM guard_role_assignments WHERE organization_id = $1 AND id = $2
	`, organizationID, assignment.ID); err != nil {
		return guard.RoleAssignment{}, fmt.Errorf("delete role assignment: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return guard.RoleAssignment{}, fmt.Errorf("commit role assignment deletion: %w", err)
	}
	return assignment, nil
}

func (s *GuardStore) RegisterResourceOwnership(ctx context.Context, ownership guard.ResourceOwnership) (guard.ResourceOwnership, bool, error) {
	if err := ownership.Validate(); err != nil {
		return guard.ResourceOwnership{}, false, err
	}
	if !ownership.WriteLocked {
		return guard.ResourceOwnership{}, false, errors.New("new resource ownership must be write-locked")
	}
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return guard.ResourceOwnership{}, false, fmt.Errorf("begin resource ownership registration: %w", err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", ownership.OrganizationID); err != nil {
		return guard.ResourceOwnership{}, false, fmt.Errorf("lock organization resource ownership: %w", err)
	}
	existing, err := scanResourceOwnership(transaction.QueryRowContext(ctx, `
		SELECT organization_id, resource_type, resource_id, source_system_id, source_record_id,
		       write_locked, registered_at, claimed_by, claimed_at
		FROM guard_resource_ownership
		WHERE organization_id = $1 AND resource_type = $2 AND resource_id = $3
	`, ownership.OrganizationID, ownership.ResourceType, ownership.ResourceID))
	if err == nil {
		if existing.SourceSystemID != ownership.SourceSystemID || existing.SourceRecordID != ownership.SourceRecordID {
			return guard.ResourceOwnership{}, false, guard.ErrConflict
		}
		return existing, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return guard.ResourceOwnership{}, false, fmt.Errorf("read existing resource ownership: %w", err)
	}
	result, err := transaction.ExecContext(ctx, `
		INSERT INTO guard_resource_ownership (
			organization_id, resource_type, resource_id, source_system_id, source_record_id,
			write_locked, registered_at
		) VALUES ($1, $2, $3, $4, $5, TRUE, $6)
		ON CONFLICT DO NOTHING
	`, ownership.OrganizationID, ownership.ResourceType, ownership.ResourceID,
		ownership.SourceSystemID, ownership.SourceRecordID, ownership.RegisteredAt)
	if err != nil {
		return guard.ResourceOwnership{}, false, fmt.Errorf("register resource ownership: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return guard.ResourceOwnership{}, false, fmt.Errorf("read registered resource ownership: %w", err)
	}
	if count == 0 {
		return guard.ResourceOwnership{}, false, guard.ErrConflict
	}
	if err := transaction.Commit(); err != nil {
		return guard.ResourceOwnership{}, false, fmt.Errorf("commit resource ownership registration: %w", err)
	}
	return ownership, true, nil
}

func (s *GuardStore) ListResourceOwnership(ctx context.Context, organizationID string) ([]guard.ResourceOwnership, error) {
	if organizationID == "" {
		return nil, errors.New("organization id is required")
	}
	rows, err := s.database.QueryContext(ctx, `
		SELECT organization_id, resource_type, resource_id, source_system_id, source_record_id,
		       write_locked, registered_at, claimed_by, claimed_at
		FROM guard_resource_ownership
		WHERE organization_id = $1
		ORDER BY registered_at, resource_type, resource_id
	`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list resource ownership: %w", err)
	}
	defer rows.Close()
	result := make([]guard.ResourceOwnership, 0)
	for rows.Next() {
		ownership, err := scanResourceOwnership(rows)
		if err != nil {
			return nil, fmt.Errorf("scan resource ownership: %w", err)
		}
		result = append(result, ownership)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate resource ownership: %w", err)
	}
	return result, nil
}

func (s *GuardStore) GetResourceOwnership(ctx context.Context, organizationID, resourceType, resourceID string) (guard.ResourceOwnership, error) {
	if organizationID == "" || resourceType == "" || resourceID == "" {
		return guard.ResourceOwnership{}, errors.New("organization, resource type, and resource id are required")
	}
	ownership, err := scanResourceOwnership(s.database.QueryRowContext(ctx, `
		SELECT organization_id, resource_type, resource_id, source_system_id, source_record_id,
		       write_locked, registered_at, claimed_by, claimed_at
		FROM guard_resource_ownership
		WHERE organization_id = $1 AND resource_type = $2 AND resource_id = $3
	`, organizationID, resourceType, resourceID))
	if errors.Is(err, sql.ErrNoRows) {
		return guard.ResourceOwnership{}, guard.ErrNotFound
	}
	if err != nil {
		return guard.ResourceOwnership{}, fmt.Errorf("get resource ownership: %w", err)
	}
	return ownership, nil
}

func (s *GuardStore) ClaimResourceOwnership(ctx context.Context, organizationID, resourceType, resourceID, claimedBy string, claimedAt time.Time) (guard.ResourceOwnership, error) {
	if organizationID == "" || resourceType == "" || resourceID == "" || claimedBy == "" || claimedAt.IsZero() {
		return guard.ResourceOwnership{}, errors.New("complete ownership claim is required")
	}
	ownership, err := scanResourceOwnership(s.database.QueryRowContext(ctx, `
		UPDATE guard_resource_ownership ownership
		SET write_locked = FALSE, claimed_by = $4, claimed_at = $5
		WHERE ownership.organization_id = $1 AND ownership.resource_type = $2 AND ownership.resource_id = $3
		  AND ownership.write_locked
		  AND EXISTS (
			SELECT 1 FROM guard_accounts account
			WHERE account.organization_id = $1 AND account.id = $4 AND account.status = 'active'
		  )
		RETURNING organization_id, resource_type, resource_id, source_system_id, source_record_id,
		          write_locked, registered_at, claimed_by, claimed_at
	`, organizationID, resourceType, resourceID, claimedBy, claimedAt))
	if err == nil {
		return ownership, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return guard.ResourceOwnership{}, fmt.Errorf("claim resource ownership: %w", err)
	}
	existing, loadErr := s.GetResourceOwnership(ctx, organizationID, resourceType, resourceID)
	if loadErr != nil {
		return guard.ResourceOwnership{}, loadErr
	}
	if !existing.WriteLocked {
		return guard.ResourceOwnership{}, guard.ErrConflict
	}
	return guard.ResourceOwnership{}, guard.ErrNotFound
}

func (s *GuardStore) DeleteResourceOwnership(ctx context.Context, ownership guard.ResourceOwnership) error {
	if err := ownership.Validate(); err != nil || !ownership.WriteLocked {
		return errors.New("valid write-locked ownership is required")
	}
	result, err := s.database.ExecContext(ctx, `
		DELETE FROM guard_resource_ownership
		WHERE organization_id = $1 AND resource_type = $2 AND resource_id = $3
		  AND source_system_id = $4 AND source_record_id = $5
		  AND write_locked AND claimed_by IS NULL AND claimed_at IS NULL
	`, ownership.OrganizationID, ownership.ResourceType, ownership.ResourceID,
		ownership.SourceSystemID, ownership.SourceRecordID)
	if err != nil {
		return fmt.Errorf("delete resource ownership: %w", err)
	}
	return requireAffected(result)
}

func (s *GuardStore) RestoreResourceOwnershipLock(ctx context.Context, claimed guard.ResourceOwnership) error {
	if err := claimed.Validate(); err != nil || claimed.WriteLocked || claimed.ClaimedAt == nil {
		return errors.New("valid claimed ownership is required")
	}
	result, err := s.database.ExecContext(ctx, `
		UPDATE guard_resource_ownership
		SET write_locked = TRUE, claimed_by = NULL, claimed_at = NULL
		WHERE organization_id = $1 AND resource_type = $2 AND resource_id = $3
		  AND source_system_id = $4 AND source_record_id = $5
		  AND NOT write_locked AND claimed_by = $6 AND claimed_at = $7
	`, claimed.OrganizationID, claimed.ResourceType, claimed.ResourceID,
		claimed.SourceSystemID, claimed.SourceRecordID, claimed.ClaimedBy, *claimed.ClaimedAt)
	if err != nil {
		return fmt.Errorf("restore resource ownership lock: %w", err)
	}
	return requireAffected(result)
}

func (s *GuardStore) CreateSession(ctx context.Context, session guard.Session) error {
	if len(session.TokenHash) != 32 || len(session.CSRFHash) != 32 {
		return errors.New("session hashes must contain 32 bytes")
	}
	if _, err := s.database.ExecContext(ctx, `
		INSERT INTO guard_sessions (
			id, organization_id, account_id, token_hash, csrf_hash, created_at, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, session.ID, session.OrganizationID, session.AccountID, session.TokenHash, session.CSRFHash,
		session.CreatedAt, session.ExpiresAt); err != nil {
		return fmt.Errorf("create guard session: %w", err)
	}
	return nil
}

func (s *GuardStore) FindSessionByTokenHash(ctx context.Context, organizationID string, tokenHash []byte, now time.Time) (guard.Session, guard.Access, error) {
	var session guard.Session
	err := s.database.QueryRowContext(ctx, `
		SELECT id, organization_id, account_id, token_hash, csrf_hash,
		       created_at, expires_at, revoked_at
		FROM guard_sessions
		WHERE organization_id = $1 AND token_hash = $2
		  AND revoked_at IS NULL AND expires_at > $3
	`, organizationID, tokenHash, now).Scan(
		&session.ID,
		&session.OrganizationID,
		&session.AccountID,
		&session.TokenHash,
		&session.CSRFHash,
		&session.CreatedAt,
		&session.ExpiresAt,
		&session.RevokedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return guard.Session{}, guard.Access{}, guard.ErrNotFound
	}
	if err != nil {
		return guard.Session{}, guard.Access{}, fmt.Errorf("find guard session: %w", err)
	}
	access, err := s.AccessForAccount(ctx, organizationID, session.AccountID)
	if err != nil {
		return guard.Session{}, guard.Access{}, err
	}
	return session, access, nil
}

func (s *GuardStore) UpdateSessionCSRF(ctx context.Context, sessionID string, csrfHash []byte) error {
	if len(csrfHash) != 32 {
		return errors.New("csrf hash must contain 32 bytes")
	}
	result, err := s.database.ExecContext(ctx, `
		UPDATE guard_sessions SET csrf_hash = $2 WHERE id = $1 AND revoked_at IS NULL
	`, sessionID, csrfHash)
	if err != nil {
		return fmt.Errorf("update guard session csrf: %w", err)
	}
	return requireAffected(result)
}

func (s *GuardStore) RevokeSession(ctx context.Context, sessionID string, revokedAt time.Time) error {
	result, err := s.database.ExecContext(ctx, `
		UPDATE guard_sessions SET revoked_at = $2 WHERE id = $1 AND revoked_at IS NULL
	`, sessionID, revokedAt)
	if err != nil {
		return fmt.Errorf("revoke guard session: %w", err)
	}
	return requireAffected(result)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanGuardAccount(row rowScanner) (guard.Account, error) {
	var account guard.Account
	err := row.Scan(
		&account.ID,
		&account.OrganizationID,
		&account.Username,
		&account.NormalizedUsername,
		&account.Email,
		&account.DisplayName,
		&account.PasswordHash,
		&account.Status,
		&account.CreatedAt,
		&account.UpdatedAt,
	)
	return account, err
}

func scanResourceOwnership(row rowScanner) (guard.ResourceOwnership, error) {
	var ownership guard.ResourceOwnership
	var claimedBy sql.NullString
	var claimedAt sql.NullTime
	err := row.Scan(
		&ownership.OrganizationID,
		&ownership.ResourceType,
		&ownership.ResourceID,
		&ownership.SourceSystemID,
		&ownership.SourceRecordID,
		&ownership.WriteLocked,
		&ownership.RegisteredAt,
		&claimedBy,
		&claimedAt,
	)
	if err != nil {
		return guard.ResourceOwnership{}, err
	}
	if claimedBy.Valid {
		ownership.ClaimedBy = claimedBy.String
	}
	if claimedAt.Valid {
		value := claimedAt.Time
		ownership.ClaimedAt = &value
	}
	return ownership, nil
}

func requireAffected(result sql.Result) error {
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read affected rows: %w", err)
	}
	if count == 0 {
		return guard.ErrNotFound
	}
	return nil
}

func appendUniquePermission(values []guard.Permission, value guard.Permission) []guard.Permission {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
