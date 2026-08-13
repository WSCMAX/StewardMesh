# Directory Expansion

- **Canonical IDs:** `identity.directory` for locations and
  `integrations.protocols` for directory imports
- **Current requirements:** `REQ-DIRECTORY-EXPANSION-001` through
  `REQ-DIRECTORY-EXPANSION-003`
- **Roadmap issues:** [#24](https://github.com/WSCMAX/StewardMesh/issues/24),
  [#25](https://github.com/WSCMAX/StewardMesh/issues/25), and
  [#26](https://github.com/WSCMAX/StewardMesh/issues/26)

Directory Expansion extends People with hierarchical locations, optional
read-only institutional provider connectors, synthetic demo data, and a
relationship graph.

The canonical requirements are REQ-DIRECTORY-EXPANSION-001 through
REQ-DIRECTORY-EXPANSION-009. The related GitHub tracking issues are #24
through #32 in the StewardMesh Roadmap and StewardMesh v1 Delivery projects.

## Delivered location hierarchy

The first expansion slice adds optional structured addresses to existing Site
records without changing the required fields used by existing clients. Sites
contain buildings and buildings contain rooms. Every location has an
organization owner, stable server-generated ID, site reference, status,
revision, and timestamps. Room writes verify that the supplied site matches the
selected building.

`GET /api/v1/buildings` and `GET /api/v1/rooms` accept optional `siteId` and
`buildingId` filters. Filters narrow the caller's Guard-derived visibility and
never widen it. The memory and PostgreSQL adapters implement the same
provider-neutral contract, and the People interface exposes a semantic nested
list and native labeled forms for keyboard users.

## Permissions, API, and audit

- `directory.read` returns only locations in the caller's organization, site,
  or department-derived site scope.
- Organization-scoped `directory.write` plus the synchronized CSRF token is
  required to create sites, buildings, and rooms.
- REST: `GET|POST /api/v1/sites`, `/api/v1/buildings`, and `/api/v1/rooms`.
- Contracts: `api/openapi/openapi.yaml` and `api/proto/stewardmesh.proto`.
- Location writes emit `people.site.created`, `people.building.created`, or
  `people.room.created` without placing names or addresses in audit metadata.

## Accessible walkthrough

1. Create a site, optionally entering line 1, city, and a two-letter country
   code for its structured address.
2. Add a building and select its active site.
3. Add a room and select its active building; the site is derived by the
   interface and verified by the server.
4. Review the nested site, building, and room lists. This text hierarchy is the
   keyboard and screen-reader alternative to future visual graph exploration.

## Delivered provider-neutral directory imports

Bridge provides a shared read-only connector and reconciliation boundary for
configured directory sources. The stable contract identifies a configuration
with `sourceSystemId`; provider brands, endpoints, credentials, and raw payloads
do not appear in the REST or gRPC request and response shapes. This keeps the
contract usable by provider-specific connectors and future Exchange workflows
without coupling either surface to a provider schema.

`POST /api/v1/directory-imports/preview` pulls one configured source and stores
its bounded snapshot as an immutable reconciliation plan, including whether the
connector confirmed that the snapshot is complete. Preview never mutates People
records. One pull accepts at most 100 provider pages, and the complete plan is
limited to 5,000 items including historical mappings considered for
deactivation; pagination loops, duplicate source IDs, and overflow fail before
the plan is stored. Every item retains its source record ID, planned action,
bounded normalized typed record, target identity and expected revision when
applicable, sanitized conflict or failure guidance, and initial outcome. Raw
provider payloads, arbitrary attributes, credentials, internal mapping rows, and
source/target digests remain private and are not returned to clients.

`POST /api/v1/directory-imports/{batchID}/apply` executes only that persisted
plan. `POST /api/v1/directory-imports/{batchID}/retry` executes only retryable
failed actions from the same plan and never re-pulls after a successful preview.
When a transient connector error prevents a preview plan from being created,
retry resumes that same failed batch using its pinned source-system and
configuration revision, then durably installs the plan before it becomes
applicable. Neither operation accepts amended records or mappings. The authoritative item plan retains the
source and observed-target digests used by preview, and a complete-snapshot flag
prevents partial source reads from causing unsafe deactivations. Conflicts remain
explicit rather than overwriting local or externally owned records. Once an
administrator claims an imported identity for local ownership, any later source
change or deactivation is reported as a conflict and cannot overwrite it;
unchanged reconciliation remains safe and does not restore the import lock.

Every preview, apply, and retry requires a bounded `Idempotency-Key`. Its digest,
the configuration revision, item outcomes, counts, errors, and timestamps
are durable. Replaying the same operation returns its original result; attempting
to reuse a key for different input is rejected. `GET /api/v1/directory-imports`
provides bounded cursor pagination and `GET /api/v1/directory-imports/{batchID}`
returns the mapping-safe plan plus append-only attempt history. All directory
import responses use `Cache-Control: no-store`. A batch retains at most 100
preview/apply/retry attempts so its complete history remains bounded. Actor
identities remain in the authoritative audit history and are not returned by
the import detail API.

### Authorization, audit, and runtime truth

- Organization-scoped `integrations.read` is required for list and detail.
- Organization-scoped `integrations.write`, an authenticated Guard session, and
  the synchronized CSRF token are required for preview, apply, and retry.
- Preview, apply, retry, completion, conflict, and failure transitions emit
  sanitized audits without raw directory values, provider payloads, credentials,
  or idempotency keys.
- PostgreSQL migration `0028` stores the batch, immutable items, attempts,
  mappings, conflicts, source identities, and idempotency records. PostgreSQL or
  another authoritative repository owns import truth.
- Valkey-compatible coordination may provide a short-lived lease around a worker,
  but it never stores the authoritative plan or outcome. Imports remain correct
  with cache disabled, and an unavailable lease fails safely without fabricating
  completion.

The protobuf contract mirrors operation results, batches, normalized items,
counts, outcomes, failures, attempts, cursor pages, and timestamp semantics.
Exchange may reuse the stable source identities and mapping-safe reconciliation
metadata, while Exchange packages and directory provider payloads remain
separate inputs with separate ownership boundaries.

## Validation

Service, memory-adapter, shared repository-contract, PostgreSQL migration,
REST, React runtime-validation, mutation, and automated accessibility tests
cover address normalization, organization isolation, hierarchy consistency,
duplicate locations, and site- or department-scoped visibility. Import contract,
repository, and HTTP tests additionally cover dry-run non-mutation, complete
snapshots, exact-plan apply/retry, source identity retention, idempotency,
conflicts, durable attempts, authorization, CSRF, audit redaction, bounds,
`no-store`, and operation with Valkey disabled.

## Microsoft Entra ID read-only synchronization

Set all four server-side values to enable one Microsoft Entra source:

```text
STEWARDMESH_ENTRA_SOURCE_SYSTEM_ID=entra
STEWARDMESH_ENTRA_TENANT_ID=<single tenant UUID>
STEWARDMESH_ENTRA_CLIENT_ID=<application UUID>
STEWARDMESH_ENTRA_CLIENT_SECRET=<secret-manager value>
```

Partial, multi-tenant (`common`, `organizations`, or `consumers`), malformed,
or short-secret configuration fails application startup without echoing the
credential. The client secret belongs in the deployment secret manager and is
cleared from the application's working configuration after connector
construction. It never appears in REST, protobuf, UI, persistence, errors, or
audits.

Register a single-tenant Microsoft Entra application and grant the application
permissions `User.Read.All` and `GroupMember.ReadBasic.All`, then grant
administrator consent. This read-only combination covers the selected user and
basic group properties plus direct member identifiers without the broader
`Directory.Read.All` grant. Add `Member.Read.Hidden` only when hidden-membership
groups are explicitly in scope. Do not grant `Directory.ReadWrite.All`,
`Group.ReadWrite.All`, `User.ReadWrite.All`, or any delegated permission to this
connector. StewardMesh requests only the `.default` application scope. OAuth
client-credential acquisition necessarily
uses a form POST to the tenant token endpoint; every Microsoft Graph directory
operation is a GET, redirects are rejected, and there is no code path for a
Graph POST, PATCH, PUT, or DELETE.

The connector reads only the fixed `https://graph.microsoft.com/v1.0` user,
group, and direct group-members endpoint set. It follows only validated
same-scheme, same-host, same-version `@odata.nextLink` values, caps response
bodies at 2 MiB, uses a 15-second client timeout, caps the snapshot at 100
pages, 5,000 users/groups, and 20,000 direct memberships, and limits every
identity to 256 direct groups. HTTP 429 and transient 5xx GET responses retry
at most three times with a two-second maximum delay; credential, permission,
shape, duplicate, and unsafe-link errors do not retry.

Users map to People person identities, including disabled users as inactive.
Groups map to shared identities. Department, direct group source IDs, and an
allowlisted projection of job title, office location, user type, and group
mail/security flags remain in the durable reconciliation record. Raw Graph
documents and unsupported
directory objects are not retained. Duplicate object IDs or memberships fail
the preview instead of silently merging records.

An authorized operator uses **People > Microsoft Entra directory import** to
select the credential-free source identity, preview the complete exact plan,
review create/update/deactivate/conflict counts and normalized audit detail,
then apply that persisted plan. `integrations.read` exposes source identity and
history; `integrations.write` plus CSRF is required for preview/apply/retry.
The same behavior is defined in REST and protobuf contracts, and all responses
remain `no-store`.

Connector tests use an injectable fake HTTP server and cover pagination,
inactive users, groups, direct memberships, attributes, duplicates, bounded
retries, malformed/oversized/unsafe responses, credential validation, safe
errors, and method assertions proving Graph traffic remains GET-only. HTTP,
React, accessibility, race, vet, vulnerability, OpenAPI, protobuf, PostgreSQL,
and browser checks complete the release gate.

## Planned provider slices

Remaining provider connectors are deliberately read-only and plug into the
delivered source-system contract. SailPoint provides identity records;
Internet2 Grouper provides groups and memberships; PeopleSoft Campus Solutions
provides configurable organization and location records. Each
provider-specific mapper must retain source IDs, use an explicit configuration
revision, produce the bounded complete snapshot, and report conflicts instead
of silently overwriting records.

Synthetic data is disabled by default. It is intended only for isolated
demonstrations and integration tests. The Grouper container is available only
through an explicit Docker Compose profile.

The graph API uses typed nodes and edges and must apply the caller's directory
visibility scope. Any graph UI must provide a keyboard-accessible text or
table representation in addition to visual exploration.
