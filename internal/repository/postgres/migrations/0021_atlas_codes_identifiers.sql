-- StewardMesh Atlas Codes -- durable barcode and QR identifier associations.
-- Requirement: REQ-ATLAS-CODES-001. Feature: inventory.identifiers.

CREATE TABLE atlas_asset_identifiers (
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    id TEXT NOT NULL CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    asset_id TEXT NOT NULL,
    symbology TEXT NOT NULL CHECK (symbology IN ('code128', 'qr')),
    normalized_value TEXT NOT NULL CHECK (
        normalized_value = btrim(normalized_value)
        AND normalized_value !~ '[[:cntrl:]]'
        AND (
            (symbology = 'code128' AND octet_length(normalized_value) BETWEEN 1 AND 128
                AND normalized_value ~ '^[ -~]+$')
            OR
            (symbology = 'qr' AND octet_length(normalized_value) BETWEEN 1 AND 512)
        )
    ),
    display_value TEXT NOT NULL CHECK (
        octet_length(display_value) BETWEEN 1 AND 512
        AND display_value = btrim(display_value)
        AND display_value !~ '[[:cntrl:]]'
    ),
    source TEXT NOT NULL CHECK (source IN ('user_entered', 'imported', 'generated')),
    is_primary BOOLEAN NOT NULL DEFAULT FALSE,
    status TEXT NOT NULL CHECK (status IN ('active', 'replaced', 'deactivated')),
    supersedes_id TEXT,
    replaced_by_id TEXT,
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_by TEXT NOT NULL CHECK (char_length(created_by) BETWEEN 1 AND 128),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    deactivated_at TIMESTAMPTZ,
    PRIMARY KEY (organization_id, id),
    UNIQUE (organization_id, asset_id, id),
    FOREIGN KEY (organization_id, asset_id)
        REFERENCES atlas_assets (organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, asset_id, supersedes_id)
        REFERENCES atlas_asset_identifiers (organization_id, asset_id, id)
        DEFERRABLE INITIALLY DEFERRED,
    FOREIGN KEY (organization_id, asset_id, replaced_by_id)
        REFERENCES atlas_asset_identifiers (organization_id, asset_id, id)
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT atlas_asset_identifiers_history_check CHECK (
        (status = 'active' AND deactivated_at IS NULL AND replaced_by_id IS NULL)
        OR
        (status = 'replaced' AND deactivated_at IS NOT NULL AND replaced_by_id IS NOT NULL)
        OR
        (status = 'deactivated' AND deactivated_at IS NOT NULL AND replaced_by_id IS NULL)
    ),
    CONSTRAINT atlas_asset_identifiers_self_link_check CHECK (
        (supersedes_id IS NULL OR supersedes_id <> id)
        AND (replaced_by_id IS NULL OR replaced_by_id <> id)
    ),
    CONSTRAINT atlas_asset_identifiers_time_check CHECK (
        updated_at >= created_at
        AND (deactivated_at IS NULL OR deactivated_at >= created_at)
    )
);

CREATE UNIQUE INDEX atlas_asset_identifiers_active_value_idx
    ON atlas_asset_identifiers (organization_id, symbology, normalized_value)
    WHERE status = 'active';

CREATE UNIQUE INDEX atlas_asset_identifiers_active_primary_idx
    ON atlas_asset_identifiers (organization_id, asset_id)
    WHERE status = 'active' AND is_primary;

CREATE INDEX atlas_asset_identifiers_asset_history_idx
    ON atlas_asset_identifiers (organization_id, asset_id, status, updated_at DESC, id);

CREATE INDEX atlas_asset_identifiers_supersedes_idx
    ON atlas_asset_identifiers (organization_id, supersedes_id)
    WHERE supersedes_id IS NOT NULL;
