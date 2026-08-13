# Directory Expansion Phase-One Release Validation

Requirement: `REQ-DIRECTORY-EXPANSION-009`

Canonical feature: `experience.help`

Tracking issue: [#32](https://github.com/WSCMAX/StewardMesh/issues/32)

Prepared: 2026-08-13

This record defines the final release gate for Directory Expansion requirements
`REQ-DIRECTORY-EXPANSION-001` through `REQ-DIRECTORY-EXPANSION-009`. It covers
security, accessible user behavior, REST and protobuf contracts, durable
repository behavior, deployment profiles, container builds, and traceability.

## Requirement evidence matrix

| Requirement | Phase-one capability | Primary release evidence |
|---|---|---|
| `REQ-DIRECTORY-EXPANSION-001` | Structured sites, buildings, and rooms | People service/repository/HTTP tests and accessible People UI tests |
| `REQ-DIRECTORY-EXPANSION-002` | Provider-neutral exact-plan preview/apply/retry | Import contract, repository, HTTP, and `DirectoryImportManager` tests plus UI trace evidence |
| `REQ-DIRECTORY-EXPANSION-003` | Microsoft Entra ID read-only connector | Connector security/bounds tests, including ambient-proxy isolation |
| `REQ-DIRECTORY-EXPANSION-004` | SailPoint ISC read-only connector | Connector normalization, conflict, bounds, and no-write tests |
| `REQ-DIRECTORY-EXPANSION-005` | Grouper read-only connector and fixture | Connector/fixture tests, integrations Compose validation, and fixture image build |
| `REQ-DIRECTORY-EXPANSION-006` | PeopleSoft QAS read-only connector | Query mapping, hierarchy, partial-snapshot, conflict, and no-write tests |
| `REQ-DIRECTORY-EXPANSION-007` | Explicit synthetic demo dataset | Opt-in, isolation, idempotency, default-off, and demo-profile tests |
| `REQ-DIRECTORY-EXPANSION-008` | Permission-scoped relationship graph | API/repository tests and keyboard-accessible table alternative |
| `REQ-DIRECTORY-EXPANSION-009` | Security, accessibility, deployment, docs, and trace closeout | This record, connector runbook, exact 001-009 trace gate, CI, and browser evidence |

`go run ./cmd/tracecheck` is the authoritative machine-readable link from each
requirement to documentation, API, code, schema, tests, and UI. Its phase-one
series check activates when all nine requirements are present in the catalog
and rejects any missing Directory Expansion trace entry.

## Security review

| Boundary | Threat | Control and verification |
|---|---|---|
| Provider credentials and tokens | Browser, log, audit, or persistence disclosure | Deployment-only injection, post-construction secret clearing, bounded safe errors, credential-free API shapes, and redaction tests |
| Provider network access | SSRF, redirect escape, DNS rebinding, or ambient proxy exfiltration | Fixed validated endpoints, redirect refusal, same-origin pagination, address checks for configurable hosts, explicit private-network opt-in, and no ambient proxy in default connector transports |
| Provider permissions | Accidental directory mutation | Dedicated least-privilege principals, fixed read routes, method-invariant tests, and operator verification of provider logs |
| Reconciliation | Stale overwrite, partial-snapshot deletion, or cross-provider takeover | Immutable exact plans, configuration revisions, optimistic target revisions, two identical normalized traversals before completeness, explicit conflicts, local ownership claims, and durable idempotency |
| Availability and resource use | Infinite pagination, oversized bodies, retry storms, or unbounded history | Per-provider timeouts, response/page/record/membership limits, bounded safe-read retries, loop detection, and bounded attempts |
| Authorization | Cross-organization or unauthorized mutations | Guard-derived scopes, `integrations.read`/`integrations.write`, synchronized CSRF, organization isolation, and `no-store` responses |
| Demo and fixture data | Production contamination | Strict opt-in profiles/flags, `demo-*` organization requirement, `.invalid` identities, default-off checks, and explicit fixture warnings |

No provider endpoint or credential is accepted from a client request. OAuth
token exchange is the only non-GET provider-adjacent call for Entra and
SailPoint; all directory data operations are read-only.

## Automated release commands

Run from the repository root with the PostgreSQL integration URL set when a
local service is available:

```sh
gofmt -l internal/directoryexpansion/entra/connector.go internal/directoryexpansion/entra/connector_test.go internal/traceability/verify.go internal/traceability/verify_test.go
go test ./internal/traceability ./internal/directoryexpansion/entra
go test -race ./...
go vet ./...
go tool govulncheck ./...
go run ./cmd/tracecheck
protoc --proto_path=api/proto --descriptor_set_out=/tmp/stewardmesh.pb api/proto/stewardmesh.proto
npm --prefix web ci
npm --prefix web audit --audit-level=high
npm --prefix web run openapi:lint
npm --prefix web run typecheck
npm --prefix web test
npm --prefix web run build
docker compose -f deploy/docker-compose.yml config --quiet
docker compose -f deploy/docker-compose.yml --profile cache config --quiet
docker compose -f deploy/docker-compose.yml --profile demo config --quiet
docker compose -f deploy/docker-compose.yml --profile integrations config --quiet
docker build --file deploy/Dockerfile --target stewardmesh --tag stewardmesh:release-check .
docker build --file deploy/Dockerfile --target grouper-fixture --tag stewardmesh-grouper-fixture:release-check .
```

CI repeats these contracts and scans the working tree for high and critical
known vulnerabilities. A PostgreSQL-backed race run is required before PR
publication; a memory-only run is useful but does not replace repository
integration coverage.

## Browser and accessibility protocol

Use a real Chromium session against the built application and API. Capture the
results under `output/playwright/directory-expansion/` without committing
sessions, tokens, credentials, or provider payloads.

1. At a desktop viewport, sign in as an integration administrator, open
   **People > Directory import**, and verify the configured source label exposes
   no endpoint or credential.
2. Preview a bounded source, inspect counts and normalized audit detail, then
   apply the exact plan. Confirm the success status and refreshed People data.
3. Force a sanitized API error. Confirm focus moves to the alert and keyboard
   navigation resumes from the error context.
4. Tab to **Recent directory imports** and **Directory import records**. Confirm
   both scrollable regions have an accessible name and visible keyboard focus.
5. Sign in with `integrations.read` but not `integrations.write`. Confirm history
   and detail remain visible while preview/apply/retry controls are absent.
6. Verify the relationship graph's named table alternative and scope behavior.
7. Repeat the People/import/graph path at 320 CSS pixels. Confirm no page-level
   horizontal overflow, clipped control, inaccessible table content, or focus
   loss. Run axe on each populated state and resolve every serious violation.

## Release evidence record

The pull request must record the commit tested, date, PostgreSQL URL shape with
credentials redacted, exact command results, browser viewport sizes, and saved
artifact paths. A failed or skipped required gate blocks phase-one release; an
environment limitation is reported as a remaining gap and rerun in CI rather
than recorded as a pass.

The isolated closeout branch passed the full race-enabled Go suite, vet,
`govulncheck`, tracecheck, protobuf descriptor generation, OpenAPI lint,
TypeScript, all 28 Vitest files and 143 tests, the production web build, npm
audit, and default/cache/demo/integrations Compose configuration on 2026-08-13.
The local Node runtime was 23.7.0 rather than the pinned CI 24.15.0 and emitted
engine warnings, so CI remains the authoritative supported-runtime rerun.
PostgreSQL-backed tests could not run locally because no test database was
listening; application and fixture builds could not start because the local
Docker daemon was unavailable. Authenticated desktop/320-pixel browser evidence
must be captured after requirements 006 and 008 are integrated. None of those
environment-limited checks is recorded as passed here.
