-- StewardMesh Stack -- software inventory, license entitlements, assignments, and usage state.
-- Requirements: REQ-STACK-001, SEC-GUARD-001. Feature: software.licenses. GitHub: #7.

CREATE TABLE stack_products (
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    id TEXT NOT NULL CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
    publisher TEXT NOT NULL CHECK (char_length(publisher) BETWEEN 1 AND 200),
    category TEXT NOT NULL DEFAULT '' CHECK (char_length(category) <= 100),
    status TEXT NOT NULL CHECK (status IN ('active', 'retired')),
    source_system_id TEXT CHECK (source_system_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    source_record_id TEXT CHECK (source_record_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,254}$'),
    revision BIGINT NOT NULL CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, id),
    UNIQUE (organization_id, publisher, name),
    CHECK ((source_system_id IS NULL) = (source_record_id IS NULL)),
    CHECK (updated_at >= created_at)
);

CREATE UNIQUE INDEX stack_products_name_unique
    ON stack_products (organization_id, lower(btrim(publisher)), lower(btrim(name)));
CREATE UNIQUE INDEX stack_products_source_unique
    ON stack_products (organization_id, lower(source_system_id), source_record_id)
    WHERE source_system_id IS NOT NULL;

CREATE TABLE stack_versions (
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    id TEXT NOT NULL CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    product_id TEXT NOT NULL,
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    released_on DATE,
    status TEXT NOT NULL CHECK (status IN ('active', 'unsupported', 'retired')),
    source_system_id TEXT CHECK (source_system_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    source_record_id TEXT CHECK (source_record_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,254}$'),
    revision BIGINT NOT NULL CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, id),
    UNIQUE (organization_id, product_id, id),
    FOREIGN KEY (organization_id, product_id) REFERENCES stack_products (organization_id, id),
    CHECK ((source_system_id IS NULL) = (source_record_id IS NULL)),
    CHECK (updated_at >= created_at)
);

CREATE UNIQUE INDEX stack_versions_name_unique
    ON stack_versions (organization_id, product_id, lower(btrim(name)));
CREATE UNIQUE INDEX stack_versions_source_unique
    ON stack_versions (organization_id, lower(source_system_id), source_record_id)
    WHERE source_system_id IS NOT NULL;

CREATE TABLE stack_installations (
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    id TEXT NOT NULL CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    version_id TEXT NOT NULL,
    asset_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('installed', 'removed')),
    usage_state TEXT NOT NULL CHECK (usage_state IN ('unknown', 'used', 'unused')),
    installed_at TIMESTAMPTZ NOT NULL,
    last_used_at TIMESTAMPTZ,
    removed_at TIMESTAMPTZ,
    source_system_id TEXT CHECK (source_system_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    source_record_id TEXT CHECK (source_record_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,254}$'),
    revision BIGINT NOT NULL CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, id),
    FOREIGN KEY (organization_id, version_id) REFERENCES stack_versions (organization_id, id),
    FOREIGN KEY (organization_id, asset_id) REFERENCES atlas_assets (organization_id, id),
    CHECK ((status = 'installed' AND removed_at IS NULL) OR (status = 'removed' AND removed_at IS NOT NULL)),
    CHECK (last_used_at IS NULL OR last_used_at >= installed_at),
    CHECK (removed_at IS NULL OR removed_at >= installed_at),
    CHECK (last_used_at IS NULL OR removed_at IS NULL OR last_used_at <= removed_at),
    CHECK ((source_system_id IS NULL) = (source_record_id IS NULL)),
    CHECK (updated_at >= created_at)
);

CREATE UNIQUE INDEX stack_installations_active_unique
    ON stack_installations (organization_id, version_id, asset_id) WHERE status = 'installed';
CREATE UNIQUE INDEX stack_installations_source_unique
    ON stack_installations (organization_id, lower(source_system_id), source_record_id)
    WHERE source_system_id IS NOT NULL;
CREATE INDEX stack_installations_asset_idx
    ON stack_installations (organization_id, asset_id, status, installed_at DESC);

CREATE TABLE stack_licenses (
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    id TEXT NOT NULL CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    product_id TEXT NOT NULL,
    version_id TEXT,
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
    entitlement_metric TEXT NOT NULL CHECK (entitlement_metric IN ('device', 'user', 'concurrent', 'site', 'enterprise')),
    quantity BIGINT NOT NULL CHECK (quantity BETWEEN 1 AND 1000000000),
    status TEXT NOT NULL CHECK (status IN ('active', 'expired', 'retired')),
    starts_on DATE,
    expires_on DATE,
    vendor_id TEXT,
    purchase_order_id TEXT,
    contract_id TEXT,
    cost_record_id TEXT,
    document_ids JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(document_ids) = 'array' AND jsonb_array_length(document_ids) <= 100),
    source_system_id TEXT CHECK (source_system_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    source_record_id TEXT CHECK (source_record_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,254}$'),
    revision BIGINT NOT NULL CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, id),
    FOREIGN KEY (organization_id, product_id) REFERENCES stack_products (organization_id, id),
    FOREIGN KEY (organization_id, product_id, version_id) REFERENCES stack_versions (organization_id, product_id, id),
    FOREIGN KEY (organization_id, vendor_id) REFERENCES ledger_vendors (organization_id, id),
    FOREIGN KEY (organization_id, purchase_order_id) REFERENCES ledger_purchase_orders (organization_id, id),
    FOREIGN KEY (organization_id, contract_id) REFERENCES ledger_contracts (organization_id, id),
    FOREIGN KEY (organization_id, cost_record_id) REFERENCES ledger_costs (organization_id, id),
    CHECK (starts_on IS NULL OR expires_on IS NULL OR expires_on >= starts_on),
    CHECK ((source_system_id IS NULL) = (source_record_id IS NULL)),
    CHECK (updated_at >= created_at)
);

CREATE UNIQUE INDEX stack_licenses_source_unique
    ON stack_licenses (organization_id, lower(source_system_id), source_record_id)
    WHERE source_system_id IS NOT NULL;
CREATE INDEX stack_licenses_expiry_idx
    ON stack_licenses (organization_id, status, expires_on, product_id);

CREATE TABLE stack_assignments (
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    id TEXT NOT NULL CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    license_id TEXT NOT NULL,
    assignee_kind TEXT NOT NULL CHECK (assignee_kind IN ('asset', 'identity', 'department', 'site')),
    assignee_id TEXT NOT NULL CHECK (assignee_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    seats BIGINT NOT NULL CHECK (seats BETWEEN 1 AND 1000000000),
    usage_state TEXT NOT NULL CHECK (usage_state IN ('unknown', 'used', 'unused')),
    assigned_at TIMESTAMPTZ NOT NULL,
    last_used_at TIMESTAMPTZ,
    ended_at TIMESTAMPTZ,
    source_system_id TEXT CHECK (source_system_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    source_record_id TEXT CHECK (source_record_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,254}$'),
    revision BIGINT NOT NULL CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, id),
    FOREIGN KEY (organization_id, license_id) REFERENCES stack_licenses (organization_id, id),
    CHECK (last_used_at IS NULL OR last_used_at >= assigned_at),
    CHECK (ended_at IS NULL OR ended_at >= assigned_at),
    CHECK (last_used_at IS NULL OR ended_at IS NULL OR last_used_at <= ended_at),
    CHECK ((source_system_id IS NULL) = (source_record_id IS NULL)),
    CHECK (updated_at >= created_at)
);

CREATE UNIQUE INDEX stack_assignments_active_unique
    ON stack_assignments (organization_id, license_id, assignee_kind, assignee_id) WHERE ended_at IS NULL;
CREATE UNIQUE INDEX stack_assignments_source_unique
    ON stack_assignments (organization_id, lower(source_system_id), source_record_id)
    WHERE source_system_id IS NOT NULL;
CREATE INDEX stack_assignments_usage_idx
    ON stack_assignments (organization_id, license_id, usage_state, ended_at);

-- Existing built-in administrators receive the new Stack permissions. Custom
-- roles remain unchanged so least-privilege decisions stay explicit.
INSERT INTO guard_policy_bundle_permissions (organization_id, bundle_id, permission)
SELECT DISTINCT rb.organization_id, rb.bundle_id, permission.name
FROM guard_role_policy_bundles rb
JOIN guard_roles r
  ON r.organization_id = rb.organization_id
 AND r.id = rb.role_id
CROSS JOIN (VALUES ('software.read'), ('software.write')) AS permission(name)
WHERE r.source = 'builtin'
  AND lower(btrim(r.name)) = 'administrator'
ON CONFLICT (bundle_id, permission) DO NOTHING;
