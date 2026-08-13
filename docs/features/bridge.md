# Bridge — REST, gRPC, OAuth, and MCP

- **Canonical ID:** `integrations.protocols`
- **Requirements:** `REQ-API-001`, `SEC-MCP-001`
- **Roadmap issue:** [#14](https://github.com/WSCMAX/StewardMesh/issues/14)
- **Protocol/SDK:** MCP `2026-07-28`; official Go SDK `github.com/modelcontextprotocol/go-sdk` `v1.7.0`

## Purpose and boundaries

Bridge exposes useful StewardMesh reads and a deliberately tiny confirmed-write surface without creating a second authorization system. Guard remains authoritative for the current account, organization, permission grants, scoped asset and directory visibility, resource ownership, and audit identity. Atlas, People, and Signals remain authoritative for their records. Bridge stores only registered public clients, short-lived consent state, hashed credentials, revocable grant metadata, hashed confirmations, and hashed abuse-control dimensions.

Bridge never accepts SQL, shell commands, filesystem paths, arbitrary URLs, dynamic code, provider credentials, or token passthrough. It does not fetch URLs. Stored names, titles, and summaries are returned as bounded JSON data with an explicit untrusted-data notice; they are never interpreted as instructions. Email, identity-provider fields, raw deployment notes, provider payloads, organization identifiers in MCP views, credentials, token hashes, and internal errors are excluded.

## OAuth 2.1 profile

Remote MCP clients are pre-registered public clients. Each has one through ten exact redirect URIs and an allowlist drawn from `mcp:resources`, `assets:read`, `directory:read`, `signals:read`, and `signals:acknowledge`. Redirects require HTTPS except exact loopback HTTP development URIs. Wildcards, fragments, userinfo, open redirects, and client secrets are prohibited.

Authorization uses the code flow with mandatory S256 PKCE. The authorization request binds client, exact redirect, actor, organization, resource audience, scopes, state, and code challenge for ten minutes. Consent shows the client and exact scopes to the signed-in actor. An approved server-generated code expires after two minutes and is consumed atomically once. The token endpoint requires the same client, redirect, resource, and verifier. Access tokens expire after 15 minutes; refresh tokens expire after eight hours and rotate atomically on every use. Only SHA-256 credential hashes are persisted. Client, grant, and token revocation take effect on the next request.

The authorization and token requests both carry the exact `/mcp` resource indicator. Bearer credentials are accepted only in the `Authorization` header, and every MCP request re-loads the active client and current Guard account/grants. Protected-resource metadata follows RFC 9728. Authorization-server metadata advertises the code and refresh grants, S256, public clients, scopes, revocation, and issuer response parameter. Client, actor, and source-IP dimensions have bounded per-minute rate windows whose persisted keys are hashes.

## MCP transports and tools

Remote MCP uses sessionless Streamable HTTP at `POST /mcp`. Requests must use `application/json`, carry matching `MCP-Protocol-Version: 2026-07-28` and MCP per-request metadata, contain one JSON-RPC object rather than a batch, and fit within 64 KiB. SDK cancellation propagates to service calls. Each call has an eight-second deadline, result pages contain at most 25 records, and one process admits at most eight concurrent tool/resource calls. Standard library cross-origin protection and SDK localhost DNS-rebinding protection remain enabled.

Local stdio uses `go run ./cmd/stewardmesh-mcp`. It requires the shared PostgreSQL repository plus `STEWARDMESH_MCP_SESSION_TOKEN` and an explicit `STEWARDMESH_MCP_SCOPES` list. Bridge validates the current HttpOnly-session credential through Guard, binds it to the configured organization and requested scopes, then removes the credential environment variable. Stdout is protocol-only; sanitized lifecycle diagnostics go to stderr. The Guard session is not converted into or stored as an OAuth token.

Advertised resources and tools are reduced to the grant:

- inventory report, exact asset resource, and `search_assets` for authorized Atlas scope;
- directory report and `search_directory`, excluding email/provider identity fields;
- Signals report, exact alert resource, and `list_alerts`; and
- `prepare_acknowledge_alert` plus `confirm_acknowledge_alert` when both the OAuth scope and current `signals.write` grant are present.

Every write is two-step. Prepare rechecks current permission, record revision, status, and Guard ownership, then generates a random confirmation token bound to organization, actor, action, and canonical exact arguments. It expires after two minutes. Confirm rechecks permission and ownership, atomically consumes that exact token once, and only then calls Signals with the bound alert ID and revision. Changed arguments, another actor, expiration, replay, a stale revision, a revoked grant, or a now-disabled account fails closed.

## REST and gRPC parity

| Operation | REST | gRPC | Parity |
| --- | --- | --- | --- |
| List/register/revoke OAuth clients | `/api/v1/bridge/clients` | `BridgeService.ListClients/CreateClient/RevokeClient` | Same fields, limits, Guard permissions, organization, and audit behavior |
| List/revoke grants | `/api/v1/bridge/grants` | `BridgeService.ListGrants/RevokeGrant` | Same redacted metadata and revocation semantics |
| OAuth metadata, authorize, token, revoke, consent redirect | Well-known, `/oauth/*`, consent API | Not exposed | Intentionally HTTP/browser protocol operations |
| MCP remote and local transports | `/mcp`, stdio command | Not exposed | MCP owns its wire protocol; wrapping JSON-RPC in gRPC would not add parity |

The checked-in OpenAPI and protobuf definitions are validated in CI. A contract check asserts the shared administrative operation set while documenting the intentional OAuth/MCP transport gaps. The current application has no deployed gRPC listener; protobuf is the versioned provider-neutral contract for a future adapter. REST is the executable administration transport in phase one.

## Administration and accessibility

Workspace includes Bridge for users with `integrations.read`. It lists clients and grants without raw credentials. Organization writers can register exact redirect URIs and narrow scopes, revoke clients or grants, and respond to a consent card. Consent navigation accepts only the server-selected registered HTTPS or loopback HTTP redirect. Native labels, fieldsets, status/alert regions, text status badges, keyboard controls, and wrapping identifiers keep the flow understandable at desktop and 320-pixel widths.

## Security review and validation

The implementation was reviewed against the repository's Go backend, JavaScript frontend, and React secure-development guidance. PostgreSQL statements are parameterized; random credentials use `crypto/rand`; secret comparisons and hashes stay server-side; OAuth and confirmation exchanges are atomic; HTTP bodies, fields, lists, rates, concurrency, and deadlines are bounded; errors are stable and non-sensitive; no token is logged or returned by administrative APIs; React renders server data through escaped text nodes; no raw HTML or persistent browser credential storage is used; and external navigation is restricted to the exact server-selected registered redirect with an HTTPS/loopback scheme check.

Validation covers memory and PostgreSQL store contracts, organization isolation, defensive copies, code/refresh/confirmation replay, exact redirect/resource/PKCE binding, revocation, rates, OAuth-to-remote-MCP smoke, JSON-RPC batch rejection, stdio discovery, malformed and adversarial inputs, UI response validation, CSRF, accessibility automation, desktop and 320-pixel browser journeys, OpenAPI/protobuf/traceability gates, race tests, vulnerability scanning, Compose, and a non-root container smoke.

## Primary protocol references

- [MCP versioning](https://modelcontextprotocol.io/specification/versioning) and the [2026-07-28 specification](https://modelcontextprotocol.io/specification/2026-07-28) define the pinned protocol date.
- [Official MCP Go SDK releases](https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.7.0) and [server documentation](https://github.com/modelcontextprotocol/go-sdk/blob/v1.7.0/mcp/server.md) define the pinned SDK and transport behavior.
- [MCP authorization](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization) defines protected-resource discovery, resource indicators, bearer usage, and the stdio credential boundary.
- [RFC 9728](https://www.rfc-editor.org/rfc/rfc9728), [RFC 8707](https://www.rfc-editor.org/rfc/rfc8707), [RFC 7636](https://www.rfc-editor.org/rfc/rfc7636), [RFC 9700](https://www.rfc-editor.org/rfc/rfc9700), [RFC 8414](https://www.rfc-editor.org/rfc/rfc8414), and [RFC 9207](https://www.rfc-editor.org/rfc/rfc9207) define protected-resource metadata, resource audience binding, S256 PKCE, OAuth security guidance, authorization-server metadata, and issuer response binding.
