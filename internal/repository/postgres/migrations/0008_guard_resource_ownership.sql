-- StewardMesh Guard resource ownership and write locks.
-- Requirement: SEC-GUARD-001.

CREATE TABLE guard_resource_ownership (
    organization_id VARCHAR(128) NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    resource_type VARCHAR(64) NOT NULL,
    resource_id VARCHAR(128) NOT NULL,
    source_system_id VARCHAR(128) NOT NULL,
    source_record_id VARCHAR(256) NOT NULL,
    write_locked BOOLEAN NOT NULL DEFAULT TRUE,
    registered_at TIMESTAMPTZ NOT NULL,
    claimed_by VARCHAR(64) REFERENCES guard_accounts(id),
    claimed_at TIMESTAMPTZ,
    PRIMARY KEY (organization_id, resource_type, resource_id),
    CONSTRAINT guard_resource_ownership_resource_type_check
        CHECK (resource_type ~ '^[a-z][a-z0-9_.-]{0,63}$'),
    CONSTRAINT guard_resource_ownership_resource_id_check
        CHECK (resource_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    CONSTRAINT guard_resource_ownership_source_system_check
        CHECK (source_system_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    CONSTRAINT guard_resource_ownership_source_record_check
        CHECK (length(btrim(source_record_id)) BETWEEN 1 AND 256),
    CONSTRAINT guard_resource_ownership_claim_check
        CHECK (
            (write_locked AND claimed_by IS NULL AND claimed_at IS NULL)
            OR
            (NOT write_locked AND claimed_by IS NOT NULL AND claimed_at IS NOT NULL AND claimed_at >= registered_at)
        )
);

CREATE UNIQUE INDEX guard_resource_ownership_source_idx
    ON guard_resource_ownership (organization_id, resource_type, source_system_id, source_record_id);

CREATE INDEX guard_resource_ownership_locked_idx
    ON guard_resource_ownership (organization_id, write_locked, registered_at);
