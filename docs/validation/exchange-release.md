# Exchange phase-one release validation

- **Requirement:** `REQ-EXCHANGE-001`
- **Feature:** `migration.packages`
- **Roadmap:** [#9](https://github.com/WSCMAX/StewardMesh/issues/9)
- **Prepared:** 2026-08-13
- **Final integrated result:** Passed on the final validated feature tree

This record is the release evidence for the complete Exchange slice. The
automated gates and authenticated browser journey below passed against the
final feature tree before its single signed commit was created.

## Delivered boundary

The branch contains schema `1.1` dependency-aware `.openinventory` ZIP
packages, Stack and Vault providers, metadata-only/included-file choices,
exact immutable Patterns IDs/versions and package-wide typed preflight,
checksummed bounded decoding, durable memory/PostgreSQL receipts, exact replay
and safe holding/failed retry, per-record durable intents with deterministic
provider tokens, slow-write lease heartbeats, stale processing lease takeover,
committed audit repair, explicit missing-dependency outcomes, Guard
write locks and claim flow, integration-permission REST and gRPC contracts,
redacted audits, accessible Workspace and documentation surfaces, configuration,
requirements, and traceability.

Migration `0032_exchange_packages.sql` creates
only organization-scoped package receipts/history constraints and does not add
or broaden a Guard policy permission; Exchange deliberately reuses the existing
`integrations.read` and `integrations.write` boundary. Migration
`0035_exchange_patterns_schema.sql` upgrades the receipt check to schema `1.1`
while preserving historical `1.0` receipt rows. New `1.0` archives are rejected
and must be re-exported; old receipt labels are not rewritten to claim
validation they never received.

## Automated scenario evidence

| Scenario | Evidence |
|---|---|
| Exact/mismatched/retired Patterns schemas, all-record schema preflight, dependency selection, closure, order, cycles, missing references, holding retry, created/unchanged replay, second-record failure checkpoints, crash between provider commit and receipt checkpoint, committed-result recovery, deterministic token reuse, blocked-provider heartbeats, active-lease exclusion, stale takeover after terminal-update failure | `internal/exchange/service_test.go` |
| ZIP round trip, corrupt archive, checksum tamper, duplicate/path entries, case-insensitive HTTP(S) credential/signed URL rejection, archive/file/manifest/payload/decompression limits | `internal/exchange/archive_test.go` |
| Stack earliest-provenance round trip, full-import operation counts with zero inventory snapshots, Stack/Vault post-commit audit repair, and Vault included/metadata-only modes with missing, exact, and content-verified targets | `internal/exchange/providers_test.go`, `internal/stack/service_test.go`, `internal/storage/service_test.go` |
| Ownership registration, compensation, durable write lock, explicit claim, mutation rejection before claim | `internal/exchange/service_test.go`, `internal/httpapi/exchange_test.go`, `internal/httpapi/server_test.go` |
| Exact package collision, compare-and-swap transitions, defensive copies, organization isolation, migration constraints | `internal/repository/contracttest/exchange.go`, `internal/repository/memory_exchange_test.go`, `internal/repository/postgres/migrate_test.go`, `internal/repository/postgres/postgres_integration_test.go` |
| Authentication, permission, CSRF, no-store, exact media type, content encoding, 32-MiB cap, attachment headers, stable errors | `internal/httpapi/exchange_test.go`, `internal/httpapi/server_test.go` |
| Runtime response validation, export download lifecycle, import/holding/error/denied states, keyboard semantics | `web/src/ExchangeManager.test.tsx`, `web/src/api.test.ts` |
| Workspace route, help, documentation, permission visibility, and narrow composition | `web/src/WorkspaceShell.test.tsx`, `web/src/documentation.test.ts`, `web/src/ExchangeManager.test.tsx` |
| API/gRPC and requirement parity | `api/openapi/openapi.yaml`, `api/proto/stewardmesh.proto`, `docs/requirements/traceability.json` |

## Security closeout

The [Exchange security review](exchange-security-review.md) records one
implementation-time privacy finding, remediated before release, and no open
critical, high, medium, or low finding. Final dependency/race scanners remain
release gates rather than assumptions.

## Required integrated command gates

Run from a clean final feature branch. PostgreSQL tests must use a clean test
database with every migration, including `0032`, applied in order.

```text
go test -race ./...
go vet ./...
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
go run ./cmd/tracecheck
protoc --proto_path=api/proto --descriptor_set_out=<temporary-file> --include_imports api/proto/stewardmesh.proto
cd web && npm audit --audit-level=high
cd web && npm run openapi:lint
cd web && npm run typecheck
cd web && npm test
cd web && npm run build
docker compose -f deploy/docker-compose.yml config --quiet
docker compose -f deploy/docker-compose.yml --profile cache config --quiet
docker build -f deploy/Dockerfile .
```

Warnings must be classified; skipped or unavailable checks are not a pass.

## Required real-browser journeys

Use the real Go API through the Vite proxy, an authenticated Guard session, and
the configured durable PostgreSQL and Vault adapters. Mocks do not satisfy this
section.

### Desktop authenticated workflow

1. Sign in as an account with `integrations.read` and `integrations.write`.
2. Open Exchange from Workspace and verify the catalog/history load without
   console or network errors.
3. Select a Stack child record, include dependencies, choose metadata-only,
   export, and verify attachment suffix, media type, package ID, byte size, and
   displayed SHA-256.
4. Import that file into a clean target organization and verify dependencies
   appear first, created/unchanged counts are exact, source provenance remains
   visible, and imported records are readable.
5. Attempt a Stack mutation and verify HTTP 423/write-lock guidance. Claim the
   resource through Guard with `guard.manage`, repeat the mutation, and verify
   it succeeds without a later import restoring the lock.
6. Import the same completed package again and verify `replay: true`, HTTP 200,
   unchanged durable counts, and no duplicate provider record.
7. Import a package with an unresolved dependency and a metadata-only Vault
   record; verify both are holding and no held domain record/file was written.
   Resolve the dependency and retry the exact package, verifying idempotent
   completion on the same receipt.
8. Export a small Vault file with include mode, import it, download through the
   target Vault, and compare bytes and SHA-256. Inspect the archive test fixture
   to confirm it contains no actor/organization identifier, object key,
   credential, token, or signed URL.
9. Exercise a corrupt archive/checksum, wrong media type, encoded request,
   oversized client file, changed package-ID collision, denied read role, denied
   write role, and invalid CSRF token; verify stable non-sensitive errors.

### Accessibility and 320-pixel workflow

- Repeat catalog selection, file-mode choice, export, import, holding detail,
  package disclosure, and Guard claim navigation using keyboard only.
- At 320 CSS pixels verify populated, empty, denied, busy, success, holding,
  failed, and validation-error states without page-level horizontal overflow;
  wide tables may scroll only within their labelled regions.
- Run axe against the authenticated Exchange surface and record zero violations
  for the automated WCAG 2.2 AA rules used by the project.
- Verify focus moves to errors, status changes are announced, controls retain
  accessible names and minimum target sizes, and outcomes are understandable
  without color.
- Capture desktop and narrow screenshots plus a console-error check under
  `output/playwright/exchange/` (git-ignored).

## Final evidence record

- **Validation completed:** 2026-08-13T14:11:01Z
- **Persistence:** isolated PostgreSQL database with migrations through `0032`;
  checksummed local Vault adapter for the authenticated browser run
- **Commit identity:** the exact signed commit SHA is recorded in the branch
  handoff and pull request because a commit cannot contain its own SHA
- **Backend:** `go test -race ./...`, the PostgreSQL race integration suite,
  `go vet ./...`, tracecheck, protobuf descriptor generation, `govulncheck`,
  Compose configuration, and the production Docker build passed
- **Frontend/contracts:** TypeScript, all 24 Vitest files and 126 tests,
  production build, npm audit, and OpenAPI lint passed. Redocly reported only
  the four pre-existing intentional redirect-operation warnings for OIDC/SAML
  `303` responses.
- **Authenticated browser:** a synthetic Stack version was exported with its
  product dependency, downloaded, imported, and replayed. A pre-claim mutation
  returned the visible Guard lock guidance; the new Guard ownership table
  claimed the product; the same mutation then succeeded. Durable history
  remained correct after server restarts.
- **Accessibility/responsiveness:** axe reported zero WCAG 2.2 AA automated
  violations on Exchange and Guard. The final authenticated tab reported zero
  console errors and zero warnings. At 320 CSS pixels, both Exchange and Guard
  measured `documentScrollWidth = viewportWidth = 320`; only their explicitly
  labelled record/outcome/ownership table regions scrolled horizontally.
- **Keyboard:** the mobile record selector received focus and toggled with
  Space while retaining focus; unit coverage also verifies focus-on-error and
  status announcements.
- **Visual evidence:** `output/playwright/exchange/exchange-desktop.png` and
  `output/playwright/exchange/exchange-mobile-320.png` (git-ignored).

The browser run found and resolved two null-versus-empty-array response
contract defects and page-level overflow from intrinsic table sizing before
this record was marked passed. No production contents, credentials, cookies,
tokens, object keys, or database URLs are stored in the evidence.
