-- Add durable Exchange recovery state and exact-Patterns schema 1.1 without
-- rewriting the already-applied 0032 migration or legacy receipts.
-- Requirements: REQ-PATTERNS-001, REQ-EXCHANGE-001. Features: templates.schemas, migration.packages. GitHub: #8.

ALTER TABLE exchange_packages ADD COLUMN progress JSONB NOT NULL DEFAULT '[]'::jsonb
    CHECK (jsonb_typeof(progress) = 'array' AND jsonb_array_length(progress) <= 10000);

-- The two named checks below are the deterministic PostgreSQL names for the
-- processing and failed zero-progress checks in the immutable 0032 migration. They
-- prohibited checkpointed successful outcomes on processing/failed receipts.
-- Only those obsolete checks and the 1.0-only version check are replaced.
ALTER TABLE exchange_packages
    DROP CONSTRAINT exchange_packages_schema_version_check,
    DROP CONSTRAINT exchange_packages_check4,
    DROP CONSTRAINT exchange_packages_check6;

ALTER TABLE exchange_packages
    ADD CONSTRAINT exchange_packages_schema_version_check
        CHECK (schema_version IN ('1.0', '1.1')),
    ADD CONSTRAINT exchange_packages_records_counts_check
        CHECK (jsonb_array_length(records) = created_count + unchanged_count + holding_count),
    ADD CONSTRAINT exchange_packages_terminal_progress_check
        CHECK (status NOT IN ('completed', 'holding') OR jsonb_array_length(progress) = 0),
    ADD CONSTRAINT exchange_packages_nonterminal_holding_check
        CHECK (status NOT IN ('processing', 'failed') OR holding_count = 0);

-- Existing 1.0 rows are immutable historical receipts. Application archive
-- decoding and all new service workflows require 1.1, while the database must
-- retain old audit/history evidence without rewriting its claimed format.
