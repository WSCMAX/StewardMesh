# StewardMesh

**StewardMesh by Binary Cornfield** is an open-source inventory and lifecycle management platform for everything an organization owns, funds, and operates.

> Connect what you steward. Plan what comes next.

StewardMesh is being built with Go, React, Tailwind CSS, PostgreSQL, and provider interfaces for DynamoDB and S3-compatible storage.

## Development

The project standard is Go 1.26.5, Node.js 24.15+, React 19.2.8, TypeScript 7.0.2, and Tailwind CSS 4.3.3. Use the pinned versions in `go.mod`, `web/package.json`, and `web/package-lock.json`; optionally use Docker for PostgreSQL.

```sh
cp .env.example .env
go test ./...
go run ./cmd/stewardmesh
```

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
