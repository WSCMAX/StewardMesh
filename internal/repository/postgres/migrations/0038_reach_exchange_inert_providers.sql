-- Reach Exchange imports provider identity and public configuration without deployment-owned routes or credentials.
-- Requirements: REQ-REACH-001, REQ-EXCHANGE-001. Features: messaging.delivery, migration.packages. GitHub: #9, #12.

ALTER TABLE reach_providers
    ALTER COLUMN endpoint_id DROP NOT NULL,
    ALTER COLUMN secret_ref DROP NOT NULL,
    DROP CONSTRAINT reach_providers_endpoint_id_check,
    DROP CONSTRAINT reach_providers_secret_ref_check;

ALTER TABLE reach_providers
    ADD CONSTRAINT reach_providers_endpoint_id_optional_check
        CHECK (endpoint_id IS NULL OR endpoint_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    ADD CONSTRAINT reach_providers_secret_ref_optional_check
        CHECK (secret_ref IS NULL OR secret_ref ~ '^(env|external):[A-Za-z0-9][A-Za-z0-9._-]{0,95}$'),
    ADD CONSTRAINT reach_providers_enabled_configuration_check
        CHECK (NOT enabled OR (endpoint_id IS NOT NULL AND secret_ref IS NOT NULL));
