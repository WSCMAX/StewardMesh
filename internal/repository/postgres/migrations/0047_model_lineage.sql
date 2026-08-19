-- StewardMesh Atlas -- model replacement lineage for Horizon lifecycle planning.
-- Requirement: REQ-HORIZON-001, REQ-ATLAS-001. Feature: lifecycle.planning, inventory.models.

ALTER TABLE atlas_models
    ADD COLUMN replacement_model_id TEXT;

ALTER TABLE atlas_models
    ADD CONSTRAINT atlas_models_replacement_model_fk
        FOREIGN KEY (organization_id, replacement_model_id) REFERENCES atlas_models (organization_id, id);
