-- StewardMesh Atlas Models -- per-instance bulk intake deployment notes
-- Requirement: REQ-ATLAS-MODELS-001. Feature: inventory.models. GitHub: #73.

ALTER TABLE atlas_assets
    ADD COLUMN deployment_notes TEXT NOT NULL DEFAULT ''
        CHECK (char_length(deployment_notes) <= 2000);
