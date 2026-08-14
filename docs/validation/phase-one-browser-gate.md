# Phase-one browser gate

- **Requirements:** `REQ-WORKSPACE-001`, `REQ-ATLAS-CODES-001`, `A11Y-001`, `DOC-001`, `DOC-002`
- **Features:** `experience.workspace`, `experience.help`, `inventory.identifiers`, `authorization.security`
- **Issues:** #38, #39, #60, #62, #63, #64

## Reproducible command

The required Chromium gate is tracked in `scripts/e2e/` and runs through the
CLI-only `@playwright/cli` interface. It deliberately does not introduce the
`@playwright/test` runner. The exact CLI version is locked in
`web/package-lock.json`.

Prerequisites are Go 1.26.5, Node 24.15.0 or newer, and a local PostgreSQL
server with a disposable maintenance login. From a clean
checkout:

```sh
cd web
npm ci
./node_modules/.bin/playwright-cli install-browser chromium
npm run e2e:phase-one
```

The defaults use PostgreSQL on loopback with the repository's development
credentials. Override them only with URL-encoded loopback credentials:

```sh
export STEWARDMESH_E2E_POSTGRES_ADMIN_URL='postgres://user:password@127.0.0.1:5432/postgres?sslmode=disable'
export STEWARDMESH_E2E_DATABASE_URL_PREFIX='postgres://user:password@127.0.0.1:5432/'
```

The runner creates a uniquely named `stewardmesh_e2e_*` database, builds the Go
server and fixture helper into a temporary directory, starts an HTTP-only
loopback backend and Vite proxy, bootstraps the administrator through the real
browser UI, and provisions one second local account. The administrator then
creates and assigns that account's read-only role through Guard's real API and
CSRF boundary. Exact process IDs, the temporary directory, browser sessions,
and the disposable database are cleaned on success, failure, or interruption.

Both the helper and runner fail closed unless all of these are true:

- the organization ID starts with `e2e-`;
- the target database starts with `stewardmesh_e2e_`;
- the database and maintenance URLs use `localhost` or a loopback IP;
- the database and maintenance URLs use the same server and credentials;
- the maintenance URL names the `postgres` database; and
- the runner supplies the explicit disposable-fixture acknowledgement.

The fixed E2E passwords are public test data scoped to the disposable database;
they are not deployment credentials. The runner never saves Playwright storage
state, cookies, CSRF values, request bodies, traces, or network logs. Each run
uses an isolated `run-*` diagnostics subdirectory so stale local output cannot
be mistaken for the current result. CI uploads only non-secret process and
scenario diagnostics from the ignored `output/playwright/phase-one-gate/`
directory.

## Automated acceptance coverage

| Scenario | Durable browser checks |
| --- | --- |
| Administrator bootstrap | Fresh PostgreSQL installation requires bootstrap; the browser creates the initial administrator; setup becomes unavailable afterward. |
| Workspace administrator | Atlas-to-People navigation preserves the Atlas filter; keyboard focus follows People steps, validation, recoverable failure, retry, cancellation, and completion; inline site creation persists while a canceled person draft does not; existing-location selection completes a real person write; Guide's People docs link remains same-origin; issue-report context omits names, email, roles, drafts, search, query/hash, session, and CSRF values. |
| Guard and real reader | The administrator creates a direct `organization.read`, `assets.read`, and `directory.read` role and assigns it at organization scope. A separate browser logs in as that account, sees real readable Atlas/People data, has no People, asset, association, or label write controls, receives a real generic `403 permission_denied` for a direct write attempt, and receives the same generic not-found response for an unknown code. |
| Scanner and history | Two real assets receive Code 128 and QR identifiers through explicit associate mode. Enter and Tab terminators, the bounded burst window, retained manual fallback, duplicate suppression, closed/background input, malformed/oversized values, find success, unknown retry/cancel, cross-asset conflict redaction, replacement history, and deactivation history are asserted. |
| Camera lifecycle | A controlled in-browser `MediaStream` and `BarcodeDetector` verify that permission is requested only after **Use camera**, the rear-facing video/no-audio constraints are used, a successful capture stops tracks, no frame or data URL reaches an API request, and denied or unavailable camera paths keep keyboard/paste/manual fallback visible. |
| Labels | A real 70 × 30 mm Code 128 SVG test print and two-page PDF batch validate artifact media type, physical headers, page count, redaction, and explicit operator confirmation. A real 50 × 30 mm QR SVG uses the immutable `organization_route` template. ZPL is not advertised. Cancellation discards a delayed response and leaves no output enabled; retry preserves the idempotency key; exact regeneration reports a safe replay. A blocked PDF viewer path leaves accessible retry guidance. |
| Accessibility and narrow layout | `axe-core` reports zero violations on empty, populated, Guide, Guard, denied-reader, and Atlas Codes states. Reduced motion is active. The 320 × 900 reader and Atlas Codes views satisfy `scrollWidth <= clientWidth`; the mobile Workspace dialog traps focus, closes on Escape, and restores focus to its launcher. Unexpected console warnings, console errors, and page errors fail every scenario. |

The controlled HTTP failures are limited to recoverable People and label error
states. All successful writes, reader authorization, identifier conflicts,
history, and artifact generation still execute through the real PostgreSQL-
backed application.

## CI contract

The `browser` CI job uses PostgreSQL 18.4, Go 1.26.5, Node 24.15.0, `npm ci`,
the exact locked Playwright CLI, and its pinned Chromium download. It executes
the same `npm run e2e:phase-one` command documented above and uploads bounded
diagnostics with `if: always()`.

## External qualification that remains manual

Chromium automation cannot certify physical or assistive hardware. Release
qualification still records these separately:

- real USB/Bluetooth keyboard-wedge timing, configured prefixes/suffixes, and
  device reconnect behavior;
- real camera permission UI, focus/quality/lens behavior, and secure-context
  behavior on supported mobile Chromium devices;
- VoiceOver, NVDA, or JAWS announcement order and human reading order;
- physical printer stock, DPI/darkness, 100% scaling, measured dimensions,
  quiet-zone scanning, and browser/OS print dialogs; and
- periodic Firefox and Safari checks for manual, paste, keyboard-wedge, SVG,
  and PDF paths.

StewardMesh does not advertise ZPL output, so no ZPL printer qualification is a
phase-one release condition. Server-side organization isolation, ownership
locks, CSRF, rate limits, audit redaction, concurrent idempotency, and renderer
byte determinism remain authoritative Go/PostgreSQL integration-test gates;
the browser job spot-checks their user-visible boundaries rather than replacing
those tests.
