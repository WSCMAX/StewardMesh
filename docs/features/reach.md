# Reach — Message delivery

- **Canonical ID:** `messaging.delivery`
- **Requirement:** `REQ-REACH-001`
- **Roadmap issue:** [#12](https://github.com/WSCMAX/StewardMesh/issues/12)

## Purpose and ownership boundary

Reach owns outbound delivery after Signals or an authenticated operator has selected a configured destination. It supports SMTP, Amazon SES, Gmail OAuth, Microsoft Outlook OAuth, Microsoft Teams-compatible Graph channel endpoints, and signed generic webhooks. Signals remains the authority for alerts and pending subscriptions; Reach owns provider references, templates, subscriber groups, message snapshots, provider tests, and immutable attempt history.

The browser never supplies or receives endpoint URLs, SMTP addresses, cloud regions, OAuth tokens, provider credentials, or provider response bodies. Deployment operators define credential-free endpoints in `STEWARDMESH_REACH_ENDPOINTS_FILE`. A Teams endpoint additionally declares one non-secret stable `destinationKey`; this is safe to display and maps one-to-one to that endpoint's fixed channel route. Provider records contain only an endpoint ID and an `env:` or `external:` secret reference. Secret values are resolved immediately before the outbound call and cleared from the temporary byte slice afterward.

## Providers, templates, and groups

An organization can store up to 50 providers, 100 templates, and 100 subscriber groups. Provider kind and deployment endpoint kind must match. Email providers require a validated sender; Teams and webhook providers reject a sender. A disabled provider fails closed. Provider creation and rotation never return the persisted secret reference, only `secretConfigured`.

Templates are bounded plain text. The only substitutions are `title`, `summary`, `severity`, `record_id`, and `organization`; unknown or malformed expressions are rejected. Reach does not render HTML. Subscriber groups pair one provider and one template with up to 100 unique recipients. SMTP, SES, Gmail, and Outlook require validated email addresses. A Teams group requires exactly one channel recipient whose address equals the selected endpoint's `destinationKey`; arbitrary or multiple channel IDs are rejected and transport dispatch rechecks the invariant before any external call. A webhook group may carry either validated recipient kind because the provider receives the complete message envelope at its fixed route.

## Explicit delivery, Signals, and retries

Provider tests, secret-reference rotations, manual sends, retries, and Signals processing require `confirm: true`. The UI labels these as external actions and requires a send confirmation checkbox. Manual sends accept an optional organization-scoped idempotency key; the deterministic message ID prevents a replay from invoking a provider twice.

Reach exposes enabled, fully configured groups and webhooks to Signals through a provider-neutral subscription-target catalog, then consumes due delivery records through the `ListPendingDeliveries`, `GetAlert`, and `RecordDeliveryAttempt` seam. A group target renders its template. An explicit group update validates every new reference and recipient, is audited, and intentionally takes effect for subscriptions bound to that same stable group ID. A webhook target references a configured Reach webhook provider whose kind and endpoint are immutable. Disabling a provider immediately removes its direct webhook and dependent groups from the new-subscription catalog; retained subscriptions receive no fallback route, and delivery records a terminal sanitized failure. Unknown, invalid, or legacy Teams targets likewise become terminal sanitized history records so they cannot loop forever or invoke an unintended destination.

Each send first acquires a durable compare-and-swap delivery claim and then records one atomic message/attempt transition. Concurrent retry or Signals workers that do not own the claim make no external call. Retryable failures begin at five minutes, double to a maximum of 24 hours, and stop after eight attempts. An explicit operator retry can run before the scheduled time but cannot reopen a delivered message or exceed the cap. A claim abandoned for two minutes is treated as an ambiguous provider outcome: takeover records `delivery_outcome_unknown` without resending, so an operator can reconcile the provider before requesting another delivery. Stored errors use a short allowlisted code; raw provider errors and bodies are never stored.

## Adapter behavior and deployment configuration

- SMTP uses a deployment-owned address/server name, requires STARTTLS and certificate verification, and accepts a bounded external JSON secret containing username/password. TLS relaxation is allowed only for an explicitly configured loopback fixture.
- SES uses the fixed region/URL and AWS Signature Version 4 with an external access-key secret. Session tokens are supported. The request uses SES v2 `SendEmail` JSON.
- Gmail uses a fixed Gmail API route, OAuth bearer reference, and RFC 2822 message encoded with base64url.
- Outlook uses a fixed Microsoft Graph route and OAuth bearer reference with a `sendMail` JSON request.
- Teams uses a fixed Microsoft Graph channel-message URL, one matching non-secret `destinationKey`, an external OAuth bearer reference, and a bounded plain-text `chatMessage` JSON body. Endpoint parsing, group mutation, dispatch, and the HTTP transport all fail closed if the single stored channel key does not match.
- Generic webhooks use a fixed URL and attach `X-StewardMesh-Timestamp`, `X-StewardMesh-Nonce`, and `X-StewardMesh-Signature` (`v1=` plus HMAC-SHA256 over timestamp, nonce, and body). The reference verifier enforces a five-minute timestamp window and single-use nonce.

HTTP clients time out after ten seconds, reject redirects, and read/discard only a bounded response. URLs must use HTTPS without embedded credentials, query, or fragment. Plain HTTP is restricted to an explicit loopback fixture. HTTP connection tests issue an authenticated or signed `GET` only to the separate deployment-owned `testUrl`; they never fall back to a send route or create a test message. The repository includes deterministic contract tests and safe local mocks; it does not claim external certification against every provider tenant, licensing plan, or future provider API revision.

The example file `deploy/reach-endpoints.example.json` contains no credentials. Copy it outside source control, select only the adapters in use, replace example HTTPS hosts, give every Teams channel route a unique stable `destinationKey`, set `STEWARDMESH_REACH_ENDPOINTS_FILE`, and inject referenced environment secrets through the deployment secret manager. Non-Teams endpoints reject `destinationKey`, preventing misleading unused configuration. With the default prefix, `env:operations-hook` resolves `STEWARDMESH_REACH_SECRET_OPERATIONS_HOOK`.

## Permissions, privacy, and audit

- `messaging.read` lists redacted endpoints, providers, templates, groups, provider tests, messages, and attempts.
- `messaging.write` configures providers/templates/groups and performs confirmed tests, rotations, sends, retries, and Signals processing.

Migration `0033_reach_messaging.sql` creates organization-scoped provider, template, group, message, attempt, and provider-test tables. It adds the two permissions only to existing built-in Administrator bundles; custom roles do not gain access automatically. Every route is authenticated, permission checked, organization scoped, and non-cacheable. Browser writes require the synchronized CSRF token and configured origin. Request bodies, text, recipients, records, history, and outbound responses are bounded.

Audit events are `reach.provider.created`, `reach.provider.updated`, `reach.provider.secret_rotated`, `reach.provider.tested`, `reach.template.created`, `reach.template.updated`, `reach.group.created`, `reach.group.updated`, `reach.message.queued`, `reach.message.retry_requested`, `reach.message.attempted`, and `reach.signals.processed`. They identify `REQ-REACH-001` and `messaging.delivery` plus stable IDs and sanitized state. They exclude secret references/values, message subject/body, recipients, routes, provider payloads/responses, tokens, cookies, and CSRF values.

## APIs and accessible workflow

- `GET /api/v1/reach/endpoints`
- `GET` and `POST /api/v1/reach/providers`
- `PUT /api/v1/reach/providers/{providerID}`
- `POST /api/v1/reach/providers/{providerID}/rotate-secret`
- `POST /api/v1/reach/providers/{providerID}/test`
- `GET /api/v1/reach/providers/{providerID}/tests`
- `GET` and `POST /api/v1/reach/templates`
- `PUT /api/v1/reach/templates/{templateID}`
- `GET` and `POST /api/v1/reach/groups`
- `PUT /api/v1/reach/groups/{groupID}`
- `GET /api/v1/reach/messages`
- `POST /api/v1/reach/messages/send`
- `POST /api/v1/reach/messages/{messageID}/retry`
- `GET /api/v1/reach/messages/{messageID}/attempts`
- `POST /api/v1/reach/signals/process`

The protobuf `ReachService` mirrors provider, template, group, send, retry, history, and Signals processing operations. Its endpoint message and the OpenAPI response expose only the safe Teams destination key while continuing to omit network routes and secret references. Signals REST/protobuf contracts expose the same current target-catalog vocabulary.

Workspace presents Reach as a focused product area. Native labelled controls configure providers, templates, and groups, then require explicit confirmation for external actions. Selecting a Teams provider replaces free-text recipient controls with a read-only labelled destination key derived from safe endpoint metadata. Status and errors always include readable text, feedback uses announced/focus-managed regions, and read-only sessions receive history without mutation controls. Cards, forms, long IDs, and recipient addresses wrap and reflow at 320 pixels without hover-only behavior. Guide links to same-host Reach documentation and keeps issue context free of messages, recipients, provider identifiers, and secret metadata.

## Security review and validation

The Go and React implementation was reviewed against the repository's secure-backend, general frontend, and React guidance. Server authorization remains authoritative; every mutation uses CSRF; destination selection is fixed by deployment; HTTPS/TLS rules resist SSRF and downgrade; parameterized PostgreSQL queries and optimistic revisions prevent injection and stale overwrites; templates remain plain text; React renders escaped nodes; client validators reject secret-bearing or malformed responses; response bodies and errors are redacted; HMAC timestamp/nonce validation addresses webhook spoofing and replay; idempotent message/attempt identities plus durable pre-send claims address duplicate delivery; and timeouts/limits address exhaustion.

Coverage includes endpoint and secret-reference validation/redaction, exact Teams destination enforcement at configuration and dispatch, each provider request contract, webhook signature and replay rejection, SMTP TLS/message behavior, plain-text template/token validation, recipient compatibility, authoritative Signals target discovery, disabled-target behavior, idempotency, concurrent pre-send claim exclusion, abandoned-claim fail-closed recovery, retry boundaries, Signals success/retry/terminal handoff, memory/PostgreSQL contract and organization isolation, authentication/permission/CSRF/no-store HTTP behavior, REST/protobuf parity, runtime response validation, read-only behavior, automated accessibility checks, and responsive browser journeys. Release validation runs race-enabled Go tests with isolated PostgreSQL, vet, vulnerability scanning, OpenAPI lint, protobuf compilation, traceability, Node typecheck/tests/build, container checks, and authenticated desktop plus 320-pixel provider/template/group/send/retry/history workflows.

The authenticated browser journey configured a redacted deployment endpoint and external secret reference, created a plain-text template and subscriber group, passed a signed webhook connection test, observed a sanitized retryable `503`, explicitly retried, and verified delivery plus both immutable attempts. A fresh saved-auth session finished with zero console errors or warnings. At an exact 320-pixel viewport, viewport, document, and body widths all remained 320 pixels with no horizontal overflow. Visual evidence is retained under `output/playwright/phase-one/issue-12/`.
