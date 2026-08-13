# Atlas Codes — Barcode/QR scanning, associations, and label printing

- **Canonical ID:** `inventory.identifiers`
- **Requirement:** `REQ-ATLAS-CODES-001`
- **Phase:** v1
- **Delivery status:** Implemented for v1. Identifier associations and manual management are delivered by [#61](https://github.com/WSCMAX/StewardMesh/issues/61), explicit scanner workflows by [#64](https://github.com/WSCMAX/StewardMesh/issues/64), versioned label preview/printing by [#62](https://github.com/WSCMAX/StewardMesh/issues/62), and the durable release validation by [#63](https://github.com/WSCMAX/StewardMesh/issues/63).
- **GitHub roadmap issue:** [#60 — Atlas Codes](https://github.com/WSCMAX/StewardMesh/issues/60)
- **Owning product area:** [Atlas](atlas.md)

## Purpose

Atlas Codes gives inventory workers a fast, hardware-neutral way to find an asset, associate a physical code with it, and produce a replacement or batch of labels. The first delivery supports one-dimensional Code 128 barcodes and two-dimensional QR codes while preserving a provider boundary for additional symbologies and printer languages.

The durable identifier association belongs to Atlas. Scanner and printer integrations translate device input or output but cannot bypass Atlas authorization, organization scope, identifier normalization, conflict handling, ownership locks, optimistic revisions, or audits.

## Roles and permissions

- Users with `assets.read` can scan or manually enter a code to find an asset they are already allowed to view.
- Users with `assets.write` can associate, replace, or deactivate identifiers on writable assets. Label generation requires both `assets.read` and `assets.write` for every selected asset, including matching resource/site/department scope.
- Imported assets that remain Guard write-locked can be found by an existing code but cannot receive a local association until ownership is claimed.
- A later fine-grained label-print permission may narrow bulk printing without changing the Atlas service boundary.

Guard remains authoritative. A device reporting a successful scan or print never proves that the user can read or change the associated record.

## Identifier association model

An asset can have multiple active or historical identifiers. Each association records a stable ID, organization, asset ID, symbology, normalized encoded value, human-readable value when applicable, source, primary flag, status, revision, creator, and timestamps.

Within an organization, the same active symbology-and-value pair cannot identify two assets. A conflicting scan must show the existing visible association or a safe conflict message; it must never silently move the code. Replacement and deactivation preserve history and require the current revision. Imported, user-entered, and StewardMesh-generated identifiers retain visible provenance.

Generated QR payloads use an opaque identifier or organization-scoped application route. They never embed session material, credentials, authorization grants, private directory values, or confidential asset details. Scanning an otherwise valid code does not reveal an asset outside the caller's authorization scope.

## Scanner workflows

The implemented scanning boundary supports:

- USB and Bluetooth scanners operating as keyboard-wedge/HID input without a vendor SDK;
- permission-gated browser camera scanning on supported desktop and mobile browsers;
- paste and manual entry as a complete fallback when a camera or scanner is absent, denied, disconnected, or inaccessible;
- explicit scan-to-find and scan-to-associate modes so background keystrokes cannot accidentally change an asset;
- duplicate-read suppression, configurable terminators, normalization, timeouts, cancel/retry behavior, and clear unsupported-format feedback.

Code 128 and QR work end to end through the same bounded REST operations as manual management. The scanner surface accepts a configurable Enter or Tab terminator and 250–2,000 millisecond burst window, retains slower input for deliberate manual submission, rejects malformed and oversized input before a request, suppresses a repeated completed scan for 1.5 seconds, retains failed input for retry, and provides an explicit cancellation path. Additional formats require fixtures and compatibility tests before they can be advertised as supported.

## Label generation and printer support

Users with current read and write access to every selected asset can preview one label or a selected batch of up to 50 unique active identifiers that share one symbology. Each label definition is an immutable Patterns `atlas.label-template` schema version. The two built-in versions provide Code 128 and QR defaults, while an active custom version with the same complete physical fields can supply its own geometry and branding. Rendering references the exact Patterns template ID and version; no configuration is inferred from naming conventions. The versioned schema includes physical width/height, margin, quiet-zone size, payload source, mandatory human-readable identifier text, organization branding, and an allowlist limited to asset name and asset tag. Serial numbers, hostnames, deployment notes, directory references, credentials, grants, sessions, and other confidential asset details cannot enter a label.

Code 128-B and QR matrices are rendered as shapes, not font approximations or raster screenshots. The service validates human-readable length, bounded printable ASCII across every renderer, minimum module width, contrast, quiet zones, physical geometry, and overflow before returning output. Unsupported text is rejected consistently rather than silently substituted by PDF or printer firmware. Single-label vector SVG can use the page's print-only stylesheet and standard browser/operating-system dialog; multi-page vector PDF supports viewer-based printing or Save as PDF for common laser, inkjet, and thermal media. Every preview shows exact millimeter dimensions. The default test-print path adds only an outer calibration border, outside symbol quiet zones, and tells the operator to print at 100% scale and measure before producing the batch.

Rendering is separate from printer transport. `SVGLabelRenderer`, `PDFLabelRenderer`, and `ZPLLabelRenderer` consume the same safe label record; `PrinterTransport` accepts only a finished artifact and cannot mutate associations. The current browser transport returns bytes for review. It never discovers, selects, or contacts local or network printers. ZPL is available as an escaped 8-dpmm adapter file with checked-in golden fixtures, but not as an advertised direct-device transport. A printer model must pass the [real-device validation protocol](../validation/atlas-codes-label-printing.md) before it can be added to the supported-device table.

## Identifier APIs and provider boundaries

The implemented association slice exposes the same organization-scoped identifier behavior through memory and PostgreSQL stores, REST/OpenAPI, and the checked-in gRPC contract:

- `POST /api/v1/asset-identifiers/resolve` resolves an active value without placing that value in a URL or routine access log;
- `GET /api/v1/assets/{assetId}/identifiers` lists current and historical associations;
- `POST /api/v1/assets/{assetId}/identifiers` creates an association or returns the existing record for an exact stable-ID or same-intent retry;
- `POST /api/v1/assets/{assetId}/identifiers/{identifierId}/replace` atomically preserves the old association and creates its replacement; and
- `POST /api/v1/assets/{assetId}/identifiers/{identifierId}/deactivate` removes an association from active resolution while retaining history.
- `GET /api/v1/asset-label-templates` lists active Code 128 and QR definitions derived from immutable Patterns versions, along with the 50-item bound and supported artifact formats; and
- `POST /api/v1/asset-label-batches` returns one-label SVG, one-to-50-page PDF, or one-to-50-label escaped ZPL bytes. A required `Idempotency-Key` returns the exact snapshotted bytes for a retained retry, rejects different intent while the digest is retained, and explicitly rejects a replay whose bounded snapshot has expired.

Code 128 input is printable ASCII and limited to 128 bytes. QR input is valid, control-free UTF-8 limited to 512 bytes. Normalization trims surrounding whitespace but preserves case because encoded values can be case-sensitive. PostgreSQL constraints and provider conformance tests enforce organization scope, one active claim per symbology and value, one active primary per asset, and optimistic revisions.

Scanner decoders, label renderers, and printer transports are provider-neutral boundaries. Device SDKs, browser APIs, and printer languages cannot own authoritative associations. Label batch generation reads existing active associations only; it never creates an association. Batches reject duplicate IDs, are limited to 50, honor request cancellation, and keep a byte-bounded artifact cache plus a bounded immutable replay snapshot. A retained exact retry reproduces the same bytes even if an allowed asset label field changes afterward; an expired snapshot returns a conflict and requires a new key. The longer bounded digest ledger continues to reject a retained key reused for different intent. Process restart clears these read-only caches, so delivery always remains behind a fresh explicit operator confirmation and never creates or duplicates an identifier association.

## Accessible walkthrough

1. Open Atlas, choose an asset, and find **Atlas Codes — Identifiers** in its detail panel.
2. Choose **Associate identifier**, select Code 128 or QR, enter the encoded value and optional display value, and choose whether it is primary.
3. Review active and historical values with their format, provenance, primary state, status, and revision shown in text.
4. Choose **Replace** to enter a new value while preserving the previous association in history.
5. Choose **Deactivate**, review the explicit confirmation, and confirm to remove the value from active resolution without deleting history.
6. Choose **Print labels**, select one or more active identifiers that use the same code format, then review the exact template version, dimensions, margins, quiet zone, safe payload behavior, output path, and selected count.
7. Keep **Test print first** enabled, generate the preview, and cancel or retry if necessary. Print the calibration output at 100% scale and measure it against the displayed millimeter dimensions.
8. Review the output and check the explicit operator confirmation. Only then can the page open the browser print dialog, open the PDF viewer, or download the ZPL adapter file. The operator chooses and confirms the printer, media, scale, and quantity outside StewardMesh.
9. Receive announced text status for success, safe retries, cancellation, rendering errors, conflicts, permission denial, ownership locks, validation failures, and stale revisions.

The manual, scanner, and label workflows are keyboard operable, screen-reader labeled, permission aware, and contained at 320 pixels without hiding controls. Selection count, format, dimensions, test-print state, retry/cancellation, and confirmation are conveyed in text rather than color alone. The scanner is inactive until a user opens it and chooses find or associate; association also requires a selected asset and `assets.write`. Browser camera capture is optional and stops on capture, explicit stop, cancellation, or unmount, so paste and manual entry remain complete fallbacks.

## Security, privacy, and audit

Association create, replace, and deactivate actions require Guard `assets.write`, same-origin CSRF protection, organization scope, and an unlocked or locally claimed parent asset. Reads require `assets.read`; resolution rechecks organization, site, department, or resource scope against the matched asset and returns the same not-found response for unknown and unauthorized codes. Audits use `atlas.identifier.created`, `atlas.identifier.replaced`, and `atlas.identifier.deactivated`; safe metadata includes `REQ-ATLAS-CODES-001`, association and asset IDs, symbology, source, status, primary state, and revision. Encoded, normalized, and display values are never audit metadata. The mutation stores its original actor and correlation ID before attempting the audit write. Audit IDs are deterministic, and providers accept only an exact replay, allowing a retry by another administrator to repair a failed audit without changing provenance or duplicating a successful event.

Label generation requires current `assets.read` and `assets.write` grants that cover every selected asset, an unlocked or locally claimed ownership state, same-origin CSRF verification, and a safe idempotency key. Authorization is rechecked before cached bytes can be returned. A read denial is indistinguishable from an unknown identifier so the endpoint cannot discover hidden assets. QR label payloads use `/atlas/codes/{opaqueIdentifierId}`; resolution applies the authenticated organization boundary and reauthorizes the matched asset. Raw QR values and confidential asset fields therefore do not enter generated QR output. Code 128 labels deliberately encode the existing selected identifier value and also render the validated display text. Label generation is read-only and creates no mutation audit; association mutations remain independently audited.

Camera access is requested only while the scanner surface is active, with an obvious stop action. Frames remain local to decoding and are not uploaded or retained. The browser label transport accepts same-origin artifact bytes only. Network printer endpoints and credentials, if a future transport requires them, use guarded configuration and are never exposed to the browser or label payload. ZPL escapes `^`, `~`, `_`, controls, and non-ASCII bytes before inserting data into printer-language fields.

## Roadmap breakdown and validation

The v1 delivery is split into four implementation slices:

1. [#61](https://github.com/WSCMAX/StewardMesh/issues/61) — Identifier associations, normalization, conflicts, provenance, history, provider contracts, APIs, and audits.
2. [#64](https://github.com/WSCMAX/StewardMesh/issues/64) — Keyboard-wedge, camera, paste, and manual scan-to-find/associate workflows (implemented).
3. [#62](https://github.com/WSCMAX/StewardMesh/issues/62) — Versioned label templates, preview, batch generation, standard printing, and provider-neutral thermal-printer output (implemented).
4. [#63](https://github.com/WSCMAX/StewardMesh/issues/63) — Security, accessibility, hardware/browser compatibility, documentation, traceability, and end-to-end validation (implemented; see the [release validation record](../validation/atlas-codes-release.md)).

Validation must cover memory and PostgreSQL conformance, concurrent and stale associations, organization isolation, ownership locks, malformed and oversized input, duplicate bursts, camera permission states, scanner disconnects, keyboard timing, Code 128 and QR fixtures, print dimensions, batch bounds, PDF/vector output, any supported printer-language adapter, WCAG 2.2 AA, reduced motion, and 320-pixel layouts.

The traceability manifest links `REQ-ATLAS-CODES-001` to association, scanning, label rendering/transport, UI, API contracts, memory/PostgreSQL providers, fixtures, migrations, tests, and the complete [security, compatibility, accessibility, and end-to-end validation record](../validation/atlas-codes-release.md).

## Test coverage

- Service tests cover case-sensitive normalization, format and size bounds, asset references, stable and same-intent retries, optimistic revisions, audit redaction, deterministic audit repair after transient persistence failure, Code 128/QR structures and quiet zones, physical overflow, safe QR routes, vector SVG/PDF framing, ZPL escaping and golden fixtures, batch bounds, cancellation, and exact idempotent artifacts.
- Shared memory and PostgreSQL conformance covers organization isolation, active uniqueness, primary conflicts, replacement and deactivation history, stale writes, and concurrent claims.
- HTTP tests cover read/write permissions, resource-scoped resolution and label generation with uniform unauthorized/unknown responses, write-only and read-only denials, per-asset batch authorization, CSRF, ownership locks, bounded strict JSON, safe conflict responses, association lifecycle operations, versioned label metadata, physical artifact headers, vector PDF/SVG bytes, exact retries, and the invariant that label generation does not add associations.
- React tests cover read-only history, manual association, replacement, explicit deactivation confirmation, Code 128 keyboard-wedge find, QR association, configurable explicit modes, duplicate suppression, malformed input, camera fallback, label selection/preview, mixed-format prevention, test printing, explicit operator confirmation, no silent printer action, safe retry/cancellation, ZPL download-only behavior, CSRF headers, status announcements, and automated accessibility checks.
- Repository validation includes race tests, vet, vulnerability analysis, traceability, OpenAPI lint, protobuf descriptor compilation, type checking, production build, and authenticated desktop and 320-pixel browser checks.

## Issue reporting

Report Atlas Codes problems through the application issue link or [GitHub roadmap issue #60](https://github.com/WSCMAX/StewardMesh/issues/60). Include a safe correlation ID, browser/device class, declared symbology, input path, label template and dimensions, and output format when relevant. Do not include raw codes, camera frames, asset details, printer addresses, credentials, session cookies, or CSRF values.
