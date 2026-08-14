-- Preserve portable Signals subscription revision and timestamp state.
-- Requirements: REQ-SIGNALS-001, REQ-EXCHANGE-001.
-- Features: alerts.rules, migration.packages. GitHub: #9, #11.

ALTER TABLE signal_subscriptions
    ADD COLUMN revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    ADD COLUMN updated_at TIMESTAMPTZ;

UPDATE signal_subscriptions SET updated_at = created_at WHERE updated_at IS NULL;

ALTER TABLE signal_subscriptions
    ALTER COLUMN updated_at SET NOT NULL,
    ADD CHECK (updated_at >= created_at);
