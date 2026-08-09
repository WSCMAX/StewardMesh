# Valkey cache and distributed runtime

- **Canonical feature:** `platform.runtime`
- **Requirement:** `REQ-PLATFORM-VALKEY-001`
- **Roadmap issue:** [#41](https://github.com/WSCMAX/StewardMesh/issues/41)

**Traceability status:** planned contract and deployment decision. Implementation
artifacts will replace the architecture placeholders as issue #41 is delivered.

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
- `memory`: deterministic bounded in-memory behavior for tests and isolated
  evaluation.
- `valkey`: shared Valkey or Redis-compatible service.

`STEWARDMESH_CACHE_URL` accepts `redis://` for local or plaintext private
networks and `rediss://` for TLS deployments such as AWS ElastiCache Serverless
Valkey. Secrets must be injected by the deployment environment and must never
be logged.

The container deployment includes an optional health-checked Valkey service.
The application defaults to `none`, so existing local development does not
require the service. A deployment enables the shared backend explicitly.

## Boundaries and keys

The cache interface is provider-neutral and exposes only context-aware get,
set-with-TTL, delete, atomic increment, ping, and close operations. Domain
interfaces remain authoritative and do not gain cache invalidation methods.
Future read caching uses decorators around those interfaces.

Keys include a deployment prefix, schema version, organization ID, resource
kind, and every visibility/filter dimension that affects the result. Cached
directory or graph responses must never be reusable across organizations or
Guard-derived scopes.

Writes commit to the authoritative store first. Cache entries are deleted or
their namespace generation is advanced only after a successful write. Future
cross-container invalidation should use a transactional outbox or equivalent
committed change event. Pub/Sub alone is not a durable invalidation mechanism.
Short TTLs and jitter are a recovery backstop, not a consistency guarantee.

## Guard rate limiting

The first active Valkey use is distributed login rate limiting. Client and
normalized-account counters are updated atomically with TTLs, so replicas share
the same five-failure window. Successful login clears both counters.

When caching is disabled, Guard uses the bounded local limiter. When Valkey is
explicitly enabled but unavailable, login protection fails closed with a safe
service-unavailable response. Sessions, CSRF hashes, account status, grants,
permission decisions, and authenticated HTTP responses remain uncached.

## Deployment guidance

For containers, use PostgreSQL as the source of truth, Valkey for shared
ephemeral state, and an S3-compatible store for blobs before running multiple
API replicas. Configure TLS and secret injection for shared environments.

For AWS-managed Valkey, use `rediss://`, private VPC networking, security
groups, and a secret manager supplied by the deployment platform. Lambda
deployments must treat process memory as disposable, initialize clients outside
the invocation path when safe, use a small database pool with RDS Proxy, and
keep migrations outside cold-start initialization. AWS infrastructure-as-code
is intentionally not selected until the repository adopts an AWS provisioning
framework.

## Observability and validation

Expose cache hit/miss counts, operation latency, errors, evictions, rate-limit
failures, and fallback or fail-closed decisions. Tests cover no-op and memory
implementations, URL/TLS configuration, TTL expiry, atomic concurrency,
organization isolation, cache outage behavior, and Redis-compatible endpoints.
