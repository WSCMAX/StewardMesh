# AWS Valkey and Lambda deployment guidance

- **Canonical feature:** `platform.runtime`
- **Requirement:** `REQ-PLATFORM-VALKEY-001`
- **Roadmap issue:** [#41](https://github.com/WSCMAX/StewardMesh/issues/41)

This guide defines the AWS target for StewardMesh's shared cache and future
Lambda HTTP runtime. It is operational guidance, not infrastructure as code.
The repository now includes reusable, transport-neutral application
construction, but it does not yet include a Lambda handler. The remaining
Lambda portions are readiness requirements rather than a claim that the
current server binary can be deployed unchanged.

## Deployment readiness

| Target | Status | Authentication |
|---|---|---|
| Long-running container with ElastiCache Serverless for Valkey | Configuration path implemented; validate against the target cache | Password-authenticated ElastiCache user supplied through `rediss://` |
| Reusable application construction | Implemented | Load secrets before construction and keep migrations disabled in request runtimes |
| Lambda HTTP handler | Not implemented | Add the transport adapter, secret loading, and configurable database pool before deployment; the S3-compatible Vault adapter is available |
| ElastiCache IAM authentication | Not implemented | Add a SigV4 credential provider and token refresh before enabling it |

The cache remains reconstructible and must not contain sessions, CSRF values,
grants, permissions, audit records, authoritative domain data, import state, or
idempotency records. PostgreSQL remains the source of truth.

## ElastiCache Serverless baseline

Create an ElastiCache Serverless cache with the `valkey` engine in the same VPC
as the application. Select private subnets in at least two Availability Zones,
associate a dedicated cache security group, and attach a Valkey user group.
Serverless caches expose VPC endpoints and always encrypt traffic in transit;
StewardMesh must therefore use `rediss://` and must never disable certificate
verification. Serverless operates in cluster mode, and the current Valkey Go
client discovers the cluster from the configured endpoint. See AWS's
[serverless deployment comparison](https://docs.aws.amazon.com/AmazonElastiCache/latest/dg/WhatIs.deployment.html),
[serverless cache options](https://docs.aws.amazon.com/cli/latest/reference/elasticache/create-serverless-cache.html),
and [ElastiCache TLS guidance](https://docs.aws.amazon.com/AmazonElastiCache/latest/dg/in-transit-encryption.html).

Use the endpoint address and port returned by ElastiCache rather than deriving
them. StewardMesh writes rate-limit counters, so configure the read/write
endpoint in `STEWARDMESH_CACHE_URL`. Use database `0` for compatibility across
Valkey versions and Redis-compatible targets even when a newer engine supports
additional cluster databases. Allow both serverless endpoint ports advertised
to the client when cluster discovery uses the read-optimized endpoint; AWS
[currently documents](https://docs.aws.amazon.com/AmazonElastiCache/latest/dg/set-up.html)
`6379` for primary traffic and `6380` for read-optimized traffic.

Set a customer-managed KMS key when organizational policy requires control of
at-rest encryption. Snapshot retention is optional because StewardMesh cache
data is ephemeral and reconstructible; do not use a cache snapshot as a backup
of application state.

## Runtime configuration and secrets

The current adapter supports a password-authenticated Valkey user through the
existing environment contract:

```text
STEWARDMESH_CACHE_DRIVER=valkey
STEWARDMESH_CACHE_URL=rediss://<username>:<percent-encoded-password>@<endpoint-address>:<endpoint-port>/0
STEWARDMESH_CACHE_KEY_SECRET=<independent-secret-containing-at-least-32-bytes>
```

Store the complete cache URL and the independent cache-key HMAC secret as two
separate AWS Secrets Manager values. The HMAC secret is not the ElastiCache
password and must rotate independently. Percent-encode the URL username and
password before constructing the URL. Never put either resolved value in an
image, task definition, source file, shell history, log field, metric, trace, or
error response.

For a long-running container, use the platform's native Secrets Manager
injection so the task definition contains secret ARNs rather than plaintext.
For the future Lambda handler, load secrets once during execution-environment
initialization and cache them with a bounded refresh interval. AWS documents
the [Parameters and Secrets Lambda Extension](https://docs.aws.amazon.com/lambda/latest/dg/with-secrets-manager.html)
as a runtime-neutral cached retrieval option. A private Lambda subnet without
NAT requires a [Secrets Manager interface VPC endpoint](https://docs.aws.amazon.com/secretsmanager/latest/userguide/vpc-endpoint-overview.html).

Grant the task or function role `secretsmanager:GetSecretValue` only for these
secret ARNs and `kms:Decrypt` only when the selected customer-managed key
requires it. Secret retrieval failure must fail initialization; it must not
silently select the local limiter after `valkey` has been requested.

### Password rotation

ElastiCache users can hold two passwords during rotation. Rotate without
downtime by adding the new password, updating Secrets Manager, recycling every
container or Lambda execution environment so new clients use it, verifying
authentication metrics, and only then removing the old password. The current
client reads credentials at construction time and does not reload a changed
secret in place.

## Valkey RBAC

Use a dedicated password-authenticated Valkey user and user group. ElastiCache
RBAC is the access-control mechanism for serverless caches; do not leave an
unauthenticated default user enabled. See AWS's [Valkey RBAC guidance](https://docs.aws.amazon.com/AmazonElastiCache/latest/dg/Clusters.RBAC.html).

Restrict the user to the `~stewardmesh:*` key pattern. Its application commands
are `GET`, `SET`, `DEL`, `EXISTS`, `INCR`, `PEXPIRE`, `EVAL`, and `PING`.
The client also uses `HELLO`, `CLUSTER SHARDS`, and `CLUSTER SLOTS` for its
connection handshake and serverless cluster discovery. It may attempt `CLIENT
SETINFO`, but tolerates a denied or unsupported response. Validate the final
access string against a staging cache because command and subcommand syntax
follows the deployed Valkey engine version. Do not grant administrative, ACL
mutation, flush, persistence, or shutdown commands.

IAM authentication uses short-lived SigV4 tokens and requires
`elasticache:Connect` permission for both the cache and ElastiCache user. It is
preferred over a long-lived password once StewardMesh supplies a refreshable
credential provider. The current URL-only adapter cannot generate or renew
those tokens, so enabling IAM authentication now would fail after credentials
expire. See AWS's [ElastiCache IAM authentication requirements](https://docs.aws.amazon.com/AmazonElastiCache/latest/dg/auth-iam.html).

## VPC and security groups

Attach the future Lambda function to private subnets in the cache VPC. Use
security-group references instead of CIDR-wide rules:

| Security group | Direction | Peer | Port |
|---|---|---|---|
| Application or Lambda | Outbound | ElastiCache security group | Returned primary and read-optimized endpoint ports |
| ElastiCache | Inbound | Application or Lambda security group | Returned primary and read-optimized endpoint ports |
| Application or Lambda | Outbound | Secrets Manager endpoint security group | TCP 443 |
| Secrets Manager endpoint | Inbound | Application or Lambda security group | TCP 443 |

Enable VPC DNS support and DNS hostnames so the ElastiCache endpoint resolves.
Do not expose the cache publicly and do not add inbound rules to Lambda. If the
function needs public APIs, route private-subnet egress through NAT; placing a
function in a public subnet does not itself provide internet access. See the
[Lambda VPC networking guide](https://docs.aws.amazon.com/lambda/latest/dg/configuration-vpc.html).

If network ACLs are used, allow the full Lambda ephemeral port range documented
by AWS in addition to the destination ports. Prefer security groups alone when
an additional stateless network boundary is not required.

## Lambda construction requirements

`internal/application.New` now constructs the repositories, services, Valkey
client, and transport-neutral HTTP handler once. It exposes the handler,
organization metadata, and idempotent cleanup. Its explicit `RunMigrations`
option lets `cmd/stewardmesh` preserve startup migrations while a future Lambda
adapter leaves them disabled.

A future Lambda transport must apply these remaining runtime rules:

1. Load configuration and Secrets Manager values, then call
   `application.New` once outside the invocation path. Do not reconstruct the
   PostgreSQL pool, Valkey client, services, or HTTP handler for each request.
2. Reuse clients across warm invocations and keep connections alive, while
   treating every execution environment as disposable. AWS recommends
   [execution-environment and connection reuse](https://docs.aws.amazon.com/lambda/latest/dg/best-practices.html).
3. Apply the invocation context and deadline to every database, cache, and HTTP
   operation. Leave time for a controlled error response before the Lambda
   timeout.
4. Keep the Valkey startup `PING`; initialization must fail when a configured
   shared limiter cannot connect. A later outage must continue to return the
   safe service-unavailable response for login attempts.
5. Pass `RunMigrations: false` and run database migrations and synthetic
   seeding as deployment jobs, never in cold-start initialization.
6. Point PostgreSQL at RDS Proxy and make the application pool configurable.
   The current fixed 20-connection pool is too large to multiply across
   unconstrained Lambda environments. AWS documents the pooling and surge
   protection provided by [RDS Proxy](https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/rds-proxy.html).
7. Set reserved concurrency and database pool limits from tested downstream
   capacity. Do not use Lambda concurrency as a substitute for application rate
   limiting.
8. Configure `STEWARDMESH_STORAGE_DRIVER=s3` and the Vault bucket, region,
   encryption, and IAM role before horizontal or Lambda deployment. The Lambda
   `/tmp` directory is temporary workspace, not authoritative storage.

Do not depend on cleanup running when Lambda freezes or discards an execution
environment. `Application.Close` exists for tests and long-running processes,
but runtime correctness must remain independent of it.

## Observability and rollout validation

Publish application metrics for cache operation latency, errors, Guard
fail-closed decisions, and startup failures without recording keys or URLs.
Alarm on the serverless `AWS/ElastiCache` metrics `AuthenticationFailures`,
`KeyAuthorizationFailures`, `CommandAuthorizationFailures`, `ThrottledCmds`,
`CurrConnections`, `NewConnections`, `Evictions`, and successful read/write
latency. AWS lists these in the [serverless Valkey metrics reference](https://docs.aws.amazon.com/AmazonElastiCache/latest/dg/serverless-metrics-events-redis.html).

Before production rollout, verify from the same subnets and security group as
the workload that:

- DNS resolves the returned serverless endpoint and TLS hostname validation
  succeeds.
- The dedicated user can start the application and execute Guard's increment,
  read, expiry, and delete path, but forbidden commands fail.
- Invalid credentials and an unreachable cache fail startup without leaking the
  URL, username, password, or HMAC secret.
- A post-start cache outage makes login protection fail closed and recover after
  service restoration; it never switches to per-environment counters.
- Password rotation succeeds across recycled execution environments before the
  old password is removed.
- Load tests keep connection counts, ECPUs, throttling, latency, Lambda
  concurrency, and database connections inside alarms and service quotas.
- PostgreSQL migrations and blob persistence remain outside the invocation
  initialization path.

AWS infrastructure as code remains intentionally out of this repository until
the project selects a provisioning framework. Any future stack must implement
this guide without embedding account IDs, subnet IDs, security-group IDs,
endpoints, or secrets in application source.
