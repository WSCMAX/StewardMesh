-- Requirement: REQ-STORAGE-001. Feature: storage.blobs.
CREATE TABLE vault_blobs (
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    id TEXT NOT NULL,
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 255),
    media_type TEXT NOT NULL CHECK (char_length(media_type) BETWEEN 1 AND 127),
    size_bytes BIGINT NOT NULL CHECK (size_bytes >= 0),
    sha256 TEXT NOT NULL CHECK (sha256 ~ '^[a-f0-9]{64}$'),
    provider TEXT NOT NULL CHECK (provider IN ('local', 's3')),
    object_key TEXT NOT NULL CHECK (object_key ~ '^[a-z0-9][a-z0-9_-]{0,127}/[a-f0-9]{32}$'),
    source_system_id TEXT,
    source_record_id TEXT,
    resource_type TEXT,
    resource_id TEXT,
    created_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, id),
    UNIQUE (organization_id, object_key),
    CHECK ((source_system_id IS NULL) = (source_record_id IS NULL)),
    CHECK ((resource_type IS NULL) = (resource_id IS NULL))
);

CREATE INDEX vault_blobs_organization_created_idx
    ON vault_blobs (organization_id, created_at DESC, id);
