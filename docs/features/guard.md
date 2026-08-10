# Guard — Authentication, roles, policies, and audit

- **Canonical ID:** `authorization.security`
- **Requirements:** `SEC-GUARD-001`, `SEC-HTTP-001`, `A11Y-001`, `DOC-001`, `DOC-002`, `REQ-PLATFORM-VALKEY-001`
- **Roadmap issue:** [#13](https://github.com/WSCMAX/StewardMesh/issues/13)

## Purpose

Guard protects organization and resource operations. This first delivery slice provides one-time local administrator bootstrap, local password authentication, opaque server-side sessions, synchronized CSRF protection, organization-scoped roles, reusable policy bundles, permission enforcement, rate limiting, and security audit events.

OIDC/OAuth, SAML, just-in-time provisioning, external group mapping, custom role management, ownership locks, and the role-building interface remain planned Guard slices. Issue #13 stays open until those acceptance criteria are delivered.

## One-time administrator setup

On a new organization, the web application opens an accessible **Create the first administrator** form. The operation is atomic: it creates the account, the Core administration policy bundle, the Administrator role, and an organization-scoped role assignment exactly once.

The default loopback listener allows setup from the configured browser origin or a loopback client. Any non-loopback listener fails configuration validation unless it uses:

- an HTTPS `STEWARDMESH_ALLOWED_ORIGIN`
- `STEWARDMESH_SESSION_COOKIE_SECURE=true`
- a `STEWARDMESH_BOOTSTRAP_TOKEN` containing at least 32 bytes

A non-loopback Go listener is intended to sit on a private interface behind a same-origin, TLS-terminating reverse proxy. Firewall the listener from direct public access, preserve the browser `Origin` header, and configure HSTS at the TLS edge. Guard deliberately uses the direct socket address rather than trusting client-controlled forwarded headers.

The bootstrap token is entered only during setup, is compared in constant time, is never persisted, and must not be logged or included in issue reports.

## Password protection

Local passwords:

- require at least 15 Unicode characters for single-factor authentication
- allow long passphrases without arbitrary symbol or capitalization composition rules
- are bounded at 1,024 UTF-8 bytes to prevent resource exhaustion
- use a unique 16-byte salt and Argon2id with `m=19456` KiB, `t=2`, `p=1`, and a 32-byte key
- are stored only in the standard Argon2id PHC string format
- are compared in constant time and automatically rehashed when approved parameters change

The parameters follow the current OWASP Argon2id baseline. The 15-character minimum follows NIST SP 800-63B for a single-factor password. A future password-policy provider will add organization-selectable compromised-password blocklists without sending passwords to a third party.

## Sessions and CSRF

Guard generates independent 256-bit session and CSRF values with Go's `crypto/rand`. Only SHA-256 digests are stored in the database. The raw session identifier is carried in an `HttpOnly`, `SameSite=Lax`, path-scoped cookie with no Domain attribute. HTTPS deployments use a `Secure`, `__Host-`-prefixed cookie.

The React application never stores session identifiers or CSRF values in localStorage or sessionStorage. It keeps the synchronized CSRF value only in memory and sends it through `X-CSRF-Token` for state-changing requests. Session restoration rotates the CSRF value. Logout revokes the server-side session before expiring the cookie.

`STEWARDMESH_SESSION_TTL` defaults to 12 hours and accepts values from 15 minutes through 24 hours.

## Roles, policy bundles, and scopes

The initial Administrator role directly contains `guard.manage` and attaches the **Core administration** policy bundle. That bundle groups:

- `organization.read`
- `assets.read`
- `assets.write`
- `directory.read`
- `directory.write`
- `goals.read`

Role assignments carry an explicit organization, site, department, or resource scope. Organization-scoped grants cover resources in that organization; narrower grants require an exact scope match. Every protected API enforces permissions on the server, and a disabled account is rejected even if one of its sessions has not expired. Frontend role display is user guidance, never the security boundary.

## Secure HTTP behavior

- Authentication, bootstrap, session, and every authenticated API response use `Cache-Control: no-store`.
- JSON bodies are size-bounded, require `application/json`, reject unknown fields, and reject trailing documents.
- CORS allows only the configured exact origin and permits credentials only for that origin.
- Browser mutations require the configured origin and a valid synchronized CSRF header.
- Login failures use one generic response and are independently limited to five failures per normalized account and direct client in 15 minutes.
- The default local limiter HMACs identifiers, does not retain successful keys, bounds tracked failures, and fails closed for one window if that bound is exhausted.
- Deployments that set `STEWARDMESH_CACHE_DRIVER=valkey` use the shared Valkey/Redis-compatible counter so multiple replicas enforce the same window. If the configured shared limiter is unavailable, login protection fails closed with a safe service-unavailable response.
- Sessions, CSRF hashes, grants, permission decisions, and authenticated responses remain authoritative or `no-store`; they are not placed in the cache. See [Valkey architecture](../architecture/valkey.md).
- Client address logic uses the direct socket address and does not trust forwarded headers.
- Existing timeout, header-size, correlation, CSP, clickjacking, MIME-sniffing, referrer, and permissions-policy controls remain centralized.

## APIs and provider boundaries

- `GET /api/v1/auth/bootstrap`
- `POST /api/v1/auth/bootstrap`
- `POST /api/v1/auth/login`
- `GET /api/v1/auth/session`
- `POST /api/v1/auth/logout`
- OpenAPI: `api/openapi/openapi.yaml`
- gRPC contract: `api/proto/stewardmesh.proto`
- PostgreSQL migration: `internal/repository/postgres/migrations/0004_guard_local_auth.sql`

The `guard.Store` interface is provider-neutral. PostgreSQL and the in-memory evaluation adapter pass the same contract tests; DynamoDB must implement that contract. The existing `identity.Authenticator` boundary is reserved for local, OIDC/OAuth, and SAML providers.

## Accessibility and guided help

The setup and login experiences use semantic headings, explicit labels, password-manager autocomplete values, descriptive help text, visible focus, a skip link, polite service status, and focus-managed error alerts. Status and errors are not conveyed by color alone. Reduced-motion behavior remains global.

The application links to this page from every Guard setup state and provides the configurable issue-reporting link beside it. Never attach passwords, bootstrap tokens, cookies, CSRF values, or database URLs to an issue.

## Audit events

- `guard.bootstrap.created`
- `guard.bootstrap.denied`
- `guard.login.succeeded`
- `guard.login.failed`
- `guard.login.rate_limited`
- `guard.login.protection_unavailable`
- `guard.logout.succeeded`
- `guard.authorization.denied`

Audit events contain stable account or resource IDs, correlation IDs, actions, timestamps, and requirement metadata. Passwords, raw tokens, usernames used in failed attempts, and credential-bearing configuration are excluded.

## Validation

- Argon2id format, verification, malformed-parameter, and fuzz tests
- minimum password and bootstrap-token tests
- login-rate-limit and uniform-error tests
- distributed rate-limit, cache outage, TTL, and organization-isolation tests
- memory and PostgreSQL provider contract tests
- session hashing, CSRF rotation, expiration, and revocation tests
- server-side permission, origin, CORS, JSON-boundary, and header tests
- React setup, login fallback, keyboard-focus, and safe-link tests
- automated axe accessibility analysis
- race detection, `go vet`, `govulncheck`, dependency audit, and filesystem security scanning in CI

## Security references

- [NIST SP 800-63B password authenticators](https://pages.nist.gov/800-63-4/sp800-63b/authenticators/)
- [OWASP Password Storage Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html)
- [OWASP Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)
- [OWASP CSRF Prevention Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html)
