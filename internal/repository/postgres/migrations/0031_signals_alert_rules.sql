-- StewardMesh Signals -- durable rules, deduplicated alerts, subscriptions, and Reach delivery handoff.
-- Requirements: REQ-SIGNALS-001, SEC-GUARD-001. Feature: alerts.rules. GitHub: #11.

CREATE TABLE signal_rules (
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    id TEXT NOT NULL CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 160),
    condition TEXT NOT NULL CHECK (condition IN ('over_budget','forecast_over_budget','unpaid','overdue','expiration','renewal','unused_commitment','reconciliation')),
    severity TEXT NOT NULL CHECK (severity IN ('info','warning','critical')),
    enabled BOOLEAN NOT NULL,
    threshold_days INTEGER[] NOT NULL DEFAULT '{}'
        CHECK (cardinality(threshold_days) <= 8 AND 0 <= ALL(threshold_days) AND 3660 >= ALL(threshold_days)),
    fiscal_period TEXT CHECK (fiscal_period ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,31}$'),
    scenario TEXT CHECK (scenario ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
    created_by TEXT NOT NULL CHECK (char_length(created_by) BETWEEN 1 AND 200),
    revision BIGINT NOT NULL CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL CHECK (updated_at >= created_at),
    PRIMARY KEY (organization_id,id),
    CHECK (
        (condition IN ('expiration','renewal','unpaid','overdue','unused_commitment') AND cardinality(threshold_days) > 0)
        OR (condition IN ('over_budget','forecast_over_budget','reconciliation') AND cardinality(threshold_days) = 0)
    ),
    CHECK ((fiscal_period IS NULL AND scenario IS NULL) OR condition IN ('over_budget','forecast_over_budget','unused_commitment','reconciliation'))
);
CREATE UNIQUE INDEX signal_rules_name_unique ON signal_rules(organization_id,lower(btrim(name)));

CREATE TABLE signal_alerts (
    organization_id TEXT NOT NULL,
    id TEXT NOT NULL CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    rule_id TEXT NOT NULL,
    condition TEXT NOT NULL CHECK (condition IN ('over_budget','forecast_over_budget','unpaid','overdue','expiration','renewal','unused_commitment','reconciliation')),
    severity TEXT NOT NULL CHECK (severity IN ('info','warning','critical')),
    status TEXT NOT NULL CHECK (status IN ('active','acknowledged','resolved')),
    title TEXT NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    summary TEXT NOT NULL CHECK (char_length(summary) BETWEEN 1 AND 500),
    target_type TEXT NOT NULL CHECK (target_type ~ '^[a-z][a-z0-9._-]{0,63}$'),
    target_id TEXT NOT NULL CHECK (target_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    due_at TIMESTAMPTZ,
    threshold_days INTEGER NOT NULL CHECK (threshold_days BETWEEN 0 AND 3660),
    deduplication_key TEXT NOT NULL CHECK (deduplication_key ~ '^[a-f0-9]{64}$'),
    assigned_kind TEXT CHECK (assigned_kind IN ('identity','group')),
    assigned_id TEXT CHECK (assigned_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    acknowledged_by TEXT,
    acknowledged_at TIMESTAMPTZ,
    first_detected_at TIMESTAMPTZ NOT NULL,
    last_observed_at TIMESTAMPTZ NOT NULL CHECK (last_observed_at >= first_detected_at),
    resolved_at TIMESTAMPTZ,
    revision BIGINT NOT NULL CHECK (revision > 0),
    PRIMARY KEY (organization_id,id),
    UNIQUE (organization_id,deduplication_key),
    FOREIGN KEY (organization_id,rule_id) REFERENCES signal_rules(organization_id,id),
    CHECK ((assigned_kind IS NULL) = (assigned_id IS NULL)),
    CHECK ((acknowledged_by IS NULL) = (acknowledged_at IS NULL)),
    CHECK ((status='resolved') = (resolved_at IS NOT NULL))
);
CREATE INDEX signal_alerts_queue_idx ON signal_alerts(organization_id,status,severity,last_observed_at DESC);

CREATE TABLE signal_alert_history (
    organization_id TEXT NOT NULL,
    id TEXT NOT NULL CHECK (id ~ '^[a-f0-9]{32}$'),
    alert_id TEXT NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('created','refreshed','reopened','resolved','acknowledged','assigned')),
    actor_id TEXT NOT NULL CHECK (char_length(actor_id) BETWEEN 1 AND 200),
    occurred_at TIMESTAMPTZ NOT NULL,
    revision BIGINT NOT NULL CHECK (revision > 0),
    PRIMARY KEY (organization_id,id),
    UNIQUE (organization_id,alert_id,revision),
    FOREIGN KEY (organization_id,alert_id) REFERENCES signal_alerts(organization_id,id) ON DELETE CASCADE
);

CREATE TABLE signal_subscriptions (
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    id TEXT NOT NULL CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    rule_id TEXT,
    target_kind TEXT NOT NULL CHECK (target_kind IN ('group','webhook')),
    target_id TEXT NOT NULL CHECK (target_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    enabled BOOLEAN NOT NULL,
    created_by TEXT NOT NULL CHECK (char_length(created_by) BETWEEN 1 AND 200),
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id,id),
    UNIQUE NULLS NOT DISTINCT (organization_id,rule_id,target_kind,target_id),
    FOREIGN KEY (organization_id,rule_id) REFERENCES signal_rules(organization_id,id) ON DELETE CASCADE
);

CREATE TABLE signal_deliveries (
    organization_id TEXT NOT NULL,
    id TEXT NOT NULL CHECK (id ~ '^[a-f0-9]{32}$'),
    alert_id TEXT NOT NULL,
    subscription_id TEXT NOT NULL,
    target_kind TEXT NOT NULL CHECK (target_kind IN ('group','webhook')),
    target_id TEXT NOT NULL CHECK (target_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    status TEXT NOT NULL CHECK (status IN ('pending','delivered','failed')),
    attempts INTEGER NOT NULL CHECK (attempts BETWEEN 0 AND 8),
    next_attempt_at TIMESTAMPTZ,
    last_error_code TEXT CHECK (last_error_code ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL CHECK (updated_at >= created_at),
    PRIMARY KEY (organization_id,id),
    FOREIGN KEY (organization_id,alert_id) REFERENCES signal_alerts(organization_id,id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id,subscription_id) REFERENCES signal_subscriptions(organization_id,id) ON DELETE CASCADE
);
CREATE INDEX signal_deliveries_pending_idx ON signal_deliveries(organization_id,next_attempt_at,id) WHERE status='pending';

INSERT INTO guard_policy_bundle_permissions (organization_id,bundle_id,permission)
SELECT DISTINCT rb.organization_id,rb.bundle_id,permission.name
FROM guard_role_policy_bundles rb JOIN guard_roles r ON r.organization_id=rb.organization_id AND r.id=rb.role_id
CROSS JOIN (VALUES ('signals.read'),('signals.write')) AS permission(name)
WHERE r.source='builtin' AND lower(btrim(r.name))='administrator'
ON CONFLICT (bundle_id,permission) DO NOTHING;
