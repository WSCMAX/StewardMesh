# Atlas Catalog — Model configurations, pricing, and upgrade paths

- **Canonical ID:** `inventory.catalog`
- **Requirement:** `REQ-ATLAS-CATALOG-001`
- **Extends:** [Atlas Models](atlas-models.md) (`REQ-ATLAS-MODELS-001`, `inventory.models`)
- **Delivery state:** Foundation in progress

## Purpose

Atlas Models already separates a type of thing from each individual Atlas asset. Its model represents a reusable laptop, server, or other product with manufacturer/model identity, baseline specifications, useful-life defaults, provenance, and an optional association from each asset. Atlas Catalog extends that record with named configurations, effective-dated prices, and directed successor, replacement, and upgrade relationships. It does not introduce a second product or model table.

An Atlas asset remains the source of truth for one owned item: its tag, serial number, hostname, location, steward, purchase date, status, and lifecycle history. Its category or kind remains useful classification, but it is not the economic planning record. Horizon remains the source of truth for scenarios and forecasts.

## Catalog model

Atlas Models owns the stable organization-scoped model ID, manufacturer, model name and number, asset kind, status, baseline specifications, warranty and useful-life defaults, import provenance, optimistic revision, and timestamps. Catalog validates every model reference through that existing boundary.

Configurations belong to one Atlas model and preserve a stable ID, unique name within the model, optional organization-unique SKU, active or retired status, bounded specification overrides, revision, and timestamps. Overrides do not mutate the base model. Consumers combine the model baseline with the selected configuration and show which value came from which level.

Prices are immutable effective-dated observations. Each price has a source type of `list`, `quote`, `contract`, or `estimate`; an exact non-negative integer minor-unit amount through `9,007,199,254,740,991`; one uppercase ISO 4217 currency; an effective start date; an optional inclusive end date; and an optional bounded source reference. A price can target the base Atlas model or one of its configurations. Catalog does not perform currency conversion or silently rewrite history.

Price resolution first considers prices for the requested configuration and falls back to base-model prices only when no configuration price applies. An explicit source type selects only that type. Otherwise the deterministic preference is contract, quote, estimate, then list; the most recent effective start wins within a type. A request without an explicit currency fails when the eligible records contain more than one currency.

Upgrade paths are directed relationships from a model or configuration to a different model or configuration. Supported relationship kinds are `successor`, `replacement`, and `upgrade`. Both ends must resolve inside the organization, a configuration must belong to its stated model, and self-links are rejected. A relationship states an available path, not an automatic purchasing decision.

## Lifecycle and forecasting flow

The intended end-to-end workflow is:

1. Create the reusable product model and baseline specifications in Atlas Models.
2. Add configurations only when price or specification differences matter.
3. Record effective-dated list, quote, contract, or estimate prices.
4. Define the supported successor, replacement, or upgrade model/configuration.
5. Associate each Atlas asset with its current Atlas model; a follow-on slice adds the optional configuration association.
6. In Horizon, choose a replacement target. Horizon resolves the relevant catalog price for the forecast date and records the selected price ID and resolution date as provenance.
7. Use a clearly labeled manual cost override only when the catalog price does not describe the exceptional item.

This prevents a fleet of identical items from requiring the same lifecycle price to be edited repeatedly. It also allows a newer product to become the planned replacement without changing what the existing asset actually is.

## Exchange, ownership, and permissions

The organization-scoped memory and PostgreSQL adapters and Catalog service are initialized in the running application. Exchange owns one provider for each Catalog family: `atlas.catalog-configuration`, `atlas.catalog-price`, and `atlas.catalog-upgrade-path`. Its portable projections omit organization IDs, timestamps, operators, private database state, and monetary audit detail; retain stable IDs, revisions, effective dates, typed fields, and model/configuration relationships; and pin the newest immutable Patterns schema version. Imports use an opaque construction-time capability so ordinary callers cannot supply source revisions or bypass ownership checks.

Every ordinary Catalog create path calls the service-layer Guard write gate with the canonical Exchange record type and stable ID. The application resolves the actor from the authenticated request scope, reloads that account through Guard, and uses Guard's audited ownership decision. An imported Catalog record therefore remains readable but rejects local service writes until an authorized operator claims ownership. Exchange alone bypasses this check after its durable package intent and ownership workflow has approved the record.

Catalog management remains intentionally unexposed in this foundation slice. A later transport and browser slice will add `catalog.read` and `catalog.write` permissions, same-origin and CSRF protection, accessible workflows, and REST/OpenAPI plus gRPC management parity. Until then, no Catalog management endpoint or browser surface is exposed.

Audit events are emitted for configuration, price, and upgrade-path creation. Metadata includes `REQ-ATLAS-CATALOG-001`, stable model and catalog record IDs, kind/status/revision, and price source/currency where applicable. Audit metadata excludes specifications, SKU values, source references, and monetary amounts.

## Delivery slices

1. **Foundation:** feature and requirement registration, provider-neutral domain/service contracts, deterministic memory adapter, PostgreSQL schema, validation, effective-price resolution, audit redaction, and tests.
2. **Catalog management:** Guard permissions, REST/OpenAPI and gRPC operations, and an accessible Atlas Catalog surface. The PostgreSQL adapter, internal runtime service seam, Exchange provider, and imported-record ownership fence are complete.
3. **Asset association:** optional configuration reference alongside the existing Atlas `modelId`, bulk association, preserved per-item overrides, search/filter support, and migration-safe API evolution.
4. **Horizon integration:** replacement model/configuration selection, effective-date price resolution, explicit source provenance, manual-override labeling, scenario comparison, and forecast/export parity.
5. **Operational hardening:** change/update history, upgrade-path cycle policy, integration reconciliation, accessibility and browser validation, security review, and release evidence.

## Accessibility and help

The future management surface will use labeled native controls, semantic headings and tables, keyboard-operable configuration and path editing, text status announcements, visible focus, non-color status and provenance, and 320-pixel containment. Price displays will always include currency and distinguish base-model values, configuration values, inherited specifications, and manual overrides in text.

Guide will explain the product-versus-asset boundary, show a permission-aware walkthrough, and provide sanitized issue reporting. Reports may include stable catalog record IDs, relationship kind, price source, currency, revision, and correlation ID. They must exclude confidential specifications, SKU values, supplier references, negotiated amounts, credentials, cookies, and CSRF material.

## Foundation test coverage

- Configuration, price, and upgrade-path normalization and bounds.
- Organization isolation, model-scoped configuration uniqueness, SKU conflicts, and defensive copies.
- Atlas-model and configuration reference validation and self-link rejection.
- Effective-date and configuration-specific price resolution with explicit source preference and no implicit currency conversion.
- Audit actions and metadata redaction.
- Lossless three-family Exchange projection/import, strict payload decoding, dependency resolution, deterministic audit replay, and service-layer ownership denial.
- Application registration plus a real package import that proves an imported Catalog record is Guard write-locked at the Catalog service boundary.
- Ordered, checksum-verified PostgreSQL schema coverage and requirement traceability.
