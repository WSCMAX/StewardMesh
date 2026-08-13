# Directory Expansion

- **Canonical IDs:** `identity.directory` for locations,
  `integrations.protocols` for directory imports, and
  `threads.relationships` for the cross-record graph, and `experience.help`
  for phase-one security, accessibility, and operational closeout
- **Phase-one requirements:** `REQ-DIRECTORY-EXPANSION-001` through
  `REQ-DIRECTORY-EXPANSION-009`
- **Trace IDs:** `REQ-DIRECTORY-EXPANSION-001`,
  `REQ-DIRECTORY-EXPANSION-002`, `REQ-DIRECTORY-EXPANSION-003`,
  `REQ-DIRECTORY-EXPANSION-004`, `REQ-DIRECTORY-EXPANSION-005`,
  `REQ-DIRECTORY-EXPANSION-006`, `REQ-DIRECTORY-EXPANSION-007`,
  `REQ-DIRECTORY-EXPANSION-008`, and `REQ-DIRECTORY-EXPANSION-009`
- **Roadmap issues:** [#24](https://github.com/WSCMAX/StewardMesh/issues/24),
  [#25](https://github.com/WSCMAX/StewardMesh/issues/25),
  [#26](https://github.com/WSCMAX/StewardMesh/issues/26),
  [#27](https://github.com/WSCMAX/StewardMesh/issues/27),
  [#28](https://github.com/WSCMAX/StewardMesh/issues/28),
  [#29](https://github.com/WSCMAX/StewardMesh/issues/29),
  [#30](https://github.com/WSCMAX/StewardMesh/issues/30),
  [#31](https://github.com/WSCMAX/StewardMesh/issues/31), and
  [#32](https://github.com/WSCMAX/StewardMesh/issues/32)

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

## Phase-one release closeout

`REQ-DIRECTORY-EXPANSION-009` (`experience.help`) closes the slice with one
operator-facing [connector deployment runbook](../deployment/directory-connectors.md)
and one [release validation record](../validation/directory-expansion-release.md).
The runbook keeps endpoints and secrets server-owned, documents exact read-only
provider access and credential rotation, and distinguishes the opt-in Grouper
fixture and synthetic data from deployable production configuration.

The release gate covers every Directory Expansion requirement from 001 through
009 and requires documentation, REST/protobuf contracts, code, schema, tests,
and UI evidence for each complete trace entry. Automated checks include the
PostgreSQL-backed Go race suite, vet, vulnerability analysis, OpenAPI lint,
protobuf compilation, React type/test/build, every Compose profile, application
and fixture images, and trace verification. Real-browser checks cover
administrator and read-only import flows, sanitized error focus, named
keyboard-focusable table regions, the graph table alternative, axe, and a
320-pixel no-overflow pass.

The security review treats provider credentials, network destinations,
upstream write access, partial snapshots, cross-provider ownership, resource
bounds, Guard scopes, and demo/fixture isolation as explicit release
boundaries. In particular, the default Microsoft Entra transport does not
inherit ambient proxy configuration; injected test transports remain available
without weakening production endpoint validation.

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

An authorized operator uses **People > Directory import**, selects the
Microsoft Entra source, and previews the complete exact plan before they
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

## SailPoint Identity Security Cloud read-only synchronization

Set all four deployment-owned values to enable one SailPoint source:

```text
STEWARDMESH_SAILPOINT_SOURCE_SYSTEM_ID=sailpoint
STEWARDMESH_SAILPOINT_BASE_URL=https://<tenant>.api.identitynow.com
STEWARDMESH_SAILPOINT_CLIENT_ID=<least-privilege client or PAT ID>
STEWARDMESH_SAILPOINT_CLIENT_SECRET=<secret-manager value>
```

The endpoint must be HTTPS, contain no credentials, port, path, query, or
fragment, and end in one exact tenant subdomain of `api.identitynow.com`.
Partial, malformed, plaintext, arbitrary-host, short-client, and short-secret
configuration fails startup with a generic error. The secret is cleared from
the application's working configuration after connector construction and is
never returned through REST, protobuf, UI, persistence, errors, or audits.
Create a dedicated read-only SailPoint client or PAT whose user has only the
authorities needed by the selected read endpoints; do not grant role, identity,
account, or governance-group management access.

StewardMesh follows SailPoint's documented client-credentials flow: one
form-encoded `POST /oauth/token` exchanges the client ID and secret for a
short-lived bearer token. All directory traffic is GET-only against the fixed
`/v2025/identities`, `/v2025/accounts`, `/v2025/workgroups`,
`/v2025/workgroups/{id}/members`, `/v2025/roles`, and
`/v2025/roles/{id}/assigned-identities` paths. Redirects are rejected, ambient
HTTP proxies are disabled, bodies are limited to 2 MiB, requests time out after
15 seconds, collection pages contain at most 50 items, and one snapshot is
bounded to 500 provider pages, 5,000 normalized records, and 20,000
memberships. Only safe GET requests retry HTTP 429 or transient 5xx responses,
at most three attempts and with a two-second delay cap. Provider bodies and
record values never appear in returned errors.

SailPoint identities become People person identities; accounts become
deliberately non-person shared identities with allowlisted account-source,
native-identity, owner, lock, and correlation metadata. Account sources,
departments, governance groups, and roles become typed managed groups, while
account-source links, department membership, workgroup membership, and role
assignment become durable memberships. Disabled or lifecycle-inactive
identities, disabled accounts, and disabled roles remain in the exact plan as
inactive records. Stable provider IDs become source mappings; raw provider
documents and arbitrary identity attributes are discarded. Duplicate object
IDs, source disagreements, duplicate memberships, inconsistent totals, invalid
attributes, and over-limit snapshots fail preview before any People mutation.

Provider precedence is intentionally absent. A SailPoint record can update only
the People target owned by the same configured source and source record. If its
normalized email already belongs to local data, Entra, or another SailPoint
source, preview reports a conflict and cannot overwrite that target; the same
rule applies in the reverse direction. Locally claimed imported records also
remain conflict-protected. Operators use the provider-neutral **People >
Directory import** panel to select the credential-free source, preview the
complete persisted plan, inspect identities/groups/memberships and inactive
status, then apply only that exact plan.

The adapter follows SailPoint's official
[authentication](https://developer.sailpoint.com/docs/api/authentication/),
[identity](https://developer.sailpoint.com/docs/api/v2025/list-identities/),
[account](https://developer.sailpoint.com/docs/api/v2025/list-accounts/),
[governance-group](https://developer.sailpoint.com/docs/api/v2025/list-workgroups/),
and [role-assignment](https://developer.sailpoint.com/docs/api/v2025/get-role-assigned-identities/)
contracts. Fake-server tests cover token isolation, pagination, updates,
inactive states, every normalized object type, transient retry, malformed and
oversized data, duplicate records/memberships, configuration validation,
cross-provider conflict, safe errors, and the no-write method invariant.

## PeopleSoft Campus Solutions synchronization

The optional PeopleSoft adapter uses the delivered provider-neutral import
engine for `REQ-DIRECTORY-EXPANSION-006`. It executes four institution-owned
PeopleSoft Query Access Service queries in a fixed order: organizations,
locations, buildings, and departments. Every provider call is a synchronous
`GET` requesting `JSON/NONFILE`; the connector has no POST, PUT, PATCH, or
DELETE provider path. Oracle documents this read operation as
[`QAS_EXECUTEQRY_REST_GET`](https://docs.oracle.com/cd/G36972_01/pt861pbr5/eng/pt/trws/QueryAccessServiceOperations-1f7e36.html)
and defines the JSON row response and case-sensitive filter fields in
[`Executing the Query`](https://docs.oracle.com/cd/G10810_01/pt860pbr4/eng/pt/trws/ExecutingtheQuery-5e7fb0.html).

Configure a dedicated PeopleSoft user with access only to the four public or
private read queries and the REST execution operation. Do not grant Query
creation/save operations or Campus Solutions update services. Use exactly one
server-side authentication mode: Basic username/password or a bearer token
issued by the institution's gateway. Secrets belong in the deployment secret
manager and are cleared from the application's working configuration after the
connector is constructed.

The base URL must identify the fixed Integration Broker endpoint
`https://<host>/PSIGW/RESTListeningConnector/<node>/ExecuteQuery.v1`, without
credentials, query, or fragment. HTTPS is required except for an explicitly
enabled loopback test fixture. Private HTTPS destinations require the separate
private-network opt-in. The default transport ignores ambient proxy variables,
re-checks resolved IP addresses before dialing, blocks link-local, multicast,
unspecified, loopback, and private destinations unless explicitly permitted,
rejects redirects, uses a 15-second timeout, and limits each body to 2 MiB.
Only GET network failures, HTTP 429, and transient 5xx responses retry, at most
three attempts. Provider bodies, URLs, query fields, and credentials never
appear in returned errors or audits.

Set these deployment values to enable one source:

```text
STEWARDMESH_PEOPLESOFT_SOURCE_SYSTEM_ID=peoplesoft
STEWARDMESH_PEOPLESOFT_BASE_URL=https://ps.example.edu/PSIGW/RESTListeningConnector/CAMPUS/ExecuteQuery.v1
STEWARDMESH_PEOPLESOFT_USERNAME=<least-privilege query user>
STEWARDMESH_PEOPLESOFT_PASSWORD=<secret-manager value>
STEWARDMESH_PEOPLESOFT_QUERY_OWNER=public
STEWARDMESH_PEOPLESOFT_ORGANIZATION_QUERY=SM_ORGANIZATIONS
STEWARDMESH_PEOPLESOFT_LOCATION_QUERY=SM_LOCATIONS
STEWARDMESH_PEOPLESOFT_BUILDING_QUERY=SM_BUILDINGS
STEWARDMESH_PEOPLESOFT_DEPARTMENT_QUERY=SM_DEPARTMENTS
```

Because institutions choose their own records and aliases, one strict JSON
mapping binds each query field twice: `selector` is the unique, qualified QAS
field sent in `filterfields` (for example, `A.SETID`), while `alias` is the
unqualified key returned by `JSON/NONFILE` (for example, `SETID`). `setId`,
`id`, `name`, and `status` are required for every kind; a location requires
`organizationId`, a building requires `locationId`, and a department requires
`organizationId` with optional `locationId`. Descriptions and location address
fields are optional. A selector or alias cannot ambiguously identify two
different fields. `activeValues` and `inactiveValues` can replace the default
`A/active/enabled/true/1` and `I/inactive/disabled/false/0` sets.

```json
{
  "organization": {
    "setId": {"selector":"A.SETID","alias":"SETID"},
    "id": {"selector":"A.ORG_ID","alias":"ORG_ID"},
    "name": {"selector":"A.DESCR","alias":"DESCR"},
    "status": {"selector":"A.EFF_STATUS","alias":"EFF_STATUS"}
  },
  "location": {
    "setId": {"selector":"A.SETID","alias":"SETID"},
    "id": {"selector":"A.LOCATION","alias":"LOCATION"},
    "name": {"selector":"A.DESCR","alias":"DESCR"},
    "status": {"selector":"A.EFF_STATUS","alias":"EFF_STATUS"},
    "organizationId": {"selector":"A.ORG_ID","alias":"ORG_ID"},
    "city": {"selector":"A.CITY","alias":"CITY"}
  },
  "building": {
    "setId": {"selector":"A.SETID","alias":"SETID"},
    "id": {"selector":"A.BUILDING","alias":"BUILDING"},
    "name": {"selector":"A.DESCR","alias":"DESCR"},
    "status": {"selector":"A.EFF_STATUS","alias":"EFF_STATUS"},
    "locationId": {"selector":"A.LOCATION","alias":"LOCATION"}
  },
  "department": {
    "setId": {"selector":"A.SETID","alias":"SETID"},
    "id": {"selector":"A.DEPTID","alias":"DEPTID"},
    "name": {"selector":"A.DESCR","alias":"DESCR"},
    "status": {"selector":"A.EFF_STATUS","alias":"EFF_STATUS"},
    "organizationId": {"selector":"A.ORG_ID","alias":"ORG_ID"},
    "locationId": {"selector":"A.LOCATION","alias":"LOCATION"}
  }
}
```

Store the compact JSON value in
`STEWARDMESH_PEOPLESOFT_FIELD_MAPPINGS_JSON`. The normalized mapping, query
names, owner, endpoint, row limit, and a nonreversible fingerprint of the
authenticated principal form a non-secret configuration revision. Basic
password rotation does not change the revision, while a different Basic
username does; neither the principal nor any credential is exposed. Each query
requests only mapped selectors and one row beyond its configured limit.
Explicit warnings, truncation/more-row markers, unknown response-envelope
metadata, omitted row arrays, inconsistent counts, and over-limit results fail
closed.

QAS and the authenticated user's query-security profile can each impose a row
cap that the response cannot prove absent. The connector therefore treats every
successful four-query result as a partial snapshot even when all reported
counts are internally consistent. Explicit inactive rows still reconcile, but
a missing row never plans an implicit deactivation. One connector run remains
capped by the shared 5,000-record exact-plan limit.

Organizations, locations, buildings, and departments become typed managed
groups. Organization-to-location, location-to-building,
organization-to-department, and optional location-to-department relationships
become durable group memberships in the shared graph. Source IDs include the
object kind, SETID, and raw ID, so identical raw IDs in different SETIDs remain
distinct. Parent lookups use that same SETID scope, and a department-to-location
relationship is rejected if the two objects do not belong to the same
organization. Raw unmapped columns are discarded, and address fields are an
allowlisted metadata projection. Unknown status values, non-scalar mapped
fields, duplicate scoped IDs or relationships, missing hierarchy parents,
inconsistent row counts or query names, malformed/multiple JSON values, and
oversized results fail the preview before mutation.

Operators use **People > Directory import** to preview, inspect, and apply the
persisted partial plan. Returned inactive records and updates remain explicit;
missing-source deactivations are never planned from QAS. Locally claimed
managed records produce explicit conflicts, and retries never re-pull after a
successful preview. Source
mappings, attempts, conflicts, and audit history reuse the authoritative
memory/PostgreSQL provider-neutral stores; no migration is needed for this
adapter. Fake QAS tests cover all four queries, mapping variants, deterministic
four-query collection traversal, duplicate raw IDs across SETIDs,
update/inactive/conflict behavior, missing and inconsistent parents,
warning/truncation/malformed/oversized responses, safe status and mid-body read
retries, authentication-before-read, audit redaction, and the GET-only
invariant.

## Planned provider slices

Remaining provider connectors are deliberately read-only and plug into the
delivered source-system contract. Each provider-specific mapper must retain
source IDs, use an explicit configuration revision, produce a bounded complete
snapshot, and report conflicts instead of silently overwriting records.

Synthetic data is disabled by default. It is intended only for isolated
demonstrations and integration tests. The Grouper container is available only
through an explicit Docker Compose profile.

The graph API uses typed nodes and edges and must apply the caller's directory
visibility scope. Any graph UI must provide a keyboard-accessible text or
table representation in addition to visual exploration.

## Grouper REST synchronization

The delivered Internet2 Grouper adapter reads the provider's SCIM v2 `Groups`
collection with `GET` only. Each normalized group retains its stable source ID,
name, display name, optional description, active state, and at most 16 bounded
string metadata fields. Direct subject and nested-group members become typed
`member_of` edges. No raw Grouper response, provider session, password, bearer
token, arbitrary schema extension, or source value is written to audit history.

The connector uses one fixed server-configured URL whose path must be exactly
`/grouper-ws/scim/v2`; callers cannot submit or override it through REST. HTTPS
is required. Plain HTTP is accepted only for an explicitly enabled loopback
development fixture; the same private-network opt-in permits trusted private
HTTPS endpoints. The default network transport does not inherit proxy
variables, refuses redirects, re-checks resolved addresses before dialing, and
blocks loopback and private addresses unless that explicit private-network
setting is enabled. Link-local, multicast, and unspecified addresses remain
blocked even with the opt-in. Responses default to 2 MiB, pages to 100 groups,
requests to 15 seconds, and transient retries to three. The shared engine adds
its 100-page and 5,000-record limits, rejects pagination loops and duplicate
normalized IDs, and only deactivates missing groups or memberships after the
connector confirms a complete snapshot.

Migration `0030_grouper_directory_graph.sql` widens the existing mapping kind
constraint and stores normalized group and membership targets with
organization/source uniqueness, optimistic revisions, typed member checks,
parent-group integrity, and graph indexes.

Configuration is opt-in. Leave `STEWARDMESH_GROUPER_URL` empty for the default
runtime. In a shared environment, inject either a bearer token or a Basic
username/password from a secret manager and configure:

```sh
export STEWARDMESH_GROUPER_URL=https://grouper.example.edu/grouper-ws/scim/v2
export STEWARDMESH_GROUPER_SOURCE_SYSTEM_ID=grouper-primary
export STEWARDMESH_GROUPER_BEARER_TOKEN="from-secret-manager"
export STEWARDMESH_GROUPER_CONFIG_REVISION=v1
```

For an isolated local fixture only, start the explicit profile and enable its
loopback endpoint. The committed fixture token is intentionally development
only and must never be reused outside this loopback container:

```sh
docker compose -f deploy/docker-compose.yml --profile integrations up -d --wait postgres grouper
export STEWARDMESH_GROUPER_URL=http://127.0.0.1:8081/grouper-ws/scim/v2
export STEWARDMESH_GROUPER_SOURCE_SYSTEM_ID=grouper-fixture
export STEWARDMESH_GROUPER_BEARER_TOKEN=stewardmesh-local-fixture-token
export STEWARDMESH_GROUPER_ALLOW_PRIVATE_NETWORK=true
```

Create fake fixture groups and memberships with authenticated development-only
`POST /fixture/groups` and `POST /fixture/memberships`; remove them with the
matching `DELETE /fixture/.../{id}` routes. The adapter itself never invokes
those routes. Preview and apply through the standard directory-import API, then
use the permission-scoped relationship graph to inspect semantic group and
subject nodes plus nested edges. Replaying an idempotency key returns the
original result; removing a fixture membership and completing a new preview
produces an explicit deactivation instead of deleting history.

## Synthetic demo dataset

Synthetic setup is disabled by default. Set `STEWARDMESH_SEED_SYNTHETIC=true`
only with an organization ID beginning with `demo-`; invalid booleans and
non-demo organization IDs fail startup before any data is written. The
versioned, network-free source creates a `[Synthetic Demo]` site, building,
room, department, three identities, two groups, and three direct or nested
memberships. Example-domain email addresses end in `.invalid` and cannot route.

Locations use collision-safe People writes: a matching label is reused only
when its complete expected location data matches, while a mismatched local
record fails closed. People, groups, memberships, provider mappings, imported
ownership locks, and relationship edges use the regular durable directory
preview/apply engine under the isolated `synthetic-demo-v1` source. Fixed
dataset-version idempotency keys make repeated startup an exact replay rather
than a duplicate seed. Audit events contain stable IDs and safe dataset/source
labels, never names, addresses, or email values.

The optional Compose initializer is a one-shot command. It starts PostgreSQL as
its only required dependency, seeds the `demo-local` organization, and exits:

```sh
docker compose -f deploy/docker-compose.yml --profile demo run --rm demo-seed
```

The default Compose profile contains no demo initializer. The `demo` profile
also makes Valkey and the development Grouper fixture available, but synthetic
seeding does not contact or require either service. Use the separate
`integrations` profile when testing only Grouper. Never enable the synthetic
flag for a production organization or treat the committed fixture credential
as a deployable secret.

## Relationship graph

`GET /api/v1/graph` is a read-only projection over authoritative People,
directory-import, and Atlas stores. It returns typed nodes for organizations,
sites, buildings, rooms, departments, person/shared/public/lab identities,
imported groups and subjects, and authorized assets. Typed edges represent
containment, membership, location, department ownership, and asset assignment.
The projection does not copy records or become a new source of truth.

The server derives directory visibility and Atlas visibility from the
authenticated Guard grants. An asset must satisfy its `assets.read` grant and
the caller's directory visibility before it can appear. A resource grant never
reveals an asset tied only to a hidden site, department, or person. Imported
groups currently have no site or department boundary, so they are included
only for organization-wide directory readers. Email addresses, source
subjects, provider endpoints, credentials, and raw payloads are never graph
attributes.

Search and node-kind filters select anchor records. A relationship-kind filter
retains the other endpoint of each matching edge as bounded context, so
cross-type relationships remain meaningful. The 1–500 record limits are
bounded and validated. Output is deterministic, uses no-store caching, collapses semantic
duplicate edges, preserves safe cycles, retains disconnected matching nodes,
and returns non-null empty arrays when nothing matches. Every returned edge has
two returned endpoints, preventing a relationship from revealing a hidden
record indirectly.

People exposes keyboard-native filter controls, a bounded visual overview, a
complete relationship table, and a separate table for disconnected records.
Both tables are focusable horizontal regions at narrow widths; the text view is
authoritative for screen readers and for results larger than the visual's
40-node overview. The same filter and result contracts are documented in
OpenAPI and protobuf, while authorization scope remains transport-owned.
