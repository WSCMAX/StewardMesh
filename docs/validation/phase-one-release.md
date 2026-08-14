# Integrated phase-one release validation

- **Prepared:** 2026-08-13
- **Branch:** `agent/phase-one-completion`
- **Final result:** **LOCAL RELEASE GATES PASSED; PULL REQUEST AND CI PENDING**
- **Release rule:** no pass is recorded until every required gate succeeds on
  the final source tree and the validated source commit is identified below

This is the authoritative integrated release record for the single phase-one
pull request. Feature-specific validation documents define deeper protocols and
retain useful slice history, but an older feature run does not substitute for a
final rerun after all changes are integrated.

## Pull-request scope

The combined pull request must close every phase-one issue included in this
delivery. The issue list is fixed here so the final pull-request body can carry
one explicit `Closes #...` line for each item.

| Area | Included issues | Primary requirements and evidence |
|---|---|---|
| Stack | #7 | `REQ-STACK-001`; `docs/features/stack.md` |
| Patterns | #8 | `REQ-PATTERNS-001`; `docs/features/patterns.md` |
| Exchange | #9 | `REQ-EXCHANGE-001`; `exchange-release.md`, `exchange-security-review.md` |
| Signals | #11 | `REQ-SIGNALS-001`; `docs/features/signals.md` |
| Reach | #12 | `REQ-REACH-001`; `docs/features/reach.md`; migrations `0033` and `0036` |
| Bridge and gRPC runtime parity | #14 | `REQ-API-001`, `SEC-MCP-001`; `grpc-runtime-parity.md`, `bridge-security-review.md` |
| Directory Expansion | #25, #26, #27, #28, #29, #30, #31, #32 | `REQ-DIRECTORY-EXPANSION-002` through `REQ-DIRECTORY-EXPANSION-009`; `directory-expansion-release.md` |
| Workspace | #33, #37, #38, #39 | `REQ-WORKSPACE-001`; `docs/features/workspace.md` |
| Atlas Codes | #60, #62, #63, #64 | `REQ-ATLAS-CODES-001`; `atlas-codes-release.md`, `atlas-codes-label-printing.md` |
| Atlas Models | #68, #75 | `REQ-ATLAS-MODELS-001`; `docs/features/atlas-models.md` |

Issue #65 (RFID) is outside phase one and must not be closed by this pull
request.

## Immutable release context

- **Validated implementation source commit SHA:**
  `e9ae485fefa99d55af9f406089ba640b79ae3b81`
- **Final pull-request head SHA:** record in the pull request and handoff; a
  commit cannot contain its own SHA
- **Signed/DCO verification:** all 61 implementation commits in
  `origin/main..e9ae485` had a good signature from the configured Max Lemke key
  and a `Signed-off-by: Max Lemke <lemke.max@gmail.com>` trailer. Reverify the
  final range after committing this release record.
- **Test date and timezone:** 2026-08-13, CDT (UTC-05:00)
- **Go version:** Go 1.26.5, darwin/arm64
- **Node/npm versions:** Node 24.15.0 and npm 10.1.0 in the final `web/` gate
  shell
- **Container engine/context:** Docker client 28.5.1, server 28.4.0, Colima
- **PostgreSQL database:** PostgreSQL 18.4 in an existing task-owned container;
  fresh disposable databases named with `phase1_final` and `phase1_container`
  prefixes on loopback only. Credentials and complete URLs are omitted.
- **Schema ceiling:** `0036_reach_delivery_claims.sql`
- **Browser and viewport:** Chromium 151.0.7922.109; 1440 × 1000 desktop and
  exact 320 × 900 CSS-pixel mobile viewports

## Final command gates

Every row below must be updated with the exact final outcome. A prior checkpoint,
focused test, skipped command, unavailable dependency, or expected CI rerun is
not a pass.

| Gate | Final result | Evidence |
|---|---|---|
| Formatting and generated-file reproducibility | **PASS** | `git diff --check`; repository-wide `gofmt -l` returned no paths; `go mod verify` reported all modules verified. Pinned protobuf regeneration matched both checked-in Go bindings byte for byte. |
| `go test -race ./...` with disposable PostgreSQL | **PASS** | Every package passed against a fresh loopback PostgreSQL 18.4 database after migrations through `0036`; no race was reported. |
| `go vet ./...` | **PASS** | No diagnostics. |
| `govulncheck ./...` | **PASS** | govulncheck 1.6.0, database updated 2026-08-11: zero called vulnerabilities; one required-module finding was not reachable from this code. |
| `go run ./cmd/tracecheck` | **PASS** | `requirement traceability verified`. |
| Protobuf descriptor and checked-in binding validation | **PASS** | protoc 3.20.3 compiled the descriptor; pinned `protoc-gen-go` 1.36.12 and `protoc-gen-go-grpc` 1.6.2 reproduced both bindings exactly. |
| OpenAPI lint | **PASS** | Redocly 2.46.0 validated the contract. Five non-blocking `operation-2xx-response` warnings are the intentional redirect-only 303 OAuth/OIDC/SAML operations. |
| Node 24 TypeScript typecheck | **PASS** | `tsc -b --pretty false` produced no diagnostics. |
| Complete Vitest suite | **PASS** | Vitest 4.1.10: 30 files and 168 tests passed; none skipped. |
| Production web build | **PASS** | Vite 8.2.1 built 47 modules. The 771.91 kB minified application chunk produced a non-blocking code-splitting performance advisory. |
| `npm audit --audit-level=high` | **PASS** | Zero vulnerabilities after a clean `npm ci` of 186 packages. |
| Default/cache/demo/integrations Compose validation | **PASS** | All four `docker compose ... config --quiet` variants exited successfully. |
| Application and Grouper fixture image builds | **PASS** | Built `stewardmesh:phase1-release` and `stewardmesh-grouper-fixture:phase1-release`; both distroless images declare `nonroot:nonroot` and the expected entrypoint. |
| Container smoke and persistence restart | **PASS** | Application and fixture ran with read-only root filesystems and bounded tmpfs mounts. Health checks passed. A PostgreSQL-backed application changed bootstrap `required` from true to false, was manually restarted, returned healthy, and retained `required: false`; the fixture passed both its external and built-in health checks. Task smoke containers were then stopped and removed. |
| GitHub Actions on the final pull request | **PENDING** | Record the PR URL and every conclusion after publication. |

## Real-browser release matrix

Use the real Go API, disposable PostgreSQL, configured task-owned fixtures, and
an authenticated Guard session. Test mocks may create a controlled failure, but
they do not replace the successful durable workflow. Store only redacted,
git-ignored artifacts under `output/playwright/phase-one/`.

| Journey | Desktop | 320 CSS pixels | Accessibility/console | Evidence |
|---|---|---|---|---|
| Workspace navigation, state preservation, guided People workflow, denied/read-only state | **PASS** | **PASS** | **PASS** | Guided cancellation preserved the new site but not the canceled person; restart with that site created the person. A direct-permission read-only role exposed only 3 of 12 areas, hid writes, retained readable history/graph, and passed desktop/mobile audits. |
| Directory Grouper preview, exact-plan apply, history, graph, sanitized-error focus, read-only controls | **PASS** | **PASS** | **PASS** | Real fixture preview/apply persisted groups and memberships; the relationship graph loaded; a controlled sanitized failure moved focus to its alert; terminal conflicts did not offer retry; read-only controls stayed absent. |
| Stack durable import success, exact replay, recoverable/terminal failure rendering | **PASS** | **PASS** | **PASS** | Real import created two records; exact replay returned zero created/two unchanged. A controlled failed receipt remained bounded and recoverable. Product/version, installation, device license, assignment, expiry dates, and compliance state remained visible. |
| Exchange export, import, replay, ownership lock/claim, failure rendering | **PASS** | **PASS** | **PASS** | Real export/import/replay and receipt details passed. A product mutation failed with the Guard ownership message, succeeded after explicit claim, and a later exact replay left Guard's product state locally managed. Read-only users could not claim. |
| Reach endpoint/provider/template/group test, retryable send, retry, success, immutable attempts | **PASS** | **PASS** | **PASS** | A task-owned fixture produced a retryable send; explicit retry delivered it; both immutable attempts and sanitized status remained visible. No secret value entered browser state or artifacts. |
| Patterns and Signals primary workflows | **PASS** | **PASS** | **PASS** | A seven-field custom Pattern validated text, number, date, money, enum, attachment holding, and reference holding; CSV round trip and immutable v1/v2 labels passed. Signals evaluated, acknowledged, and assigned a real alert and served authenticated CSV. |
| Atlas Codes scan/associate and SVG/PDF label workflow | **PASS** | **PASS** | **PASS** | Code 128 and QR association/find/manual fallback/replacement/deactivation passed. A 70 × 30 mm SVG preview fit its rendered text; a two-item PDF had two pages and correct physical headers. Output stayed operator-confirmed; no physical printer was contacted by design. |
| Atlas Models defaults/provenance, instance overrides, model-to-asset filters/grouping | **PASS** | **PASS** | **PASS** | Model revision/default edits did not rewrite asset revision-1 snapshots; kind override and 32 GB/36-month defaults remained explicit; bulk assets, filters/grouping, retirement, and the linked asset's post-retirement snapshot passed. |

For each populated state, record keyboard behavior, focus recovery, visible
status/error meaning, page-level horizontal overflow, axe results, and console
errors/warnings. Expected diagnostic network failures must be identified,
resolved or classified, and cleared before the clean final capture.

All final populated-state audits reported zero axe violations, zero page-level
horizontal overflow, and zero retained console warnings/errors. Keyboard focus
returned to workflow launchers after cancel/success, the mobile navigation
drawer restored focus on Escape, and controlled failures focused their visible
alerts. axe-core 4.13.0 left color contrast incomplete where translucent and
backdrop-filtered surfaces resolve over radial gradients. A representative
Atlas classification found 167 of 170 nodes marked `bgGradient`, one partially
obscured node, and two short-text nodes. Manual WCAG calculations verified
`#f7fafc/#0b2238` at 15.41:1, `#16bfa7/#0b2238` at 6.96:1,
`#061827/#16bfa7` at 7.75:1, semantic status pairs at 9.54–10.37:1, and the
corrected `#90a4b5` secondary text at 6.42:1 on panels and 5.08:1 on selected
navigation. Fresh corrected-token audits passed at both 1440 × 1000 and 320 ×
900. Reviewed normal-text pairs meet 4.5:1 and focus/non-text indicators meet
3:1.

Evidence is retained in the git-ignored
`output/playwright/phase-one/closeout/` tree. Representative final artifacts
include `people-aria-fixed.audit.md`, `people-directory-read-only.audit.md`,
`stack-install-license-assignment.audit.md`, `reach-delivered.audit.md`,
`patterns-seven-field-versioned.audit.md`, `atlas-codes-svg-label-fixed.audit.md`,
`atlas-model-retired-final.audit.md`,
`mobile-patterns-seven-field-selected-fixed.audit.md`,
`overview-contrast-corrected-clean.audit.md`, and
`overview-mobile-contrast-corrected.audit.md` plus their screenshots and final
traces.

## gRPC deployment decision

The all-domain adapter covers all 16 services and 154 RPCs in the checked-in
descriptor. Production activation is **APPROVED AND IMPLEMENTED FOR PHASE ONE**
under `REQ-API-001`, `SEC-MCP-001`, and issue #14's REST/gRPC domain-parity
acceptance criterion. The standalone command now exposes the complete fixed
adapter through the reviewed dual-listener boundary:

1. a separate public Guard listener containing only bootstrap status,
   bootstrap, local authentication, and unary gRPC health Check with a 64 KiB
   envelope;
2. a separate authenticated listener for protected methods with the bounded
   34 MiB envelope needed by Exchange/Vault;
3. 16 KiB header lists, per-connection stream limits, five-minute RPC
   deadlines, and shared plus listener-specific pre-decode concurrency limits;
4. loopback-only plaintext plus TLS 1.3 or newer, HTTPS application origin, and
   secure-cookie policy for non-loopback binding; and
5. public unary health state tied to listener startup/shutdown, health
   Watch/List disabled to preserve Guard capacity, bounded graceful
   cleanup, remote-bootstrap fail-closed policy, and copied-secret scrubbing.

Command-level tests validate the registration split, authenticated Foundation
and Bridge calls, public message/header limits, concurrency admission before
authentication, TLS 1.3, health transitions, and cleanup. The non-root gRPC
container target is built independently by CI.

## Final sign-off

- **All issue-closing lines present:** **READY FOR PR BODY** — one line is
  prepared for each issue in the fixed scope table
- **No phase-one issue omitted:** **PASS**
- **No out-of-scope issue closed:** **PASS** — RFID #65 remains open and absent
  from the prepared closing lines
- **Working tree clean after generated/build checks:** **PASS FOR GENERATED AND
  BUILD OUTPUTS** — only the intended release-document changes remain before
  their signed commit
- **All required local gates passed:** **PASS**
- **All required browser rows passed:** **PASS**
- **Production gRPC activation decision recorded:** **PASS — ALL-DOMAIN
  DUAL-LISTENER RUNTIME ACTIVE**
- **Pull request checks passed:** **PENDING**
- **Final release decision:** **LOCAL PASS; PUBLICATION/CI PENDING**
