-- StewardMesh Atlas and Horizon -- asset lifecycle dates, model end-of-life, and kind defaults.
-- Requirement: REQ-HORIZON-001, REQ-ATLAS-001. Feature: lifecycle.planning, inventory.assets.

ALTER TABLE atlas_assets
    ADD COLUMN lifecycle_start_date DATE,
    ADD COLUMN installed_date DATE,
    ADD COLUMN replacement_model_id TEXT;

ALTER TABLE atlas_assets
    ADD CONSTRAINT atlas_assets_replacement_model_fk
        FOREIGN KEY (organization_id, replacement_model_id) REFERENCES atlas_models (organization_id, id);

ALTER TABLE atlas_models
    ADD COLUMN last_effective_date DATE;

CREATE TABLE horizon_kind_defaults (
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    asset_kind TEXT NOT NULL CHECK (asset_kind IN (
        'server', 'computer', 'desktop', 'laptop', 'tablet', 'phone',
        'network', 'peripheral', 'virtual', 'other'
    )),
    scenario TEXT NOT NULL CHECK (scenario ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
    expected_useful_life_months INTEGER NOT NULL CHECK (expected_useful_life_months BETWEEN 1 AND 1200),
    replacement_model_id TEXT,
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, asset_kind, scenario),
    FOREIGN KEY (organization_id, replacement_model_id) REFERENCES atlas_models (organization_id, id),
    CHECK (updated_at >= created_at)
);

CREATE INDEX horizon_kind_defaults_lookup_idx
    ON horizon_kind_defaults (organization_id, scenario, asset_kind);
