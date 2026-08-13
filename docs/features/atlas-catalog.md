# Atlas Catalog — Products, configurations, pricing, and upgrade paths

- **Canonical ID:** `inventory.products`
- **Requirement:** `REQ-ATLAS-CATALOG-001`
- **Delivery state:** Foundation in progress

## Purpose

Atlas Catalog separates a type of thing from each individual Atlas asset. A product represents a manufacturer and model such as a laptop or server model. It owns reusable baseline specifications and an optional default useful life. Named configurations add SKU-specific or deployment-specific specification overrides. Effective-dated prices attach to either the base product or one configuration, and directed relationships describe supported successors, replacements, and upgrades.

An Atlas asset remains the source of truth for one owned item: its tag, serial number, hostname, location, steward, purchase date, status, and lifecycle history. Its category or kind remains useful classification, but it is not the economic planning record. Horizon remains the source of truth for scenarios and forecasts.

## Catalog model

Products preserve:

- a stable organization-scoped ID;
- manufacturer and model, unique together without case sensitivity;
- the compatible Atlas asset kind;
- active or retired status;
- bounded baseline specification key/value pairs;
- an optional default useful life from 1 through 1200 whole months;
- optimistic revision and creation/update timestamps.

Configurations belong to one product and preserve a stable ID, unique name within the product, optional organization-unique SKU, active or retired status, bounded specification overrides, revision, and timestamps. Overrides do not mutate the base product. Consumers combine the product baseline with the selected configuration and show which value came from which level.

Prices are immutable effective-dated observations. Each price has a source type of `list`, `quote`, `contract`, or `estimate`; an exact non-negative integer minor-unit amount through `9,007,199,254,740,991`; one uppercase ISO 4217 currency; an effective start date; an optional inclusive end date; and an optional bounded source reference. A price can target the base product or one of its configurations. Catalog does not perform currency conversion or silently rewrite history.

Price resolution first considers prices for the requested configuration and falls back to base-product prices only when no configuration price applies. An explicit source type selects only that type. Otherwise the deterministic preference is contract, quote, estimate, then list; the most recent effective start wins within a type. A request without an explicit currency fails when the eligible records contain more than one currency.

Upgrade paths are directed relationships from a product or configuration to a different product or configuration. Supported relationship kinds are `successor`, `replacement`, and `upgrade`. Both ends must resolve inside the organization, a configuration must belong to its stated product, and self-links are rejected. A relationship states an available path, not an automatic purchasing decision.

## Lifecycle and forecasting flow

The intended end-to-end workflow is:

1. Create the reusable product and its baseline specifications.
2. Add configurations only when price or specification differences matter.
3. Record effective-dated list, quote, contract, or estimate prices.
4. Define the supported successor, replacement, or upgrade product/configuration.
5. Associate each Atlas asset with its current product and optional configuration.
6. In Horizon, choose a replacement target. Horizon resolves the relevant catalog price for the forecast date and records the selected price ID and resolution date as provenance.
7. Use a clearly labeled manual cost override only when the catalog price does not describe the exceptional item.

This prevents a fleet of identical items from requiring the same lifecycle price to be edited repeatedly. It also allows a newer product to become the planned replacement without changing what the existing asset actually is.

## Ownership and permissions

The foundation package is organization-scoped and provider-neutral. The transport and browser slices will add `catalog.read` and `catalog.write` authorization, same-origin and CSRF protection for browser writes, ownership-lock handling for imported catalog records, accessible management workflows, and REST/OpenAPI plus gRPC parity. Until those slices are complete, the catalog service is not wired into the running application.

Audit events are emitted for product, configuration, price, and upgrade-path creation. Metadata includes `REQ-ATLAS-CATALOG-001`, stable record IDs, kind/status/revision, and price source/currency where applicable. Audit metadata excludes specifications, SKU values, source references, and monetary amounts.

## Delivery slices

1. **Foundation:** feature and requirement registration, provider-neutral domain/service contracts, deterministic memory adapter, PostgreSQL schema, validation, effective-price resolution, audit redaction, and tests.
2. **Catalog management:** PostgreSQL adapter, Guard permissions, REST/OpenAPI and gRPC operations, import ownership, and an accessible Atlas Catalog surface.
3. **Asset association:** optional Atlas product/configuration references, bulk association, preserved per-item overrides, search/filter support, and migration-safe API evolution.
4. **Horizon integration:** replacement product/configuration selection, effective-date price resolution, explicit source provenance, manual-override labeling, scenario comparison, and forecast/export parity.
5. **Operational hardening:** change/update history, upgrade-path cycle policy, integration reconciliation, accessibility and browser validation, security review, and release evidence.

## Accessibility and help

The future management surface will use labeled native controls, semantic headings and tables, keyboard-operable configuration and path editing, text status announcements, visible focus, non-color status and provenance, and 320-pixel containment. Price displays will always include currency and distinguish base-product values, configuration values, inherited specifications, and manual overrides in text.

Guide will explain the product-versus-asset boundary, show a permission-aware walkthrough, and provide sanitized issue reporting. Reports may include stable catalog record IDs, relationship kind, price source, currency, revision, and correlation ID. They must exclude confidential specifications, SKU values, supplier references, negotiated amounts, credentials, cookies, and CSRF material.

## Foundation test coverage

- Product, configuration, price, and upgrade-path normalization and bounds.
- Organization isolation, case-insensitive product/configuration uniqueness, SKU conflicts, and defensive copies.
- Configuration-to-product reference validation and self-link rejection.
- Effective-date and configuration-specific price resolution with explicit source preference and no implicit currency conversion.
- Audit actions and metadata redaction.
- Ordered, checksum-verified PostgreSQL schema coverage and requirement traceability.
