# Ledger — Procurement, contracts, budgets, and costs

- **Canonical ID:** `procurement.finance`
- **Requirement:** `REQ-LEDGER-001`
- **Roadmap issue:** [#4](https://github.com/WSCMAX/StewardMesh/issues/4)

## Purpose

Ledger is the organization-scoped financial system for vendors, purchase orders, contracts, multi-year commitments, fiscal-period budgets, and current cost records. It uses integer minor units and an explicit three-letter currency on every monetary value. Floating-point money does not cross the domain, repository, REST, gRPC, CSV, or browser boundaries.

This slice establishes the budget-period and cost primitives required by Horizon. Signals and Reach remain downstream consumers of Ledger's over-budget, renewal, expiration, and reconciliation states; they are not required for Ledger to persist or calculate those states.

## Financial records

### Vendors and purchase orders

Vendor names are unique per organization after trimming and case normalization. An optional external ID preserves the upstream vendor identity.

A purchase order stores a stable number, vendor, currency, total, order date, optimistic revision, and status. One PO can reference any number of organization-visible Atlas assets and any number of Vault receipt or evidence records. Ledger validates those references through the owning services before persistence and preserves the original IDs. Status transitions move forward through `draft`, `approved`, `ordered`, `partially_received`, and `received`; cancellation is allowed only before a terminal state.

### Contracts and commitments

Contracts keep operational state separate from financial state:

- operational: `planned`, `active`, `suspended`, `expired`, `terminated`, or `cancelled`
- financial: `planned`, `committed`, `billed`, `paid`, `closed`, or `cancelled`

Both state machines use optimistic revisions and reject backward or terminal transitions. Contract start, end, renewal, ceiling, vendor, currency, and Vault document references are durable.

Commitments belong to a contract and support savings plans, subscriptions, reserved capacity, leases, maintenance, licenses, financing, and an explicit other category. Each commitment has its own date range, fiscal period, scenario, amount, and currency, so multi-year obligations can be divided into reportable periods without changing the parent contract.

### Budgets, costs, and reconciliation

Budgets are scoped by organization, fiscal period, scenario, optional department, optional site, and currency. Cost records distinguish:

- planned
- estimated
- actual
- billed
- paid
- committed
- normalized real cost
- total cost of ownership (TCO)

A cost can reference a PO, contract, asset, department, site, Vault document, and external invoice or payment reference. When `sourceSystemId` and `sourceRecordId` are supplied, the pair is an idempotency key. Replaying identical source data makes no change. Changed source data updates the same cost and revision instead of duplicating the obligation.

Budget variance is calculated for one fiscal period and scenario. `actual`, `billed`, `paid`, and `committed` current cost states count as recognized; planned, estimated, normalized-real, and TCO values remain visible in the category breakdown without being double-counted as recognized spend. Mixed currencies fail with a conflict rather than silently converting. A negative variance produces an explicit `overBudget` state for future Signals rules.

## Permissions and security

- `finance.read` lists Ledger data, calculates variance, and exports CSV.
- `finance.write` creates financial records, reconciles costs, and advances status state machines.

The built-in Administrator policy bundle receives both permissions through migration `0017`. Custom roles remain unchanged until an administrator explicitly adds them. Every route is authenticated, organization-scoped, non-cacheable, and server-authorized. Mutations require the synchronized CSRF token and exact configured browser origin.

Ledger never accepts an organization ID from the client. Repository queries include the configured organization boundary, and PostgreSQL keys and uniqueness constraints include that organization. CSV uses Go's standards-compliant encoder and prefixes cells that begin with spreadsheet formula sigils; consumers must still treat exports as untrusted data and avoid enabling spreadsheet macros.

Audit events contain stable record IDs, status/category metadata, fiscal period, scenario, currency, revision, and `REQ-LEDGER-001`. They exclude vendor names, descriptions, invoice references, document contents, and money values.

## Exchange portability

Exchange owns six typed Ledger record families: vendors, purchase orders,
contracts, commitments, budgets, and costs. Version-2 Patterns projections
preserve stable IDs, arbitrary positive revisions, source timestamps, exact
minor-unit values and currency, current states, evidence and domain
relationships, and a cost's earliest source identity. PostgreSQL exports one
bounded repeatable-read snapshot; memory exports use the same organization and
count boundary.

Imports call an opaque capability returned only with the owning Ledger service.
Exact retries are idempotent, changed reuse conflicts, and an ambiguous
post-commit audit failure is repaired with the same organization-scoped event
identity. Organization IDs stay destination-owned. Imported Ledger resources
are Guard write-locked, and every ordinary create, status change, or cost
reconciliation checks that ownership fence before mutation.

## APIs

- `GET /api/v1/ledger`
- `POST /api/v1/ledger/vendors`
- `POST /api/v1/ledger/purchase-orders`
- `PUT /api/v1/ledger/purchase-orders/{purchaseOrderID}/status`
- `POST /api/v1/ledger/contracts`
- `PUT /api/v1/ledger/contracts/{contractID}/status`
- `POST /api/v1/ledger/commitments`
- `POST /api/v1/ledger/budgets`
- `POST /api/v1/ledger/costs/reconcile`
- `GET /api/v1/ledger/budget-variance?fiscalPeriod=...&scenario=...`
- `GET /api/v1/ledger/export.csv?fiscalPeriod=...&scenario=...`

The REST/OpenAPI and protobuf contracts use integer minor units. The provider-neutral `ledger.Store` contract is implemented by PostgreSQL and the deterministic in-memory adapter; future persistence adapters must pass the same contract tests.

## Accessible interface

The Ledger workbench provides semantic headings, labelled controls, expandable creation panels, live success and focus-managed error messages, text over-budget state, keyboard-operable status forms, visible focus, and horizontally contained data tables. It does not communicate state by color alone. At narrow widths, controls reflow while each wide table scrolls inside its own region instead of widening the page. Global reduced-motion behavior applies.

## Validation

Automated coverage includes:

- exact minor-unit parsing and currency validation
- purchase-order and contract transition rules with stale revision rejection
- date range and multi-year commitment validation
- Atlas, People, Vault, vendor, PO, and contract reference validation
- source reconciliation create, replay, and update behavior
- fiscal-period variance, over-budget state, mixed-currency rejection, and CSV export
- memory/PostgreSQL provider contract behavior and organization isolation
- all six lossless Exchange projections, strict DTO/dependency validation,
  arbitrary revision preservation, deterministic audit repair, bounded
  repeatable-read PostgreSQL export, and imported-resource write fencing
- REST authentication, CSRF, status, reconciliation, variance, and export behavior
- React runtime response validation, financial forms, narrow table containment, and automated accessibility checks

Run `go test -race ./...`, `go vet ./...`, `go run ./cmd/tracecheck`, `npm run typecheck`, `npm test`, `npm run openapi:lint`, and `npm run build` before release.
