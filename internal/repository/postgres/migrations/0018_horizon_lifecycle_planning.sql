-- StewardMesh Horizon -- effective-dated asset lifecycle plans and immutable versions.
-- Requirement: REQ-HORIZON-001. Feature: lifecycle.planning.

CREATE TABLE horizon_plans (
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    id TEXT NOT NULL CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    asset_id TEXT NOT NULL,
    scenario TEXT NOT NULL CHECK (scenario ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
    expected_useful_life_months INTEGER NOT NULL CHECK (expected_useful_life_months BETWEEN 1 AND 1200),
    replacement_date DATE,
    lifecycle_stage TEXT NOT NULL CHECK (lifecycle_stage IN ('planned', 'in_service', 'refresh_due', 'approved', 'retired')),
    replacement_cost_minor BIGINT NOT NULL CHECK (replacement_cost_minor >= 0),
    currency TEXT NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    effective_from DATE NOT NULL,
    revision BIGINT NOT NULL CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, id),
    FOREIGN KEY (organization_id, asset_id) REFERENCES atlas_assets (organization_id, id),
    UNIQUE (organization_id, asset_id, scenario),
    CHECK (updated_at >= created_at)
);

CREATE INDEX horizon_plans_forecast_idx
    ON horizon_plans (organization_id, scenario, effective_from, replacement_date, lifecycle_stage);

CREATE TABLE horizon_plan_versions (
    organization_id TEXT NOT NULL,
    plan_id TEXT NOT NULL,
    asset_id TEXT NOT NULL,
    scenario TEXT NOT NULL CHECK (scenario ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
    expected_useful_life_months INTEGER NOT NULL CHECK (expected_useful_life_months BETWEEN 1 AND 1200),
    replacement_date DATE,
    lifecycle_stage TEXT NOT NULL CHECK (lifecycle_stage IN ('planned', 'in_service', 'refresh_due', 'approved', 'retired')),
    replacement_cost_minor BIGINT NOT NULL CHECK (replacement_cost_minor >= 0),
    currency TEXT NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    effective_from DATE NOT NULL,
    revision BIGINT NOT NULL CHECK (revision > 0),
    actor_id TEXT NOT NULL CHECK (char_length(actor_id) BETWEEN 1 AND 128),
    recorded_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, plan_id, revision),
    FOREIGN KEY (organization_id, plan_id) REFERENCES horizon_plans (organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, asset_id) REFERENCES atlas_assets (organization_id, id),
    UNIQUE (organization_id, plan_id, effective_from, revision)
);

CREATE INDEX horizon_plan_versions_effective_idx
    ON horizon_plan_versions (organization_id, plan_id, effective_from DESC, revision DESC);
