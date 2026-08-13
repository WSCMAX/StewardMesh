# Horizon — Lifecycle planning and forecasting

- **Canonical ID:** `lifecycle.planning`
- **Requirement:** `REQ-HORIZON-001`
- **Roadmap issue:** [#3](https://github.com/WSCMAX/StewardMesh/issues/3)

## Purpose

Horizon turns Atlas inventory and Ledger financial facts into organization-scoped replacement plans and live forecasts. It owns planning assumptions and their effective-dated history without copying asset identity, location, financial reconciliation, tags, or goals out of the modules that own those records.

Each plan currently belongs to one Atlas asset and one normalized scenario. A plan records a lifecycle stage, useful-life target, replacement timing, and exact manual replacement cost. Changes create durable versions so users can reproduce which assumption was effective at a point in time instead of silently replacing planning history.

[Atlas Catalog](atlas-catalog.md) (`REQ-ATLAS-CATALOG-001`, `inventory.products`) introduces the reusable product, configuration, effective-price, and upgrade-path records needed to move the normal replacement-cost flow above the individual asset. The follow-on Horizon integration will let a plan select a target product or configuration, resolve the price effective for the forecast date, and preserve that price provenance while retaining an explicit manual override for exceptional assets. The current per-plan amount remains authoritative until that integration lands.

## Roles and permissions

- `planning.read` lists plans and their history, calculates forecasts, and exports supported analytics formats.
- `planning.write` creates and updates plans. Browser mutations also require the in-memory CSRF token and an allowed same-origin request.

The built-in Administrator policy bundle receives both permissions through migration `0019`. Custom roles remain unchanged until an administrator explicitly adds them. Guard remains authoritative: hiding a control in the browser cannot widen server access.

Every route is authenticated, organization-scoped, non-cacheable, and server-authorized. Horizon accepts stable record IDs but never accepts an organization ID from a client. Planning records cannot cross the organization boundary of the referenced Atlas asset, Ledger data, Threads relationships, or Guard scope.

## Lifecycle plans and assumptions

A plan is scoped to one asset and scenario and has a stable ID plus optimistic revision. Its effective-dated versions preserve:

- an `effectiveFrom` calendar date, represented at UTC midnight on transport boundaries;
- lifecycle stage: `planned`, `in_service`, `refresh_due`, `approved`, or `retired`;
- expected useful life from 1 through 1200 whole months;
- an optional manual replacement calendar date;
- replacement cost as a non-negative integer minor-unit amount through `9,007,199,254,740,991` and one uppercase ISO 4217 currency;
- actor, revision, creation time, and update time.

At most one version of an asset-and-scenario plan is effective at a time. A replacement date is the manual date when supplied. Otherwise Horizon derives it from the Atlas purchase date plus expected useful-life months using Go `time.Time.AddDate` semantics. A plan without either a manual date or an Atlas purchase date remains valid but cannot contribute a dated replacement need until one becomes available.

Assumption changes append a version and advance the plan revision. Stale revisions fail with a conflict. Earlier versions remain immutable and queryable; Horizon does not rewrite them when Atlas or Ledger records later change.

## Forecast model

Forecasts are live deterministic views evaluated at an explicit `asOf` timestamp against calendar-date plan versions. The request supplies one or more scenarios, an inclusive year window, a fiscal-year start month, and a grouping dimension. The fiscal-year start month defaults to January and is a report parameter rather than hidden organization state.

Supported `groupBy` values are:

- `fiscal_year`
- `department`
- `site`
- `tag`
- `goal`
- `asset_class`

Atlas supplies the asset kind used as `asset_class`, plus the current department and site dimensions. Threads supplies direct goal links and effective tags; suppressed tags are excluded. Tag and goal relationships can be multi-valued, so their rows are explicitly non-additive: totals across tag or goal rows must not be summed as an organization total. Horizon returns an ungrouped total alongside grouped results when an additive overall total is required.

Every forecast row exposes replacement need and explicit amounts by the Ledger kinds relevant to planning: `actual`, `estimated`, `committed`, `normalized_real`, and `tco`. Money remains integer minor units with an explicit currency throughout service, repository, REST, gRPC, CSV, and browser boundaries. Values and aggregates are capped at the JavaScript-safe integer boundary `9,007,199,254,740,991`, so JSON browser clients preserve every minor unit exactly. A request whose matching plans or Ledger facts contain multiple currencies fails with a conflict; Horizon does not perform implicit exchange-rate conversion.

Scenario results remain separate and comparable. Horizon does not silently substitute a baseline scenario, reinterpret fiscal-period labels, or count one cost kind as another.

## API and provider boundaries

REST endpoints:

- `GET|POST /api/v1/horizon/plans`
- `PUT /api/v1/horizon/plans/{planID}`
- `GET /api/v1/horizon/plans/{planID}/history`
- `GET /api/v1/horizon/forecast`
- `GET /api/v1/horizon/export.csv`

Plan-list filters include `assetId` and `scenario`. Forecast and export parameters include comma-separated `scenarios`, `asOf`, inclusive `fromYear` and `toYear`, `fiscalYearStartMonth`, and `groupBy`. JSON is the interactive analytics contract. CSV carries the same dimensions and integer values and prefixes cells beginning with spreadsheet formula sigils; consumers must still treat exported files as untrusted data and must not enable spreadsheet macros.

OpenAPI and protobuf definitions carry the same planning versions, forecast parameters, grouping semantics, and money boundaries. `horizon.Store` is the provider-neutral persistence contract implemented by deterministic memory and PostgreSQL adapters. Atlas, Ledger, and Threads remain authoritative behind narrow reader interfaces; Horizon never writes their records or reads provider tables directly.

Migration `0018_horizon_lifecycle_planning.sql` creates organization-scoped plans and immutable effective-dated versions. Migration `0019_horizon_administrator_permissions.sql` grants `planning.read` and `planning.write` to existing built-in Administrator bundles without modifying custom roles.

## Audit events

Horizon emits:

- `horizon.plan.created`
- `horizon.plan.updated`

Audit metadata includes `REQ-HORIZON-001`, plan and asset IDs, scenario, lifecycle stage, effective date, revision, and currency. It excludes asset names and identifiers intended for people, planning notes, monetary amounts, private Ledger details, cookies, CSRF material, and identity-provider data.

## Accessible workflow and walkthrough

1. Open **Horizon — Lifecycle planning and forecasting** with `planning.read`.
2. Filter existing plans by asset or scenario, then inspect the effective assumptions and version history.
3. With `planning.write`, create or revise a plan using an Atlas asset, lifecycle stage, useful life, replacement timing, cost, currency, and effective timestamp.
4. Select scenarios, an as-of timestamp, year range, fiscal-year start month, and grouping dimension.
5. Compare replacement needs and the named Ledger cost kinds in the authoritative table.
6. Export the same report parameters as formula-safe CSV when needed.

The forecast table is the authoritative accessible representation. Compact bars are supplemental and repeat their value in text; they never encode category or state by color alone. The surface uses semantic headings, labeled native controls, visible focus, minimum-height actions, live status messages, keyboard-operable history, responsive reflow, and contained horizontal scrolling for wide tables. At 320 pixels the page does not gain horizontal overflow outside a labeled table region. Reduced-motion settings are respected, and no animation is required to understand the report.

Guide provides contextual Horizon help, a permission-aware walkthrough, and an example that can be dismissed, skipped, and replayed without gating normal work.

## Issue reporting

Report Horizon problems through Guide or GitHub issue #3. The sanitized report may include the page, Horizon component, application version, coarse browser and operating-system family, viewport, and a valid correlation ID. When describing a calculation, users may add non-sensitive plan and asset IDs, scenario, as-of timestamp, year range, fiscal-year start month, grouping dimension, and revision.

Do not include asset names, serial numbers, hostnames, private strategy text, financial descriptions, external invoice references, money values, directory details, session cookies, CSRF values, provider credentials, or identity assertions.

## Test coverage

- Useful-life bounds, manual and `AddDate`-derived replacement dates, missing-date behavior, lifecycle stages, normalization, version history, effective-time selection, stale revisions, audits, and organization isolation.
- Deterministic fiscal-year boundaries, as-of behavior, inclusive year windows, scenario comparisons, all six grouping dimensions, multi-valued non-additive tag/goal rows, suppressed-tag exclusion, and direct-goal selection.
- Explicit actual, estimated, committed, normalized-real, and TCO aggregation; exact integer arithmetic; overflow and mixed-currency rejection.
- Shared memory/PostgreSQL provider conformance and migration structure, constraints, administrator-permission upgrade, and optional real-database integration tests.
- REST authentication, permissions, CSRF, origin, plan filters, history, validation, conflicts, forecast parameters, safe errors, and formula-safe CSV export.
- OpenAPI and protobuf parity, React response validation, accessible editing and comparison workflows, keyboard behavior, table/bar equivalence, axe checks, narrow-table containment, and Guide integration.
- Repository-wide race tests, vet, vulnerability checks, traceability, API lint, protobuf validation, frontend typecheck/tests/build, container build, and authenticated desktop and 320-pixel browser validation.
