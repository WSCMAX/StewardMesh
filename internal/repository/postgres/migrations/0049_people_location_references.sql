-- Primary building/room on directory identities, plus typed location
-- references (office, instructor, class, dormitory, lab) for occupancy.

ALTER TABLE people_identities
    ADD COLUMN IF NOT EXISTS building_id TEXT,
    ADD COLUMN IF NOT EXISTS room_id TEXT;

ALTER TABLE people_identities
    DROP CONSTRAINT IF EXISTS people_identities_building_id_fkey,
    DROP CONSTRAINT IF EXISTS people_identities_room_id_fkey;

ALTER TABLE people_identities
    ADD CONSTRAINT people_identities_building_id_fkey
        FOREIGN KEY (organization_id, building_id) REFERENCES people_buildings (organization_id, id),
    ADD CONSTRAINT people_identities_room_id_fkey
        FOREIGN KEY (organization_id, room_id) REFERENCES people_rooms (organization_id, id);

CREATE INDEX IF NOT EXISTS people_identities_building_idx
    ON people_identities (organization_id, building_id)
    WHERE building_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS people_identities_room_idx
    ON people_identities (organization_id, room_id)
    WHERE room_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS people_location_reference_types (
    id TEXT NOT NULL,
    organization_id TEXT NOT NULL REFERENCES organizations (id),
    name TEXT NOT NULL,
    normalized_name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    relationship_kind TEXT NOT NULL,
    location_kind TEXT NOT NULL,
    status TEXT NOT NULL,
    revision BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, id),
    UNIQUE (organization_id, normalized_name),
    CHECK (status IN ('active', 'inactive')),
    CHECK (location_kind IN ('site', 'building', 'room')),
    CHECK (relationship_kind IN ('located_at', 'uses_office', 'teaches_in', 'attends_class', 'resides_in', 'uses_lab')),
    CHECK (revision >= 1)
);

CREATE TABLE IF NOT EXISTS people_location_references (
    id TEXT NOT NULL,
    organization_id TEXT NOT NULL REFERENCES organizations (id),
    identity_id TEXT NOT NULL,
    type_id TEXT NOT NULL,
    location_kind TEXT NOT NULL,
    location_id TEXT NOT NULL,
    priority TEXT NOT NULL,
    status TEXT NOT NULL,
    revision BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, id),
    FOREIGN KEY (organization_id, identity_id) REFERENCES people_identities (organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, type_id) REFERENCES people_location_reference_types (organization_id, id),
    CHECK (status IN ('active', 'inactive')),
    CHECK (priority IN ('primary', 'secondary')),
    CHECK (location_kind IN ('site', 'building', 'room')),
    CHECK (revision >= 1)
);

CREATE UNIQUE INDEX IF NOT EXISTS people_location_references_primary_idx
    ON people_location_references (organization_id, identity_id, type_id)
    WHERE status = 'active' AND priority = 'primary';

CREATE INDEX IF NOT EXISTS people_location_references_identity_idx
    ON people_location_references (organization_id, identity_id, status);
CREATE INDEX IF NOT EXISTS people_location_references_location_idx
    ON people_location_references (organization_id, location_kind, location_id, status);
CREATE INDEX IF NOT EXISTS people_location_references_type_idx
    ON people_location_references (organization_id, type_id, status);
