-- Checkout groups, reservations, and bulk checkout parent records.
-- Reservations share assignment history but do not occupy the exclusive
-- active checkout slot.
-- Requirement: REQ-PEOPLE-001. Feature: identity.directory.

CREATE TABLE people_checkout_groups (
    id TEXT PRIMARY KEY CHECK (id ~ '^[a-f0-9]{32}$'),
    organization_id TEXT NOT NULL REFERENCES organizations(id),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
    description TEXT NOT NULL DEFAULT '' CHECK (char_length(description) <= 500),
    status TEXT NOT NULL CHECK (status IN ('active', 'inactive')),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (organization_id, id),
    CHECK (updated_at >= created_at)
);

CREATE TABLE people_checkout_group_members (
    organization_id TEXT NOT NULL,
    group_id TEXT NOT NULL,
    identity_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (group_id, identity_id),
    FOREIGN KEY (organization_id, group_id)
        REFERENCES people_checkout_groups (organization_id, id),
    FOREIGN KEY (organization_id, identity_id)
        REFERENCES people_identities (organization_id, id)
);

CREATE INDEX people_checkout_group_members_identity_idx
    ON people_checkout_group_members (organization_id, identity_id);

CREATE TABLE people_bulk_checkouts (
    id TEXT PRIMARY KEY CHECK (id ~ '^[a-f0-9]{32}$'),
    organization_id TEXT NOT NULL REFERENCES organizations(id),
    assignee_kind TEXT NOT NULL CHECK (assignee_kind IN ('identity', 'group')),
    assignee_id TEXT NOT NULL CHECK (assignee_id ~ '^[a-f0-9]{32}$'),
    purpose TEXT NOT NULL CHECK (purpose IN ('checkout', 'reservation')),
    effective_from TIMESTAMPTZ NOT NULL,
    due_at TIMESTAMPTZ,
    event_summary TEXT NOT NULL DEFAULT '' CHECK (char_length(event_summary) <= 500),
    label_definition_id TEXT NOT NULL DEFAULT '',
    label_value TEXT NOT NULL DEFAULT '',
    requested_count INTEGER NOT NULL CHECK (requested_count > 0),
    created_by TEXT NOT NULL CHECK (char_length(created_by) BETWEEN 1 AND 128),
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (organization_id, id),
    CHECK (due_at IS NULL OR due_at >= effective_from)
);

ALTER TABLE people_asset_assignments
    ADD COLUMN purpose TEXT NOT NULL DEFAULT 'checkout',
    ADD COLUMN event_summary TEXT NOT NULL DEFAULT '',
    ADD COLUMN group_id TEXT,
    ADD COLUMN bulk_checkout_id TEXT;

ALTER TABLE people_asset_assignments
    DROP CONSTRAINT IF EXISTS people_asset_assignments_assignee_kind_check;

ALTER TABLE people_asset_assignments
    ADD CONSTRAINT people_asset_assignments_assignee_kind_check
    CHECK (assignee_kind IN ('identity', 'department', 'group'));

ALTER TABLE people_asset_assignments
    DROP CONSTRAINT IF EXISTS people_asset_assignments_check;

ALTER TABLE people_asset_assignments
    ADD CONSTRAINT people_asset_assignments_assignee_chk CHECK (
        (assignee_kind = 'identity' AND identity_id IS NOT NULL AND department_id IS NULL AND group_id IS NULL AND role IN ('primary', 'user'))
        OR
        (assignee_kind = 'department' AND department_id IS NOT NULL AND identity_id IS NULL AND group_id IS NULL AND role = 'department')
        OR
        (assignee_kind = 'group' AND group_id IS NOT NULL AND identity_id IS NULL AND department_id IS NULL AND role IN ('primary', 'user'))
    );

ALTER TABLE people_asset_assignments
    ADD CONSTRAINT people_asset_assignments_purpose_chk
    CHECK (purpose IN ('checkout', 'reservation'));

ALTER TABLE people_asset_assignments
    ADD CONSTRAINT people_asset_assignments_event_summary_chk
    CHECK (char_length(event_summary) <= 500);

ALTER TABLE people_asset_assignments
    ADD CONSTRAINT people_asset_assignments_group_fk
    FOREIGN KEY (organization_id, group_id)
        REFERENCES people_checkout_groups (organization_id, id);

ALTER TABLE people_asset_assignments
    ADD CONSTRAINT people_asset_assignments_bulk_fk
    FOREIGN KEY (organization_id, bulk_checkout_id)
        REFERENCES people_bulk_checkouts (organization_id, id);

DROP INDEX IF EXISTS people_asset_assignments_active_role_idx;
CREATE UNIQUE INDEX people_asset_assignments_active_role_idx
    ON people_asset_assignments (organization_id, asset_id, role)
    WHERE effective_to IS NULL AND role IN ('primary', 'department') AND purpose = 'checkout';

DROP INDEX IF EXISTS people_asset_assignments_active_user_idx;
CREATE UNIQUE INDEX people_asset_assignments_active_user_idx
    ON people_asset_assignments (organization_id, asset_id, role, identity_id)
    WHERE effective_to IS NULL AND role = 'user' AND purpose = 'checkout';

CREATE UNIQUE INDEX people_asset_assignments_active_group_user_idx
    ON people_asset_assignments (organization_id, asset_id, role, group_id)
    WHERE effective_to IS NULL AND role = 'user' AND purpose = 'checkout' AND group_id IS NOT NULL;

CREATE INDEX people_asset_assignments_bulk_idx
    ON people_asset_assignments (organization_id, bulk_checkout_id)
    WHERE bulk_checkout_id IS NOT NULL;
