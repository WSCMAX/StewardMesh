-- Additional current users on an Atlas asset, beside the primary user_id.
-- Requirements: REQ-ATLAS-001. Feature: inventory.assets.

ALTER TABLE atlas_assets
    ADD COLUMN additional_user_ids JSONB NOT NULL DEFAULT '[]'::jsonb
        CHECK (jsonb_typeof(additional_user_ids) = 'array');
