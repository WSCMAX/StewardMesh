-- REQ-DIRECTORY-EXPANSION-005 / integrations.protocols
ALTER TABLE directory_import_mappings
    DROP CONSTRAINT directory_import_mappings_kind_check,
    ADD CONSTRAINT directory_import_mappings_kind_check
        CHECK (kind IN ('identity', 'group', 'membership'));

CREATE TABLE directory_managed_groups (
    id TEXT PRIMARY KEY,
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    source_system_id TEXT NOT NULL,
    source_record_id TEXT NOT NULL,
    name TEXT NOT NULL,
    display_name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('active', 'inactive')),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    revision BIGINT NOT NULL CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (organization_id, source_system_id, source_record_id),
    UNIQUE (organization_id, id)
);

CREATE INDEX directory_managed_groups_organization_name_idx
    ON directory_managed_groups (organization_id, name, id);

CREATE TABLE directory_managed_memberships (
    id TEXT PRIMARY KEY,
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    source_system_id TEXT NOT NULL,
    source_record_id TEXT NOT NULL,
    group_id TEXT NOT NULL,
    group_source_id TEXT NOT NULL,
    member_id TEXT NOT NULL,
    member_source_id TEXT NOT NULL,
    member_kind TEXT NOT NULL CHECK (member_kind IN ('subject', 'group')),
    member_display_name TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('active', 'inactive')),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    revision BIGINT NOT NULL CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (organization_id, source_system_id, source_record_id),
    UNIQUE (organization_id, id),
    FOREIGN KEY (organization_id, group_id)
        REFERENCES directory_managed_groups (organization_id, id)
        ON DELETE CASCADE
);

CREATE INDEX directory_managed_memberships_graph_idx
    ON directory_managed_memberships (organization_id, group_id, member_kind, member_id, id);
