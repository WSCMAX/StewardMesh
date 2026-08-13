# Directory Connector Deployment

Requirement: `REQ-DIRECTORY-EXPANSION-009`

Canonical feature: `experience.help`

This runbook is the deployment boundary for the optional Microsoft Entra ID,
SailPoint Identity Security Cloud, Internet2 Grouper, and PeopleSoft Campus
Solutions directory connectors. Every connector is disabled unless its required
server configuration is complete. Provider endpoints, credentials, access
tokens, query mappings, and private-network opt-ins are deployment-owned and
must never be entered in the browser or committed to source control.

## Provider matrix

| Provider | Imported objects | Upstream operations | Minimum upstream access | Snapshot behavior |
|---|---|---|---|---|
| Microsoft Entra ID | Users, groups, direct memberships, departments, allowlisted attributes | OAuth client-credential token exchange, then fixed Microsoft Graph `GET` routes | Application permissions `User.Read.All` and `GroupMember.ReadBasic.All`; add `Member.Read.Hidden` only when hidden membership is in scope | Complete, bounded snapshot |
| SailPoint ISC | Identities, accounts, account sources, departments, governance groups, roles, and memberships | OAuth token exchange, then fixed `/v2025` `GET` routes | Dedicated client or PAT limited to read access for the configured identity, account, workgroup, and role endpoints | Complete, bounded snapshot |
| Internet2 Grouper | Groups, direct subjects, nested-group memberships | Fixed SCIM v2 `GET` routes | Dedicated bearer token or Basic account with read-only access to the selected SCIM groups and members | Complete, bounded snapshot |
| PeopleSoft Campus Solutions | Organizations, locations, buildings, departments, and hierarchy links | Four fixed Query Access Service `GET` queries using `JSON/NONFILE` | Dedicated user with QAS execute access only to the four configured read queries; no query-save or Campus update services | Partial by design; missing rows never deactivate records |

StewardMesh never writes to a configured directory provider. Preview stores an
exact durable reconciliation plan. Apply and retry operate only on that plan,
and a locally claimed record remains protected by an explicit conflict.

## Shared deployment rules

1. Create a dedicated non-human provider principal with only the read access in
   the matrix. Never reuse an administrator or interactive user credential.
2. Store credentials in the platform secret manager and inject them only into
   the StewardMesh server process. Do not place secrets in Compose overrides,
   frontend variables, issue reports, logs, or database records.
3. Configure one stable `SOURCE_SYSTEM_ID` per upstream tenant. Changing it
   creates a new ownership namespace and must be treated as a migration.
4. Keep the default public HTTPS boundary. Enable a private-network option only
   for an explicitly trusted internal provider; never enable it merely to work
   around DNS, certificate, or routing failures.
5. Restart StewardMesh after credential rotation or connector configuration
   changes. The process clears secret fields from its working configuration
   after connector construction, so a running connector cannot observe an
   in-place secret-manager update.
6. Preview and review creates, updates, deactivations, and conflicts before
   applying. Use a new idempotency key for a genuinely new operation and retain
   the audit correlation ID in the change record.

To disable a connector, clear its endpoint or required credential settings and
restart StewardMesh. Disabling stops new pulls without deleting durable import
history, mappings, ownership locks, or previously imported People records.

## Microsoft Entra ID

```text
STEWARDMESH_ENTRA_SOURCE_SYSTEM_ID=entra-primary
STEWARDMESH_ENTRA_TENANT_ID=<single-tenant UUID>
STEWARDMESH_ENTRA_CLIENT_ID=<application UUID>
STEWARDMESH_ENTRA_CLIENT_SECRET=<secret-manager value>
```

Reject the multi-tenant aliases `common`, `organizations`, and `consumers`.
Grant admin consent only for the application permissions in the provider
matrix. Do not grant delegated permissions, `Directory.Read.All`, or any
`*.ReadWrite.All` permission. StewardMesh pins Graph operations to
`https://graph.microsoft.com/v1.0`, rejects redirects and unsafe pagination,
and disables ambient HTTP proxy inheritance for token and Graph traffic.

## SailPoint Identity Security Cloud

```text
STEWARDMESH_SAILPOINT_SOURCE_SYSTEM_ID=sailpoint-primary
STEWARDMESH_SAILPOINT_BASE_URL=https://<tenant>.api.identitynow.com
STEWARDMESH_SAILPOINT_CLIENT_ID=<least-privilege client or PAT ID>
STEWARDMESH_SAILPOINT_CLIENT_SECRET=<secret-manager value>
```

The base URL must be the exact HTTPS tenant host with no credentials, custom
port, path, query, or fragment. Limit the client to read access for identities,
accounts, workgroups, roles, and their selected membership/assignment routes.
Do not grant identity, role, account, or governance-group management access.

## Internet2 Grouper

```text
STEWARDMESH_GROUPER_URL=https://grouper.example.edu/grouper-ws/scim/v2
STEWARDMESH_GROUPER_SOURCE_SYSTEM_ID=grouper-primary
STEWARDMESH_GROUPER_BEARER_TOKEN=<secret-manager value>
STEWARDMESH_GROUPER_CONFIG_REVISION=v1
STEWARDMESH_GROUPER_ALLOW_PRIVATE_NETWORK=false
```

Use either the bearer token or the Basic username/password settings, never
both. The endpoint path is fixed to `/grouper-ws/scim/v2`. The
`ALLOW_PRIVATE_NETWORK` flag is required for an intentionally private HTTPS
deployment and for the loopback fixture; link-local and other unsafe address
classes remain blocked.

The committed Grouper fixture is test data, not a production connector. Start
it only with the explicit profile:

```sh
docker compose -f deploy/docker-compose.yml --profile integrations up -d --wait postgres grouper
```

Its fixed local token must never be copied into a shared environment.

## PeopleSoft Campus Solutions

```text
STEWARDMESH_PEOPLESOFT_SOURCE_SYSTEM_ID=peoplesoft-primary
STEWARDMESH_PEOPLESOFT_BASE_URL=https://ps.example.edu/PSIGW/RESTListeningConnector/CAMPUS/ExecuteQuery.v1
STEWARDMESH_PEOPLESOFT_USERNAME=<least-privilege query user>
STEWARDMESH_PEOPLESOFT_PASSWORD=<secret-manager value>
STEWARDMESH_PEOPLESOFT_QUERY_OWNER=public
STEWARDMESH_PEOPLESOFT_ORGANIZATION_QUERY=SM_ORGANIZATIONS
STEWARDMESH_PEOPLESOFT_LOCATION_QUERY=SM_LOCATIONS
STEWARDMESH_PEOPLESOFT_BUILDING_QUERY=SM_BUILDINGS
STEWARDMESH_PEOPLESOFT_DEPARTMENT_QUERY=SM_DEPARTMENTS
STEWARDMESH_PEOPLESOFT_FIELD_MAPPINGS_JSON=<strict deployment-owned JSON>
STEWARDMESH_PEOPLESOFT_ALLOW_PRIVATE_NETWORK=false
```

Use exactly one authentication mode: Basic username/password or a gateway
bearer token. Each query must be read-only and return only the selectors needed
by the strict field mapping. Because QAS cannot prove that an upstream row cap
did not truncate results, the connector never infers deactivation from an
absent PeopleSoft row.

## Preflight and operational checks

Before enabling a provider in a shared environment:

- validate startup fails safely for a partial configuration and emits no secret;
- confirm the source list exposes only the source ID, provider, and non-secret
  configuration revision;
- preview with a read/write integration administrator and inspect the complete
  or partial snapshot label before apply;
- verify a read-only operator can inspect history but cannot preview or apply;
- verify provider logs contain no write operation from the StewardMesh principal;
- rotate the credential, restart, preview again, and revoke the old credential;
- retain the StewardMesh audit correlation ID and provider change ticket.

Run the full [Directory Expansion release validation](../validation/directory-expansion-release.md)
before promotion. The default deployment must remain usable with every
connector, Valkey, the Grouper fixture, and synthetic demo data disabled.
