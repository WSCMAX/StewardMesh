# Development toolchain

StewardMesh standardizes on the newest stable versions validated for the repository:

| Tool | Version | Source of truth |
|---|---:|---|
| Go | 1.26.5 | `go.mod` and CI |
| Node.js | 24.15+ | `web/package.json` and CI |
| React | 19.2.8 | `web/package.json` |
| TypeScript | 7.0.2 | `web/package.json` |
| Tailwind CSS | 4.3.3 | `web/package.json` |
| Vite | 8.2.1 | `web/package.json` |
| PostgreSQL | 18.4 | `deploy/docker-compose.yml` and CI |
| Valkey | 9.1.1 | `deploy/docker-compose.yml` |
| pgx | 5.10.0 | `go.mod` |
| go-oidc | 3.20.0 | `go.mod` |
| x/oauth2 | 0.36.0 | `go.mod` |
| govulncheck | 1.6.0 | `go.mod` |

Dependencies are pinned exactly and refreshed deliberately. A toolchain upgrade must update the source-of-truth files, run all checks, and document any migration or compatibility changes.
