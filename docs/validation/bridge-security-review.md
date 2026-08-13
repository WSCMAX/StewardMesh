# Bridge security review

Reviewed 2026-08-13 for `REQ-API-001`, `SEC-MCP-001`, `integrations.protocols`, and issue #14.

## Threat decisions

- **Credential theft/replay:** Persist only SHA-256 hashes; short access/code/confirmation lifetimes; rotating refresh tokens; atomic one-use code, refresh, and confirmation updates; revocation cascades from clients.
- **OAuth mix-up/injection:** Exact pre-registered redirects, S256 PKCE, issuer response parameter, exact resource indicator at authorization and token exchange, same-origin issuer/resource configuration, bearer-header-only MCP auth.
- **Cross-tenant access:** Organization ID comes only from server configuration and current Guard records; stores key every record by organization; MCP output omits the organization field.
- **Stale or excessive authority:** Every remote operation reloads the active client and current Guard account/grants and requires current `integrations.read` plus its domain permission. Stdio revalidates the originating session, expiry, account, and grants per operation without widening its original scope list. OAuth scope narrows rather than replaces Guard. Asset and directory reads honor scoped grants.
- **Confused-deputy writes:** The sole phase-one write requires prepare and confirm. Confirmation is random, short-lived, actor/organization/action/arguments bound, and one-use; revision and Guard ownership are checked.
- **Prompt/data injection:** Stored text is returned only as escaped JSON data with an untrusted-data notice. There is no dynamic execution, prompt concatenation, SQL, shell, path, URL fetch, or arbitrary provider target.
- **Resource exhaustion:** Direct-IP limiting happens before bearer authentication/body reads, actor/client limiting happens after authentication and before JSON decode, and limits emit HTTP 429 plus `Retry-After`. MCP retains 64 KiB bodies, one-object JSON-RPC, at most 25 results, 200-character searches, eight-second deadlines, and eight concurrent calls. Administration lists default to 25 and cannot exceed 100; PostgreSQL serializes the 50-active-client capacity check.
- **Secret/error leakage and audit:** Admin views omit tokens and hashes; MCP views exclude sensitive domain fields; public errors are fixed; stdout is protocol-only for stdio; logs never receive raw credentials. Every MCP resource/tool operation writes bounded actor/client/grant/method/resource/count/outcome metadata without query or record content and fails closed if auditing fails.
- **Browser risks:** Administrative writes use the existing HttpOnly session, same-origin CSRF validation, and no-store responses. React has no raw HTML or browser token persistence. Consent external navigation validates scheme and userinfo after the server chooses the exact registered redirect.

## Verified transport boundary

- Generated Go protobuf and gRPC bindings are checked in with an explicit all-domain runtime adapter. Descriptor-driven tests prove all 16 services and 154 RPCs can be registered; real in-memory calls exercise every route plus Bridge, Atlas Codes, Patterns, Vault, and Exchange state/byte parity against the same application as REST. The production command remains on its narrower approved Bridge-only registration until all-domain activation receives explicit deployment approval.
- The standalone `stewardmesh-grpc` listener defaults to loopback. Non-loopback binding requires configured TLS certificate/key files and TLS 1.3 or newer. In the all-domain adapter, except for the three Guard bootstrap/login operations, every RPC revalidates exactly one bounded opaque Guard session bearer before decoding. Client cookies, origin, and CSRF metadata are stripped, while browser CSRF state and values are not exposed or rotated.

## Accepted phase-one limits

- OAuth dynamic client registration, confidential client secrets, device flow, client credentials, token introspection, wildcard redirects, and third-party authorization servers are intentionally unsupported.
- The confirmed MCP write surface contains only Signals alert acknowledgement. More writes require a separate threat review and the same two-step contract.
- Rate windows are durable PostgreSQL/memory records rather than shared Valkey counters; they are authoritative per application repository and intentionally fail closed on store errors.
