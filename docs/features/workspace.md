# Workspace — Connected work view

- Canonical feature: `experience.workspace`
- Requirement: `REQ-WORKSPACE-001`
- Owning roadmap issue: #33
- Shell and navigation delivery: #34
- Permission-aware panels and scoped access: #35
- Guided person-plus-location delivery: #36
- Reusable related-record workflow pattern: #37

## Purpose

Workspace gives authenticated users one coherent application shell without presenting every product surface in one continuous page. An overview provides the starting context, while persistent navigation opens Atlas, Horizon, Ledger, Threads, Vault, People, or Guard as a focused work area.

## Delivered behavior

- Desktop layouts use a persistent left navigation rail and a bounded work surface. Narrow layouts convert the rail into a horizontally scrollable, touch-sized navigation row before the active content.
- Only the active work area is visible. An area mounts when first opened and remains mounted while hidden so search terms, selected records, open forms, and other in-progress React state survive navigation.
- Each navigation action updates a stable `#workspace-{area}` deep link. Browser history restores the matching area, and invalid hashes safely return to Overview.
- The context header always identifies the current area, signed-in role summary, visible-record boundary, change capability, and live service state.
- Overview reports tracked asset count, the number of product areas available under current grants, service health, and direct entry points to each area.
- Guide links activate the owning work area before focusing its section. Help and issue reporting remain available from both the global header and Workspace navigation.
- People includes a three-step person-plus-location task. Person values remain controlled while the user moves between details, location, and review; an existing visible site, building, or room can be selected, or a user with `directory.write` can create the missing location inline.
- Building and room choices resolve to their containing site for the existing People persistence contract. The exact selected location remains visible during review so the user can confirm the relationship before the person is submitted.
- The person-plus-location task now uses the reusable related-record workflow pattern: preserve and validate the source draft, select or create the related record, return without losing work, and explicitly confirm the relationship. Loading, failure, retry, and cancellation use the same announced states across consumers.

## Reusable related-record workflow pattern

Issue #37 defines a UI composition pattern for relationships among records owned by different StewardMesh features. The pattern owns only temporary in-memory workflow state. It does not own either domain record, add a combined persistence model, or call a repository directly.

- **Preserve and validate:** controlled source values survive forward and backward navigation. Consumer validation identifies the owning step and leaves the draft intact.
- **Create or select:** a readable existing record remains selectable when the caller lacks the related feature's create grant. Creation is an optional, separately authorized path.
- **Return and confirm:** users can return to either earlier step before an explicit final confirmation. Successful completion or cancellation clears the temporary draft.
- **Recover:** asynchronous related-record creation and final confirmation expose announced loading, focused failure, retry of the same preserved operation for transient failures, and cancellation without claiming that a failed write succeeded. Authorization and validation rejections are not blindly retried.
- **Respect ownership:** each consumer declares the source and related feature owner, API path, and required permission. Workspace coordinates calls, while the owning APIs retain CSRF, authorization, organization scope, validation, audit, and ownership-lock enforcement.

The first consumer remains People: `POST /api/v1/identities` owns person creation, while the existing site, building, and room collection APIs own location reads and writes. Both use Guard's existing `directory.read` and `directory.write` checks; Workspace introduces no bypass or replacement endpoint.

## Roles and permissions

Workspace itself requires an authenticated session. It does not grant access or bypass feature authorization.

| Area | Read grant | Change grant |
|---|---|---|
| Atlas | `assets.read` | `assets.write` |
| Horizon | `planning.read` | `planning.write` |
| Ledger | `finance.read` | `finance.write` |
| Threads | `goals.read` | `goals.write` |
| Vault | `storage.read` | `storage.write` |
| People | `directory.read` | `directory.write` |
| Guard | `guard.manage` | `guard.manage` |

The authenticated session returns organization-scoped permission strings plus structured organization, site, department, and resource grant hints. Workspace uses those hints only to compose the interface:

- Organization-wide read and write access is labeled `Read and change` and mounts the owning feature with its normal actions.
- Organization-wide read without its write grant is labeled `Read only`; feature-owned mutation controls remain hidden or disabled.
- Site, department, or resource grants are labeled `Scoped`. Broad collections are not requested or mounted because their current endpoints require organization scope. Direct record requests still require a matching server authorization decision.
- Areas without a read grant remain discoverable with a `Limited` label and an explicit permission explanation. Their protected components are not mounted.

These hints cannot widen access. Every API route continues to authenticate the current server-managed session and check the requested permission against the target organization or record scope.

## State treatment

- Guard session restoration has an announced loading state before Workspace renders.
- First entry into a work area uses a text loading placeholder while the area mounts.
- Feature-owned panels retain their existing empty, loading, success, and error states.
- Loss of service health produces a visible text warning that previously loaded context may be stale and protected operations may fail.
- Permission-limited areas identify the missing grant and direct users to contextual Guide help.
- A location or person validation failure stays on the failing workflow step without clearing person values. Read-only People users receive a direct `directory.write` alternative instead of inactive creation controls.
- Scoped areas explain why organization-wide collections remain closed and identify whether a separate write grant is required.
- A 401 from an authenticated API request clears in-memory principal, grant, CSRF, and record state, announces that the session expired, and returns the user to sign-in.
- Invalid deep links fall back to Overview rather than exposing a blank or broken shell.

## Accessibility

- The global skip link targets the main landmark, Workspace has a labeled navigation landmark, and the active context uses a stable level-two heading.
- Navigation uses ordinary links with real deep-link destinations and `aria-current="page"`; it does not require a custom tab keyboard model.
- Every target is at least 44 CSS pixels high, keyboard focus moves to the current-context heading after navigation, and active, limited, connected, and unavailable states include text instead of relying on color.
- Horizontal navigation remains operable at 320 CSS pixels without forcing document-level overflow.
- Reduced-motion behavior continues to use the application-wide media query.

## Security and privacy

Workspace stores no credentials, grants, record identifiers, or form values in URLs or browser persistence. Its hash contains only a fixed area identifier. Permission and scope hints remain in memory for the current session and are cleared on sign-out or session expiry. Server-managed authorization remains authoritative for every API request.

## APIs, audit, and storage

`GET /api/v1/auth/session` now includes a sorted, deduplicated `grants` collection alongside its existing organization-wide `permissions` hint. Each grant contains only the permission, scope kind, and scope resource ID already assigned to the authenticated principal. The response remains `Cache-Control: no-store`. This delivery adds no database schema, audit event, or persistent browser storage contract.

## Validation

- `internal/httpapi/server_test.go` proves scoped session hints are returned while the organization-wide asset list remains denied.
- `web/src/App.test.tsx` covers authenticated rendering, read/write labels, scoped collection suppression, focused navigation, deep-link updates, session expiry, context preservation, Guide entry, and automated accessibility checks.
- `web/src/PeopleDirectory.test.tsx` covers guided draft retention, existing room selection, inline missing-room creation, containing-site submission, step-specific validation, read-only alternatives, and automated accessibility checks.
- `web/src/RelatedRecordWorkflow.test.tsx` covers preservation, validation, selection-only fallback, return, confirmation, explicit ownership/API boundaries, loading, failure, retry, cancellation, and reset behavior.
- `web/src/WorkspaceShell.test.tsx` covers safe hash parsing and stable deep-link generation.
- `web/src/workspaceAccess.test.ts` covers deterministic organization, scoped, and absent grant classification.
- Browser validation covers desktop and 320-pixel layouts, keyboard navigation, state preservation, service and permission states, console errors, and document-level overflow.

## Follow-up work

Issue #38 owns the broader accessibility and mobile validation pass, and #39 owns final traceability and release evidence for the parent Workspace feature.
