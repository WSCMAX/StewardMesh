-- StewardMesh Reach -- safe provider configuration, subscriber groups, templates, delivery attempts, and Signals processing.
-- Requirements: REQ-REACH-001, REQ-SIGNALS-001, SEC-GUARD-001. Feature: messaging.delivery. GitHub: #12.

CREATE TABLE reach_providers (
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    id TEXT NOT NULL CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 160),
    kind TEXT NOT NULL CHECK (kind IN ('smtp','ses','gmail_oauth','outlook_oauth','teams','webhook')),
    endpoint_id TEXT NOT NULL CHECK (endpoint_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    sender TEXT CHECK (char_length(sender) <= 320),
    secret_ref TEXT NOT NULL CHECK (secret_ref ~ '^(env|external):[A-Za-z0-9][A-Za-z0-9._-]{0,95}$'),
    enabled BOOLEAN NOT NULL,
    revision BIGINT NOT NULL CHECK (revision > 0),
    created_by TEXT NOT NULL CHECK (char_length(created_by) BETWEEN 1 AND 200),
    updated_by TEXT NOT NULL CHECK (char_length(updated_by) BETWEEN 1 AND 200),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL CHECK (updated_at >= created_at),
    PRIMARY KEY (organization_id,id),
    CHECK ((kind IN ('smtp','ses','gmail_oauth','outlook_oauth')) = (sender IS NOT NULL))
);
CREATE UNIQUE INDEX reach_providers_name_unique ON reach_providers(organization_id,lower(btrim(name)));

CREATE TABLE reach_templates (
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    id TEXT NOT NULL CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 160),
    subject TEXT NOT NULL CHECK (char_length(subject) BETWEEN 1 AND 200),
    body TEXT NOT NULL CHECK (char_length(body) BETWEEN 1 AND 4000),
    revision BIGINT NOT NULL CHECK (revision > 0),
    created_by TEXT NOT NULL CHECK (char_length(created_by) BETWEEN 1 AND 200),
    updated_by TEXT NOT NULL CHECK (char_length(updated_by) BETWEEN 1 AND 200),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL CHECK (updated_at >= created_at),
    PRIMARY KEY (organization_id,id)
);
CREATE UNIQUE INDEX reach_templates_name_unique ON reach_templates(organization_id,lower(btrim(name)));

CREATE TABLE reach_subscriber_groups (
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    id TEXT NOT NULL CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 160),
    provider_id TEXT NOT NULL,
    template_id TEXT NOT NULL,
    recipients JSONB NOT NULL CHECK (jsonb_typeof(recipients)='array' AND jsonb_array_length(recipients) BETWEEN 1 AND 100),
    revision BIGINT NOT NULL CHECK (revision > 0),
    created_by TEXT NOT NULL CHECK (char_length(created_by) BETWEEN 1 AND 200),
    updated_by TEXT NOT NULL CHECK (char_length(updated_by) BETWEEN 1 AND 200),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL CHECK (updated_at >= created_at),
    PRIMARY KEY (organization_id,id),
    FOREIGN KEY (organization_id,provider_id) REFERENCES reach_providers(organization_id,id),
    FOREIGN KEY (organization_id,template_id) REFERENCES reach_templates(organization_id,id)
);
CREATE UNIQUE INDEX reach_subscriber_groups_name_unique ON reach_subscriber_groups(organization_id,lower(btrim(name)));

CREATE TABLE reach_messages (
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    id TEXT NOT NULL CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    group_id TEXT,
    provider_id TEXT NOT NULL CHECK (provider_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    template_id TEXT,
    source_kind TEXT NOT NULL CHECK (source_kind IN ('manual','signals')),
    source_id TEXT,
    subject TEXT NOT NULL CHECK (char_length(subject) BETWEEN 1 AND 200),
    body TEXT NOT NULL CHECK (char_length(body) BETWEEN 1 AND 4000),
    recipients JSONB NOT NULL CHECK (jsonb_typeof(recipients)='array' AND jsonb_array_length(recipients) <= 100),
    status TEXT NOT NULL CHECK (status IN ('queued','retrying','delivered','failed')),
    attempts INTEGER NOT NULL CHECK (attempts BETWEEN 0 AND 8),
    next_attempt_at TIMESTAMPTZ,
    last_error_code TEXT CHECK (last_error_code ~ '^[a-z][a-z0-9_]{0,63}$'),
    created_by TEXT NOT NULL CHECK (char_length(created_by) BETWEEN 1 AND 200),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL CHECK (updated_at >= created_at),
    PRIMARY KEY (organization_id,id),
    FOREIGN KEY (organization_id,group_id) REFERENCES reach_subscriber_groups(organization_id,id),
    CHECK ((source_kind='signals') = (source_id IS NOT NULL))
);
CREATE INDEX reach_messages_history_idx ON reach_messages(organization_id,created_at DESC,id);
CREATE INDEX reach_messages_retry_idx ON reach_messages(organization_id,next_attempt_at,id) WHERE status='retrying';

CREATE TABLE reach_delivery_attempts (
    organization_id TEXT NOT NULL,
    id TEXT NOT NULL CHECK (id ~ '^[a-f0-9]{32}$'),
    message_id TEXT NOT NULL,
    attempt INTEGER NOT NULL CHECK (attempt BETWEEN 1 AND 8),
    outcome TEXT NOT NULL CHECK (outcome IN ('succeeded','retrying','failed')),
    error_code TEXT CHECK (error_code ~ '^[a-z][a-z0-9_]{0,63}$'),
    retryable BOOLEAN NOT NULL,
    next_attempt_at TIMESTAMPTZ,
    occurred_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id,id),
    UNIQUE (organization_id,message_id,attempt),
    FOREIGN KEY (organization_id,message_id) REFERENCES reach_messages(organization_id,id) ON DELETE CASCADE,
    CHECK ((outcome='succeeded') = (error_code IS NULL)),
    CHECK ((outcome='retrying') = retryable),
    CHECK ((outcome='retrying') = (next_attempt_at IS NOT NULL))
);

CREATE TABLE reach_provider_tests (
    organization_id TEXT NOT NULL,
    id TEXT NOT NULL CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    provider_id TEXT NOT NULL,
    outcome TEXT NOT NULL CHECK (outcome IN ('succeeded','failed')),
    error_code TEXT CHECK (error_code ~ '^[a-z][a-z0-9_]{0,63}$'),
    tested_by TEXT NOT NULL CHECK (char_length(tested_by) BETWEEN 1 AND 200),
    tested_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id,id),
    FOREIGN KEY (organization_id,provider_id) REFERENCES reach_providers(organization_id,id) ON DELETE CASCADE,
    CHECK ((outcome='succeeded') = (error_code IS NULL))
);
CREATE INDEX reach_provider_tests_history_idx ON reach_provider_tests(organization_id,provider_id,tested_at DESC);

INSERT INTO guard_policy_bundle_permissions (organization_id,bundle_id,permission)
SELECT DISTINCT rb.organization_id,rb.bundle_id,permission.name
FROM guard_role_policy_bundles rb JOIN guard_roles r ON r.organization_id=rb.organization_id AND r.id=rb.role_id
CROSS JOIN (VALUES ('messaging.read'),('messaging.write')) AS permission(name)
WHERE r.source='builtin' AND lower(btrim(r.name))='administrator'
ON CONFLICT (bundle_id,permission) DO NOTHING;
