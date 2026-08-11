# Threads — Tags and strategic goals

- **Canonical ID:** `goals.tags`
- **Requirement:** `REQ-THREADS-001`
- **GitHub issue:** [#5 — Threads tags and strategic goals](https://github.com/WSCMAX/StewardMesh/issues/5)

## Purpose

Threads adds durable organization context to StewardMesh records. Tags form a parent-child classification hierarchy, while goals form a parent-child strategy hierarchy that can be linked to assets and purchases. Explicit rules, inherited values, overrides, suppressions, and their sources remain visible so integrations and users never need to infer why a record has a value.

This slice provides the complete asset workflow and the provider-neutral attachment boundary for purchases, contracts, software, budgets, and goals. Future modules validate their own target IDs through that boundary instead of placing feature-owned records inside Threads.

## Roles and permissions

- Users with `goals.read` can list tags and goals and inspect effective tag provenance and goal links.
- Users with `goals.write` can create and update hierarchies, set or remove explicit tag rules, and link or unlink goals. Browser mutations also require the in-memory CSRF token and an allowed same-origin request.
- Relationship mutations also pass Guard resource-write checks for the target. Imported records remain write-locked until an authorized administrator claims local ownership.

Guard remains authoritative. Hiding write controls in the interface is a usability aid and cannot widen server access.

## Hierarchies and inheritance

Tags and goals have stable organization-scoped IDs, case-insensitively unique names, optimistic revisions, and created/updated timestamps. Parent references stay within the same organization. The service walks every proposed parent chain and rejects self-reference or indirect cycles before persistence.

Each tag declares whether it is inherited when an explicitly applied descendant is present. Effective evaluation exposes three states:

- `explicit` means the target has a direct include rule.
- `inherited` means an explicitly included descendant caused an inheritance-enabled ancestor to apply; `sourceTagId` identifies that descendant.
- `suppressed` means a direct suppress rule prevents an inherited value from silently applying.

Direct include and suppress rules take precedence over inherited values. Every rule has its own revision and actor provenance. Removing a rule restores normal inheritance rather than copying or deleting hierarchy data.

## Goals and relationships

Goals include a name, optional description, and optional parent. They link idempotently to assets and purchases and become stable dimensions for later planning and analytics. The current web workflow manages asset links; the purchase target is already represented in the API and repository contracts for Ledger to adopt without a schema redesign.

Tags can target assets, purchases, contracts, software, budgets, and goals. Atlas targets are validated now. Future module target validators will replace the current stable-ID seam as those record types become available.

## API and provider boundaries

REST endpoints:

- `GET|POST /api/v1/tags`
- `GET|PUT /api/v1/tags/{tagId}`
- `GET|POST /api/v1/goals`
- `GET|PUT /api/v1/goals/{goalId}`
- `GET /api/v1/threads/{targetType}/{targetId}/tags`
- `PUT|DELETE /api/v1/threads/{targetType}/{targetId}/tags/{tagId}`
- `GET /api/v1/threads/{targetType}/{targetId}/goals`
- `PUT|DELETE /api/v1/threads/{targetType}/{targetId}/goals/{goalId}`

OpenAPI and protobuf contracts carry the same hierarchy, revision, target, provenance, and relationship fields. `threads.Store` is the adapter contract shared by memory and PostgreSQL implementations and reserved for a future DynamoDB adapter. Provider conformance tests cover hierarchy persistence, name conflicts, stale revisions, tag-rule replacement and deletion, and idempotent goal links.

Migration `0012_threads.sql` creates scoped tag and goal hierarchies, explicit tag rules, and goal links. Migration `0013_threads_administrator_permission.sql` grants `goals.write` to existing built-in Administrator bundles without modifying custom roles. PostgreSQL checks constrain supported target and rule types; foreign keys protect hierarchy and tag/goal identities. Feature-owned target IDs remain intentionally outside cross-module foreign keys and are validated through services.

## Audit events

Threads emits:

- `threads.tag.created`
- `threads.tag.updated`
- `threads.tag.rule.set`
- `threads.tag.rule.deleted`
- `threads.goal.created`
- `threads.goal.updated`
- `threads.goal.linked`
- `threads.goal.unlinked`

Audit metadata includes `REQ-THREADS-001`, target type, mode, parent, and revision when relevant. Events contain identifiers and relationship state, not session, CSRF, or provider credentials.

## Accessible workflow

1. Create parent and child tags and choose whether the parent is inherited by child assignments.
2. Create parent and child strategic goals.
3. Select an asset.
4. Review the nested tag hierarchy. Every tag has non-color text for unassigned, explicit, inherited, or suppressed state and identifies its source or direct rule revision.
5. With write access, apply, suppress, or remove an explicit rule to return to inherited behavior.
6. Select and link a goal, or unlink an existing goal.
7. Read announced success and error states before continuing.

The surface uses semantic headings and nested lists, native controls, labeled forms, minimum-height actions, keyboard operation, non-color provenance, responsive grids, and a single-column narrow-width reflow. It does not require motion.

## Issue reporting

Report Threads problems through the application issue link or GitHub issue #5. Include the safe correlation ID, target type and ID, tag or goal ID, and current revision. Do not include session cookies, CSRF values, provider credentials, confidential asset content, or private strategy text.

## Test coverage

- Service validation, cycle prevention, precedence, provenance, target validation, audit, and revision tests.
- Shared memory and PostgreSQL store conformance tests.
- PostgreSQL migration structure and optional real-database integration tests.
- HTTP authentication, permissions, CSRF, hierarchy, cycle, provenance, asset validation, goal-link, conflict, and ownership-lock coverage.
- React hierarchy, provenance, create, suppress, goal-link, permission, status announcement, and axe checks.
- Repository-wide race tests, vet, vulnerability checks, traceability, OpenAPI lint, protobuf validation, typecheck, frontend tests, production build, deployment configuration, and authenticated frontend-proxy browser validation.
