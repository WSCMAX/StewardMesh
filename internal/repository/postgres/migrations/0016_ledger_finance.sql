-- StewardMesh Ledger -- procurement, contracts, commitments, budgets, costs, and reconciliation.
-- Requirement: REQ-LEDGER-001. Feature: procurement.finance.

CREATE TABLE ledger_vendors (
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    id TEXT NOT NULL CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
    normalized_name TEXT NOT NULL CHECK (normalized_name = lower(btrim(name))),
    external_id TEXT,
    status TEXT NOT NULL CHECK (status IN ('active', 'inactive')),
    revision BIGINT NOT NULL CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, id),
    UNIQUE (organization_id, normalized_name),
    CHECK (updated_at >= created_at)
);

CREATE UNIQUE INDEX ledger_vendors_external_id_idx
    ON ledger_vendors (organization_id, lower(external_id)) WHERE external_id IS NOT NULL;

CREATE TABLE ledger_purchase_orders (
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    id TEXT NOT NULL CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    number TEXT NOT NULL CHECK (char_length(number) BETWEEN 1 AND 100),
    normalized_number TEXT NOT NULL CHECK (normalized_number = lower(btrim(number))),
    vendor_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('draft', 'approved', 'ordered', 'partially_received', 'received', 'cancelled')),
    currency TEXT NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    total_minor BIGINT NOT NULL CHECK (total_minor >= 0),
    ordered_on DATE,
    asset_ids TEXT[] NOT NULL DEFAULT '{}',
    receipt_document_ids TEXT[] NOT NULL DEFAULT '{}',
    revision BIGINT NOT NULL CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, id),
    FOREIGN KEY (organization_id, vendor_id) REFERENCES ledger_vendors (organization_id, id),
    UNIQUE (organization_id, normalized_number),
    CHECK (updated_at >= created_at),
    CHECK (status NOT IN ('ordered', 'partially_received', 'received') OR ordered_on IS NOT NULL)
);

CREATE INDEX ledger_purchase_orders_vendor_idx
    ON ledger_purchase_orders (organization_id, vendor_id, status, ordered_on);

CREATE TABLE ledger_contracts (
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    id TEXT NOT NULL CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
    vendor_id TEXT NOT NULL,
    operational_status TEXT NOT NULL CHECK (operational_status IN ('planned', 'active', 'suspended', 'expired', 'terminated', 'cancelled')),
    financial_status TEXT NOT NULL CHECK (financial_status IN ('planned', 'committed', 'billed', 'paid', 'closed', 'cancelled')),
    currency TEXT NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    ceiling_minor BIGINT NOT NULL CHECK (ceiling_minor >= 0),
    starts_on DATE NOT NULL,
    ends_on DATE NOT NULL,
    renews_on DATE,
    document_ids TEXT[] NOT NULL DEFAULT '{}',
    revision BIGINT NOT NULL CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, id),
    FOREIGN KEY (organization_id, vendor_id) REFERENCES ledger_vendors (organization_id, id),
    CHECK (ends_on >= starts_on),
    CHECK (renews_on IS NULL OR renews_on >= starts_on),
    CHECK (updated_at >= created_at)
);

CREATE INDEX ledger_contracts_status_idx
    ON ledger_contracts (organization_id, operational_status, financial_status, ends_on);

CREATE TABLE ledger_commitments (
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    id TEXT NOT NULL CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    contract_id TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('savings_plan', 'subscription', 'reserved_capacity', 'lease', 'maintenance', 'license', 'financing', 'other')),
    description TEXT NOT NULL CHECK (char_length(description) BETWEEN 1 AND 500),
    currency TEXT NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    amount_minor BIGINT NOT NULL CHECK (amount_minor >= 0),
    starts_on DATE NOT NULL,
    ends_on DATE NOT NULL,
    fiscal_period TEXT NOT NULL CHECK (fiscal_period ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,31}$'),
    scenario TEXT NOT NULL CHECK (scenario ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
    revision BIGINT NOT NULL CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, id),
    FOREIGN KEY (organization_id, contract_id) REFERENCES ledger_contracts (organization_id, id),
    CHECK (ends_on >= starts_on),
    CHECK (updated_at >= created_at)
);

CREATE INDEX ledger_commitments_period_idx
    ON ledger_commitments (organization_id, fiscal_period, scenario, contract_id);

CREATE TABLE ledger_budgets (
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    id TEXT NOT NULL CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
    normalized_name TEXT NOT NULL CHECK (normalized_name = lower(btrim(name))),
    fiscal_period TEXT NOT NULL CHECK (fiscal_period ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,31}$'),
    scenario TEXT NOT NULL CHECK (scenario ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
    department_id TEXT,
    site_id TEXT,
    currency TEXT NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    allocated_minor BIGINT NOT NULL CHECK (allocated_minor >= 0),
    revision BIGINT NOT NULL CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, id),
    CHECK (updated_at >= created_at)
);

CREATE UNIQUE INDEX ledger_budgets_scope_idx
    ON ledger_budgets (organization_id, fiscal_period, scenario, COALESCE(department_id, ''), COALESCE(site_id, ''), normalized_name);

CREATE TABLE ledger_costs (
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    id TEXT NOT NULL CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
    description TEXT NOT NULL CHECK (char_length(description) BETWEEN 1 AND 500),
    kind TEXT NOT NULL CHECK (kind IN ('planned', 'estimated', 'actual', 'billed', 'paid', 'committed', 'normalized_real', 'tco')),
    currency TEXT NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    amount_minor BIGINT NOT NULL CHECK (amount_minor >= 0),
    fiscal_period TEXT NOT NULL CHECK (fiscal_period ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,31}$'),
    scenario TEXT NOT NULL CHECK (scenario ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
    purchase_order_id TEXT,
    contract_id TEXT,
    asset_id TEXT,
    department_id TEXT,
    site_id TEXT,
    document_id TEXT,
    external_reference TEXT,
    source_system_id TEXT,
    source_record_id TEXT,
    revision BIGINT NOT NULL CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, id),
    FOREIGN KEY (organization_id, purchase_order_id) REFERENCES ledger_purchase_orders (organization_id, id),
    FOREIGN KEY (organization_id, contract_id) REFERENCES ledger_contracts (organization_id, id),
    CHECK ((source_system_id IS NULL) = (source_record_id IS NULL)),
    CHECK (updated_at >= created_at)
);

CREATE UNIQUE INDEX ledger_costs_source_idx
    ON ledger_costs (organization_id, lower(source_system_id), source_record_id)
    WHERE source_system_id IS NOT NULL;

CREATE INDEX ledger_costs_period_idx
    ON ledger_costs (organization_id, fiscal_period, scenario, currency, kind);
