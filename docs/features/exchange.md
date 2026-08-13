# Exchange — Import and export packages

- **Canonical ID:** `migration.packages`
- **Requirement:** `REQ-EXCHANGE-001`
- **Roadmap issue:** [#9](https://github.com/WSCMAX/StewardMesh/issues/9)
- **Package media type:** `application/vnd.stewardmesh.openinventory+zip`
- **Package suffix:** `.openinventory`
- **Manifest schema:** `1.0`

## Purpose and supported boundary

Exchange moves selected organization records and their required relationships
between StewardMesh installations without coupling the package format to an
HTTP handler, repository, or object-storage provider. Operators can preview the
portable catalog, select an explicit scope, add available transitive
dependencies, choose how Vault files travel, export one compressed package,
and inspect durable export/import receipts.

The v1 provider registry carries Stack products, versions, licenses,
installations, and assignments plus Vault blobs. The registry is deliberately
provider-neutral: additional domains implement the same bounded record,
existence, import, and optional file-reader contract. An unregistered record
type is never interpreted generically or silently discarded; it receives an
explicit holding outcome.

Exchange does not synchronize records continuously, write back to a source
system, overwrite a conflicting local revision, infer missing relationships,
or treat an archive as trusted merely because it has the expected suffix.

## Package format and dependency graph

An `.openinventory` package is a ZIP archive containing exactly one
`manifest.json` and, only in include-file mode, content-addressed entries named
`files/{sha256}`. Other paths, directories, traversal segments, duplicate
entries, unsupported compression methods, unreferenced files, and multiple
manifest entries fail verification.

The manifest carries:

- schema version, package ID, source-system ID, UTC export time, and file mode;
- each record's type, stable ID, revision, SHA-256 checksum, typed JSON payload,
  sorted relationships, earliest known source-system/source-record provenance,
  and ownership state;
- optional file name, exact media type, byte count, checksum, and fixed internal
  archive entry; and
- no organization-selected operator identity, database key, private object key,
  provider credential, session value, access token, or signed URL.

Record checksums cover identity, revision, dependencies, provenance, ownership,
file metadata, and typed payload. The archive has a separate SHA-256 checksum
returned as `X-Content-SHA256` and retained in the receipt. Import verifies both
layers before any domain write. Dependencies are topologically ordered so a
record is attempted only after every packaged dependency has completed. Cycles,
duplicate identities, duplicate/unsorted dependencies, a checksum mismatch, or
an invalid identity fail closed.

The **Include required dependencies** option closes the selected scope over all
dependencies that are present in the portable catalog. If an external
relationship is not selected or not available, import preserves that fact as a
holding result instead of inventing a target.

## Vault and S3-compatible file choices

Every export explicitly chooses one file mode:

- **Metadata only** carries the safe Vault record, filename, media type, size,
  checksum, provider name, provenance, and relationships, but no bytes or
  private S3-compatible object key. An exact target Vault record is unchanged;
  otherwise the missing exact content places that record in holding with an
  `exchange.file` dependency.
- **Include file bytes** reads content through Vault's integrity-checking
  service, verifies its stored size and checksum, and embeds it under the fixed
  content-addressed path. Import writes through Vault's local or S3-compatible
  adapter using a deterministic private key and re-verifies the stored object.

For both modes, an exact-target check opens and hashes the target object rather
than trusting metadata alone. The import operation repeats that verification
immediately before it reports `unchanged`, so missing content or same-size
tampering cannot pass through a metadata-only time-of-check/time-of-use gap.

The choice is the same for local and S3-compatible Vault adapters. Exchange
never exports AWS access keys, session tokens, role credentials, download
grants, presigned URLs, or internal object keys. A future external-path resolver
must remain server-configured and may not place those values in a package.

## Import, idempotency, holding, and ownership

Import first verifies the complete archive and reserves a durable
organization-scoped receipt keyed by direction and package ID. The same package
ID with a different archive digest, source system, or schema conflicts. An
exact replay of a completed receipt returns the existing outcomes with
`replay: true` and performs no provider writes. A holding or failed receipt can
retry the exact archive after its blocker is resolved; domain providers retain
their own exact source identity and payload replay checks, so work completed
before a later holding or failure is unchanged rather than duplicated.

Before each provider call, Exchange persists a private per-record intent with a
deterministic idempotency/fencing token and the exact pre-write created-versus-
unchanged expectation. Guard registration is checkpointed on that intent. If a
domain write commits but its audit delivery fails, the provider reports the
committed result, Exchange persists the truthful visible outcome, and the Guard
lock remains in place. Retrying the receipt reuses the token to repair the same
deterministic provider audit event before clearing the intent; it does not
attribute the already-created record as unchanged.

Receipt changes use compare-and-swap updates. Failed receipts therefore retain
durable created and unchanged outcomes as well as any private recovery intent.
Every checkpoint renews a five-minute processing lease, and Exchange also sends
lease heartbeats while a provider call is running. A concurrent request cannot
take over an active slow write; after an actual crash or terminal receipt-update
failure, the exact archive can claim an expired lease and resume with the same
provider token.

Each record has one of three visible outcomes:

- `created` means the target provider created the exact record;
- `unchanged` means the provider found an exact idempotent replay; or
- `holding` means a dependency, provider, or required file is unresolved, so
  the record was not written.

Before a provider write, Exchange registers the record's earliest provenance
with Guard. Created and unchanged imported records are readable, but Guard
marks them write-locked. An organization administrator with `guard.manage` must
explicitly claim the resource in Guard before a mutating domain operation can
change it. Guard's imported-resource table shows the earliest source, current
lock state, and an explicit claim action. A claim is durable, audited, and
cannot be silently reversed by a later import. If the provider proves that no
domain write committed, a newly created lock is compensated; if the domain
write committed but audit delivery failed, the lock and created truth are
retained for deterministic repair. An existing or already claimed ownership
record is never deleted.

Holding is intentionally non-destructive. The receipt names each missing typed
relationship, provider diagnostic, or file checksum. Resolve the dependency,
make required file content available, or install the matching provider, then
retry the same package. Exchange re-evaluates its fixed manifest and updates the
same durable receipt without duplicating earlier provider work.

## Limits and validation

The server enforces all limits even when a client omits its own checks:

| Boundary | Limit |
|---|---:|
| Explicit export selections | 1,000 |
| Records after dependency expansion | 10,000 |
| Dependencies per record | 128 |
| Holding diagnostics per outcome | 130 |
| Included files / ZIP file entries | 1,000 |
| Compressed archive | 32 MiB |
| Expanded archive | 64 MiB |
| Manifest | 32 MiB |
| One included file | 16 MiB |
| One typed JSON payload | 1 MiB |
| All typed payloads | 24 MiB |
| Package history query | 1–100 receipts; default 25 |

ZIP entries also fail when their declared expansion is greater than 200 times
the compressed size plus 1 MiB. JSON decoding rejects unknown manifest fields
and trailing content. Resource types, IDs, checksums, revisions, UTF-8 file
names, exact parameter-free media types, timestamps, counts, states, and source
identities use explicit allowlists or bounds. Typed payloads are JSON objects
and reject credential-like keys and credential-bearing or signed HTTP URLs
before a domain provider receives them. Stack and Vault then strictly decode
their own payload shape and verify that envelope identity, provenance,
relationships, and file metadata agree with the typed record.

## Permissions, APIs, and configuration

- Organization-scoped `integrations.read` lists the portable catalog and recent
  package receipts.
- Organization-scoped `integrations.write` exports and imports packages.
- `guard.manage` is separately required to claim an imported record.

Exchange reuses the existing integration permissions; migration
`0032_exchange_packages.sql` does not broaden any role or policy bundle. Every
route is authenticated and organization-scoped. Browser writes require the
synchronized CSRF token and configured origin. Responses use `no-store`; raw
imports require the exact Exchange media type, reject HTTP content encoding,
and are capped before reading.

REST endpoints:

- `GET /api/v1/exchange/records`
- `GET /api/v1/exchange/packages?limit=25`
- `POST /api/v1/exchange/export`
- `POST /api/v1/exchange/import`

The OpenAPI binary contracts and `ExchangeService` protobuf RPCs expose the
same catalog, selection, archive, receipt, replay, holding, and ownership-lock
semantics. Configure the stable installation identity with
`STEWARDMESH_EXCHANGE_SOURCE_SYSTEM_ID`; it defaults to
`STEWARDMESH_ORGANIZATION_ID`. Changing it after packages have been exchanged
creates a different provenance identity and should be an intentional migration
decision.

## Audit and privacy

Exchange records `exchange.package.exported`, `exchange.package.imported`, and
`exchange.package.import_failed`. Safe metadata includes requirement/feature,
direction, schema, status, archive checksum, and record/file/outcome counts.
The package ID is the audit resource ID. Audit metadata intentionally excludes
manifest payloads, file names and bytes, source-record IDs, relationship IDs,
operator-supplied text, credentials, object keys, signed URLs, cookies, and
CSRF tokens. Guard separately audits ownership registration and claim.

Package history exposes the package/source identities, checksums, modes, counts,
stable outcome references, missing dependencies, and write-lock booleans. It
omits the manifest payload, file bytes, organization ID, and actor identity.

## Accessible operator workflow

1. Review the portable record table and select records with native checkboxes.
2. Choose dependency closure and metadata-only or included-file handling.
3. Prepare the export, verify the visible byte size and SHA-256 checksum, then
   activate the server-generated `.openinventory` download.
4. Choose a non-empty package and import it. The browser performs only early
   size/type checks; the server remains authoritative and never renders an
   uploaded package inline.
5. Inspect text-labelled created, unchanged, holding, failure, dependency, and
   ownership-lock outcomes in package history. Claim imported records through
   Guard only after reviewing their provenance.

The workbench uses semantic headings and fieldsets, labelled native controls,
keyboard-operable tables and disclosures, focus-managed errors, polite status
announcements, visible non-color status text, minimum-size controls, and
contained horizontal scrolling for dense tables. At 320 CSS pixels the page
does not gain horizontal overflow. Object URLs created for a verified download
are revoked on replacement and unmount.

## Validation and issue reporting

Automated coverage includes dependency closure and ordering, package round
trips, duplicate selections and records, missing and unknown references,
metadata-only files with missing and exact target content, exact replay and
changed-package conflicts, missing and same-size-tampered target objects,
stale-lease recovery, lease heartbeats around a blocked provider, second-record
failure with durable saga progress, committed Stack/Vault audit-failure repair,
bounded keyed Stack lookup for full Exchange imports, failed retry,
ownership registration/claim behavior, compensation, corruption, path
traversal, duplicate ZIP entries, checksum tampering, mixed-case HTTP(S)
credential and signed-URL rejection, compressed and expanded limits,
memory/PostgreSQL repository
conformance, organization isolation, HTTP authentication/permission/CSRF/media
type/no-store/error behavior, OpenAPI/protobuf parity, accessible React states,
and narrow-width containment.

The [security review](../validation/exchange-security-review.md) and
[release validation record](../validation/exchange-release.md) define the final
gate and real authenticated desktop/320-pixel browser evidence.

Report problems through the application issue link or GitHub issue #9. Include
a safe correlation ID, direction, package ID, schema, source-system ID, archive
checksum, mode, counts, stable error code, and whether the operation was an
exact replay. Never attach a production package, manifest payload, file, source
record ID, private relationship data, object key, credential, signed URL,
session cookie, or CSRF token.
