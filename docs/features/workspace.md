# Workspace — Connected work view

- Canonical feature: `experience.workspace`
- Requirement: `REQ-WORKSPACE-001`
- Owning roadmap issue: #33
- Shell and navigation delivery: #34

## Purpose

Workspace gives authenticated users one coherent application shell without presenting every product surface in one continuous page. An overview provides the starting context, while persistent navigation opens Atlas, Horizon, Ledger, Threads, Vault, People, or Guard as a focused work area.

## Delivered behavior

- Desktop layouts use a persistent left navigation rail and a bounded work surface. Narrow layouts convert the rail into a horizontally scrollable, touch-sized navigation row before the active content.
- Only the active work area is visible. An area mounts when first opened and remains mounted while hidden so search terms, selected records, open forms, and other in-progress React state survive navigation.
- Each navigation action updates a stable `#workspace-{area}` deep link. Browser history restores the matching area, and invalid hashes safely return to Overview.
- The context header always identifies the current area, the signed-in role summary, and live service state.
- Overview reports tracked asset count, the number of product areas available under current grants, service health, and direct entry points to each area.
- Guide links activate the owning work area before focusing its section. Help and issue reporting remain available from both the global header and Workspace navigation.

## Roles and permissions

Workspace itself requires an authenticated session. It does not grant access or bypass feature authorization.

| Area | Read or management grant |
|---|---|
| Atlas | `assets.read` |
| Horizon | `planning.read` |
| Ledger | `finance.read` |
| Threads | `goals.read` |
| Vault | `storage.read` |
| People | `directory.read` |
| Guard | `guard.manage` |

Areas without the required grant remain discoverable with a non-color `Limited` label and an explicit permission explanation. Protected components are not mounted for a limited area. Detailed permission-aware panel ordering and administrator remediation remain tracked in #35.

## State treatment

- Guard session restoration has an announced loading state before Workspace renders.
- First entry into a work area uses a text loading placeholder while the area mounts.
- Feature-owned panels retain their existing empty, loading, success, and error states.
- Loss of service health produces a visible text warning that previously loaded context may be stale and protected operations may fail.
- Permission-limited areas identify the missing grant and direct users to contextual Guide help.
- Invalid deep links fall back to Overview rather than exposing a blank or broken shell.

## Accessibility

- The global skip link targets the main landmark, Workspace has a labeled navigation landmark, and the active context uses a stable level-two heading.
- Navigation uses ordinary links with real deep-link destinations and `aria-current="page"`; it does not require a custom tab keyboard model.
- Every target is at least 44 CSS pixels high, keyboard focus moves to the current-context heading after navigation, and active, limited, connected, and unavailable states include text instead of relying on color.
- Horizontal navigation remains operable at 320 CSS pixels without forcing document-level overflow.
- Reduced-motion behavior continues to use the application-wide media query.

## Security and privacy

Workspace stores no credentials, grants, record identifiers, or form values in URLs or browser persistence. Its hash contains only a fixed area identifier. React state remains in memory for the current page lifetime and is cleared on sign-out. Server-managed authorization remains authoritative for every API request.

## APIs, audit, and storage

This shell adds no backend API, database schema, audit event, or persistent storage contract. `WorkspaceShell` exports its bounded component API and fixed deep-link area grammar; it otherwise composes existing feature-owned UI and API boundaries.

## Validation

- `web/src/App.test.tsx` covers authenticated rendering, focused navigation, deep-link updates, context preservation, Guide entry, and automated accessibility checks.
- `web/src/WorkspaceShell.test.tsx` covers safe hash parsing and stable deep-link generation.
- Browser validation covers desktop and 320-pixel layouts, keyboard navigation, state preservation, service and permission states, console errors, and document-level overflow.

## Follow-up work

Issue #35 owns deeper permission-aware panel composition, #36 owns the guided person-to-location workflow, #37 owns reusable related-record workflows, #38 owns the broader accessibility and mobile validation pass, and #39 owns final traceability and release evidence for the parent Workspace feature.
