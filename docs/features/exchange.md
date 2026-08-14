# Exchange — Import and export packages

- **Canonical ID:** `migration.packages`
- **Requirement:** `REQ-EXCHANGE-001`
- **Patterns integration:** `REQ-PATTERNS-001`, `templates.schemas`
- **Directory integration:** `REQ-DIRECTORY-EXPANSION-005`, `identity.directory`
- **Roadmap issue:** [#9](https://github.com/WSCMAX/StewardMesh/issues/9)
- **Package media type:** `application/vnd.stewardmesh.openinventory+zip`
- **Package suffix:** `.openinventory`
- **Manifest schema:** `1.1`

## Purpose and supported boundary

Exchange moves selected organization records and their required relationships
between StewardMesh installations without coupling the package format to an
HTTP handler, repository, or object-storage provider. Operators can preview the
portable catalog, select an explicit scope, add available transitive
dependencies, choose how Vault files travel, export one compressed package,
and inspect durable export/import receipts.

The phase-one portability policy classifies every durable record family in the
authoritative Patterns core list. Thirty-nine business, configuration, and
relationship families are portable and must have exactly one owning domain
provider before the phase-one gate can pass. Twelve deployment-owned, derived,
operational, security-sensitive, or self-referential families are deliberately
excluded with an operator-visible reason. The executable registry rejects
missing, overlapping, duplicate, and out-of-policy classifications.

The provider contract remains domain-neutral: each owning service supplies its
bounded record projection, exact existence comparison, idempotent import, and
optional file-reader/dependency resolver. Exchange never falls back to generic
JSON writes. A foreign record whose provider is unavailable receives an
explicit holding outcome; a phase-one application build with an unavailable
portable provider is incomplete and must not use that behavior as a scope
reduction.

Every portable instant uses the shortest canonical RFC 3339 representation in
UTC (`Z`) with no precision finer than one microsecond. This is the precision
that PostgreSQL `timestamptz` preserves. Provider imports reject offsets,
noncanonical spellings, and sub-microsecond values before any domain write;
exports reject legacy values that would require conversion instead of silently
changing them. Ordinary service clocks and effective-dated inputs are
normalized before they become owned state, PostgreSQL connections scan
`timestamptz` in UTC, and mutable records use the later of the normalized local
clock and their existing source timestamp so a legitimate future-dated import
cannot regress on its next ordinary update.

The Atlas providers own models, assets, immutable lifecycle events, and Atlas
Codes identifier history. Version-2 Patterns projections preserve stable IDs,
arbitrary positive revisions, source timestamps, complete model defaults and
immutable per-asset default snapshots, current asset state and People
references, lifecycle actor provenance, and the complete Code 128/QR
replacement lineage. One identifier record is one lineage; PostgreSQL imports
all of its mutually referencing rows in a single transaction, so a partial
history cannot become visible. Long histories are split only across bounded
canonical JSON chunks and are reassembled before validation and the atomic
write. Organization IDs remain destination-owned, identifier values never
enter import audits, and all ordinary Atlas and Atlas Codes mutations pass
through Guard's imported-resource ownership fence. Only each service's opaque
construction-time importer can preserve exact portable state after Exchange
has durably reserved its write intent and ownership lock.

The Atlas Catalog provider owns configurations, effective prices, and upgrade
paths. It preserves stable IDs, revisions, typed Catalog fields, effective
dates, and Atlas Model/configuration dependencies while excluding organization
IDs, timestamps, operator identities, and audit-only state. Imports execute
through Catalog's opaque construction-time importer capability; ordinary
Catalog service writes continue through Guard's audited imported-ownership
fence.

The Patterns provider owns organization-defined templates. One portable record
contains the complete bounded immutable history so a target cannot receive a
latest version without every earlier version it supersedes. Built-ins remain
code-owned and are not exported; creator identities, organization IDs, and
timestamps are target-local metadata rather than portable schema truth.

The People provider owns sites, buildings, rooms, departments, identities, and
effective-dated asset assignments. Its version-2 Patterns projections preserve
stable IDs, revisions, status, structured addresses, location and assignee
relationships, directory-source mapping, timestamps, and complete assignment
history while omitting organization IDs and creating-operator identity.
PostgreSQL exports use one bounded repeatable-read snapshot. Imports use
People's opaque construction-time capability to recreate exact state, and all
ordinary People service writes pass through Guard's imported-ownership fence.

The Directory provider owns normalized managed groups and memberships, while
connector intake batches and attempts remain destination-local. Version-2
Patterns projections preserve stable IDs, provider source identities, names,
descriptions, status, canonical metadata, group/member source IDs, member kind
and display name, arbitrary positive revisions, and exact UTC source
timestamps. Every membership depends on its parent `directory.group`; a nested
group membership also depends on the member `directory.group`, while a subject
membership's stable Directory subject ID is self-contained and does not invent
a People identity dependency. Exports use one bounded organization-consistent
snapshot, with PostgreSQL holding a repeatable-read transaction across both
tables. Imports use Directory's opaque construction-time capability, preserve
the same stable IDs independently in each destination organization, reject
credential-shaped metadata and signed URLs, and repair one deterministic
organization-scoped audit on exact replay. Guard's imported-resource fence
remains authoritative for ordinary mutations, while provider-owned connector
reconciliation continues through its dedicated target contract.

The Threads provider owns tags, goals, revisioned include/suppress tag rules,
and asset/purchase goal links. Version-2 Patterns projections preserve stable
hierarchies, explicit inheritance behavior, target relationships, and arbitrary
positive source revisions while excluding organization IDs, timestamps, and
operator identities. Relationship records use deterministic compound IDs, and
their dependency graphs name both the owning tag or goal and the typed target.
Imports use Threads' opaque construction-time capability; exact retries repair
one organization-scoped deterministic audit event, and all ordinary hierarchy
or relationship writes pass through Guard's imported-ownership fence.

The Ledger provider owns vendors, purchase orders, contracts, commitments,
budgets, and costs. Version-2 Patterns projections preserve stable IDs,
arbitrary positive revisions, source timestamps, exact integer minor-unit
money and currency, independent contract states, asset/evidence/directory
relationships, and a cost's earliest source identity. Exports use a bounded
organization-scoped snapshot and PostgreSQL holds one repeatable-read
transaction across all six tables. Imports use Ledger's opaque
construction-time capability, exact retries repair one organization-scoped
deterministic audit event, and every ordinary Ledger mutation passes through
Guard's imported-resource ownership fence.

The Signals provider owns portable alert rules and credential-free subscriber
references. Version-2 Patterns projections preserve stable IDs, arbitrary
positive revisions, exact source timestamps, canonical threshold arrays,
enabled state, optional period/scenario filters, and typed dependencies on a
rule plus either a Reach subscriber group or webhook provider. Organization and
operator identity, routes, credentials, derived alerts, histories, and delivery
state remain excluded. Memory exports are bounded and PostgreSQL exports use
one repeatable-read snapshot across rules and subscriptions. Imports use an
opaque construction-time capability, revalidate the same-organization raw Reach
record graph without making inert imported providers operational, repair one
organization-scoped deterministic audit on exact replay, and leave ordinary rule
and subscription mutations Guard write-locked until claimed. Ordinary
subscription creation continues to require Reach's operationally ready target
catalog.

The Reach provider owns provider definitions, plain-text templates, and
subscriber groups. Version-2 Patterns projections preserve stable IDs,
arbitrary positive revisions, exact UTC source timestamps, public provider
kind/name/sender fields, template content, canonical recipients, and typed
provider/template group dependencies. Endpoint IDs and network routes, secret
references and values, enabled state, organization/operator identity, provider
tests, messages, attempts, and delivery history never enter the package or
import audit. Imports use Reach's opaque construction-time capability and one
bounded organization-consistent snapshot. Every imported provider is persisted
inert: disabled and without an endpoint or secret. After Guard ownership is
explicitly claimed, an operator must select a local endpoint while disabled,
perform a separately confirmed secret rotation, and then explicitly enable the
provider. Exact retries repair one deterministic organization-scoped audit, and
ordinary provider, template, and group mutations remain Guard write-locked until
claimed.

The Bridge provider owns only registered public OAuth clients. Version 2 uses
revision 1 for an active client and revision 2 for its irreversible revoked
state, preserving the stable ID, name, sorted exact redirect URI allowlist,
sorted scope allowlist, and optional revocation instant. Organization and
creator identity remain destination-local. Client secrets do not exist in the
Bridge public-client profile, and OAuth grants, token hashes, authorization
requests/codes, consent state, credentials, confirmations, and rate windows
have no portable payload field. Imports re-run Bridge's normal HTTPS or exact
loopback-HTTP redirect and scope validation, use an opaque construction-time
capability, persist exact/replay/conflict atomically in memory and PostgreSQL,
repair an organization-scoped deterministic audit, and leave ordinary revoke
blocked by Guard until local ownership is explicitly claimed.

### Deliberately non-portable phase-one records

| Record family | Reason |
|---|---|
| `foundation.organization` | The destination organization is a deployment-owned security boundary. |
| `atlas.label-template` | Label layouts are immutable code-owned definitions and the family has multiple active variants. |
| `guard.role`, `guard.account`, `guard.policy-bundle`, `guard.role-assignment` | Destination authorization and authentication state cannot be imported without widening privilege or transferring credentials. |
| `guard.resource-ownership` | Exchange creates destination ownership locks; importing its control plane would be circular. |
| `signals.alert` | Alerts are derived operational state and are regenerated from rules plus imported domain records. |
| `reach.message` | Queue and delivery state is operational; import could replay a message. |
| `directory.import-batch` | Connector receipts are not meaningful without deliberately excluded intake and attempt rows. |
| `exchange.package` | Package receipts are destination-local and self-referential. |
| `bridge.oauth-grant` | OAuth grants are credential-bearing session state that cannot be reconstructed without excluded token hashes. |

These exclusions are semantic and security boundaries. An unfinished domain,
including Atlas Catalog, is never excluded merely because its provider or
runtime management surface still needs implementation.

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
- a sorted registry of each record family's exact immutable Patterns template ID/version;
- each record's type, stable ID, revision, repeated exact template reference, SHA-256 checksum, schema-only typed JSON payload,
  sorted relationships, earliest known source-system/source-record provenance,
  and ownership state;
- optional file name, exact media type, byte count, checksum, and fixed internal
  archive entry; and
- no organization-selected operator identity, database key, private object key,
  provider credential, session value, access token, or signed URL.

Record checksums cover identity, revision, exact Patterns schema identity, dependencies, provenance, ownership,
file metadata, and typed payload. The archive has a separate SHA-256 checksum
returned as `X-Content-SHA256` and retained in the receipt. Import verifies both
layers before any domain write. Exchange then resolves the exact active template, verifies its record family, and calls Patterns validation before any Guard ownership registration or provider write. Unknown, mismatched, ambiguous, or retired schemas fail closed. Dependencies are topologically ordered so a
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

The server enforces all limits even when a client omits its own checks. Manifest schema `1.1` is an intentional breaking compatibility boundary: pre-Patterns `1.0` archives are rejected and must be re-exported by the source installation. Durable `1.0` receipt rows remain readable as honest historical evidence; migration `0035_exchange_patterns_schema.sql` allows both receipt labels without rewriting history, while archive decoding and every new workflow require `1.1`.

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
names, exact parameter-free media types, timestamps, counts, states, source
identities, and schema references use explicit allowlists or bounds. Typed payloads are JSON objects
and reject credential-like keys and credential-bearing or signed HTTP URLs
before a domain provider receives them. Stack and Vault export minimal Patterns-defined projections instead of full domain records, then strictly reconstruct and decode
their own provider shape and verify that envelope identity, provenance,
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
