-- StewardMesh Atlas Models -- immutable model defaults and override provenance
-- Requirement: REQ-ATLAS-MODELS-001. Feature: inventory.models. GitHub: #74.

ALTER TABLE atlas_assets
    ADD COLUMN model_context JSONB NOT NULL DEFAULT '{}'::jsonb
        CHECK (jsonb_typeof(model_context) = 'object');

-- Existing model links predate the snapshot column. Capture the best durable
-- provenance available without changing any instance-specific asset values.
UPDATE atlas_assets AS asset
SET model_context = jsonb_build_object(
    'manufacturer', model.manufacturer,
    'name', model.name,
    'modelNumber', model.model_number,
    'kind', model.kind,
    'vendorIdentifier', model.vendor_identifier,
    'specifications', model.specifications,
    'supportUrl', model.support_url,
    'warrantyMonths', model.warranty_months,
    'usefulLifeMonths', model.useful_life_months,
    'sourceSystemId', model.source_system_id,
    'sourceRecordId', model.source_record_id,
    'modelRevision', model.revision,
    'defaultsEffectiveAt', model.updated_at,
    'appliedAt', CURRENT_TIMESTAMP,
    'overrides', CASE WHEN asset.kind = model.kind THEN '[]'::jsonb ELSE '["kind"]'::jsonb END
)
FROM atlas_models AS model
WHERE asset.organization_id = model.organization_id
  AND asset.model_id = model.id;

ALTER TABLE atlas_assets
    ADD CONSTRAINT atlas_assets_model_context_consistency_check CHECK (
        (model_id IS NULL AND model_context = '{}'::jsonb)
        OR
        (model_id IS NOT NULL
            AND model_context ?& ARRAY[
                'manufacturer', 'name', 'kind', 'modelRevision',
                'defaultsEffectiveAt', 'appliedAt', 'overrides'
            ]
            AND jsonb_typeof(model_context -> 'modelRevision') = 'number'
            AND jsonb_typeof(model_context -> 'overrides') = 'array')
    );
