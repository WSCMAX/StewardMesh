# Atlas — Asset inventory

- **Canonical ID:** `inventory.assets`
- **Requirement:** `REQ-ATLAS-001`
- **GitHub issue:** [#2 — Atlas asset inventory](https://github.com/WSCMAX/StewardMesh/issues/2)
- **Product catalog extension:** [Atlas Catalog](atlas-catalog.md) (`REQ-ATLAS-CATALOG-001`)
- **Identification extension:** [Atlas Codes](atlas-codes.md) (`REQ-ATLAS-CODES-001`, [GitHub #60](https://github.com/WSCMAX/StewardMesh/issues/60))

## Purpose

Atlas is the organization-scoped source of truth for servers, computers, network equipment, virtual resources, and related devices. It keeps stable identity, location, stewardship, purchase, revision, and lifecycle information together without absorbing People, Guard, Exchange, or provider-specific behavior.

## Roles and permissions

- Users with `assets.read` can list, search, filter, inspect, and read lifecycle history for assets in their organization.
- Users with `assets.write` can create and update assets. Browser mutations also require the in-memory CSRF token and an allowed same-origin request.
- Directory-backed site, building, room, department, and user choices are shown when the user also has `directory.read`.
- Imported assets registered through Guard remain write-locked until an authorized administrator claims local ownership.

Guard remains authoritative. Frontend action visibility is a usability hint and cannot widen server access.

## Asset model and validation

Every asset has an organization, stable ID, name, kind, status, optimistic revision, and created/updated timestamps. Optional identity fields include asset tag, serial number, and hostname. Asset tag and serial number are case-insensitively unique within an organization when present.

Supported kinds are server, computer, desktop, laptop, tablet, phone, network, peripheral, virtual, and other. Supported statuses are draft, active, inactive, retired, and disposed.

Locations use People-owned site, building, and room records. Buildings require their owning site; rooms require their owning site and building. Department and primary-user references also resolve through People. PostgreSQL foreign keys and the provider-neutral reference validator enforce the same organization and hierarchy boundaries.

Purchase dates are stored as calendar dates. Updates require the current revision, so a stale browser or integration receives a conflict rather than silently overwriting a newer record.

Barcode and QR identifiers are implemented through the separately traceable Atlas Codes extension. Its association model supports multiple active or historical identifiers without overloading asset tags or serial numbers.

Reusable manufacturer/model facts, configurations, prices, and upgrade paths belong to Atlas Catalog rather than to individual asset categories. A later association slice will let an asset reference a catalog product and optional configuration while preserving item-specific identity, ownership, location, purchase facts, and lifecycle history in Atlas.

## Lifecycle history and audit

Creation records the initial asset status at revision 1. Every later status change appends an immutable lifecycle event containing the previous status, new status, note, revision, actor, and time. Non-status edits advance the asset revision without manufacturing a lifecycle transition.

Atlas emits:

- `atlas.asset.created`
- `atlas.asset.updated`

Audit metadata includes `REQ-ATLAS-001`, kind, status, revision, and the previous status when it changes. Audit responses and lifecycle views contain identifiers and state transitions, not credentials or private authentication material.

## APIs and provider boundaries

REST endpoints:

- `GET|POST /api/v1/assets`
- `GET|PUT /api/v1/assets/{assetId}`
- `GET /api/v1/assets/{assetId}/lifecycle`

List filters include bounded search, kind, status, site, department, user, and limit. Search covers name, asset tag, serial number, and hostname. OpenAPI and protobuf contracts carry the same fields and optimistic revision boundary.

The `atlas.Store` interface is the adapter contract for memory, PostgreSQL, and a future DynamoDB implementation. Repository conformance tests cover create, retrieve, search, update, stale revisions, conflicts, and lifecycle ordering. Exchange and future provider imports must call the Atlas service rather than bypassing normalization, reference checks, revision behavior, or audits.

Migration `0011_atlas_assets.sql` creates the durable asset and lifecycle tables, scoped uniqueness, People foreign keys, and search indexes. The upgrade deliberately does not retrofit a foreign key onto older People assignment rows; new assignments continue to verify asset existence through Atlas before persistence.

## Accessible workflow

1. Search or filter assets by identifying values, kind, or status.
2. Choose an asset to inspect its current record and lifecycle timeline.
3. With write permission, choose **Add asset** or **Edit**.
4. Enter core identity fields and optional visible People references.
5. When changing status, add a concise lifecycle note and save with the current revision.
6. Success, loading, validation, permission, stale-revision, and failure states are announced in text.

The surface uses semantic headings, a labeled search region, native form controls, minimum-height actions, keyboard-operable details, non-color status text, responsive grids, and a single-column narrow-width reflow. It does not require motion.

## Issue reporting

Report Atlas problems through the application issue link or GitHub issue #2. Include the safe correlation ID from an API error plus the asset ID and revision when relevant. Do not attach confidential serials, hostnames, purchase documents, directory details, session cookies, CSRF values, or provider credentials.

## Test coverage

- Atlas service validation, normalization, reference failure, audit, revision, and lifecycle tests.
- Shared memory and PostgreSQL store conformance tests.
- PostgreSQL migration structure and optional integration tests.
- HTTP authentication, CSRF, origin, search, detail, update, conflict, ownership-lock, and lifecycle tests.
- React create, edit, filter, detail, lifecycle, permission, status announcement, and axe checks.
- Repository-wide race tests, vet, vulnerability checks, traceability, OpenAPI lint, typecheck, frontend tests, production build, deployment configuration, and authenticated frontend-proxy smoke validation.
