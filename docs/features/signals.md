# Signals — Alerts and action queue

- **Canonical ID:** `alerts.rules`
- **Requirement:** `REQ-SIGNALS-001`
- **Roadmap issue:** [#11](https://github.com/WSCMAX/StewardMesh/issues/11)

## Purpose and ownership boundary

Signals turns authoritative operational and financial state into a durable action queue. It evaluates Ledger budgets, purchase orders, contracts, commitments, and costs; Horizon replacement forecasts; and Stack license expirations. Those product areas retain ownership of their records and calculations. Signals stores only the rule, a bounded human-readable observation, target IDs, action history, configured subscriber references, and provider-neutral delivery state.

Signals does not send mail or messages directly. Reach consumes the pending-delivery seam and owns provider configuration and transport. This separation keeps provider credentials, webhook URLs, access tokens, response bodies, and message-provider implementation details out of Signals persistence and browser responses.

## Rules and deterministic evaluation

An organization can configure at most 100 rules. Each rule has a name, condition, severity, enabled state, optional supported fiscal-period/scenario filters, and up to eight unique whole-day thresholds from 0 through 3660. Supported conditions are:

- `over_budget`: recognized actual, billed, paid, and committed Ledger costs exceed the matching fiscal-period, scenario, and currency allocation;
- `forecast_over_budget`: the matching Horizon replacement forecast exceeds the matching Ledger allocation;
- `unpaid`: a received purchase order has no paid cost after its threshold;
- `overdue`: an ordered or partially received purchase order remains open after its threshold;
- `expiration`: an active contract or non-retired software license reaches an expiration threshold;
- `renewal`: an active contract reaches a renewal-decision threshold;
- `unused_commitment`: a matching commitment has no recognized contract cost as its end date approaches; and
- `reconciliation`: an actual or billed cost is missing a complete source-system identity.

Renewal and expiration default to 180, 90, 60, and 30 days. Unpaid, overdue, and unused-commitment rules default to 30 days. Period and scenario filters are accepted only for conditions whose authoritative inputs expose those fields; unsupported combinations fail validation instead of being ignored. Calendar comparisons normalize timestamps to UTC dates so time-of-day and local daylight-saving transitions cannot move a threshold unexpectedly.

Evaluations are explicitly bounded to 500 candidates per rule. A SHA-256 key over organization, rule, target type, and target ID deduplicates repeated observations. A first observation creates one alert, later observations refresh it, disappearance resolves it, and a later recurrence reopens it. Every transition increments an optimistic revision and appends immutable `created`, `refreshed`, `acknowledged`, `assigned`, `resolved`, or `reopened` history. Concurrent stale changes conflict rather than overwrite another user.

## Alert actions, reports, and delivery handoff

Alerts always expose machine-stable condition, severity, and status plus a bounded text title and summary. Status is `active`, `acknowledged`, or `resolved`. An authenticated writer can acknowledge an unresolved alert and assign it to a configured identity or group ID. Acknowledgment captures the actor and UTC timestamp; assignment retains its type and target ID. Resolved alerts remain readable with their complete history.

The filtered report is RFC 4180 CSV. Any cell beginning with `=`, `+`, `-`, or `@` receives a leading apostrophe so untrusted names and IDs cannot become spreadsheet formulas.

An enabled group or webhook subscription can apply to one rule or every rule. The target is a stable deployment-configured ID, not a URL. Reach supplies a provider-neutral, organization-scoped catalog containing only enabled webhook providers and subscriber groups whose provider, endpoint, template, and recipients remain valid. Both the API and Workspace require an exact kind-and-ID match from that catalog, so nonexistent IDs, wrong-kind IDs, cross-organization records, disabled providers, and incomplete groups fail before a subscription is stored. When a new alert is created, Signals creates one deterministic pending delivery per matching subscription. Reach can list due work and record a successful, retryable, or terminal attempt. Retryable failures use bounded exponential delay from five minutes through 24 hours and become terminal after eight attempts. Only a stable sanitized error code is accepted.

Subscriptions intentionally retain their stable kind and ID if an administrator later disables a provider or updates a group. An explicit, validated, audited group update takes effect for subscriptions bound to that same group ID; Signals never rewrites the subscription to another ID. A disabled or invalid target disappears from the new-subscription catalog and Workspace marks the existing subscription unavailable. Already queued or subsequently created work still reaches Reach, which records a sanitized terminal `provider_disabled`, `target_not_found`, or `recipient_invalid` result instead of selecting a fallback destination. Re-enabling the same valid target restores it for future subscriptions without exposing its route.

## Permissions, privacy, and audit

- `signals.read` lists rules, alerts, immutable history, subscriber references, and reports.
- `signals.write` creates and updates rules, runs evaluation, acknowledges and assigns alerts, and manages subscriber references.

Migration `0031_signals_alert_rules.sql` creates organization-scoped rules, alerts, history, subscriptions, and delivery work, and adds the two permissions only to existing built-in Administrator bundles. Custom roles do not gain access automatically. Every route is authenticated, permission checked, organization scoped, and non-cacheable. Browser writes require the synchronized CSRF token and configured origin. Request bodies, IDs, text, thresholds, record counts, dates, and queries are bounded, and the client cannot submit an organization ID.

Audit events are `signals.rule.created`, `signals.rule.updated`, `signals.evaluation.completed`, `signals.alert.acknowledged`, `signals.alert.assigned`, `signals.subscription.created`, and `signals.subscription.deleted`. They contain stable record IDs, condition/severity or non-sensitive state, `REQ-SIGNALS-001`, and `alerts.rules`. They exclude source payloads, financial amounts, contract/license names, directory identity details, message content, URLs, credentials, tokens, cookies, CSRF values, and provider responses.

## APIs and accessible workflow

- `GET` and `POST /api/v1/signals/rules`
- `PUT /api/v1/signals/rules/{ruleID}`
- `GET /api/v1/signals/alerts`
- `GET /api/v1/signals/alerts/{alertID}/history`
- `POST /api/v1/signals/evaluate`
- `POST /api/v1/signals/alerts/{alertID}/acknowledge`
- `PUT /api/v1/signals/alerts/{alertID}/assignment`
- `GET` and `POST /api/v1/signals/subscriptions`
- `GET /api/v1/signals/subscription-targets`
- `DELETE /api/v1/signals/subscriptions/{subscriptionID}`
- `GET /api/v1/signals/report.csv`

The protobuf contract also exposes the safe subscription-target catalog plus the narrow pending-delivery and attempt-result seam for Reach workers. REST and gRPC contracts use the same stable conditions, revisions, thresholds, and configured-target vocabulary.

Workspace presents Signals as a focused product area. Native labelled controls support rule creation, queue filtering, evaluation, acknowledgment, assignment, subscription management, and CSV export. Subscription creation uses the server-authoritative labelled Reach-target selector rather than a free-text target ID, disables the action when no target is valid, and gives retained unavailable subscriptions visible explanatory text. Severity and status are always readable text rather than color alone. Feedback uses announced status and focus-managed alert regions. Read-only sessions receive the queue and report without mutation controls. At 320 pixels, cards and forms reflow, long IDs wrap, and no control relies on a hover-only interaction.

Guide includes the Signals purpose, a concrete renewal workflow, same-host documentation, and a sanitized issue-report path. Issue context never includes rule text, alert summaries, target IDs, financial values, identity details, subscriber references, credentials, or session material.

## Security review and validation

The Go and React implementation was reviewed against the repository's secure-backend, general frontend, and React guidance. Server authorization remains authoritative; every mutation uses CSRF protection; route and target values are fixed or strictly validated; PostgreSQL uses parameterized queries; React renders server text through ordinary escaped nodes; client response validators fail closed; no raw HTML or dynamic external navigation is used; and no secret-bearing value is accepted or persisted. Resource limits, deterministic IDs, optimistic revisions, formula-safe CSV, sanitized error codes, and fixed provider boundaries address exhaustion, injection, replay, stale-write, and secret-exposure risks.

Coverage includes all eight condition evaluators and UTC threshold boundaries, defaults and invalid combinations, deduplication/reopen/resolution history, acknowledgment and assignment revisions, exact target-catalog validation, disabled and wrong-kind target rejection, subscription and retry behavior, CSV formula defense, memory/PostgreSQL provider conformance and organization isolation, authentication/permission/CSRF/no-store HTTP behavior, unsafe webhook rejection, OpenAPI/protobuf parity, runtime response validation, keyboard semantics, automated accessibility checks, and responsive browser validation. Release validation runs race-enabled Go tests with PostgreSQL, vet, vulnerability scanning, OpenAPI lint, protobuf compilation, traceability, Node typecheck/tests/build, container checks, and authenticated desktop and 320-pixel browser journeys.
