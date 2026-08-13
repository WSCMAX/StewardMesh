<!-- REQ-DIRECTORY-EXPANSION-005 / integrations.protocols; REQ-DIRECTORY-EXPANSION-007 / platform.foundation; REQ-DIRECTORY-EXPANSION-009 / experience.help -->
# StewardMesh

**StewardMesh by Binary Cornfield** is an open-source inventory and lifecycle management platform for everything an organization owns, funds, and operates.

> Connect what you steward. Plan what comes next.

StewardMesh is being built with Go, React, Tailwind CSS, PostgreSQL, and provider interfaces for DynamoDB and S3-compatible storage.

## Development

The project standard is Go 1.26.5, Node.js 24.15+, React 19.2.8, TypeScript 7.0.2, and Tailwind CSS 4.3.3. Use the pinned versions in `go.mod`, `web/package.json`, and `web/package-lock.json`; optionally use Docker for PostgreSQL.

```sh
cp .env.example .env
set -a
. ./.env
set +a
docker compose -f deploy/docker-compose.yml up -d --wait postgres
export STEWARDMESH_TEST_DATABASE_URL="${STEWARDMESH_DATABASE_URL}"
go test ./...
go run ./cmd/stewardmesh
```

PostgreSQL is the default durable foundation adapter. For a deliberate,
non-durable evaluation without PostgreSQL, set
`STEWARDMESH_REPOSITORY_DRIVER=memory`. See
[Foundation](docs/features/foundation.md) for bootstrap, migration, audit, and
provider-contract behavior.

On the first web launch, Guard prompts for the one-time local administrator.
Local passwords use Argon2id and are never stored in plaintext. Sessions use an
opaque HttpOnly cookie, while CSRF material stays only in application memory.
OpenID Connect and SAML 2.0 are opt-in. OIDC uses authorization code flow with
state, nonce, S256 PKCE, and signed ID-token verification. SAML uses signed
SP-initiated requests, verified assertions, one-time replay-resistant request
tracking, and published SP metadata. Both create JIT Guard accounts without
storing provider tokens or assertions. See [Guard](docs/features/guard.md) for
secure local setup, provider configuration, claim mapping, permissions, and
audit behavior.

The cache driver defaults to `none`, which preserves Guard's bounded local
login limiter. Shared deployments set `STEWARDMESH_CACHE_DRIVER=valkey`, provide
a `redis://` or `rediss://` URL, and inject a separate
`STEWARDMESH_CACHE_KEY_SECRET` containing at least 32 bytes. The key secret
HMAC-protects account and client dimensions and must come from the deployment's
secret manager rather than source control.

For local Valkey evaluation, start the optional health-checked `cache` profile
and enable it in the current shell:

```sh
docker compose -f deploy/docker-compose.yml --profile cache up -d --wait postgres valkey
export STEWARDMESH_CACHE_DRIVER=valkey
export STEWARDMESH_CACHE_URL=redis://127.0.0.1:6379/0
export STEWARDMESH_CACHE_KEY_SECRET="$(openssl rand -hex 32)"
go run ./cmd/stewardmesh
```

The Compose port is bound to loopback and has no authentication or TLS, so it
is for local development only. Shared environments must use private networking,
TLS, authentication, and secret-manager injection as described in
[Valkey cache and distributed runtime](docs/architecture/valkey.md). AWS
operators should also follow the
[ElastiCache and Lambda deployment guidance](docs/deployment/aws-valkey-lambda.md),
including its current Lambda and IAM-authentication readiness boundaries.

Atlas provides the durable organization asset registry for servers and devices,
including searchable identity, People-owned location/department/user references,
purchase dates, optimistic revisions, and immutable lifecycle status history.
Memory and PostgreSQL adapters share one provider-neutral contract so a future
DynamoDB adapter and Exchange imports reuse the same validation and audit rules.
See [Atlas](docs/features/atlas.md) for permissions, API behavior, the accessible
workflow, migration details, and validation coverage.

Atlas Models (`REQ-ATLAS-MODELS-001`, `inventory.models`) adds reusable product records plus exact import resolution and
atomic bulk intake for up to 100 model-linked asset instances. Each instance
retains its own identity, assignment, lifecycle, purchase, and deployment-note
fields plus an immutable snapshot of the applied defaults, source provenance,
effective dates, and explicit overrides. Model detail adds exact lifecycle,
site, department, assigned-user, and deployment filters, optional grouped
counts, and links back to matching Atlas asset detail. Memory and PostgreSQL providers enforce the same validation. See
[Atlas Models](docs/features/atlas-models.md).

Atlas Catalog (`REQ-ATLAS-CATALOG-001`, `inventory.catalog`) extends reusable
Atlas Models with named configurations, effective-dated prices, and directed
upgrade paths instead of creating a competing product record. Its foundation
service and schema establish the lifecycle inputs that later Atlas association
and Horizon forecasting slices will consume. See
[Atlas Catalog](docs/features/atlas-catalog.md) for the boundary, price semantics,
delivery slices, and current integration status.

Patterns (`REQ-PATTERNS-001`, `templates.schemas`) provides immutable built-in
schemas for every current core record type plus organization-scoped custom
copies and versions. The same typed field metadata drives API validation, an
accessible generated record workbench, bounded typed CSV row import/export,
explicit unresolved-reference holding results, and exact schema `1.1`
validation before Exchange provider writes. See
[Patterns](docs/features/patterns.md).

Atlas Codes adds organization-scoped Code 128 and QR associations with visible
provenance, active uniqueness, optimistic replacement and deactivation history,
Guard ownership locks, redacted audits, explicit scanner workflows, immutable
physical label templates, operator-confirmed SVG/PDF output, and an internal
ZPL qualification seam that is not exposed before real-device validation. Memory and
PostgreSQL providers, REST/OpenAPI and gRPC contracts, camera/keyboard/manual
fallbacks, and accessible 320-pixel management and printing panels use the same
`REQ-ATLAS-CODES-001` rules. See [Atlas Codes](docs/features/atlas-codes.md) and
its [release validation record](docs/validation/atlas-codes-release.md).

Horizon provides effective-dated per-asset lifecycle plans, useful-life and
replacement assumptions, scenario comparisons, and deterministic fiscal-year
forecasts across Atlas, Ledger, and Threads dimensions. Forecasts preserve exact
minor-unit money, reject mixed currencies, distinguish actual, estimated,
committed, normalized-real, and TCO amounts, and expose accessible tables plus
formula-safe CSV. See [Horizon](docs/features/horizon.md) for planning semantics,
permissions, grouping and non-additive tag/goal behavior, APIs, audit history,
and validation coverage.

Threads provides organization tag and strategy hierarchies, explicit include
and suppression rules, visible inheritance provenance, and guarded goal links
for assets and purchases. Memory and PostgreSQL adapters share the same
provider-neutral relationship contract so future Ledger, Stack, and Exchange
work can reuse it. See [Threads](docs/features/threads.md) for permissions,
precedence rules, API behavior, audit events, and the accessible workflow.

Vault provides private, checksummed file storage through the same service
contract for a hardened local filesystem or S3-compatible object storage. Its
metadata preserves ownership and provenance without persisting object keys,
credentials, or temporary URLs. Shared deployments can use IAM/workload
identity, STS assume-role, or secret-manager-injected provider credentials with
mandatory server-side encryption and short-lived downloads. See
[Vault](docs/features/vault.md) for configuration, permissions, threat
boundaries, API behavior, and validation coverage.

Exchange provides selectable, dependency-aware `.openinventory` archives for
Stack and Vault records, with schema/version metadata, earliest provenance,
record and archive checksums, explicit metadata-only or included-file handling,
idempotent imports, per-record durable intents and fenced provider retries,
heartbeat-based stale-worker recovery, visible holding results, and Guard write
locks until local ownership is explicitly claimed. Packages are bounded and
never carry storage credentials, private object keys, or signed URLs. Configure
a stable deployment identity with `STEWARDMESH_EXCHANGE_SOURCE_SYSTEM_ID`
(defaulting to the organization ID). See [Exchange](docs/features/exchange.md), its
[security review](docs/validation/exchange-security-review.md), and
[release validation](docs/validation/exchange-release.md).

Ledger provides organization-scoped vendors, multi-asset purchase orders,
independent operational and financial contract states, multi-year commitments,
integer-minor-unit costs, idempotent reconciliation, fiscal-period budgets,
variance, over-budget state, and CSV export. Atlas and Vault references are
validated through their owning services, while PostgreSQL and memory adapters
share one provider-neutral financial contract. See
[Ledger](docs/features/ledger.md) for money semantics, status transitions,
permissions, APIs, security boundaries, and validation coverage.

Stack connects software products and versions to Atlas assets, preserves
purchased entitlements and assignments, and calculates explicit expiration,
over-assignment, under-use, and missing-license conditions. Signals
(`REQ-SIGNALS-001`, `alerts.rules`) evaluates those conditions alongside Ledger
and Horizon state as durable, deduplicated
alerts with acknowledgment, assignment, formula-safe reports, and an
authoritative credential-free catalog of enabled Reach targets. See [Stack](docs/features/stack.md)
and [Signals](docs/features/signals.md) for their ownership boundaries,
permissions, APIs, security reviews, and validation coverage.

Reach (`REQ-REACH-001`, `messaging.delivery`) turns those configured handoffs
into explicitly confirmed email, Teams, or webhook delivery. Deployment-owned
endpoints and external secret references keep routes and credentials out of the
browser and database responses. Teams maps one safe destination key to each
fixed channel endpoint; bounded retries and sanitized history make each attempt
reviewable. See [Reach](docs/features/reach.md) for adapter behavior,
deployment configuration, permissions, security boundaries, and validation.

People now provides sites, departments, person/shared/public/lab identities,
structured site addresses, buildings and rooms, scoped directory search, and
effective-dated multi-user asset assignments.
Its optional SailPoint Identity Security Cloud connector
(`REQ-DIRECTORY-EXPANSION-004`, `identity.directory`) normalizes identities,
accounts, account sources, departments, governance groups, roles, and
memberships through a bounded read-only exact-plan workflow with explicit
cross-provider conflicts and no provider write path.
See [People](docs/features/people.md) and
[Directory Expansion](docs/features/directory-expansion.md) for permissions,
privacy boundaries, provider contracts, location behavior, assignments, and
the guided accessible workflow. Operators should use the
[directory connector deployment runbook](docs/deployment/directory-connectors.md)
for least-privilege setup and secret rotation. The
[Directory Expansion release validation record](docs/validation/directory-expansion-release.md)
defines the security, traceability, container, integration, browser, and
accessibility gates for `REQ-DIRECTORY-EXPANSION-001` through
`REQ-DIRECTORY-EXPANSION-009`.

The current combined-branch command, browser, container, gRPC activation, and
pull-request status is tracked in the
[integrated phase-one release record](docs/validation/phase-one-release.md).

Bridge can optionally reconcile Internet2 Grouper groups, nested groups, and
memberships through its read-only SCIM v2 adapter. The default runtime does not
connect to or start Grouper. Operators configure a fixed HTTPS endpoint and
secret-manager-injected bearer token or Basic credential; a bounded loopback
fixture is available only under the Compose `integrations` profile. See
[Directory Expansion](docs/features/directory-expansion.md#grouper-rest-synchronization)
for the secure setup and fixture walkthrough.

An isolated synthetic dataset is available only for explicit demonstrations.
It requires `STEWARDMESH_SEED_SYNTHETIC=true` together with a `demo-*`
organization ID, uses clearly labeled `.invalid` data and the regular durable
directory mapping/ownership workflow, and replays idempotently. The default
runtime seeds nothing. To initialize the local demo database once, run
`docker compose -f deploy/docker-compose.yml --profile demo run --rm demo-seed`.
The optional Grouper fixture is not contacted by this initializer. See
[Directory Expansion](docs/features/directory-expansion.md#synthetic-demo-dataset).

Bridge can also import PeopleSoft Campus Solutions organizations, locations,
buildings, departments, and their hierarchy through four institution-owned,
read-only Query Access Service queries. Deployment configuration owns the fixed
endpoint, least-privilege credential, query names, and strict qualified-selector
to JSON-alias map. QAS results remain partial so missing rows never cause
implicit deactivation; the browser receives only the source ID and a non-secret
configuration revision. See [PeopleSoft Campus Solutions synchronization](docs/features/directory-expansion.md#peoplesoft-campus-solutions-synchronization)
for the query contract and secure setup.

Guide provides contextual module help and examples, dismissible and replayable
permission-aware walkthroughs, a WCAG 2.2 AA branding-color gate with safe
fallbacks, and sanitized issue-report context. See
[Guide](docs/features/guide.md) for public branding variables, accessibility
checks, privacy boundaries, and validation coverage.

Bridge (`REQ-API-001`, `SEC-MCP-001`, `integrations.protocols`) provides
documented REST/gRPC administration contracts plus authenticated MCP
`2026-07-28` over stateless Streamable HTTP and explicit local stdio. Remote
clients use OAuth authorization code with S256 PKCE, exact redirects and
resource audience, granular consent, hashed rotating credentials, and
revocation. MCP reads are bounded and redacted; the sole phase-one write,
Signals alert acknowledgement, requires a short-lived argument-bound one-use
confirmation. See [Bridge](docs/features/bridge.md) and its
[security review](docs/validation/bridge-security-review.md).

Every domain service in the checked-in protobuf descriptor has a validated
gRPC runtime adapter. The current standalone command remains Bridge-only until
the all-domain listener activation receives its deployment security approval.
A fixed adapter routes each method through the same in-process
REST application, repositories, Guard authorization, ownership, validation,
limits, audit, and error handling. Except for Guard bootstrap status, bootstrap,
and local login, it revalidates exactly one opaque Guard session bearer from
gRPC `authorization` metadata on every RPC:

```sh
STEWARDMESH_GRPC_ADDR=127.0.0.1:9090 \
go run ./cmd/stewardmesh-grpc
```

That command currently serves the approved Bridge administration surface; it
does not activate the all-domain adapter described below.

When the all-domain adapter is activated, loopback plaintext is for local
adapters only. A non-loopback address requires
both `STEWARDMESH_GRPC_TLS_CERT_FILE` and `STEWARDMESH_GRPC_TLS_KEY_FILE`; the
listener enforces TLS 1.3 or newer. Cookies, origins, and CSRF values supplied
by a gRPC client are never forwarded. The 34 MiB protobuf envelope admits the
documented 32 MiB Exchange archive plus framing; narrower domain limits still
apply. OAuth and MCP retain their native HTTP and stdio protocols rather than
being wrapped in gRPC.

For a local stdio client, first sign in through Guard and supply the current
opaque session and explicit scopes only to the child process. PostgreSQL is
required so the command sees the same Guard session as the web server:

```sh
STEWARDMESH_MCP_SESSION_TOKEN='<current-session>' \
STEWARDMESH_MCP_SCOPES='mcp:resources assets:read' \
go run ./cmd/stewardmesh-mcp
```

Do not save the session token in `.env`, client configuration committed to
source control, shell history, or logs. The command removes its credential
environment variable after reading it and reserves stdout for MCP frames.

The loopback development settings in `.env.example` deliberately use an HTTP
cookie. A shared listener must use an HTTPS allowed origin, secure cookies, and
a deployment bootstrap token containing at least 32 bytes; configuration fails
closed otherwise. Put the Go listener behind a same-origin TLS reverse proxy
and prevent direct public access to its plaintext HTTP socket.

In another terminal:

```sh
cd web
npm install
npm run typecheck
npm test
npm run dev
```

The React application uses Tailwind CSS v4's CSS-first configuration and the stable React Compiler through the Vite plugin.

The API is available at `http://localhost:8080`; the web application is available at `http://localhost:5173`.

## Product areas

See the [feature dictionary](docs/features/dictionary.md) and [requirements](docs/requirements/README.md).

## Brand and interface design

The [StewardMesh design guide](docs/design/stewardmesh-design-guide.md) defines the logo assets, color tokens, typography, component styling, and accessibility baseline used by the web application.

## Project documentation

- [Contributing](CONTRIBUTING.md)
- [Security](SECURITY.md)
- [Governance](GOVERNANCE.md)
- [Support](SUPPORT.md)
- [Releasing](RELEASING.md)

## License

Apache-2.0. See [LICENSE](LICENSE).
