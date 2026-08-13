-- StewardMesh Patterns -- versioned standard and custom record templates.
-- Requirement: REQ-PATTERNS-001. Feature: templates.schemas. GitHub: #8.

CREATE TABLE pattern_template_versions (
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    id TEXT NOT NULL CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    record_type TEXT NOT NULL CHECK (record_type ~ '^[a-z][a-z0-9.-]{1,79}$'),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 160),
    normalized_name TEXT GENERATED ALWAYS AS (lower(btrim(name))) STORED,
    description TEXT NOT NULL DEFAULT '' CHECK (char_length(description) <= 1000),
    version BIGINT NOT NULL CHECK (version >= 1),
    status TEXT NOT NULL CHECK (status IN ('active', 'retired')),
    fields JSONB NOT NULL CHECK (jsonb_typeof(fields) = 'array' AND jsonb_array_length(fields) BETWEEN 1 AND 64),
    created_by TEXT NOT NULL CHECK (char_length(created_by) BETWEEN 1 AND 200),
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, id, version)
);

CREATE UNIQUE INDEX pattern_template_name_unique
    ON pattern_template_versions (organization_id, normalized_name)
    WHERE version = 1;

CREATE INDEX pattern_template_record_type_latest_idx
    ON pattern_template_versions (organization_id, record_type, id, version DESC);
