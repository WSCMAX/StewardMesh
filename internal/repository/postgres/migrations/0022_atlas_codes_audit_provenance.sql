-- StewardMesh Atlas Codes -- durable audit provenance for repairable mutations.
-- Requirement: REQ-ATLAS-CODES-001. Feature: inventory.identifiers.

ALTER TABLE atlas_asset_identifiers
    ADD COLUMN created_correlation_id TEXT,
    ADD COLUMN updated_by TEXT,
    ADD COLUMN updated_correlation_id TEXT;

-- Migration 0021 predates durable retry provenance. Recover it from the
-- immutable audit log where possible and use an explicit migration identity
-- only for records that never reached their audit write.
UPDATE atlas_asset_identifiers AS identifier
SET created_correlation_id = COALESCE(
        (
            SELECT event.correlation_id
            FROM audit_events AS event
            WHERE event.organization_id = identifier.organization_id
              AND event.resource_type = 'asset_identifier'
              AND event.resource_id = identifier.id
              AND event.correlation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
              AND event.action = CASE
                    WHEN identifier.supersedes_id IS NULL THEN 'atlas.identifier.created'
                    ELSE 'atlas.identifier.replaced'
                  END
            ORDER BY event.occurred_at, event.id
            LIMIT 1
        ),
        'migration:atlas-codes-provenance'
    ),
    updated_by = COALESCE(
        (
            SELECT event.actor_id
            FROM audit_events AS event
            WHERE event.organization_id = identifier.organization_id
              AND event.resource_type = 'asset_identifier'
              AND event.actor_id = btrim(event.actor_id)
              AND char_length(event.actor_id) BETWEEN 1 AND 128
              AND (
                    (identifier.status = 'deactivated'
                        AND event.resource_id = identifier.id
                        AND event.action = 'atlas.identifier.deactivated')
                    OR
                    (identifier.status = 'replaced'
                        AND event.resource_id = identifier.replaced_by_id
                        AND event.action = 'atlas.identifier.replaced')
                  )
            ORDER BY event.occurred_at DESC, event.id DESC
            LIMIT 1
        ),
        CASE
            WHEN identifier.status = 'active' THEN NULLIF(btrim(identifier.created_by), '')
            ELSE 'system:atlas-codes'
        END,
        'system:atlas-codes'
    ),
    updated_correlation_id = COALESCE(
        (
            SELECT event.correlation_id
            FROM audit_events AS event
            WHERE event.organization_id = identifier.organization_id
              AND event.resource_type = 'asset_identifier'
              AND event.correlation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
              AND (
                    (identifier.status = 'deactivated'
                        AND event.resource_id = identifier.id
                        AND event.action = 'atlas.identifier.deactivated')
                    OR
                    (identifier.status = 'replaced'
                        AND event.resource_id = identifier.replaced_by_id
                        AND event.action = 'atlas.identifier.replaced')
                  )
            ORDER BY event.occurred_at DESC, event.id DESC
            LIMIT 1
        ),
        (
            SELECT event.correlation_id
            FROM audit_events AS event
            WHERE event.organization_id = identifier.organization_id
              AND event.resource_type = 'asset_identifier'
              AND event.resource_id = identifier.id
              AND event.correlation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
              AND event.action = CASE
                    WHEN identifier.supersedes_id IS NULL THEN 'atlas.identifier.created'
                    ELSE 'atlas.identifier.replaced'
                  END
            ORDER BY event.occurred_at, event.id
            LIMIT 1
        ),
        'migration:atlas-codes-provenance'
    );

ALTER TABLE atlas_asset_identifiers
    ALTER COLUMN created_correlation_id SET NOT NULL,
    ALTER COLUMN updated_by SET NOT NULL,
    ALTER COLUMN updated_correlation_id SET NOT NULL,
    ADD CONSTRAINT atlas_asset_identifiers_created_correlation_check CHECK (
        created_correlation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
    ),
    ADD CONSTRAINT atlas_asset_identifiers_updated_by_check CHECK (
        char_length(updated_by) BETWEEN 1 AND 128
        AND updated_by = btrim(updated_by)
    ),
    ADD CONSTRAINT atlas_asset_identifiers_updated_correlation_check CHECK (
        updated_correlation_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
    );
