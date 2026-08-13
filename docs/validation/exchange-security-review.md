# Exchange security review

- **Requirement:** `REQ-EXCHANGE-001`
- **Feature:** `migration.packages`
- **Patterns integration:** `REQ-PATTERNS-001`, `templates.schemas`
- **Roadmap:** [#9](https://github.com/WSCMAX/StewardMesh/issues/9)
- **Reviewed:** 2026-08-13
- **Scope:** Exchange archive, service, Stack/Vault provider, repository, HTTP,
  React, API-contract, configuration, and migration paths introduced for phase
  one

## Executive summary

The active Go `net/http` and React/TypeScript review found no open critical,
high, medium, or low-severity vulnerability in the `REQ-EXCHANGE-001` slice.
One medium-severity privacy issue found during implementation was remediated
before release: the initial Vault adapter would have serialized the full Vault
record and therefore included organization and creator identifiers that are not
needed by a migration target. Exchange now uses a strict private payload,
strictly decodes it on import, and has a regression round trip proving that
private transport and operator data are absent.

This is a feature-slice review, not a claim that every dependency, deployment,
reverse proxy, object-store policy, or unrelated repository path was audited.
The integrated release gates passed race tests, `govulncheck`, `npm audit`,
OpenAPI/protobuf validation, and a real authenticated browser journey.

## Method and threat boundary

The review applied the repository's Go backend, general browser, and
React/TypeScript security baselines. It traced attacker-controlled JSON and ZIP
bytes from HTTP and browser inputs through archive decoding, provider writes,
Guard ownership registration, PostgreSQL receipts, history responses, downloads,
and audit events. Searches covered unbounded body reads, unsafe archive paths,
dynamic SQL, filesystem/shell execution, arbitrary outbound requests, raw HTML
or code execution, untrusted navigation, browser token storage, and credential
or signed-URL serialization.

An `.openinventory` file is untrusted even when its extension and media type
are correct. Integrity checks establish internal consistency, not source
authenticity. The authorization boundary is the authenticated organization
principal with `integrations.write`; the package itself is not an authority.

## Critical and high findings

None.

## Medium finding — remediated

### EXCH-SEC-001 — Vault payload exposed unnecessary organization/operator identifiers

- **Rule ID:** GO-CONFIG-001 / data minimization
- **Severity:** Medium, resolved before release
- **Location:** `internal/exchange/providers.go:137-149`,
  `internal/exchange/providers.go:160-188`, and
  `internal/exchange/providers_test.go:98-169`
- **Evidence:** Vault's general API record contains `organizationId` and
  `createdBy`, neither of which is required to recreate a portable blob. The
  Exchange provider now marshals `vaultPortablePayload`, whose allowlisted
  fields are only ID, filename, media type, size, checksum, descriptive provider,
  and optional resource relationship. The test rejects the source actor,
  organization, object-key name, credential markers, and signed-URL markers
  from the complete archive before verifying the target bytes and provenance.
- **Impact:** Before remediation, anyone authorized to receive an exported
  package could learn an internal organization identifier and operator account
  identifier unrelated to the migration operation.
- **Fix:** Replace full-record serialization with the private DTO; strictly
  decode unknown fields and cross-check envelope/file/relationship metadata on
  import (`internal/exchange/providers.go:223-252`).
- **Mitigation:** Package access still requires `integrations.write`, but that
  authorization was not treated as a substitute for minimizing exported data.
- **False positive notes:** This was not a false positive. The general Vault
  JSON contract intentionally exposes those values to Vault readers, while the
  cross-installation Exchange payload has a narrower purpose and trust boundary.

## Low findings

None open.

## Verified controls

| Threat | Control and evidence |
|---|---|
| Unauthorized or cross-site migration | Exchange routes bind organization-scoped `integrations.read` or `integrations.write`; both writes pass the existing same-origin and synchronized CSRF boundary before the handler (`internal/httpapi/server.go:195-198`, `internal/httpapi/server.go:3141-3185`). No organization ID is accepted from the request. |
| Oversized body or decompression bomb | Export JSON is capped at 128 KiB. Import rejects HTTP content encoding and non-exact media types, checks declared length, and wraps the body in `http.MaxBytesReader` at 32 MiB (`internal/httpapi/exchange.go:61-125`). ZIP entry count, entry size, total expansion, per-entry 200x-plus-1-MiB ratio, manifest, record payload, payload total, file, 128-dependency, and record limits are enforced before provider writes (`internal/exchange/archive.go:132-260`). |
| ZIP traversal or ambiguous archive | Only `manifest.json` and `files/{lowercase-sha256}` are accepted; directories, cleaned-name differences, duplicates, unsupported compression, missing/unreferenced entries, size mismatches, and file/record checksum mismatches fail closed (`internal/exchange/archive.go:145-258`). No archive path is passed to an OS filesystem API. |
| Malformed or credential-bearing payload | Manifest decoding rejects unknown fields and trailing JSON. Typed payloads must be bounded JSON objects; credential-like field names, URL user info, and signed/credential query parameters are rejected for case-insensitive HTTP(S) schemes (`internal/exchange/archive.go`). Stack and Vault providers then strictly decode their own schema and verify envelope identity, provenance, dependencies, and file metadata. |
| Schema substitution, drift, or late failure | Manifest schema `1.1` carries a sorted unique Patterns registry and repeats the exact template ID/version in every record checksum. Export resolves and validates the full provider catalog before file reads or receipt creation. Import completes a bounded all-record preflight before the first durable record intent, Guard call, or provider write. Unknown, mismatched, ambiguous, retired, and invalid schemas fail closed; holding-capable reference gaps remain visible. Stack and Vault payloads are allowlisted schema projections (`internal/exchange/archive.go`, `internal/exchange/service.go`, `internal/exchange/providers.go`). |
| Missing dependency or unsupported type | Providers are registered against unique allowlisted record types. Imports run in topological order; unavailable relationships, metadata-only Vault content without an exact target, and unknown providers produce visible holding outcomes without a domain write (`internal/exchange/service.go`, `internal/exchange/providers.go`). |
| Ownership-policy bypass | Guard registers earliest source identity before the provider write and the durable intent records the returned lock state. A newly created lock is compensated only when the provider reports that no write committed; a commit followed by audit failure retains the lock and truthful outcome for repair. Domain mutation handlers call the shared Guard write boundary; claims require separate `guard.manage`. The Guard UI strictly validates the ownership list, sends the synchronized CSRF token, and presents locked and locally managed states without trusting package-supplied actors. Package ownership metadata never bypasses the target's Guard decision. |
| Replay, collision, crash, or partial failure | Receipt identity includes organization, direction, package ID, source, schema, and complete archive digest. Changed reuse conflicts. Before a provider mutation, Exchange persists a per-record intent containing an immutable deterministic provider token and created-versus-unchanged expectation. Provider commits are checkpointed even when audit delivery fails, and retries use the same token to repair the deterministic Stack/Vault audit event. Compare-and-swap checkpoints plus heartbeats renew the processing lease throughout slow calls, so another worker cannot concurrently invoke the provider; a crashed worker can be replaced only after expiry. PostgreSQL persists the private intents beside outcomes and constrains terminal states to have none (`internal/exchange/service.go`, `internal/exchange/providers.go`, `internal/repository/postgres/exchange.go`, `internal/repository/postgres/migrations/0032_exchange_packages.sql`). |
| Credential, object-key, signed-URL, or stale-object export | Archive payload traversal rejects credential fields and signed URLs. The Vault DTO excludes account, organization, object-key, credential, token, and URL fields; object keys remain unexported server state. File bytes are read through Vault's checksum-verifying reader, and both included and metadata-only exact replays open and hash target bytes before returning unchanged (`internal/exchange/providers.go`, `internal/storage/service.go`). |
| Audit or history disclosure | Exchange audit metadata contains only stable feature/status/checksum/count values (`internal/exchange/service.go:536-560`). HTTP history omits actor, organization, manifest, and bytes. The database retains `created_by` for controlled audit/reconciliation but the `Package` JSON tag excludes it from responses. |
| Browser XSS, exfiltration, or active preview | Server values render through escaped JSX. Requests use fixed same-origin paths and CSRF headers. The file is never rendered or previewed; export response type, package ID, digest, and size are validated before creating a fixed-name object URL, which is revoked on replacement and unmount (`web/src/ExchangeManager.tsx:145-235`). |

The scoped sink search found no Exchange use of shell execution, dynamic SQL
interpolation, user-derived filesystem paths, arbitrary outbound HTTP, raw HTML
insertion, `eval`/`Function`, `postMessage`, browser credential storage, or
untrusted redirects.

## Residual and operational limits

- Packages are checksummed but not encrypted or cryptographically signed.
  Operators must transfer them over an approved confidential channel, restrict
  filesystem access, and verify the separately communicated SHA-256 when source
  authenticity matters. A future signing format requires a versioned schema and
  key-management threat review.
- Included Vault bytes are validated for size and checksum, not malware. They
  remain private and are served as controlled downloads, but organizations that
  require content scanning must place a scanner/quarantine workflow at the
  Vault or object-storage boundary.
- Edge TLS, reverse-proxy request limits, S3 bucket/KMS/IAM policy, audit-store
  retention, and backup access are deployment controls not proven by this code
  review. Verify them in each shared environment.
- The current provider registry supports Stack and Vault. Adding another domain
  requires an active built-in or uniquely resolved custom Patterns schema, a
  typed payload allowlist, dependency and idempotency tests, privacy review,
  and write-lock enforcement in every mutation path before registration.

## Validation evidence

The implementation provides focused corruption, traversal, duplicate-entry,
checksum, compression-ratio, size, mixed-case credential/signed-URL, dependency,
post-write crash recovery, post-commit audit repair, slow-provider heartbeats,
lease takeover, partial-progress retry, bounded provider lookup, target-object
tampering, ownership, privacy, HTTP, repository, and UI regression tests. The final command
and browser evidence is tracked in [Exchange release validation](exchange-release.md).
