-- StewardMesh Atlas Models -- reusable product model defaults and asset links
-- Requirement: REQ-ATLAS-MODELS-001. Feature: inventory.models.

CREATE TABLE atlas_models (
    organization_id TEXT NOT NULL REFERENCES organizations(id),
    id TEXT NOT NULL CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    manufacturer TEXT NOT NULL CHECK (char_length(manufacturer) BETWEEN 1 AND 120),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 160),
    model_number TEXT NOT NULL DEFAULT '' CHECK (char_length(model_number) <= 120),
    normalized_manufacturer TEXT NOT NULL CHECK (normalized_manufacturer = lower(btrim(manufacturer))),
    normalized_name TEXT NOT NULL CHECK (normalized_name = lower(btrim(name))),
    normalized_model_number TEXT NOT NULL CHECK (normalized_model_number = lower(btrim(model_number))),
    kind TEXT NOT NULL CHECK (kind IN (
        'server', 'computer', 'desktop', 'laptop', 'tablet', 'phone',
        'network', 'peripheral', 'virtual', 'other'
    )),
    vendor_identifier TEXT NOT NULL DEFAULT '' CHECK (char_length(vendor_identifier) <= 160),
    specifications JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(specifications) = 'object'),
    support_url TEXT NOT NULL DEFAULT '' CHECK (char_length(support_url) <= 500),
    warranty_months INTEGER NOT NULL DEFAULT 0 CHECK (warranty_months BETWEEN 0 AND 1200),
    useful_life_months INTEGER NOT NULL DEFAULT 0 CHECK (useful_life_months BETWEEN 0 AND 1200),
    status TEXT NOT NULL CHECK (status IN ('active', 'retired')),
    source_system_id TEXT NOT NULL DEFAULT '' CHECK (char_length(source_system_id) <= 120),
    source_record_id TEXT NOT NULL DEFAULT '' CHECK (char_length(source_record_id) <= 160),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, id),
    UNIQUE (organization_id, normalized_manufacturer, normalized_name, normalized_model_number),
    CHECK (support_url = '' OR support_url ~ '^https://[A-Za-z0-9][A-Za-z0-9.-]*(?::[0-9]{1,5})?(/.*)?$'),
    CHECK (updated_at >= created_at)
);

CREATE INDEX atlas_models_search_idx
    ON atlas_models (organization_id, status, kind, normalized_manufacturer, normalized_name, normalized_model_number, id);

ALTER TABLE atlas_assets
    ADD COLUMN model_id TEXT,
    ADD CONSTRAINT atlas_assets_model_fk
        FOREIGN KEY (organization_id, model_id) REFERENCES atlas_models (organization_id, id);

CREATE INDEX atlas_assets_model_idx
    ON atlas_assets (organization_id, model_id)
    WHERE model_id IS NOT NULL;
