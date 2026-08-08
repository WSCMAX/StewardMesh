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

Dependencies are pinned exactly and refreshed deliberately. A toolchain upgrade must update the source-of-truth files, run all checks, and document any migration or compatibility changes.
