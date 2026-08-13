# Atlas Codes — Barcode/QR scanning, associations, and label printing

- **Canonical ID:** `inventory.identifiers`
- **Requirement:** `REQ-ATLAS-CODES-001`
- **Phase:** v1
- **Delivery status:** Identifier associations and manual management are implemented by [#61](https://github.com/WSCMAX/StewardMesh/issues/61), and explicit scanner workflows are implemented by [#64](https://github.com/WSCMAX/StewardMesh/issues/64); label printing and final validation remain planned.
- **GitHub roadmap issue:** [#60 — Atlas Codes](https://github.com/WSCMAX/StewardMesh/issues/60)
- **Owning product area:** [Atlas](atlas.md)

## Purpose

Atlas Codes gives inventory workers a fast, hardware-neutral way to find an asset, associate a physical code with it, and produce a replacement or batch of labels. The first delivery supports one-dimensional Code 128 barcodes and two-dimensional QR codes while preserving a provider boundary for additional symbologies and printer languages.

The durable identifier association belongs to Atlas. Scanner and printer integrations translate device input or output but cannot bypass Atlas authorization, organization scope, identifier normalization, conflict handling, ownership locks, optimistic revisions, or audits.

## Roles and permissions

- Users with `assets.read` can scan or manually enter a code to find an asset they are already allowed to view.
- Users with `assets.write` can associate, replace, deactivate, or generate identifiers and labels for writable assets.
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

Users can preview and print one label or a selected batch. Versioned label templates define physical dimensions, margins, encoded and human-readable content, organization branding, and optional safe asset fields. Output must remain legible at the selected size and include text so a damaged or inaccessible code does not make the asset unidentifiable.

The initial printer path uses print-ready vector/PDF output and the standard browser/operating-system print dialog, covering common laser, inkjet, and thermal label printers without silent local device access. Rendering and transport remain separate so a tested thermal-printer language adapter, beginning with ZPL-compatible output, can be added without changing identifier associations or templates. Printer selection, calibration, density, and media handling remain local operator concerns; StewardMesh must show dimensions and a test-print workflow.

## Identifier APIs and provider boundaries

The implemented association slice exposes the same organization-scoped identifier behavior through memory and PostgreSQL stores, REST/OpenAPI, and the checked-in gRPC contract:

- `POST /api/v1/asset-identifiers/resolve` resolves an active value without placing that value in a URL or routine access log;
- `GET /api/v1/assets/{assetId}/identifiers` lists current and historical associations;
- `POST /api/v1/assets/{assetId}/identifiers` creates an association or returns the existing record for an exact stable-ID or same-intent retry;
- `POST /api/v1/assets/{assetId}/identifiers/{identifierId}/replace` atomically preserves the old association and creates its replacement; and
- `POST /api/v1/assets/{assetId}/identifiers/{identifierId}/deactivate` removes an association from active resolution while retaining history.

Code 128 input is printable ASCII and limited to 128 bytes. QR input is valid, control-free UTF-8 limited to 512 bytes. Normalization trims surrounding whitespace but preserves case because encoded values can be case-sensitive. PostgreSQL constraints and provider conformance tests enforce organization scope, one active claim per symbology and value, one active primary per asset, and optimistic revisions.

Scanner decoders, label renderers, and printer transports remain planned provider-neutral adapters. Device SDKs, browser APIs, and printer languages cannot own authoritative associations. Future bulk jobs must be bounded, cancellable, and safe to retry without generating duplicate associations.

## Accessible walkthrough

1. Open Atlas, choose an asset, and find **Atlas Codes — Identifiers** in its detail panel.
2. Choose **Associate identifier**, select Code 128 or QR, enter the encoded value and optional display value, and choose whether it is primary.
3. Review active and historical values with their format, provenance, primary state, status, and revision shown in text.
4. Choose **Replace** to enter a new value while preserving the previous association in history.
5. Choose **Deactivate**, review the explicit confirmation, and confirm to remove the value from active resolution without deleting history.
6. Receive announced text status for success, safe retries, conflicts, permission denial, ownership locks, validation failures, and stale revisions.

The manual and scanner workflows are keyboard operable, screen-reader labeled, permission aware, and usable at narrow widths. The scanner is inactive until a user opens it and chooses find or associate; association also requires a selected asset and `assets.write`. Browser camera capture is optional and stops on capture, explicit stop, cancellation, or unmount, so paste and manual entry remain complete fallbacks. Label preview and printing remain tracked in [#62](https://github.com/WSCMAX/StewardMesh/issues/62).

## Security, privacy, and audit

Association create, replace, and deactivate actions require Guard `assets.write`, same-origin CSRF protection, organization scope, and an unlocked or locally claimed parent asset. Reads require `assets.read`; resolution rechecks organization, site, department, or resource scope against the matched asset and returns the same not-found response for unknown and unauthorized codes. Audits use `atlas.identifier.created`, `atlas.identifier.replaced`, and `atlas.identifier.deactivated`; safe metadata includes `REQ-ATLAS-CODES-001`, association and asset IDs, symbology, source, status, primary state, and revision. Encoded, normalized, and display values are never audit metadata. The mutation stores its original actor and correlation ID before attempting the audit write. Audit IDs are deterministic, and providers accept only an exact replay, allowing a retry by another administrator to repair a failed audit without changing provenance or duplicating a successful event.

Camera access is requested only while the scanner surface is active, with an obvious stop action. Frames remain local to decoding and are not uploaded or retained. Network printer endpoints and credentials, if a future transport requires them, use guarded configuration and are never exposed to the browser or label payload.

## Roadmap breakdown and validation

The v1 delivery is split into four implementation slices:

1. [#61](https://github.com/WSCMAX/StewardMesh/issues/61) — Identifier associations, normalization, conflicts, provenance, history, provider contracts, APIs, and audits.
2. [#64](https://github.com/WSCMAX/StewardMesh/issues/64) — Keyboard-wedge, camera, paste, and manual scan-to-find/associate workflows (implemented).
3. [#62](https://github.com/WSCMAX/StewardMesh/issues/62) — Versioned label templates, preview, batch generation, standard printing, and provider-neutral thermal-printer output.
4. [#63](https://github.com/WSCMAX/StewardMesh/issues/63) — Security, accessibility, hardware/browser compatibility, documentation, traceability, and end-to-end validation.

Validation must cover memory and PostgreSQL conformance, concurrent and stale associations, organization isolation, ownership locks, malformed and oversized input, duplicate bursts, camera permission states, scanner disconnects, keyboard timing, Code 128 and QR fixtures, print dimensions, batch bounds, PDF/vector output, any supported printer-language adapter, WCAG 2.2 AA, reduced motion, and 320-pixel layouts.

The traceability manifest links `REQ-ATLAS-CODES-001` to the implemented association and scanning UI, APIs, domain service, memory and PostgreSQL providers, migrations, and tests. It does not mark the parent Atlas Codes roadmap complete: label generation and printing remain in #62, followed by final compatibility and end-to-end validation in #63.

## Test coverage

- Service tests cover case-sensitive normalization, format and size bounds, asset references, stable and same-intent retries, optimistic revisions, audit redaction, and deterministic audit repair after transient persistence failure.
- Shared memory and PostgreSQL conformance covers organization isolation, active uniqueness, primary conflicts, replacement and deactivation history, stale writes, and concurrent claims.
- HTTP tests cover read/write permissions, resource-scoped resolution with uniform unauthorized/unknown responses, CSRF, ownership locks, bounded strict JSON, safe conflict responses, and association lifecycle operations.
- React tests cover read-only history, manual association, replacement, explicit deactivation confirmation, Code 128 keyboard-wedge find, QR association, configurable explicit modes, duplicate suppression, malformed input, camera fallback, retry/cancellation, CSRF headers, status announcements, and automated accessibility checks.
- Repository validation includes race tests, vet, vulnerability analysis, traceability, OpenAPI lint, protobuf descriptor compilation, type checking, production build, and authenticated desktop and 320-pixel browser checks.

## Issue reporting

Report Atlas Codes problems through the application issue link or [GitHub roadmap issue #60](https://github.com/WSCMAX/StewardMesh/issues/60). Include a safe correlation ID, browser/device class, declared symbology, input path, label template and dimensions, and output format when relevant. Do not include raw codes, camera frames, asset details, printer addresses, credentials, session cookies, or CSRF values.
