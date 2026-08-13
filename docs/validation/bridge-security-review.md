# Bridge security review

Reviewed 2026-08-13 for `REQ-API-001`, `SEC-MCP-001`, `integrations.protocols`, and issue #14.

## Threat decisions

- **Credential theft/replay:** Persist only SHA-256 hashes; short access/code/confirmation lifetimes; rotating refresh tokens; atomic one-use code, refresh, and confirmation updates; revocation cascades from clients.
- **OAuth mix-up/injection:** Exact pre-registered redirects, S256 PKCE, issuer response parameter, exact resource indicator at authorization and token exchange, same-origin issuer/resource configuration, bearer-header-only MCP auth.
- **Cross-tenant access:** Organization ID comes only from server configuration and current Guard records; stores key every record by organization; MCP output omits the organization field.
- **Stale or excessive authority:** Every request reloads the active client and current Guard account/grants. OAuth scope narrows rather than replaces Guard. Asset and directory reads honor scoped grants.
- **Confused-deputy writes:** The sole phase-one write requires prepare and confirm. Confirmation is random, short-lived, actor/organization/action/arguments bound, and one-use; revision and Guard ownership are checked.
- **Prompt/data injection:** Stored text is returned only as escaped JSON data with an untrusted-data notice. There is no dynamic execution, prompt concatenation, SQL, shell, path, URL fetch, or arbitrary provider target.
- **Resource exhaustion:** 64 KiB HTTP bodies, one-object JSON-RPC, at most 25 results, 200-character searches, eight-second deadlines, eight concurrent calls, and client/actor/IP rate windows.
- **Secret/error leakage:** Admin views omit tokens and hashes; MCP views exclude sensitive domain fields; public errors are fixed; stdout is protocol-only for stdio; logs never receive raw credentials.
- **Browser risks:** Administrative writes use the existing HttpOnly session, same-origin CSRF validation, and no-store responses. React has no raw HTML or browser token persistence. Consent external navigation validates scheme and userinfo after the server chooses the exact registered redirect.

## Accepted phase-one limits

- Protobuf documents Bridge administration parity, but no gRPC listener is deployed yet.
- OAuth dynamic client registration, confidential client secrets, device flow, client credentials, token introspection, wildcard redirects, and third-party authorization servers are intentionally unsupported.
- The confirmed MCP write surface contains only Signals alert acknowledgement. More writes require a separate threat review and the same two-step contract.
- Rate windows are durable PostgreSQL/memory records rather than shared Valkey counters; they are authoritative per application repository and intentionally fail closed on store errors.
