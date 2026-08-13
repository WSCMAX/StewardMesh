# People — Users, locations, departments, and assignments

- **Canonical ID:** `identity.directory`
- **Requirement:** `REQ-PEOPLE-001`
- **Location requirement:** `REQ-DIRECTORY-EXPANSION-001`
- **Workspace requirement:** `REQ-WORKSPACE-001`
- **Roadmap issue:** [#6](https://github.com/WSCMAX/StewardMesh/issues/6)

## Purpose

People gives StewardMesh a provider-neutral directory for the people and groups that use, support, or steward assets. It organizes identities by optional sites and departments and preserves effective-dated assignment history. The model supports single-user equipment, shared workstations, public computers, computer labs, and devices with several simultaneous users.

## Directory model

People uses four explicit identity types:

- **Person** for an individual. A valid email address is required.
- **Shared identity** for a named shared or pseudo-user list without pretending it is a person.
- **Public users** for equipment available to a broad, non-enumerated population.
- **Computer lab users** for multi-user lab or classroom equipment.

Sites and departments are independent first-class records. A site can include a
validated structured address and contain buildings; each building contains
rooms. Buildings and rooms retain both organization and site ownership, and a
room's site must match its building. A department may belong to a site. An
identity may belong to a department and a site; when a department has a site
and the identity does not specify one, the site is inherited. Conflicting
department, site, building, and room references are rejected.

Every record carries its organization owner, stable server-generated ID, status, revision, and timestamps. Provider and provider-subject fields reserve a unique mapping seam for future OIDC, OAuth, and SAML provisioning. People does not store passwords, session tokens, or raw identity-provider claims.

## Permissions and scoped visibility

- `directory.read` allows directory and assignment reads.
- `directory.write` allows organization-level directory changes.
- Asset assignment changes also require `assets.write`.
- Assignment history reads also require `assets.read`.

An organization-scoped `directory.read` grant can see the whole organization directory. A site-scoped grant returns only identities, departments, buildings, and rooms in that site. A department-scoped grant returns only identities in that department and its related site, including that site's locations. Every memory and PostgreSQL query receives an explicit visibility object; an empty visibility scope fails closed. Search and location filters can only narrow the records already allowed by Guard.

The web interface uses organization permission hints only to hide unavailable controls. Guard enforces every permission on the server.

## Guided person and location workflow

The People work area composes existing directory APIs into a three-step person task without changing People ownership of the records:

1. Enter the person's display name, email address, and optional department.
2. Select a visible site, building, or room, or create the missing location inline when `directory.write` is available.
3. Review the person and resolved location before creating the identity.

The controlled draft survives forward and backward navigation as well as step-level validation errors. A building or room resolves to its containing site because the current identity contract persists a `siteId`; the exact selected building or room remains visible through review. Inline location writes and the final person write use the synchronized CSRF token and remain independently authorized by the existing endpoints. Read-only users keep the visible location inventory and receive a clear administrator path instead of creation controls.

This task is the first consumer of Workspace's reusable related-record workflow pattern. It declares People ownership and the existing identity/location API boundaries, announces asynchronous creation and confirmation, preserves inputs after a recoverable failure, offers retry, and clears temporary values on explicit cancellation. Workspace coordinates the steps but never weakens People validation or Guard authorization.

The matching same-host **People** documentation links back to `#workspace-people`; the same-host **Workspace** documentation explains the reusable cross-feature pattern and its ownership boundary. Guide's People topic opens both the focused People work area and this local documentation without putting a selected person, location, search term, or draft value into the URL.

## Asset assignments

People records three relationship roles:

- **Primary assignee:** one active primary identity per asset.
- **Additional user:** any number of active identities per asset, with duplicate active relationships rejected.
- **Responsible department:** one active department per asset.

Adding a new primary assignee or responsible department automatically closes the previous matching role at the new effective date. Additional users remain concurrent until ended individually. History records effective-from and effective-to timestamps, the creating actor, and the original assignee reference. A replacement cannot predate the active assignment it replaces.

Atlas supplies the asset-existence check through a small `AssetReader` interface backed by its organization-scoped service. The assignment schema deliberately does not retrofit a database foreign key because earlier deployments could contain assignment history created while Atlas was memory-backed. New assignments verify durable Atlas existence before persistence, preserving People service and API contracts while avoiding an unsafe upgrade migration.

## APIs and provider boundaries

- `GET|POST /api/v1/sites`
- `GET|POST /api/v1/buildings`
- `GET|POST /api/v1/rooms`
- `GET|POST /api/v1/departments`
- `GET|POST /api/v1/identities`
- `GET /api/v1/users` as a deprecated person-only compatibility alias
- `GET|POST /api/v1/assets/{assetId}/assignments`
- `PATCH /api/v1/assets/{assetId}/assignments/{assignmentId}`
- OpenAPI: `api/openapi/openapi.yaml`
- gRPC contract: `api/proto/stewardmesh.proto`
- PostgreSQL migrations: `internal/repository/postgres/migrations/0005_people_directory.sql` and `0006_directory_expansion.sql`

The `people.Store` interface is the behavior contract for memory, PostgreSQL, and future DynamoDB adapters. The same conformance suite validates organization isolation, scoped search, unique email and provider mappings, multi-user assignments, replacement history, and ending assignments.

## Security and privacy

- Search terms and emails are parameterized in PostgreSQL queries.
- Search limits default to 50 and cannot exceed 100.
- JSON writes are size-bounded, reject unknown fields, require the configured browser origin, and require the in-memory synchronized CSRF token.
- Server-generated IDs use cryptographic randomness.
- Audit metadata records stable IDs, identity kinds, assignment roles, and the requirement ID. It excludes display names, emails, search terms, and provider claims.
- Read responses use `Cache-Control: no-store` because directory data can contain personal information.
- Disabled directory records remain in history but cannot receive new assignments.

## Accessibility, help, and walkthrough

The People workspace targets WCAG 2.2 AA. It provides a semantic heading hierarchy, native labels, a keyboard-operable search form, native disclosure controls, explicit help text, visible focus, non-color status text, polite completion announcements, focus-managed errors, and reduced-motion behavior inherited from the application shell. Automated axe coverage runs against the populated directory and assignment interface.

The on-page quick guide follows this sequence:

1. Use the guided task to add an individual person and resolve their location together.
2. Use standalone controls for location hierarchies, departments, and shared-use identities.
3. Review the scoped directory and location inventory.
4. Select an asset and add one or more relationships.

Use Workspace's Guide report view to report a People component problem. It includes only the URL pathname, selected component, public version, coarse browser/system/viewport, and a bounded response correlation ID. It excludes the Workspace hash, query string, person and location drafts, selected record values, search terms, roles, permissions, cookies, CSRF values, and request bodies. The editable destination opens only after the user selects **Review issue before submitting**; remove any private directory data added manually.

## Audit events

- `people.site.created`
- `people.building.created`
- `people.room.created`
- `people.department.created`
- `people.identity.created`
- `people.asset_assignment.created`
- `people.asset_assignment.ended`

## Validation

- Domain validation plus department/site and site/building/room consistency tests
- PII-safe audit tests
- Memory and PostgreSQL provider conformance tests
- PostgreSQL migration and integration tests
- Organization and scoped-visibility tests
- REST permission, CSRF, strict-JSON, search, and assignment-history tests
- React type, mutation, API-response, and permission-hint tests
- Automated WCAG checks with axe
- Full Go race, vet, vulnerability, dependency, and filesystem security checks in CI
