-- Asset templates, accessories, unit costs, and purchase-order line items.
-- Requirements: REQ-ATLAS-001, REQ-ATLAS-MODELS-001, REQ-LEDGER-001. Features: inventory.assets, inventory.models, procurement.finance.

ALTER TABLE atlas_models
    ADD COLUMN template_fields JSONB NOT NULL DEFAULT '[]'::jsonb
        CHECK (jsonb_typeof(template_fields) = 'array' AND jsonb_array_length(template_fields) <= 25),
    ADD COLUMN unit_cost_minor BIGINT NOT NULL DEFAULT 0
        CHECK (unit_cost_minor BETWEEN 0 AND 9007199254740991),
    ADD COLUMN currency TEXT NOT NULL DEFAULT 'USD'
        CHECK (currency ~ '^[A-Z]{3}$');

ALTER TABLE atlas_assets
    ADD COLUMN attributes JSONB NOT NULL DEFAULT '{}'::jsonb
        CHECK (jsonb_typeof(attributes) = 'object'),
    ADD COLUMN components JSONB NOT NULL DEFAULT '[]'::jsonb
        CHECK (jsonb_typeof(components) = 'array' AND jsonb_array_length(components) <= 40),
    ADD COLUMN unit_cost_minor BIGINT NOT NULL DEFAULT 0
        CHECK (unit_cost_minor BETWEEN 0 AND 9007199254740991),
    ADD COLUMN currency TEXT NOT NULL DEFAULT 'USD'
        CHECK (currency ~ '^[A-Z]{3}$');

ALTER TABLE ledger_purchase_orders
    ADD COLUMN lines JSONB NOT NULL DEFAULT '[]'::jsonb
        CHECK (jsonb_typeof(lines) = 'array' AND jsonb_array_length(lines) <= 200);

CREATE INDEX ledger_purchase_orders_asset_ids_idx
    ON ledger_purchase_orders USING GIN (asset_ids);

CREATE INDEX ledger_costs_asset_idx
    ON ledger_costs (organization_id, asset_id)
    WHERE asset_id IS NOT NULL;
