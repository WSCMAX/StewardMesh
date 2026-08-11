# Guard — Authentication, roles, policies, and audit

- **Canonical ID:** `authorization.security`
- **Requirements:** `SEC-GUARD-001`, `SEC-HTTP-001`, `A11Y-001`, `DOC-001`, `DOC-002`, `REQ-PLATFORM-VALKEY-001`, `REQ-LEDGER-001`
- **Roadmap issue:** [#13](https://github.com/WSCMAX/StewardMesh/issues/13)

## Purpose

Guard protects organization and resource operations. The implemented boundary provides one-time local administrator bootstrap, local password authentication, OpenID Connect authorization-code login, SAML 2.0 SP-initiated login, just-in-time external accounts, opaque server-side sessions, synchronized CSRF protection, administrator-built custom roles, scoped role assignments, reusable policy bundles, externally sourced resource ownership locks and claims, permission enforcement, rate limiting, and security audit events.

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

## OpenID Connect and JIT provisioning

OpenID Connect is disabled unless `STEWARDMESH_OIDC_ISSUER_URL` is configured.
An enabled provider also requires a client ID, client secret, exact callback
URL, and an independent transaction secret containing at least 32 bytes. The
callback must be the configured application origin plus
`/api/v1/auth/oidc/callback`. Issuer and callback URLs require HTTPS except for
explicit loopback development.

Guard uses the OAuth 2.0 authorization code flow with provider discovery,
signed ID-token verification, a transaction-specific nonce, one-time state,
and S256 PKCE. State, nonce, and the PKCE verifier are carried only in a
short-lived AEAD-encrypted, HttpOnly, SameSite=Lax transaction cookie. The
callback redirects only to the fixed configured application origin. Provider
errors become one generic browser message and are not reflected into the
redirect. Access tokens, refresh tokens, authorization codes, ID tokens, and
provider errors are never persisted or returned to the React application.

The first local administrator must be created before external sign-in is
available. A verified ID token is keyed by its exact issuer and subject. Guard
creates a passwordless external account on first login, refreshes display name
and email metadata on later logins, and uses a stable derived username rather
than mutable profile claims as identity. `STEWARDMESH_OIDC_REQUIRE_VERIFIED_EMAIL`
defaults to `true`; an operator may disable that check only when the trusted
provider does not emit `email_verified` and email is not used as an
authorization key.

Administrator mapping is opt-in. Set
`STEWARDMESH_OIDC_ADMINISTRATOR_CLAIM` (default `groups`) and a comma-separated
`STEWARDMESH_OIDC_ADMINISTRATOR_VALUES`. Any exact string match maps the
provider-managed organization Administrator assignment. The configured claim
must be emitted in the signed ID token. Matching is case-sensitive and never
uses substrings. If the claim later stops matching, Guard removes only that
provider-managed assignment and preserves independent local assignments.

## SAML 2.0 and JIT provisioning

SAML is disabled unless `STEWARDMESH_SAML_IDP_METADATA_URL` is configured. An
enabled provider also requires `STEWARDMESH_SAML_SP_CERTIFICATE_FILE` and
`STEWARDMESH_SAML_SP_PRIVATE_KEY_FILE`. StewardMesh loads the certificate and
private key at startup, requires a currently valid RSA or ECDSA certificate,
fetches IdP metadata once over HTTPS (loopback HTTP is development-only), and
accepts at most one same-origin metadata redirect and 1 MiB of XML. The private
key must be provisioned through the deployment secret mechanism and must never
be committed.

The default SP entity ID is the configured application origin plus
`/api/v1/auth/saml/metadata`; set `STEWARDMESH_SAML_ENTITY_ID` only when the IdP
registration requires another stable absolute URI. The assertion consumer
service is always the configured origin plus `/api/v1/auth/saml/acs`.
StewardMesh publishes SP metadata at the default entity-ID URL and signs
HTTP-Redirect AuthnRequests with SHA-256. IdP-initiated SSO is rejected.

Each SP-initiated request receives a 256-bit opaque RelayState below the SAML
80-byte limit. Only its SHA-256 digest, the AuthnRequest ID, and a ten-minute
expiry are stored. The record is atomically consumed before the POSTed response
is checked, so it works across replicas and cannot be replayed. The verified
response must match that request ID, issuer, destination, audience, timing, and
IdP signing keys. POST bodies are type-checked and capped at 2 MiB. Raw
assertions, XML, attribute payloads, and session indexes are never persisted or
logged. Guard stores only the verified IdP entity ID and stable NameID value as
the external identity key needed to recognize the account on later logins.

The email attribute defaults to `urn:oid:0.9.2342.19200300.100.1.3` and the
display-name attribute defaults to `urn:oid:2.16.840.1.113730.3.1.241`. Override
them with `STEWARDMESH_SAML_EMAIL_ATTRIBUTE` and
`STEWARDMESH_SAML_DISPLAY_NAME_ATTRIBUTE`. Email-address NameIDs are accepted
as an email fallback; display name falls back to the verified email.

Administrator mapping is opt-in through
`STEWARDMESH_SAML_ADMINISTRATOR_ATTRIBUTE` and the comma-separated
`STEWARDMESH_SAML_ADMINISTRATOR_VALUES`. Attribute Name and FriendlyName are
matched exactly, and authorization values are exact and case-sensitive. The
JIT lifecycle matches OIDC: Guard keys identities by the exact IdP entity ID
and NameID, refreshes profile fields, and removes only the SAML-managed
administrator assignment if a later assertion no longer maps it.

## Roles, policy bundles, and scopes

The initial Administrator role directly contains `guard.manage` and attaches the **Core administration** policy bundle. That bundle groups:

- `organization.read`
- `assets.read`
- `assets.write`
- `directory.read`
- `directory.write`
- `goals.read`
- `goals.write`
- `storage.read`
- `storage.write`
- `finance.read`
- `finance.write`

Role assignments carry an explicit organization, site, department, or resource scope. Organization-scoped grants cover resources in that organization; narrower grants require an exact scope match. Every protected API enforces permissions on the server, and a disabled account is rejected even if one of its sessions has not expired. Frontend role display is user guidance, never the security boundary.

An organization administrator can list Guard accounts, roles, and assignments in the **Guard · Access administration** panel, then assign an existing role to the whole organization or to one exact site, department, or resource ID. The organization boundary comes from the authenticated server configuration and is never accepted from request data. Mutations require synchronized CSRF validation and `guard.manage` at organization scope in both the HTTP and service layers.

The same panel provides an accessible role-building workflow. An administrator supplies a unique name and optional description, then selects one or more direct permissions, reusable policy bundles, or both. Guard publishes the supported permission catalog from the server, validates every submitted permission and bundle reference, removes duplicates, and stores custom roles with a `local` source. Role names are unique per organization after trimming and case normalization. The built-in Administrator role has a `builtin` source, is visibly marked protected, and cannot be changed through the custom-role persistence boundary.

Role creation is serialized per organization and recorded as `guard.role.created` with permission and bundle counts, never the role name or submitted description. If audit persistence fails, Guard removes the newly created role before returning an error. A newly created role is immediately available in the scoped-assignment workflow.

Local assignments can be removed through StewardMesh. Assignments synchronized from an OpenID Connect claim or SAML assertion attribute are marked read-only and must be changed at the identity provider. The storage adapters serialize assignment changes and reject removal of the final active organization-scoped assignment that grants `guard.manage`, preventing administrator lockout. Assignment changes take effect when Guard resolves access on the next authenticated request.

## Resource ownership and write locks

Guard keeps a provider-neutral ownership registry for records brought in from an external system. Each entry preserves the organization, resource type and ID, stable source-system ID, source-record ID, registration time, and current claim state. A newly registered external record is readable but write-locked. Re-registering the same resource and source identity is idempotent, and a later import cannot silently restore a lock after local ownership has been claimed. Conflicting resource or source identities fail closed.

Only an active account with organization-scoped `guard.manage` can list ownership records, register imported provenance, or claim local ownership. A claim records the administrator and timestamp before the resource becomes writable. Asset relationship creation and ending now consult the owning asset's Guard state and return HTTP 423 while its lock is active. Other domain services must call the same `CheckResourceWrite` boundary before mutating an imported record.

Source record IDs remain available to authorized administrators for reconciliation, but are excluded from audit metadata. Lock registration, explicit ownership claims, and blocked write attempts are all audited with stable resource and source-system IDs. If an ownership-change audit cannot be persisted, Guard restores the previous ownership state before returning an error.

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
- `GET /api/v1/auth/oidc/start`
- `GET /api/v1/auth/oidc/callback`
- `GET /api/v1/auth/saml/metadata`
- `GET /api/v1/auth/saml/start`
- `POST /api/v1/auth/saml/acs`
- `GET /api/v1/auth/session`
- `POST /api/v1/auth/logout`
- `GET /api/v1/guard/access`
- `POST /api/v1/guard/roles`
- `POST /api/v1/guard/role-assignments`
- `DELETE /api/v1/guard/role-assignments/{assignmentID}`
- `GET /api/v1/guard/resource-ownership`
- `POST /api/v1/guard/resource-ownership`
- `POST /api/v1/guard/resource-ownership/{resourceType}/{resourceID}/claim`
- OpenAPI: `api/openapi/openapi.yaml`
- gRPC contract: `api/proto/stewardmesh.proto`
- PostgreSQL migrations: `internal/repository/postgres/migrations/0004_guard_local_auth.sql`, `0007_guard_oidc.sql`, `0008_guard_resource_ownership.sql`, `0009_guard_custom_roles.sql`, and `0010_guard_saml.sql`

The `guard.Store` interface is provider-neutral. PostgreSQL and the in-memory evaluation adapter pass the same local, external-account, and one-time SAML-request contract tests; DynamoDB must implement that contract. `identity.OIDCAuthenticator` and `identity.SAMLAuthenticator` isolate protocol verification from Guard's JIT account and session behavior.

## Accessibility and guided help

The setup, login, scoped-assignment, and custom-role experiences use semantic headings, explicit labels and grouped fieldsets, password-manager autocomplete values, descriptive help text, visible focus, a skip link, polite service status, and focus-managed error alerts. The role builder pairs human-readable capability names with stable permission identifiers and identifies protected roles with text. Status and errors are not conveyed by color alone. Reduced-motion behavior remains global.

The application links to this page from every Guard setup state and provides the configurable issue-reporting link beside it. Never attach passwords, bootstrap tokens, cookies, CSRF values, or database URLs to an issue.

## Audit events

- `guard.bootstrap.created`
- `guard.bootstrap.denied`
- `guard.login.succeeded`
- `guard.login.failed`
- `guard.login.rate_limited`
- `guard.login.protection_unavailable`
- `guard.oidc.login.succeeded`
- `guard.oidc.login.failed`
- `guard.saml.login.succeeded`
- `guard.saml.login.failed`
- `guard.logout.succeeded`
- `guard.role.created`
- `guard.role_assignment.created`
- `guard.role_assignment.revoked`
- `guard.ownership.locked`
- `guard.ownership.claimed`
- `guard.ownership.write_denied`
- `guard.authorization.denied`

Audit events contain stable account or resource IDs, correlation IDs, actions, timestamps, and requirement metadata. Passwords, raw tokens, usernames used in failed attempts, and credential-bearing configuration are excluded.

## Validation

- Argon2id format, verification, malformed-parameter, and fuzz tests
- minimum password and bootstrap-token tests
- login-rate-limit and uniform-error tests
- distributed rate-limit, cache outage, TTL, and organization-isolation tests
- memory and PostgreSQL provider contract tests
- custom-role validation, permission and policy-bundle composition, normalized-name conflict, built-in protection, and audit rollback tests
- local and provider-managed assignment contract tests, duplicate detection, and final-administrator protection
- memory and PostgreSQL ownership registration, provenance conflict, idempotency, claim, and write-lock contract tests
- session hashing, CSRF rotation, expiration, and revocation tests
- state, nonce, S256 PKCE, encrypted transaction, exact claim mapping, JIT refresh, and provider-mapping removal tests
- signed SAML request, metadata, exact attribute mapping, one-time request consumption, replay rejection, JIT refresh, and provider-mapping removal tests
- server-side permission, ownership lock, origin, CORS, JSON-boundary, and header tests
- React setup, login fallback, custom-role building, scoped-assignment management, keyboard-focus, and safe-link tests
- automated axe accessibility analysis
- race detection, `go vet`, `govulncheck`, dependency audit, and filesystem security scanning in CI

## Security references

- [OpenID Connect Core 1.0](https://openid.net/specs/openid-connect-core-1_0.html)
- [RFC 9700 — OAuth 2.0 Security Best Current Practice](https://www.rfc-editor.org/rfc/rfc9700.html)
- [RFC 7636 — Proof Key for Code Exchange](https://www.rfc-editor.org/rfc/rfc7636.html)
- [SAML 2.0 Core](https://docs.oasis-open.org/security/saml/v2.0/saml-core-2.0-os.pdf)
- [SAML 2.0 Bindings](https://docs.oasis-open.org/security/saml/v2.0/saml-bindings-2.0-os.pdf)
- [NIST SP 800-63B password authenticators](https://pages.nist.gov/800-63-4/sp800-63b/authenticators/)
- [OWASP Password Storage Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html)
- [OWASP Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)
- [OWASP CSRF Prevention Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html)
