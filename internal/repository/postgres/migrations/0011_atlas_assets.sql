-- StewardMesh Atlas -- durable organization-scoped assets and lifecycle history
-- Requirement: REQ-ATLAS-001. Feature: inventory.assets.

CREATE UNIQUE INDEX people_rooms_organization_location_idx
    ON people_rooms (organization_id, site_id, building_id, id);

CREATE TABLE atlas_assets (
    organization_id TEXT NOT NULL REFERENCES organizations(id),
    id TEXT NOT NULL CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
    kind TEXT NOT NULL CHECK (kind IN (
        'server', 'computer', 'desktop', 'laptop', 'tablet', 'phone',
        'network', 'peripheral', 'virtual', 'other'
    )),
    asset_tag TEXT NOT NULL DEFAULT '' CHECK (char_length(asset_tag) <= 128),
    normalized_asset_tag TEXT NOT NULL DEFAULT '' CHECK (char_length(normalized_asset_tag) <= 128),
    serial_number TEXT NOT NULL DEFAULT '' CHECK (char_length(serial_number) <= 255),
    normalized_serial_number TEXT NOT NULL DEFAULT '' CHECK (char_length(normalized_serial_number) <= 255),
    hostname TEXT NOT NULL DEFAULT '' CHECK (char_length(hostname) <= 253),
    site_id TEXT,
    building_id TEXT,
    room_id TEXT,
    department_id TEXT,
    user_id TEXT,
    status TEXT NOT NULL CHECK (status IN ('draft', 'active', 'inactive', 'retired', 'disposed')),
    purchase_date DATE,
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, id),
    FOREIGN KEY (organization_id, site_id)
        REFERENCES people_sites (organization_id, id),
    FOREIGN KEY (organization_id, site_id, building_id)
        REFERENCES people_buildings (organization_id, site_id, id),
    FOREIGN KEY (organization_id, site_id, building_id, room_id)
        REFERENCES people_rooms (organization_id, site_id, building_id, id),
    FOREIGN KEY (organization_id, department_id)
        REFERENCES people_departments (organization_id, id),
    FOREIGN KEY (organization_id, user_id)
        REFERENCES people_identities (organization_id, id),
    CHECK (normalized_asset_tag = lower(btrim(asset_tag))),
    CHECK (normalized_serial_number = lower(btrim(serial_number))),
    CHECK (building_id IS NULL OR site_id IS NOT NULL),
    CHECK (room_id IS NULL OR (site_id IS NOT NULL AND building_id IS NOT NULL)),
    CHECK (updated_at >= created_at)
);

CREATE UNIQUE INDEX atlas_assets_asset_tag_idx
    ON atlas_assets (organization_id, normalized_asset_tag)
    WHERE normalized_asset_tag <> '';

CREATE UNIQUE INDEX atlas_assets_serial_number_idx
    ON atlas_assets (organization_id, normalized_serial_number)
    WHERE normalized_serial_number <> '';

CREATE INDEX atlas_assets_search_idx
    ON atlas_assets (organization_id, status, kind, lower(name), id);

CREATE INDEX atlas_assets_scope_idx
    ON atlas_assets (organization_id, site_id, department_id, user_id);

CREATE TABLE atlas_asset_lifecycle_events (
    organization_id TEXT NOT NULL,
    id TEXT NOT NULL CHECK (id ~ '^[a-f0-9]{32}$'),
    asset_id TEXT NOT NULL,
    from_status TEXT NOT NULL DEFAULT '' CHECK (
        from_status = '' OR from_status IN ('draft', 'active', 'inactive', 'retired', 'disposed')
    ),
    to_status TEXT NOT NULL CHECK (to_status IN ('draft', 'active', 'inactive', 'retired', 'disposed')),
    note TEXT NOT NULL DEFAULT '' CHECK (char_length(note) <= 1000),
    revision BIGINT NOT NULL CHECK (revision > 0),
    actor_id TEXT NOT NULL CHECK (char_length(actor_id) BETWEEN 1 AND 128),
    occurred_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, id),
    FOREIGN KEY (organization_id, asset_id)
        REFERENCES atlas_assets (organization_id, id),
    UNIQUE (organization_id, asset_id, revision)
);

CREATE INDEX atlas_asset_lifecycle_history_idx
    ON atlas_asset_lifecycle_events (organization_id, asset_id, revision, occurred_at, id);
