# Valkey cache and distributed runtime

- **Canonical feature:** `platform.runtime`
- **Requirement:** `REQ-PLATFORM-VALKEY-001`
- **Roadmap issue:** [#41](https://github.com/WSCMAX/StewardMesh/issues/41)

**Traceability status:** the provider-neutral contract, disabled mode, bounded
in-memory adapter, official Valkey Go client adapter, validated connection URL,
namespaced key builder, distributed Guard login limiter, runtime configuration,
optional self-hosted Compose service, and their validation are implemented.
AWS ElastiCache and Lambda deployment guidance is also documented. Reusable
application construction remains tracked by issue #41.

## Decision

Valkey is StewardMesh's golden-path shared cache and coordination backend.
The application uses the Redis wire protocol so operators may connect a
compatible Redis deployment with the same configuration contract. PostgreSQL
remains authoritative for domain records, sessions, permissions, imports,
reconciliation state, audit history, and idempotency records.

The cache is reconstructible. A cache outage may reduce availability or deny
security-sensitive operations, but it must never make cache contents the only
copy of an auditable or authoritative record.

## Runtime modes

`STEWARDMESH_CACHE_DRIVER` selects the cache mode:

- `none`: no shared cache; local rate limiting remains available for local
  development.
- `memory`: the cache-backed limiter over deterministic bounded in-memory
  storage for tests and isolated evaluation.
- `valkey`: shared Valkey or Redis-compatible service.

`STEWARDMESH_CACHE_URL` accepts `redis://` for local or plaintext private
networks and `rediss://` for TLS deployments such as AWS ElastiCache Serverless
Valkey. Secrets must be injected by the deployment environment and must never
be logged.

`STEWARDMESH_CACHE_KEY_SECRET` is required in `valkey` mode and must contain at
least 32 bytes from a deployment secret manager. It is independent from the
cache password and HMAC-protects low-entropy account and direct-client
dimensions before namespaced key construction. It is intentionally absent in
`none` and `memory` modes; the memory runtime generates an ephemeral process
secret.

The Valkey adapter uses the official `github.com/valkey-io/valkey-go` client,
supports standalone, cluster, and sentinel URL options, disables the client's
separate in-process response cache, and uses Valkey-first names in application
code. URL validation rejects unsupported schemes and reports errors without
including credentials. TLS URLs cannot disable certificate verification.

The container deployment includes an optional health-checked Valkey service in
the `cache` and `demo` Compose profiles. The application defaults to `none`, so
existing local development does not require the service. A deployment enables
the shared backend explicitly.

## Boundaries and keys

The cache interface is provider-neutral and exposes only context-aware get,
set-with-TTL, delete, atomic increment, ping, and close operations. Domain
interfaces remain authoritative and do not gain cache invalidation methods.
Future read caching uses decorators around those interfaces.

Keys include a deployment prefix, schema version, organization ID, resource
kind, and every visibility/filter dimension that affects the result. Cached
directory or graph responses must never be reusable across organizations or
Guard-derived scopes. Variable dimensions are SHA-256 hashed before they enter a
key so raw account names, client addresses, filters, and other values are not
accidentally embedded in cache tooling or logs. Hashing is not a confidentiality
boundary; callers HMAC low-entropy sensitive identifiers before key creation.

Writes commit to the authoritative store first. Cache entries are deleted or
their namespace generation is advanced only after a successful write. Future
cross-container invalidation should use a transactional outbox or equivalent
committed change event. Pub/Sub alone is not a durable invalidation mechanism.
Short TTLs and jitter are a recovery backstop, not a consistency guarantee.

## Guard rate limiting

The first active Valkey use is distributed login rate limiting. Each direct
client and normalized-account counter is incremented atomically with a fixed
first-failure TTL, so replicas share the same five-failure, fifteen-minute
window. Successful login clears both counters.

When caching is disabled, Guard uses the bounded local limiter. When Valkey is
explicitly enabled, startup verifies connectivity. A later cache outage causes
login protection to fail closed with a safe service-unavailable response.
Sessions, CSRF hashes, account status, grants, permission decisions, and
authenticated HTTP responses remain uncached.

## Deployment guidance

For local evaluation, the Compose `cache` profile publishes Valkey only on
`127.0.0.1:6379` and uses `redis://127.0.0.1:6379/0`. It disables snapshot and
append-only persistence because the cache is reconstructible, limits memory to
128 MB, and uses `noeviction`. Reaching the limit therefore returns an error;
Guard's cache-backed limiter fails closed instead of silently losing counters.
The local service has no authentication or TLS and must not be exposed to a
shared network.

For containers, use PostgreSQL as the source of truth, Valkey for shared
ephemeral state, and an S3-compatible store for blobs before running multiple
API replicas. Configure TLS and secret injection for shared environments.

For AWS-managed Valkey, use `rediss://`, private VPC networking, dedicated
security groups and RBAC, and Secrets Manager injection. Lambda deployments must
treat process memory as disposable, initialize clients outside the invocation
path, use a bounded database pool with RDS Proxy, and keep migrations outside
cold-start initialization. The operational requirements and current adapter
limitations are defined in
[AWS Valkey and Lambda deployment guidance](../deployment/aws-valkey-lambda.md).
AWS infrastructure as code is intentionally not selected until the repository
adopts a provisioning framework.

## Observability and validation

Expose cache hit/miss counts, operation latency, errors, evictions, rate-limit
failures, and fallback or fail-closed decisions. Tests cover no-op and memory
implementations, URL/TLS configuration, TTL expiry, atomic concurrency,
organization isolation, cache outage behavior, and Redis-compatible endpoints.
