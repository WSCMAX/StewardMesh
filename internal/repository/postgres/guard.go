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
		INSERT INTO guard_roles (id, organization_id, name, description)
		VALUES ($1, $2, $3, $4)
	`, role.ID, role.OrganizationID, role.Name, role.Description); err != nil {
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
			id, organization_id, account_id, role_id, scope_kind, scope_id, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, assignment.ID, assignment.OrganizationID, assignment.AccountID, assignment.RoleID,
		string(assignment.Scope.Kind), assignment.Scope.ResourceID, assignment.CreatedAt); err != nil {
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
		SELECT r.id, r.organization_id, r.name, r.description,
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
		if err := rows.Scan(&roleID, &roleOrganizationID, &roleName, &roleDescription,
			&scopeKind, &scopeID, &permission, &source, &bundleID); err != nil {
			return guard.Access{}, fmt.Errorf("scan account grant: %w", err)
		}
		role, exists := roles[roleID]
		if !exists {
			role = &guard.Role{ID: roleID, OrganizationID: roleOrganizationID, Name: roleName, Description: roleDescription}
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
	return access, nil
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
