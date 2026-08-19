-- StewardMesh Labels -- configurable tags applicable to any durable record.
-- Requirement: REQ-LABELS-001. Feature: identity.labels.

CREATE TABLE labels_definitions (
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    id TEXT NOT NULL CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    normalized_name TEXT NOT NULL CHECK (normalized_name = lower(btrim(name))),
    description TEXT NOT NULL DEFAULT '' CHECK (char_length(description) <= 500),
    value_kind TEXT NOT NULL CHECK (value_kind IN ('flag', 'text', 'select', 'multiselect')),
    applicable_record_types JSONB NOT NULL CHECK (jsonb_typeof(applicable_record_types) = 'array'),
    options JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(options) = 'array'),
    status TEXT NOT NULL CHECK (status IN ('active', 'retired')),
    revision BIGINT NOT NULL CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, id),
    UNIQUE (organization_id, normalized_name),
    CHECK (updated_at >= created_at)
);

CREATE INDEX labels_definitions_status_idx
    ON labels_definitions (organization_id, status, normalized_name, id);

CREATE TABLE labels_assignments (
    organization_id TEXT NOT NULL,
    definition_id TEXT NOT NULL,
    record_type TEXT NOT NULL CHECK (record_type ~ '^[a-z][a-z0-9]*(\.[a-z][a-z0-9-]*)+$'),
    record_id TEXT NOT NULL CHECK (record_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    value_text TEXT NOT NULL DEFAULT '' CHECK (char_length(value_text) <= 500),
    values JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(values) = 'array'),
    revision BIGINT NOT NULL CHECK (revision > 0),
    updated_by TEXT NOT NULL CHECK (char_length(updated_by) BETWEEN 1 AND 128),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, definition_id, record_type, record_id),
    FOREIGN KEY (organization_id, definition_id)
        REFERENCES labels_definitions (organization_id, id),
    CHECK (updated_at >= created_at)
);

CREATE INDEX labels_assignments_record_idx
    ON labels_assignments (organization_id, record_type, record_id, definition_id);

CREATE INDEX labels_assignments_definition_idx
    ON labels_assignments (organization_id, definition_id, record_type, record_id);
