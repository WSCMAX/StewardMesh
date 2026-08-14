-- StewardMesh Exchange -- bounded .openinventory package receipts and holding outcomes.
-- Requirement: REQ-EXCHANGE-001. Feature: migration.packages. GitHub: #9.

CREATE TABLE exchange_packages (
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    direction TEXT NOT NULL CHECK (direction IN ('export', 'import')),
    package_id TEXT NOT NULL CHECK (package_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    schema_version TEXT NOT NULL CHECK (schema_version = '1.0'),
    source_system_id TEXT NOT NULL CHECK (source_system_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    archive_sha256 TEXT NOT NULL CHECK (archive_sha256 ~ '^[a-f0-9]{64}$'),
    size_bytes BIGINT NOT NULL CHECK (size_bytes BETWEEN 1 AND 33554432),
    file_mode TEXT NOT NULL CHECK (file_mode IN ('metadata', 'include')),
    status TEXT NOT NULL CHECK (status IN ('processing', 'completed', 'holding', 'failed')),
    record_count INTEGER NOT NULL CHECK (record_count BETWEEN 1 AND 10000),
    file_count INTEGER NOT NULL CHECK (file_count BETWEEN 0 AND 1000 AND file_count <= record_count),
    created_count INTEGER NOT NULL DEFAULT 0 CHECK (created_count >= 0),
    unchanged_count INTEGER NOT NULL DEFAULT 0 CHECK (unchanged_count >= 0),
    holding_count INTEGER NOT NULL DEFAULT 0 CHECK (holding_count >= 0),
    records JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(records) = 'array' AND jsonb_array_length(records) <= 10000),
    error_code TEXT CHECK (error_code ~ '^[a-z][a-z0-9_]{0,63}$'),
    created_by TEXT NOT NULL CHECK (char_length(created_by) BETWEEN 1 AND 128),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, direction, package_id),
    CHECK (created_count + unchanged_count + holding_count <= record_count),
    CHECK ((status = 'holding') = (holding_count > 0)),
    CHECK ((status = 'failed') = (error_code IS NOT NULL)),
	CHECK (status <> 'processing' OR (created_count = 0 AND unchanged_count = 0 AND holding_count = 0 AND jsonb_array_length(records) = 0)),
	CHECK (status NOT IN ('completed', 'holding') OR jsonb_array_length(records) = record_count),
	CHECK (status <> 'failed' OR (created_count = 0 AND unchanged_count = 0 AND holding_count = 0 AND jsonb_array_length(records) = 0)),
	CHECK (direction <> 'export' OR (status = 'completed' AND created_count = 0 AND holding_count = 0 AND unchanged_count = record_count)),
    CHECK (updated_at >= created_at)
);

CREATE INDEX exchange_packages_history_idx
    ON exchange_packages (organization_id, created_at DESC, direction, package_id);
