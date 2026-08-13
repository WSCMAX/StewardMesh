# Atlas Codes v1 release validation

- **Requirement:** `REQ-ATLAS-CODES-001`
- **Feature:** `inventory.identifiers`
- **Roadmap:** [#60](https://github.com/WSCMAX/StewardMesh/issues/60)
- **Cross-cutting closeout:** [#63](https://github.com/WSCMAX/StewardMesh/issues/63)
- **Validated:** 2026-08-13

This record closes the security, accessibility, compatibility, and traceability
slice for Atlas Codes. It records what StewardMesh actually supports. A browser,
scanner, printer language, or physical device is not advertised merely because
an extension point exists.

## Delivered boundary

Atlas owns authoritative, organization-scoped Code 128 and QR associations.
The v1 user surface supports manual entry, paste, USB/Bluetooth
keyboard-wedge input, optional in-browser camera decoding, accessible
single/batch label previews, and operator-confirmed SVG or PDF output. ZPL is a
renderer export with immutable fixtures; StewardMesh does not silently discover,
select, or write to a printer.

Durable associations, history, provenance, conflicts, revisions, and audits
live in the configured repository. Browser/device state is transient and never
owns an association. A scan or successful render is input to an authorized
server operation, not proof of access.

## Security threat review

The review followed the repository's Go `net/http` and React/TypeScript secure
coding baseline. No raw-HTML insertion, string-to-code execution, arbitrary
outbound URL, shell, filesystem, or dynamic SQL sink exists in the Atlas Codes
path. React renders server values through escaped JSX, and PostgreSQL values are
passed as query parameters; the only SQL concatenation in the adapter joins a
compile-time column-list constant.

| Threat | Control and evidence |
|---|---|
| Untrusted code payloads | The service and UI accept only Code 128 printable ASCII up to 128 bytes or control-free printable QR UTF-8 up to 512 bytes. Strict JSON decoding rejects trailing or unknown fields, and request bodies are capped. Unsupported symbologies and malformed or oversized values fail before persistence. |
| Cross-organization or scoped-record discovery | Every repository lookup includes the organization ID. Resolution reloads and authorizes the matched asset, including resource/site/department scope, and returns the same not-found response for unknown and unauthorized values. Denied tests assert that names, tags, serials, and encoded values are absent. |
| Camera lifetime and frame retention | Camera permission is requested only from the open scanner surface. Every captured stream is stopped on capture, explicit stop, cancellation, generation change, or component unmount. Frames are decoded locally and are neither uploaded nor stored. Manual entry and paste remain complete fallbacks. |
| Keyboard injection and unintended mutation | Scanner input is ignored while the scanner surface is closed. The operator explicitly selects find or associate mode; association additionally requires a selected asset and server-enforced `assets.write`. Terminators and 250–2,000 ms burst windows are bounded, slow input is retained for deliberate submission, duplicate completed bursts are suppressed, and cancellation changes no record. |
| Ownership-lock bypass | All association, replacement, deactivation, and label-generation mutations pass Guard permission and resource-write checks. Imported assets remain write-locked until ownership is explicitly claimed. Read-only resolution does not weaken the write boundary. |
| Encoded-data or secret leakage | Codes are posted in bounded bodies rather than URL paths or query strings. Audit metadata includes IDs, symbology, source, status, primary state, and revision but never encoded/display values. QR generation permits only an opaque identifier or organization-scoped application route. Labels exclude credentials, session/CSRF material, grants, private directory values, and confidential asset fields. |
| Printer endpoints and credentials | v1 produces browser-downloadable/printable artifacts only. There is no direct network-printer transport, endpoint entry, token passthrough, background discovery, or implicit print call. ZPL metacharacters are escaped and fixtures are byte compared. Any future transport must keep credentials server-side and repeat this review. |
| Resource exhaustion and duplicate work | HTTP bodies, text fields, template dimensions, label counts, artifact bytes, cache size, replay lifetime, rendering time, and repository operations are bounded. Per-principal operation windows limit resolution, reads, mutations, and label generation before body decoding; only digested organization/subject/operation keys are retained, and a redacted `429` includes `Retry-After`. Batch generation is cancellable. Idempotency keys and exact snapshot replay prevent duplicate association work while authorization is rechecked before artifact access. |
| Cross-site request and browser exfiltration | Cookie-authenticated writes require same-origin CSRF tokens. Frontend requests use fixed same-origin API paths, never user-controlled destinations. Sensitive responses use `no-store`; downloadable artifacts expose only allowlisted CORS headers. |

The active review found no critical or high-severity Atlas Codes vulnerability.
Dependency analysis is separately enforced by `govulncheck`, `npm audit`, the
lockfile-based CI install, and the repository filesystem scan.

## Compatibility matrix

“Supported” below means the workflow has an explicit implementation and safe
fallback. “Automated” means repository tests exercise the behavior without
claiming a specific physical model.

| Surface | Supported/tested v1 behavior | Evidence and limitations |
|---|---|---|
| Chromium desktop | Manual, paste, keyboard wedge, native `BarcodeDetector` camera when exposed by the browser, SVG/PDF preview and OS print/download | Automated component tests plus real-browser desktop validation. Controlled detector/media fixtures exercise successful Code 128 find, successful QR association, CSRF, and capture shutdown; live camera quality remains device-dependent. |
| Chromium narrow layout | Manual, paste, camera when `BarcodeDetector` is available, responsive preview | Real Chromium validation at 320 CSS pixels; no page-level horizontal overflow. Mobile hardware is covered by the same responsive contract but was not physically certified. |
| Firefox desktop/mobile | Manual, paste, USB/Bluetooth keyboard wedge, SVG/PDF output | Camera decoding is not advertised without a compatible `BarcodeDetector`; the UI announces this and retains all non-camera paths. |
| Safari desktop/mobile | Manual, paste, USB/Bluetooth keyboard wedge, SVG/PDF output | Camera decoding is not advertised without a compatible `BarcodeDetector`; the UI announces this and retains all non-camera paths. |
| Scanner hardware class | Any USB or Bluetooth HID keyboard-wedge scanner that emits Code 128/QR decoded text and Enter or Tab | No vendor SDK is required. Configurable burst timing and explicit modes are tested. Device-specific prefixes/suffixes beyond Enter/Tab are not advertised. |
| Camera hardware class | Browser-visible rear/front camera in a secure context with `getUserMedia` and `BarcodeDetector` | Permission denial, missing API, decode failure, stop, cancel, and unmount are tested. Frames are local only. |
| Symbologies | Code 128-B and QR | Validation, normalized resolution, rendering, quiet zones, and fixtures are covered. Other formats fail safely. |
| Standard output | One-page vector SVG and one-label-per-page vector PDF | Exact physical dimensions are embedded and shown before confirmation. Browser/OS scaling and media calibration remain operator responsibilities. |
| Printer language | ZPL at 8 or 12 dpmm as an exported renderer | Golden Code 128 and QR fixtures and escaping are automated. No direct transport or physical Zebra-compatible model is advertised until the qualification record in `atlas-codes-label-printing.md` is completed. |
| Printer hardware class | Laser, inkjet, or thermal device reachable through the browser/OS print path | StewardMesh never selects the device or bypasses the operating-system dialog. Physical stock, DPI, darkness, speed, and calibration are local concerns. |

## Accessibility validation

- Scanner and label surfaces are fully operable with keyboard controls and do
  not depend on camera, pointer, or vendor hardware.
- Modes, busy states, success, conflict, permission denial, cancellation,
  camera state, and print confirmation are conveyed in text and announced by
  status/alert semantics; color is supplementary.
- Inputs and controls have programmatic labels, grouped fieldsets, visible
  focus treatment, and logical DOM/focus order. Destructive/deactivation and
  print actions require explicit confirmation.
- Narrow-width validation covers populated, empty, error, denied, scanning,
  preview, and batch states at 320 CSS pixels without page-level horizontal
  overflow. Wide record tables/previews use contained scrolling.
- The UI does not introduce animation that overrides `prefers-reduced-motion`;
  camera preview is live content only while the operator explicitly enables it.
- Automated axe checks reported zero violations against the automated rules
  available for the documented WCAG 2.2 AA baseline in the final authenticated
  Chromium workflows. Keyboard-only walkthroughs covered open,
  mode selection, input, retry/cancel, selection, preview, and confirmation.

## End-to-end scenario coverage

| Scenario | Durable evidence |
|---|---|
| Scan-to-find and scoped denial | `web/src/AtlasScanner.test.tsx`, `internal/httpapi/server_test.go` |
| Scan-to-associate and explicit mode | `web/src/AtlasScanner.test.tsx`, `web/src/AtlasInventory.test.tsx` |
| Duplicate bursts, malformed values, retry, cancellation, successful Code 128/QR camera capture, camera denial/disconnect | `web/src/AtlasScanner.test.tsx` |
| Association conflicts, stable retries, replacement/history, ownership locks, audit redaction | `internal/atlascodes/service_test.go`, `internal/repository/contracttest/atlascodes.go`, `internal/httpapi/server_test.go`, `web/src/AtlasIdentifiers.test.tsx` |
| Single and bounded batch labels, test print, safe QR route, rendering errors, cancellation, exact retry | `internal/atlascodes/labels_test.go`, `internal/httpapi/server_test.go`, `web/src/AtlasLabelPrint.test.tsx` |
| Memory/PostgreSQL organization isolation and concurrency | `internal/repository/memory_atlascodes_test.go`, `internal/repository/postgres/postgres_integration_test.go` |
| API and gRPC contract parity | `api/openapi/openapi.yaml`, `api/proto/stewardmesh.proto`, protobuf descriptor compilation, HTTP contract tests |
| Desktop, 320-pixel, accessibility, console, and print isolation | Authenticated Playwright release walkthrough recorded by this validation and the label-printing protocol |

## Release gates

The integrated branch must pass all of the following after the label-printing
slice is merged. Counts are intentionally not used as the success criterion;
the complete current suites must pass.

```text
go test -race ./...
go vet ./...
go tool govulncheck ./...
go run ./cmd/tracecheck
protoc --proto_path=api/proto --descriptor_set_out=<temporary-file> api/proto/stewardmesh.proto
npm audit --audit-level=high
npm run openapi:lint
npm run typecheck
npm test
npm run build
docker compose -f deploy/docker-compose.yml config --quiet
docker compose -f deploy/docker-compose.yml --profile cache config --quiet
docker build -f deploy/Dockerfile .
```

PostgreSQL provider tests run against a clean database so all migrations,
constraints, and concurrent association/label reads are exercised. The browser
walkthrough uses the real Go API through the Vite proxy, a real authenticated
Guard session, and real PostgreSQL persistence rather than mocked browser
responses.

## Operator reporting and residual limits

When reporting a problem, include a safe correlation ID, browser and hardware
class, selected symbology/input path, template/version, physical dimensions,
output format, and whether the operation was a test print. Never attach a raw
code, camera frame, private asset or directory field, printer address or
credential, session cookie, CSRF token, or downloaded artifact containing
production data.

Residual v1 limits are explicit: there is no RFID support, silent/background
scanning, arbitrary printer transport, physical-device certification, or
guaranteed native camera decoder in every browser. These limits preserve the
manual, paste, keyboard, and standard print fallbacks and do not weaken the
authoritative server boundary.
