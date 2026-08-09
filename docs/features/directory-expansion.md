# Directory Expansion

- **Canonical ID:** `identity.directory`
- **Current requirement:** `REQ-DIRECTORY-EXPANSION-001`
- **Roadmap issue:** [#24](https://github.com/WSCMAX/StewardMesh/issues/24)

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

## Validation

Service, memory-adapter, shared repository-contract, PostgreSQL migration,
REST, React runtime-validation, mutation, and automated accessibility tests
cover address normalization, organization isolation, hierarchy consistency,
duplicate locations, and site- or department-scoped visibility.

## Planned integration slices

Provider connectors are deliberately read-only. Microsoft Entra ID and
SailPoint provide identity records; Internet2 Grouper provides groups and
memberships; PeopleSoft Campus Solutions provides configurable organization
and location records. Imports must retain provider source IDs and report
conflicts instead of silently overwriting records.

Synthetic data is disabled by default. It is intended only for isolated
demonstrations and integration tests. The Grouper container is available only
through an explicit Docker Compose profile.

The graph API uses typed nodes and edges and must apply the caller's directory
visibility scope. Any graph UI must provide a keyboard-accessible text or
table representation in addition to visual exploration.
