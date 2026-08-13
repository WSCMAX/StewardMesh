-- StewardMesh Atlas Catalog -- Atlas Model configurations, prices, and upgrade paths.
-- Requirement: REQ-ATLAS-CATALOG-001. Feature: inventory.catalog.

CREATE TABLE atlas_catalog_configurations (
    organization_id TEXT NOT NULL,
    id TEXT NOT NULL CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    model_id TEXT NOT NULL,
    name TEXT NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 200),
    sku TEXT NOT NULL DEFAULT '' CHECK (char_length(btrim(sku)) <= 128),
    status TEXT NOT NULL CHECK (status IN ('active', 'retired')),
    specifications JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(specifications) = 'object'),
    revision BIGINT NOT NULL CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, id),
    UNIQUE (organization_id, model_id, id),
    FOREIGN KEY (organization_id, model_id) REFERENCES atlas_models (organization_id, id) ON DELETE CASCADE,
    CHECK (updated_at >= created_at)
);

CREATE UNIQUE INDEX atlas_catalog_configurations_name_idx
    ON atlas_catalog_configurations (organization_id, model_id, lower(btrim(name)));

CREATE UNIQUE INDEX atlas_catalog_configurations_sku_idx
    ON atlas_catalog_configurations (organization_id, lower(btrim(sku)))
    WHERE btrim(sku) <> '';

CREATE TABLE atlas_catalog_prices (
    organization_id TEXT NOT NULL,
    id TEXT NOT NULL CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    model_id TEXT NOT NULL,
    configuration_id TEXT,
    price_kind TEXT NOT NULL CHECK (price_kind IN ('list', 'quote', 'contract', 'estimate')),
    amount_minor BIGINT NOT NULL CHECK (amount_minor BETWEEN 0 AND 9007199254740991),
    currency TEXT NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    effective_from DATE NOT NULL,
    effective_to DATE,
    source_reference TEXT NOT NULL DEFAULT '' CHECK (char_length(btrim(source_reference)) <= 200),
    revision BIGINT NOT NULL CHECK (revision = 1),
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, id),
    FOREIGN KEY (organization_id, model_id) REFERENCES atlas_models (organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, model_id, configuration_id)
        REFERENCES atlas_catalog_configurations (organization_id, model_id, id),
    CHECK (effective_to IS NULL OR effective_to >= effective_from)
);

CREATE INDEX atlas_catalog_prices_resolution_idx
    ON atlas_catalog_prices (organization_id, model_id, configuration_id, currency, effective_from DESC, effective_to);

CREATE TABLE atlas_catalog_upgrade_paths (
    organization_id TEXT NOT NULL,
    id TEXT NOT NULL CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    from_model_id TEXT NOT NULL,
    from_configuration_id TEXT,
    to_model_id TEXT NOT NULL,
    to_configuration_id TEXT,
    relationship_kind TEXT NOT NULL CHECK (relationship_kind IN ('successor', 'replacement', 'upgrade')),
    effective_from DATE NOT NULL,
    revision BIGINT NOT NULL CHECK (revision = 1),
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, id),
    FOREIGN KEY (organization_id, from_model_id) REFERENCES atlas_models (organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, from_model_id, from_configuration_id)
        REFERENCES atlas_catalog_configurations (organization_id, model_id, id),
    FOREIGN KEY (organization_id, to_model_id) REFERENCES atlas_models (organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, to_model_id, to_configuration_id)
        REFERENCES atlas_catalog_configurations (organization_id, model_id, id),
    CHECK (from_model_id <> to_model_id OR from_configuration_id IS DISTINCT FROM to_configuration_id)
);

CREATE UNIQUE INDEX atlas_catalog_upgrade_paths_identity_idx
    ON atlas_catalog_upgrade_paths (
        organization_id, from_model_id, COALESCE(from_configuration_id, ''),
        to_model_id, COALESCE(to_configuration_id, ''), relationship_kind, effective_from
    );

CREATE INDEX atlas_catalog_upgrade_paths_source_idx
    ON atlas_catalog_upgrade_paths (organization_id, from_model_id, from_configuration_id, effective_from DESC);
