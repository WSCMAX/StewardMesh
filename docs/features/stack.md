# Stack — Software inventory and license management

- **Canonical ID:** `software.licenses`
- **Requirement:** `REQ-STACK-001`
- **Roadmap issue:** [#7](https://github.com/WSCMAX/StewardMesh/issues/7)

## Purpose

Stack records which software products and versions are installed on Atlas assets and how license entitlements cover that use. It connects license purchases to Ledger vendors, purchase orders, contracts, and current costs, while Vault remains authoritative for supporting documents and People remains authoritative for identity, department, and site assignees.

Stack owns software inventory and entitlement calculations. Atlas, People, Ledger, and Vault records are referenced by stable organization-scoped IDs and are never copied or updated by Stack. Signals can consume the explicit compliance conditions without reimplementing Stack's calculations.

## Records and calculations

Products contain a publisher, name, optional category, and lifecycle status. Versions belong to one product and can carry a release date and support status. Retired products and versions are terminal; unsupported versions may return to active service. An installation connects one version to one Atlas asset, records installation/removal times, and exposes `unknown`, `used`, or `unused` usage state. Removal and assignment end are terminal. Only one active installation of the same version may exist on an asset. Every lifecycle change uses an optimistic revision.

Licenses define a positive quantity and one entitlement metric: device, user, concurrent, site, or enterprise. They may be product-wide or version-specific, effective-dated, and linked to a Ledger vendor, purchase order, contract, cost record, and up to 100 Vault documents. Start and expiration dates are UTC calendar dates; an entitlement remains active through its expiration date and expires at the start of the following date. The service verifies that linked versions belong to the selected product and that supplied financial references agree with each other.

Assignments allocate positive seat counts to an Atlas asset or a People identity, department, or site. Device entitlements require asset assignments, user entitlements require identities, and site entitlements require sites; concurrent and enterprise entitlements may be allocated to any supported scope. Only one active assignment for the same license and assignee can exist. Usage and end changes use optimistic revisions so concurrent edits cannot silently overwrite one another.

Analytics are evaluated at an explicit timestamp and identify:

- expired or soon-expiring licenses;
- license quantities with more assigned seats than entitlements;
- active assignments explicitly marked unused; and
- active software installations with no matching asset, assigned identity, department, site, or enterprise entitlement.

The response always includes a human-readable state and severity in addition to the machine-stable condition code. Conditions are deterministically ordered and are not communicated by color alone.

## Import, export, and provider boundaries

`GET /api/v1/stack/exchange` returns bounded, dependency-aware records for products, versions, licenses, installations, and assignments. Each record includes its stable ID, revision, typed dependencies, and bounded typed JSON payload. Before writing, `POST /api/v1/stack/exchange/import` strictly decodes every payload, rejects unknown fields and duplicate records, verifies that IDs and revisions match their envelopes, and verifies the exact dependency set.

The JSON import is canonicalized into a deterministic metadata-only `.openinventory` package and executed by Exchange rather than applying an in-process batch. Because the legacy JSON envelope has no schema field, its five Stack record types are permanently pinned to the v1 built-in template IDs; later schema-aware import surfaces must carry an explicit template version. This keeps byte identity and package identity stable across built-in evolution. Exchange reserves the durable receipt and a per-record operation intent before each provider call, registers the earliest source identity with Guard before mutation, and checkpoints created or unchanged truth even if the Stack store, ownership registration, or post-commit audit delivery fails. A failure response contains the package ID, failed status, error code, every checkpointed record outcome, and any Guard ownership lock that could not yet be paired with a durable provider outcome, so no committed prefix or recovery fence is hidden. Retrying the exact JSON addresses the same package and resumes or replays it without duplicate records or audit events. Changed content derives a different package identity and conflicts with existing source/record identity instead of replacing it.

Imported records are readable but Guard write-locks them until an administrator explicitly claims local ownership. Organization IDs and timestamps inside portable payloads are transport-local data: Exchange projects only the allowlisted Stack schema and Stack always writes into the server-configured organization. The low-level Stack provider seam accepts only one Exchange-reserved operation token at a time; arbitrary callers cannot bypass the receipt/ownership workflow with a batch call.

The portable record seam remains provider-neutral: Stack is not coupled to archive, filesystem, object-storage, or concrete database details. The `stack.Store` contract is implemented by deterministic memory and PostgreSQL adapters and includes organization isolation, defensive copying, unique active relationships, exact source replay, and optimistic updates.

## Permissions, privacy, and audit

- `software.read` lists Stack records, analytics, and portable exports.
- `software.write` creates and imports records and manages product, version, installation, entitlement, assignment, and usage lifecycles.

Migration `0029_stack_software_licenses.sql` creates the durable organization-scoped schema and adds both permissions only to existing built-in Administrator policy bundles. Custom roles remain unchanged until an administrator explicitly grants access.

Every route is authenticated, permission checked, organization scoped, and non-cacheable. Browser writes require the synchronized CSRF token and configured origin. Request sizes, record counts, text, IDs, dates, quantities, dependencies, and portable payloads are bounded. Stack never accepts an organization ID from a client.

Audit events include the stable action, record ID, status or non-sensitive relationship IDs, revision where applicable, and `REQ-STACK-001`. They omit product and publisher names, document contents, financial descriptions and amounts, identity details, session material, and source payloads.

## APIs and accessible workflow

- `GET /api/v1/stack`
- `GET /api/v1/stack/analytics`
- `POST /api/v1/stack/products`
- `PUT /api/v1/stack/products/{productID}/status`
- `POST /api/v1/stack/versions`
- `PUT /api/v1/stack/versions/{versionID}/status`
- `POST /api/v1/stack/installations`
- `PUT /api/v1/stack/installations/{installationID}`
- `POST /api/v1/stack/licenses`
- `PUT /api/v1/stack/licenses/{licenseID}/entitlement`
- `POST /api/v1/stack/assignments`
- `PUT /api/v1/stack/assignments/{assignmentID}/usage`
- `PUT /api/v1/stack/assignments/{assignmentID}/end`
- `GET /api/v1/stack/exchange`
- `POST /api/v1/stack/exchange/import`

The Stack workbench uses labelled native controls, semantic headings, focus-managed feedback, text condition summaries, and contained data regions. Users can define products and versions, associate a version with an Atlas asset, record and revise a purchased entitlement and its references, assign and end seats, update usage, retire records, remove installations, and inspect the resulting compliance report without leaving Workspace. Write controls are absent for read-only sessions and terminal records. At 320 pixels controls reflow and wide record details remain inside their own labelled scroll region instead of widening the page. Reduced-motion behavior applies.

Guide provides contextual help, a dismissible and replayable walkthrough, a safe example, and a link to this documentation. Issue reports are sanitized to application/page/component/version, coarse browser and system families, viewport, and a valid correlation ID. Do not include software names, license keys, purchase details, money values, directory identities, documents, provider credentials, cookies, or CSRF tokens.

## Validation

Coverage includes product/version relationships, Atlas and People associations, Ledger/Vault reference validation, date and quantity limits, assignment uniqueness and revisions, expiration and entitlement calculations, deterministic import/export replay, disclosed and resumable mid-batch Stack-store/Guard/audit failures, imported-resource ownership locks, organization isolation, redacted audit metadata, memory/PostgreSQL provider conformance, authentication/permissions/CSRF/no-store HTTP behavior, OpenAPI/protobuf parity, accessible React workflows, narrow-width containment, and real authenticated browser checks.

Release validation runs race-enabled Go tests with PostgreSQL, vet, vulnerability scanning, OpenAPI lint, protobuf validation, traceability, Node typecheck/tests/build, container checks, and authenticated desktop and 320-pixel browser journeys.
