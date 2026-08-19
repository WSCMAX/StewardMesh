-- StewardMesh Atlas -- criticality score on models and assets.
-- Requirement: REQ-ATLAS-001, REQ-HORIZON-001. Feature: lifecycle.planning, inventory.assets.

ALTER TABLE atlas_models
    ADD COLUMN criticality_score INTEGER NOT NULL DEFAULT 0 CHECK (criticality_score BETWEEN 0 AND 5);

ALTER TABLE atlas_assets
    ADD COLUMN criticality_score INTEGER NOT NULL DEFAULT 0 CHECK (criticality_score BETWEEN 0 AND 5);
