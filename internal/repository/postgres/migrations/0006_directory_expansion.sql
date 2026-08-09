-- StewardMesh Directory Expansion -- structured sites, buildings, and rooms
-- Requirement: REQ-DIRECTORY-EXPANSION-001. Feature: identity.directory.

ALTER TABLE people_sites
    ADD COLUMN address_line1 TEXT NOT NULL DEFAULT '' CHECK (char_length(address_line1) <= 200),
    ADD COLUMN address_line2 TEXT NOT NULL DEFAULT '' CHECK (char_length(address_line2) <= 200),
    ADD COLUMN address_city TEXT NOT NULL DEFAULT '' CHECK (char_length(address_city) <= 100),
    ADD COLUMN address_region TEXT NOT NULL DEFAULT '' CHECK (char_length(address_region) <= 100),
    ADD COLUMN address_postal_code TEXT NOT NULL DEFAULT '' CHECK (char_length(address_postal_code) <= 32),
    ADD COLUMN address_country TEXT NOT NULL DEFAULT '' CHECK (address_country = '' OR address_country ~ '^[A-Z]{2}$'),
    ADD CONSTRAINT people_sites_address_complete CHECK (
        (
            address_line1 = '' AND address_line2 = '' AND address_city = ''
            AND address_region = '' AND address_postal_code = '' AND address_country = ''
        )
        OR (address_line1 <> '' AND address_city <> '' AND address_country ~ '^[A-Z]{2}$')
    );

CREATE TABLE people_buildings (
    id TEXT PRIMARY KEY CHECK (id ~ '^[a-f0-9]{32}$'),
    organization_id TEXT NOT NULL REFERENCES organizations(id),
    site_id TEXT NOT NULL,
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
    normalized_name TEXT NOT NULL CHECK (char_length(normalized_name) BETWEEN 1 AND 200),
    status TEXT NOT NULL CHECK (status IN ('active', 'inactive')),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (organization_id, site_id, normalized_name),
    UNIQUE (organization_id, site_id, id),
    FOREIGN KEY (organization_id, site_id)
        REFERENCES people_sites (organization_id, id),
    CHECK (updated_at >= created_at)
);

CREATE TABLE people_rooms (
    id TEXT PRIMARY KEY CHECK (id ~ '^[a-f0-9]{32}$'),
    organization_id TEXT NOT NULL REFERENCES organizations(id),
    site_id TEXT NOT NULL,
    building_id TEXT NOT NULL,
    room_number TEXT NOT NULL CHECK (char_length(room_number) BETWEEN 1 AND 100),
    normalized_number TEXT NOT NULL CHECK (char_length(normalized_number) BETWEEN 1 AND 100),
    name TEXT NOT NULL DEFAULT '' CHECK (char_length(name) <= 200),
    status TEXT NOT NULL CHECK (status IN ('active', 'inactive')),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (organization_id, building_id, normalized_number),
    FOREIGN KEY (organization_id, site_id)
        REFERENCES people_sites (organization_id, id),
    FOREIGN KEY (organization_id, site_id, building_id)
        REFERENCES people_buildings (organization_id, site_id, id),
    CHECK (updated_at >= created_at)
);

CREATE INDEX people_buildings_site_idx
    ON people_buildings (organization_id, site_id, normalized_name, id);

CREATE INDEX people_rooms_building_idx
    ON people_rooms (organization_id, site_id, building_id, normalized_number, id);

CREATE TABLE directory_import_batches (
    id TEXT PRIMARY KEY CHECK (id ~ '^[a-f0-9]{32}$'),
    organization_id TEXT NOT NULL REFERENCES organizations(id),
    provider TEXT NOT NULL CHECK (provider IN ('entra', 'sailpoint', 'grouper', 'peoplesoft')),
    dry_run BOOLEAN NOT NULL,
    status TEXT NOT NULL,
    created_count INT NOT NULL DEFAULT 0,
    updated_count INT NOT NULL DEFAULT 0,
    conflict_count INT NOT NULL DEFAULT 0,
    error_count INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL
);
