-- StewardMesh Guard — Authentication, roles, policies, and audit
-- Requirements: SEC-GUARD-001, SEC-HTTP-001
CREATE TABLE guard_accounts (
    id TEXT PRIMARY KEY CHECK (id ~ '^[a-f0-9]{32}$'),
    organization_id TEXT NOT NULL REFERENCES organizations(id),
    username TEXT NOT NULL CHECK (char_length(username) BETWEEN 3 AND 64),
    normalized_username TEXT NOT NULL CHECK (normalized_username ~ '^[a-z0-9][a-z0-9._-]{2,63}$'),
    email TEXT NOT NULL CHECK (char_length(email) BETWEEN 3 AND 320),
    display_name TEXT NOT NULL CHECK (char_length(display_name) BETWEEN 1 AND 200),
    password_hash TEXT NOT NULL CHECK (char_length(password_hash) BETWEEN 32 AND 512),
    status TEXT NOT NULL CHECK (status IN ('active', 'disabled')),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (organization_id, normalized_username),
    UNIQUE (organization_id, id),
    CHECK (updated_at >= created_at)
);

CREATE TABLE guard_policy_bundles (
    id TEXT PRIMARY KEY CHECK (id ~ '^[a-f0-9]{32}$'),
    organization_id TEXT NOT NULL REFERENCES organizations(id),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120),
    description TEXT NOT NULL DEFAULT '' CHECK (char_length(description) <= 1000),
    UNIQUE (organization_id, name),
    UNIQUE (organization_id, id)
);

CREATE TABLE guard_policy_bundle_permissions (
    organization_id TEXT NOT NULL,
    bundle_id TEXT NOT NULL,
    permission TEXT NOT NULL CHECK (permission ~ '^[a-z][a-z0-9._-]{2,127}$'),
    PRIMARY KEY (bundle_id, permission),
    FOREIGN KEY (organization_id, bundle_id)
        REFERENCES guard_policy_bundles (organization_id, id) ON DELETE CASCADE
);

CREATE TABLE guard_roles (
    id TEXT PRIMARY KEY CHECK (id ~ '^[a-f0-9]{32}$'),
    organization_id TEXT NOT NULL REFERENCES organizations(id),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120),
    description TEXT NOT NULL DEFAULT '' CHECK (char_length(description) <= 1000),
    UNIQUE (organization_id, name),
    UNIQUE (organization_id, id)
);

CREATE TABLE guard_role_permissions (
    organization_id TEXT NOT NULL,
    role_id TEXT NOT NULL,
    permission TEXT NOT NULL CHECK (permission ~ '^[a-z][a-z0-9._-]{2,127}$'),
    PRIMARY KEY (role_id, permission),
    FOREIGN KEY (organization_id, role_id)
        REFERENCES guard_roles (organization_id, id) ON DELETE CASCADE
);

CREATE TABLE guard_role_policy_bundles (
    organization_id TEXT NOT NULL,
    role_id TEXT NOT NULL,
    bundle_id TEXT NOT NULL,
    PRIMARY KEY (role_id, bundle_id),
    FOREIGN KEY (organization_id, role_id)
        REFERENCES guard_roles (organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, bundle_id)
        REFERENCES guard_policy_bundles (organization_id, id) ON DELETE CASCADE
);

CREATE TABLE guard_role_assignments (
    id TEXT PRIMARY KEY CHECK (id ~ '^[a-f0-9]{32}$'),
    organization_id TEXT NOT NULL REFERENCES organizations(id),
    account_id TEXT NOT NULL,
    role_id TEXT NOT NULL,
    scope_kind TEXT NOT NULL CHECK (scope_kind IN ('organization', 'site', 'department', 'resource')),
    scope_id TEXT NOT NULL CHECK (char_length(scope_id) BETWEEN 1 AND 128),
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (account_id, role_id, scope_kind, scope_id),
    FOREIGN KEY (organization_id, account_id)
        REFERENCES guard_accounts (organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, role_id)
        REFERENCES guard_roles (organization_id, id) ON DELETE CASCADE
);

CREATE TABLE guard_sessions (
    id TEXT PRIMARY KEY CHECK (id ~ '^[a-f0-9]{32}$'),
    organization_id TEXT NOT NULL REFERENCES organizations(id),
    account_id TEXT NOT NULL,
    token_hash BYTEA NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    csrf_hash BYTEA NOT NULL CHECK (octet_length(csrf_hash) = 32),
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    FOREIGN KEY (organization_id, account_id)
        REFERENCES guard_accounts (organization_id, id) ON DELETE CASCADE,
    CHECK (expires_at > created_at),
    CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);

CREATE INDEX guard_role_assignments_account_idx
    ON guard_role_assignments (organization_id, account_id);

CREATE INDEX guard_sessions_account_expiry_idx
    ON guard_sessions (organization_id, account_id, expires_at DESC);
