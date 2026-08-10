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
See [Guard](docs/features/guard.md) for secure local setup, shared-server
requirements, permissions, audit behavior, and the planned external identity
provider boundary.

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

People now provides sites, departments, person/shared/public/lab identities,
structured site addresses, buildings and rooms, scoped directory search, and
effective-dated multi-user asset assignments.
See [People](docs/features/people.md) and
[Directory Expansion](docs/features/directory-expansion.md) for permissions,
privacy boundaries, provider contracts, location behavior, assignments, and
the guided accessible workflow.

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

## Project documentation

- [Contributing](CONTRIBUTING.md)
- [Security](SECURITY.md)
- [Governance](GOVERNANCE.md)
- [Support](SUPPORT.md)
- [Releasing](RELEASING.md)

## License

Apache-2.0. See [LICENSE](LICENSE).
