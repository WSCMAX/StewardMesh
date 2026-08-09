-- StewardMesh People -- sites, departments, identities, and asset assignment history
-- Requirement: REQ-PEOPLE-001. Feature: identity.directory.

CREATE TABLE people_sites (
    id TEXT PRIMARY KEY CHECK (id ~ '^[a-f0-9]{32}$'),
    organization_id TEXT NOT NULL REFERENCES organizations(id),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
    normalized_name TEXT NOT NULL CHECK (char_length(normalized_name) BETWEEN 1 AND 200),
    status TEXT NOT NULL CHECK (status IN ('active', 'inactive')),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (organization_id, normalized_name),
    UNIQUE (organization_id, id),
    CHECK (updated_at >= created_at)
);

CREATE TABLE people_departments (
    id TEXT PRIMARY KEY CHECK (id ~ '^[a-f0-9]{32}$'),
    organization_id TEXT NOT NULL REFERENCES organizations(id),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
    normalized_name TEXT NOT NULL CHECK (char_length(normalized_name) BETWEEN 1 AND 200),
    site_id TEXT,
    status TEXT NOT NULL CHECK (status IN ('active', 'inactive')),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (organization_id, normalized_name),
    UNIQUE (organization_id, id),
    FOREIGN KEY (organization_id, site_id)
        REFERENCES people_sites (organization_id, id),
    CHECK (updated_at >= created_at)
);

CREATE TABLE people_identities (
    id TEXT PRIMARY KEY CHECK (id ~ '^[a-f0-9]{32}$'),
    organization_id TEXT NOT NULL REFERENCES organizations(id),
    kind TEXT NOT NULL CHECK (kind IN ('person', 'shared', 'public', 'lab')),
    display_name TEXT NOT NULL CHECK (char_length(display_name) BETWEEN 1 AND 200),
    normalized_name TEXT NOT NULL CHECK (char_length(normalized_name) BETWEEN 1 AND 200),
    email TEXT NOT NULL DEFAULT '' CHECK (char_length(email) <= 320),
    normalized_email TEXT NOT NULL DEFAULT '' CHECK (char_length(normalized_email) <= 320),
    department_id TEXT,
    site_id TEXT,
    status TEXT NOT NULL CHECK (status IN ('active', 'inactive')),
    provider TEXT NOT NULL DEFAULT '' CHECK (char_length(provider) <= 64),
    provider_subject TEXT NOT NULL DEFAULT '' CHECK (char_length(provider_subject) <= 255),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (organization_id, id),
    FOREIGN KEY (organization_id, department_id)
        REFERENCES people_departments (organization_id, id),
    FOREIGN KEY (organization_id, site_id)
        REFERENCES people_sites (organization_id, id),
    CHECK ((provider = '') = (provider_subject = '')),
    CHECK (kind <> 'person' OR normalized_email <> ''),
    CHECK (updated_at >= created_at)
);

CREATE UNIQUE INDEX people_identities_email_idx
    ON people_identities (organization_id, normalized_email)
    WHERE normalized_email <> '';

CREATE UNIQUE INDEX people_identities_provider_idx
    ON people_identities (organization_id, provider, provider_subject)
    WHERE provider <> '';

CREATE INDEX people_identities_directory_idx
    ON people_identities (organization_id, normalized_name, id);

CREATE INDEX people_identities_scope_idx
    ON people_identities (organization_id, department_id, site_id);

CREATE TABLE people_asset_assignments (
    id TEXT PRIMARY KEY CHECK (id ~ '^[a-f0-9]{32}$'),
    organization_id TEXT NOT NULL REFERENCES organizations(id),
    asset_id TEXT NOT NULL CHECK (char_length(asset_id) BETWEEN 1 AND 128),
    assignee_kind TEXT NOT NULL CHECK (assignee_kind IN ('identity', 'department')),
    identity_id TEXT,
    department_id TEXT,
    role TEXT NOT NULL CHECK (role IN ('primary', 'user', 'department')),
    effective_from TIMESTAMPTZ NOT NULL,
    effective_to TIMESTAMPTZ,
    created_by TEXT NOT NULL CHECK (char_length(created_by) BETWEEN 1 AND 128),
    created_at TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (organization_id, identity_id)
        REFERENCES people_identities (organization_id, id),
    FOREIGN KEY (organization_id, department_id)
        REFERENCES people_departments (organization_id, id),
    CHECK (
        (assignee_kind = 'identity' AND identity_id IS NOT NULL AND department_id IS NULL AND role IN ('primary', 'user'))
        OR
        (assignee_kind = 'department' AND department_id IS NOT NULL AND identity_id IS NULL AND role = 'department')
    ),
    CHECK (effective_to IS NULL OR effective_to > effective_from)
);

CREATE UNIQUE INDEX people_asset_assignments_active_role_idx
    ON people_asset_assignments (organization_id, asset_id, role)
    WHERE effective_to IS NULL AND role IN ('primary', 'department');

CREATE UNIQUE INDEX people_asset_assignments_active_user_idx
    ON people_asset_assignments (organization_id, asset_id, role, identity_id)
    WHERE effective_to IS NULL AND role = 'user';

CREATE INDEX people_asset_assignments_history_idx
    ON people_asset_assignments (organization_id, asset_id, effective_from DESC, id);

-- Existing administrators receive the write grant introduced by this slice.
INSERT INTO guard_policy_bundle_permissions (organization_id, bundle_id, permission)
SELECT organization_id, id, 'directory.write'
FROM guard_policy_bundles
WHERE name = 'Core administration'
ON CONFLICT (bundle_id, permission) DO NOTHING;
