-- StewardMesh Horizon -- named replacement plans that group many assets.
-- Requirement: REQ-HORIZON-001. Feature: lifecycle.planning.

CREATE TABLE horizon_replacement_plans (
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    id TEXT NOT NULL CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
    grouping TEXT NOT NULL CHECK (grouping IN ('department', 'building', 'type', 'manufacturer', 'site', 'custom')),
    group_key TEXT NOT NULL DEFAULT '' CHECK (char_length(group_key) <= 128),
    scenario TEXT NOT NULL CHECK (scenario ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
    revision BIGINT NOT NULL CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, id),
    CHECK (updated_at >= created_at)
);

CREATE INDEX horizon_replacement_plans_name_idx
    ON horizon_replacement_plans (organization_id, lower(name), id);

CREATE TABLE horizon_replacement_plan_assets (
    organization_id TEXT NOT NULL,
    plan_id TEXT NOT NULL,
    asset_id TEXT NOT NULL,
    PRIMARY KEY (organization_id, asset_id),
    FOREIGN KEY (organization_id, plan_id) REFERENCES horizon_replacement_plans (organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, asset_id) REFERENCES atlas_assets (organization_id, id) ON DELETE CASCADE
);

CREATE INDEX horizon_replacement_plan_assets_plan_idx
    ON horizon_replacement_plan_assets (organization_id, plan_id, asset_id);
