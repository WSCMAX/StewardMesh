-- Provider-neutral durable directory import reconciliation engine.
-- Requirement: REQ-DIRECTORY-EXPANSION-002. Feature: integrations.protocols. GitHub: #25.

ALTER TABLE directory_import_batches
    DROP CONSTRAINT directory_import_batches_provider_check,
    ADD COLUMN source_system_id TEXT NOT NULL DEFAULT 'legacy' CHECK (source_system_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    ADD COLUMN config_revision TEXT NOT NULL DEFAULT 'legacy' CHECK (char_length(config_revision) BETWEEN 1 AND 128),
    ADD COLUMN complete_snapshot BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN unchanged_count INT NOT NULL DEFAULT 0 CHECK (unchanged_count >= 0),
    ADD COLUMN deactivated_count INT NOT NULL DEFAULT 0 CHECK (deactivated_count >= 0),
    ADD COLUMN lease_token TEXT NOT NULL DEFAULT '' CHECK (lease_token = '' OR lease_token ~ '^[a-f0-9]{32}$'),
    ADD COLUMN lease_expires_at TIMESTAMPTZ,
    ADD COLUMN updated_at TIMESTAMPTZ,
    ADD COLUMN completed_at TIMESTAMPTZ,
    ADD CONSTRAINT directory_import_batches_provider_name_check CHECK (provider ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
    ADD CONSTRAINT directory_import_batches_counts_check CHECK (
        created_count >= 0 AND updated_count >= 0 AND conflict_count >= 0 AND error_count >= 0
    ),
    ADD CONSTRAINT directory_import_batches_lease_check CHECK ((lease_token = '') = (lease_expires_at IS NULL));

UPDATE directory_import_batches SET updated_at = created_at WHERE updated_at IS NULL;
ALTER TABLE directory_import_batches ALTER COLUMN updated_at SET NOT NULL;
CREATE UNIQUE INDEX directory_import_batches_org_id_idx ON directory_import_batches (organization_id, id);
CREATE INDEX directory_import_batches_list_idx ON directory_import_batches (organization_id, created_at DESC, id DESC);

CREATE TABLE directory_import_items (
    organization_id TEXT NOT NULL,
    batch_id TEXT NOT NULL,
    id TEXT NOT NULL CHECK (id ~ '^[a-f0-9]{32}$'),
    ordinal INT NOT NULL CHECK (ordinal >= 0),
    source_record_id TEXT NOT NULL CHECK (char_length(source_record_id) BETWEEN 1 AND 255),
    record JSONB NOT NULL CHECK (jsonb_typeof(record) = 'object'),
    target_id TEXT NOT NULL DEFAULT '' CHECK (target_id = '' OR target_id ~ '^[a-f0-9]{32}$'),
    expected_revision BIGINT NOT NULL DEFAULT 0 CHECK (expected_revision >= 0),
    source_digest TEXT NOT NULL CHECK (source_digest ~ '^[a-f0-9]{64}$'),
    observed_target_digest TEXT NOT NULL DEFAULT '' CHECK (observed_target_digest = '' OR observed_target_digest ~ '^[a-f0-9]{64}$'),
    planned_target_digest TEXT NOT NULL CHECK (planned_target_digest ~ '^[a-f0-9]{64}$'),
    action TEXT NOT NULL CHECK (action IN ('create','update','deactivate','unchanged','conflict')),
    outcome TEXT NOT NULL CHECK (outcome IN ('pending','applied','unchanged','conflict','failed')),
    failure_class TEXT NOT NULL DEFAULT '' CHECK (failure_class IN ('','transient','permanent','conflict')),
    retryable BOOLEAN NOT NULL DEFAULT FALSE,
    error_message TEXT NOT NULL DEFAULT '' CHECK (char_length(error_message) <= 240),
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, batch_id, id),
    UNIQUE (organization_id, batch_id, ordinal),
    UNIQUE (organization_id, batch_id, source_record_id),
    FOREIGN KEY (organization_id, batch_id) REFERENCES directory_import_batches (organization_id, id) ON DELETE CASCADE
);

CREATE TABLE directory_import_attempts (
    organization_id TEXT NOT NULL,
    batch_id TEXT NOT NULL,
    id TEXT NOT NULL CHECK (id ~ '^[a-f0-9]{32}$'),
    operation TEXT NOT NULL CHECK (operation IN ('preview','apply','retry')),
    idempotency_hash TEXT NOT NULL CHECK (idempotency_hash ~ '^[a-f0-9]{64}$'),
    request_fingerprint TEXT NOT NULL CHECK (request_fingerprint ~ '^[a-f0-9]{64}$'),
    attempt_number INT NOT NULL CHECK (attempt_number > 0),
    status TEXT NOT NULL,
    failure_class TEXT NOT NULL DEFAULT '' CHECK (failure_class IN ('','transient','permanent','conflict')),
    retryable BOOLEAN NOT NULL DEFAULT FALSE,
    error_message TEXT NOT NULL DEFAULT '' CHECK (char_length(error_message) <= 240),
    actor_id TEXT NOT NULL CHECK (char_length(actor_id) BETWEEN 1 AND 128),
    correlation_id TEXT NOT NULL CHECK (char_length(correlation_id) BETWEEN 1 AND 128),
    result JSONB,
    started_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    PRIMARY KEY (organization_id, id),
    UNIQUE (organization_id, batch_id, attempt_number),
    UNIQUE (organization_id, operation, idempotency_hash),
    FOREIGN KEY (organization_id, batch_id) REFERENCES directory_import_batches (organization_id, id) ON DELETE CASCADE,
    CHECK (result IS NULL OR jsonb_typeof(result) = 'object')
);

CREATE INDEX directory_import_attempts_batch_idx ON directory_import_attempts (organization_id, batch_id, attempt_number);

CREATE TABLE directory_import_mappings (
    organization_id TEXT NOT NULL,
    source_system_id TEXT NOT NULL CHECK (source_system_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    provider TEXT NOT NULL CHECK (provider ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
    source_record_id TEXT NOT NULL CHECK (char_length(source_record_id) BETWEEN 1 AND 255),
    kind TEXT NOT NULL CHECK (kind = 'identity'),
    target_id TEXT NOT NULL CHECK (target_id ~ '^[a-f0-9]{32}$'),
    source_digest TEXT NOT NULL CHECK (source_digest ~ '^[a-f0-9]{64}$'),
    applied_target_digest TEXT NOT NULL CHECK (applied_target_digest ~ '^[a-f0-9]{64}$'),
    last_record JSONB NOT NULL CHECK (jsonb_typeof(last_record) = 'object'),
    active BOOLEAN NOT NULL,
    last_seen_batch_id TEXT NOT NULL CHECK (last_seen_batch_id ~ '^[a-f0-9]{32}$'),
    last_applied_batch_id TEXT NOT NULL CHECK (last_applied_batch_id ~ '^[a-f0-9]{32}$'),
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, source_system_id, source_record_id),
    FOREIGN KEY (organization_id, last_seen_batch_id) REFERENCES directory_import_batches (organization_id, id),
    FOREIGN KEY (organization_id, last_applied_batch_id) REFERENCES directory_import_batches (organization_id, id)
);

CREATE INDEX directory_import_mappings_target_idx ON directory_import_mappings (organization_id, target_id);

-- Existing built-in administrator bundles gain the new integration permissions.
INSERT INTO guard_policy_bundle_permissions (organization_id, bundle_id, permission)
SELECT DISTINCT rb.organization_id, rb.bundle_id, permission
FROM guard_role_policy_bundles rb
JOIN guard_roles r ON r.organization_id = rb.organization_id AND r.id = rb.role_id
CROSS JOIN (VALUES ('integrations.read'), ('integrations.write')) requested(permission)
WHERE r.source = 'builtin'
ON CONFLICT DO NOTHING;
