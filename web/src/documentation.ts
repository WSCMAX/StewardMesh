// Requirements: REQ-DIRECTORY-EXPANSION-005, REQ-SIGNALS-001, REQ-REACH-001, REQ-EXCHANGE-001, DOC-001. Features: integrations.protocols, alerts.rules, messaging.delivery, migration.packages, experience.help.

export type DocumentationTopicID = 'overview' | 'workspace' | 'atlas' | 'horizon' | 'ledger' | 'stack' | 'signals' | 'reach' | 'threads' | 'vault' | 'exchange' | 'people' | 'guard' | 'guide'

export type DocumentationStep = {
  title: string
  body: string
}

export type DocumentationSection = {
  id: string
  title: string
  paragraphs?: readonly string[]
  bullets?: readonly string[]
  steps?: readonly DocumentationStep[]
  callout?: { title: string; body: string; tone?: 'info' | 'success' | 'warning' }
}

export type DocumentationPage = {
  id: DocumentationTopicID
  group: 'Start here' | 'Product areas' | 'Administration'
  kicker: string
  title: string
  summary: string
  appHref: string
  appLabel: string
  searchTerms: readonly string[]
  sections: readonly DocumentationSection[]
  related: readonly DocumentationTopicID[]
}

export const documentationPages: readonly DocumentationPage[] = [
  {
    id: 'overview',
    group: 'Start here',
    kicker: 'StewardMesh documentation',
    title: 'Connect what you steward',
    summary: 'Learn the core StewardMesh workflow, find the right product area, and move between documentation and the live application without leaving your current host.',
    appHref: '#workspace-overview',
    appLabel: 'Open StewardMesh',
    searchTerms: ['getting started', 'home', 'documentation', 'products', 'sign in'],
    sections: [
      {
        id: 'start',
        title: 'Start with one focused task',
        paragraphs: ['StewardMesh organizes inventory, planning, finance, software, alerts, relationships, files, people, and access into focused product areas. Begin in Workspace, choose the area that owns the task, and use Guide when you need contextual help.'],
        steps: [
          { title: 'Sign in through Guard', body: 'Use your organization account. A new local installation first asks an authorized operator to create the initial administrator.' },
          { title: 'Choose a product area', body: 'Open Atlas, Horizon, Ledger, Threads, Vault, People, or Guard from Workspace. Your current grants determine which records and actions are available.' },
          { title: 'Keep context while you work', body: 'Workspace preserves opened areas so filters, selected records, and incomplete forms remain available when you move between products.' },
        ],
      },
      {
        id: 'product-map',
        title: 'Know where work belongs',
        bullets: [
          'Atlas owns assets, reusable product models, identifiers, locations on asset records, and lifecycle history.',
          'Horizon owns useful-life assumptions, replacement timing, scenarios, and forecasts.',
          'Ledger owns vendors, purchases, contracts, commitments, costs, and budgets.',
          'Stack owns software products, installations, entitlements, assignments, and compliance conditions.',
          'Signals owns operational and financial alert rules, acknowledgment, assignment, and delivery handoffs.',
          'Reach owns approved delivery adapters, templates, subscriber groups, confirmed sends, retries, and sanitized history.',
          'Threads owns hierarchical tags, strategic goals, links, and inheritance provenance.',
          'Vault owns private evidence metadata, integrity checks, and authorized downloads.',
          'Exchange owns bounded migration package assembly, validation, import receipts, and holding outcomes.',
          'People owns sites, buildings, rooms, departments, identities, and assignments.',
          'Guard owns authentication, roles, scoped grants, ownership controls, and authorization policy.',
        ],
      },
      {
        id: 'documentation-boundary',
        title: 'Documentation on this host',
        paragraphs: ['These pages are first-party product guidance bundled with the StewardMesh web application. Their links remain on the current host and are available before sign-in. Repository Markdown remains a separate engineering, architecture, and requirement reference, so either layer can evolve for its own audience.'],
        callout: { title: 'No GitHub redirect', body: 'Product help links open this local documentation surface. Only the explicit issue-reporting workflow may use a separately configured external destination.', tone: 'success' },
      },
    ],
    related: ['workspace', 'guide', 'guard'],
  },
  {
    id: 'workspace',
    group: 'Start here',
    kicker: 'Connected work view',
    title: 'Workspace',
    summary: 'Move between focused product areas while keeping your current task, access boundary, and service state visible.',
    appHref: '#workspace-overview',
    appLabel: 'Open Workspace',
    searchTerms: ['navigation', 'dashboard', 'work area', 'permissions', 'deep link', 'overview'],
    sections: [
      {
        id: 'navigate',
        title: 'Navigate without losing your place',
        paragraphs: ['Desktop layouts keep product navigation beside the active work area. Narrow layouts use an accessible navigation drawer. Previously opened areas remain mounted, which preserves in-progress React state as you move around.'],
        bullets: ['Use the current-area heading to confirm where you are.', 'Use the context band to check access, visible-record scope, change capability, and service state.', 'Copy the fixed workspace hash when you need to return to a specific product area.'],
      },
      {
        id: 'access',
        title: 'Understand access labels',
        bullets: ['Read and change means organization-wide read and write grants are present.', 'Read only means records can be viewed but feature-owned mutation actions stay unavailable.', 'Scoped means access is limited to assigned sites, departments, or resources; broad collections remain closed.', 'Limited means the required read grant is absent. The product stays discoverable without mounting protected content.'],
        callout: { title: 'Server authorization stays authoritative', body: 'Workspace uses session hints to compose the interface, but every API request is still authenticated and authorized by Guard.', tone: 'info' },
      },
      {
        id: 'related-records',
        title: 'Connect related records safely',
        paragraphs: ['Guided related-record tasks preserve and validate the source draft, let you select an existing related record or create one when authorized, return to earlier steps, and ask for explicit confirmation. Loading, failure, retry, and cancellation are announced without moving record ownership into Workspace.'],
        callout: { title: 'Owning features keep control', body: 'Each task identifies the source and related feature APIs and permissions. Those APIs still enforce validation, authorization, organization scope, and audit behavior.', tone: 'info' },
      },
      {
        id: 'recover',
        title: 'Recover from interruptions',
        paragraphs: ['If service health is unavailable, retained content is marked as potentially stale. If a session expires, StewardMesh clears in-memory identity, grants, CSRF material, and protected records before returning to sign-in. Invalid workspace hashes safely return to Overview.'],
      },
    ],
    related: ['overview', 'guide', 'guard'],
  },
  {
    id: 'atlas',
    group: 'Product areas',
    kicker: 'Asset inventory',
    title: 'Atlas',
    summary: 'Register organization-owned assets, reuse model defaults, maintain identity and location details, and preserve lifecycle history.',
    appHref: '#workspace-atlas',
    appLabel: 'Open Atlas',
    searchTerms: ['asset', 'inventory', 'model', 'barcode', 'qr', 'serial', 'hostname', 'lifecycle'],
    sections: [
      {
        id: 'assets',
        title: 'Work with individual assets',
        steps: [
          { title: 'Find or add the asset', body: 'Search by name, tag, serial number, or hostname, or create a new organization-owned record.' },
          { title: 'Describe the item', body: 'Record its kind, model, status, purchase date, site, building, room, department, and primary user as available.' },
          { title: 'Preserve lifecycle context', body: 'Status changes use optimistic revisions and retain an immutable lifecycle note and timestamp history.' },
        ],
      },
      {
        id: 'models',
        title: 'Reuse product models',
        paragraphs: ['The Model catalog describes a purchased product once and lets many assets reference it. Manufacturer, model identity, kind, vendor identifier, support URL, warranty, and useful-life defaults stay separate from per-item tags, serials, assignments, and lifecycle state.'],
        bullets: ['Choose Use on a model to prefill a new asset.', 'Edit shared model details without silently overwriting instance-specific asset fields.', 'Retire a model to prevent new assignments while preserving historical references.'],
      },
      {
        id: 'identifiers',
        title: 'Associate barcodes and QR codes',
        paragraphs: ['Atlas Codes keeps identifier values unique within the organization and attached to a specific asset. Replacement and deactivation preserve history rather than silently reusing an old code.'],
        callout: { title: 'Scan safely', body: 'Treat a scan as a lookup or association request. Confirm the resolved asset before changing assignments or lifecycle state.', tone: 'warning' },
      },
    ],
    related: ['people', 'horizon', 'vault'],
  },
  {
    id: 'horizon',
    group: 'Product areas',
    kicker: 'Lifecycle planning',
    title: 'Horizon',
    summary: 'Set effective-dated replacement assumptions, compare scenarios, and forecast lifecycle needs using connected StewardMesh records.',
    appHref: '#workspace-horizon',
    appLabel: 'Open Horizon',
    searchTerms: ['forecast', 'replacement', 'scenario', 'useful life', 'fiscal year', 'planning'],
    sections: [
      {
        id: 'plans',
        title: 'Create a lifecycle plan',
        steps: [
          { title: 'Choose an Atlas asset', body: 'Plans remain connected to the inventory item they describe.' },
          { title: 'Set an effective assumption', body: 'Record useful life, replacement date, stage, scenario, and cost without rewriting older effective versions.' },
          { title: 'Compare outcomes', body: 'Forecast by fiscal year and group needs by site, department, effective tag, direct goal, or Atlas asset class.' },
        ],
      },
      {
        id: 'money',
        title: 'Keep cost provenance explicit',
        paragraphs: ['Replacement amounts use exact minor units and explicit currency. Actual, estimated, committed, normalized-real, and total-cost-of-ownership values remain labeled so forecasts do not imply certainty that is not present in source records.'],
      },
      {
        id: 'exports',
        title: 'Review and export',
        paragraphs: ['Authoritative tables remain readable without charts. Compact bars supplement values but never replace labels. JSON and CSV exports preserve scenario, grouping, currency, and forecast boundaries.'],
      },
    ],
    related: ['atlas', 'ledger', 'threads'],
  },
  {
    id: 'ledger',
    group: 'Product areas',
    kicker: 'Procurement and budgets',
    title: 'Ledger',
    summary: 'Track the financial records that explain what was ordered, committed, paid, budgeted, and reconciled.',
    appHref: '#workspace-ledger',
    appLabel: 'Open Ledger',
    searchTerms: ['vendor', 'purchase order', 'contract', 'commitment', 'budget', 'cost', 'invoice', 'currency'],
    sections: [
      {
        id: 'records',
        title: 'Build the financial chain',
        bullets: ['Vendors identify the organization providing goods or services.', 'Purchase orders and contracts preserve status transitions and exact amounts.', 'Commitments describe ongoing obligations such as subscriptions, leases, maintenance, or financing.', 'Reconciled costs connect billed evidence to the owning financial context.'],
      },
      {
        id: 'budgets',
        title: 'Compare budgets and actuals',
        paragraphs: ['Budget variance uses explicit fiscal periods, scenarios, currency, and exact minor-unit arithmetic. Remaining and over-budget states always include text and amounts rather than relying on color alone.'],
      },
      {
        id: 'safe-export',
        title: 'Export deliberately',
        paragraphs: ['Ledger exports preserve the selected period and scenario. CSV cells are encoded to prevent spreadsheet formulas from being interpreted when values begin with formula-significant characters.'],
      },
    ],
    related: ['horizon', 'vault', 'atlas'],
  },
  {
    id: 'stack',
    group: 'Product areas',
    kicker: 'Software and licenses',
    title: 'Stack',
    summary: 'Connect installed software to Atlas assets, preserve purchased entitlements, assign seats, and review explicit license conditions.',
    appHref: '#workspace-stack',
    appLabel: 'Open Stack',
    searchTerms: ['software', 'license', 'entitlement', 'installation', 'version', 'assignment', 'expiration', 'compliance'],
    sections: [
      {
        id: 'inventory',
        title: 'Connect software to inventory',
        steps: [
          { title: 'Define the product', body: 'Record the publisher and product once, then add the versions your organization installs or uses.' },
          { title: 'Associate an installation', body: 'Choose a version and an organization-visible Atlas asset. Stack preserves installation time and usage state without copying the asset record.' },
          { title: 'Review coverage', body: 'The compliance summary identifies an active installation that lacks a matching asset, identity, department, site, or enterprise entitlement.' },
        ],
      },
      {
        id: 'entitlements',
        title: 'Preserve the purchased entitlement',
        paragraphs: ['A license records its device, user, concurrent, site, or enterprise metric, positive quantity, effective dates, and optional version scope. Device seats assign to assets, user seats to identities, and site seats to sites. Ledger vendor, purchase order, contract, and cost references explain the purchase while Vault document IDs preserve supporting evidence.'],
        callout: { title: 'References stay authoritative', body: 'Stack validates every Atlas, People, Ledger, and Vault reference through the owning feature before saving it.', tone: 'info' },
      },
      {
        id: 'conditions',
        title: 'Act on explicit conditions',
        bullets: ['Expiring and expired conditions include the remaining day count.', 'Over-assigned conditions compare assigned seats with purchased quantity.', 'Under-used conditions identify assignments explicitly marked unused.', 'Missing-license conditions identify installed assets without a matching entitlement.'],
      },
      {
        id: 'lifecycle',
        title: 'Keep lifecycle history explicit',
        paragraphs: ['Revise entitlement quantities and dates, mark unsupported or retired software, remove installations, update usage, and end assignments from the record table. Every change carries the current revision; retirement, removal, and assignment end cannot be silently reopened.'],
      },
      {
        id: 'portable-records',
        title: 'Move records safely',
        paragraphs: ['Portable export keeps stable IDs, revisions, typed dependencies, and bounded typed payloads. Import validates every envelope and dependency set before writing, orders dependencies, and uses the supplied source identity for exact replay: unchanged data is not duplicated and changed data conflicts for review.'],
      },
    ],
    related: ['atlas', 'ledger', 'people'],
  },
  {
    id: 'signals',
    group: 'Product areas',
    kicker: 'Alerts and action queue',
    title: 'Signals',
    summary: 'Evaluate operational and financial conditions, keep repeated observations deduplicated, and preserve action history until each alert is resolved.',
    appHref: '#workspace-signals',
    appLabel: 'Open Signals',
    searchTerms: ['alert', 'rule', 'renewal', 'expiration', 'over budget', 'overdue', 'unpaid', 'reconciliation', 'subscription'],
    sections: [
      {
        id: 'rules',
        title: 'Configure bounded rules',
        paragraphs: ['Rules evaluate over-budget, forecast-over-budget, unpaid, overdue, expiration, renewal, unused-commitment, and reconciliation conditions against authoritative Ledger, Horizon, and Stack records. Optional fiscal period and scenario filters keep evaluation explicit.'],
        bullets: ['Renewal and expiration rules default to 180, 90, 60, and 30 days.', 'Unpaid, overdue, and unused-commitment rules default to 30 days.', 'An administrator may provide up to eight unique thresholds from 0 through 3660 days.', 'Disabled rules remain visible but do not evaluate.'],
      },
      {
        id: 'queue',
        title: 'Work the alert queue',
        paragraphs: ['The same rule and target produce one durable alert. A repeated observation refreshes that alert; a missing observation resolves it; and a later recurrence reopens it with history intact.'],
        bullets: ['Severity and status always include readable labels.', 'Acknowledgment records the actor and timestamp.', 'Assignments name an existing identity or group by configured ID.', 'CSV exports protect formula-significant cells before spreadsheet use.'],
      },
      {
        id: 'delivery',
        title: 'Route through Reach safely',
        paragraphs: ['Signals creates durable provider-neutral delivery work for enabled group and webhook subscriptions. Reach can process pending work with bounded exponential retries while Signals retains only stable target references and sanitized error codes.'],
        callout: { title: 'Do not paste delivery secrets', body: 'The Signals interface accepts configured subscriber IDs, never webhook URLs, OAuth tokens, provider credentials, or provider response bodies.', tone: 'warning' },
      },
      {
        id: 'access',
        title: 'Respect access and audit boundaries',
        paragraphs: ['Signals reads require signals.read and every rule, evaluation, acknowledgment, assignment, and subscription change requires signals.write plus the current CSRF token. Audit records identify the requirement and feature without copying sensitive source payloads.'],
      },
    ],
    related: ['reach', 'ledger', 'horizon'],
  },
  {
    id: 'reach',
    group: 'Product areas',
    kicker: 'Message delivery',
    title: 'Reach',
    summary: 'Configure approved delivery adapters, reusable plain-text templates, subscriber groups, confirmed sends, and sanitized retry history.',
    appHref: '#workspace-reach',
    appLabel: 'Open Reach',
    searchTerms: ['email', 'smtp', 'ses', 'gmail', 'outlook', 'teams', 'webhook', 'delivery', 'retry', 'subscriber', 'template'],
    sections: [
      {
        id: 'providers',
        title: 'Select deployment-approved providers',
        paragraphs: ['Operators choose endpoint IDs from deployment configuration. The browser cannot submit or discover provider URLs, SMTP addresses, cloud regions, OAuth tokens, or credentials. Gmail and Outlook use OAuth bearer references; SES uses SigV4 credentials; SMTP uses a structured external credential; Teams and generic webhooks use fixed routes.'],
        callout: { title: 'References, never secrets', body: 'Enter a stable env: or external: reference. Secret values remain in the deployment secret system and are resolved only for the outbound call.', tone: 'warning' },
      },
      {
        id: 'compose',
        title: 'Compose reusable delivery policy',
        steps: [
          { title: 'Create a plain-text template', body: 'Use only title, summary, severity, record_id, and organization tokens. HTML and unknown template expressions are rejected.' },
          { title: 'Create a subscriber group', body: 'Pair one enabled provider and template with validated email recipients or configured Teams channel IDs.' },
          { title: 'Confirm the external action', body: 'Provider tests, manual sends, secret-reference rotations, retries, and pending Signals processing require an explicit confirmation and messaging.write access.' },
        ],
      },
      {
        id: 'history',
        title: 'Review bounded retry history',
        paragraphs: ['Each attempt records a provider-neutral outcome and sanitized error code. Retryable failures use exponential delays beginning at five minutes, capped at 24 hours, and stop after eight attempts. Provider response bodies, exception text, endpoint routes, and credentials are never retained in delivery history.'],
      },
      {
        id: 'security',
        title: 'Understand outbound protections',
        bullets: ['Fixed HTTPS endpoints prevent callers from turning Reach into an arbitrary network client; HTTP and relaxed SMTP TLS are restricted to explicit loopback fixtures.', 'Webhook deliveries carry a timestamp, nonce, and HMAC-SHA256 signature; receivers should reject stale timestamps and repeated nonces.', 'Outbound clients use bounded timeouts, reject redirects, and discard bounded response bodies.', 'messaging.read protects configuration and history; messaging.write plus CSRF protects every mutation.'],
        callout: { title: 'Adapter status', body: 'StewardMesh validates adapter request contracts with deterministic mocks. These adapters are not claimed as externally certified against every provider tenant or licensing configuration.', tone: 'info' },
      },
    ],
    related: ['signals', 'guard', 'guide'],
  },
  {
    id: 'threads',
    group: 'Product areas',
    kicker: 'Tags and strategic goals',
    title: 'Threads',
    summary: 'Connect records through hierarchical tags and goals while keeping every inherited, explicit, and suppressed relationship understandable.',
    appHref: '#workspace-threads',
    appLabel: 'Open Threads',
    searchTerms: ['tag', 'goal', 'inheritance', 'provenance', 'hierarchy', 'relationship'],
    sections: [
      {
        id: 'tags',
        title: 'Use a shared tag hierarchy',
        paragraphs: ['Tags can describe domains such as service, risk, program, platform, or organizational grouping. Child tags retain their relationship to parent tags, which lets connected views use consistent dimensions.'],
      },
      {
        id: 'provenance',
        title: 'Read relationship provenance',
        bullets: ['Explicit means the tag was applied directly to the record.', 'Inherited means the value came from a visible parent relationship.', 'Suppressed means an inherited value was deliberately excluded for that record.'],
        callout: { title: 'Never hide inheritance', body: 'StewardMesh shows where an effective tag came from so users can distinguish a direct decision from a derived relationship.', tone: 'info' },
      },
      {
        id: 'goals',
        title: 'Link work to goals',
        paragraphs: ['Strategic goals provide a durable planning dimension across inventory and lifecycle views. A direct goal link remains distinct from tags and does not silently change the asset record.'],
      },
    ],
    related: ['horizon', 'atlas', 'workspace'],
  },
  {
    id: 'vault',
    group: 'Product areas',
    kicker: 'Private files and evidence',
    title: 'Vault',
    summary: 'Store private evidence with organization ownership, checksummed integrity, provenance, and explicit download authorization.',
    appHref: '#workspace-vault',
    appLabel: 'Open Vault',
    searchTerms: ['file', 'upload', 'download', 'evidence', 'checksum', 'sha-256', 's3', 'storage'],
    sections: [
      {
        id: 'upload',
        title: 'Upload evidence',
        steps: [
          { title: 'Choose the file deliberately', body: 'File size and allowed metadata are validated before storage.' },
          { title: 'Describe provenance', body: 'Record the source identity and the StewardMesh record or workflow the evidence supports.' },
          { title: 'Verify integrity', body: 'Vault records a SHA-256 checksum so later reads can verify the stored bytes.' },
        ],
      },
      {
        id: 'privacy',
        title: 'Keep storage private',
        paragraphs: ['Storage keys are server-owned and organization-scoped. Backend adapters do not expose durable public URLs, cloud credentials, or provider tokens to the browser.'],
      },
      {
        id: 'downloads',
        title: 'Authorize downloads when needed',
        paragraphs: ['A download requires current storage read access and a short-lived authorization step. Authorization does not turn the underlying object into a public file.'],
      },
    ],
    related: ['ledger', 'atlas', 'guard'],
  },
  {
    id: 'exchange',
    group: 'Product areas',
    kicker: 'Portable migration packages',
    title: 'Exchange',
    summary: 'Export and import bounded, dependency-aware .openinventory packages with checksums, provenance, ownership metadata, and visible holding outcomes.',
    appHref: '#workspace-exchange',
    appLabel: 'Open Exchange',
    searchTerms: ['migration', 'import', 'export', 'openinventory', 'package', 'checksum', 'dependency', 'holding', 'ownership'],
    sections: [
      {
        id: 'export',
        title: 'Build a complete export',
        steps: [
          { title: 'Select explicit records', body: 'Choose only the records the receiving organization needs. Exchange keeps stable record types, IDs, revisions, provenance, and relationships.' },
          { title: 'Include required dependencies', body: 'Leave dependency inclusion enabled for a complete round trip. The package orders dependencies before the records that use them.' },
          { title: 'Choose file handling', body: 'Metadata mode carries checksums and relationships without file bytes. Include mode embeds bounded, checksummed Vault content.' },
        ],
        callout: { title: 'No cloud secrets in packages', body: 'Exchange never exports credentials, access tokens, private keys, object-store credentials, or signed download URLs.', tone: 'success' },
      },
      {
        id: 'import',
        title: 'Verify before writing',
        paragraphs: ['Exchange accepts only bounded .openinventory archives. It validates the schema version, archive structure, package identity, record and file checksums, typed relationships, duplicate identity, and dependency graph before importing records through their owning domain service.'],
        bullets: ['Exact replays are idempotent and report unchanged records.', 'The same package identity with changed bytes is rejected as a conflict.', 'Corrupt archives, failed checksums, unsafe metadata, and oversized content are rejected.', 'Missing external references and unavailable file bytes produce visible holding outcomes instead of partial records.'],
      },
      {
        id: 'ownership',
        title: 'Claim ownership explicitly',
        paragraphs: ['Successfully imported records preserve their original source identity and are readable but write-protected. An authorized administrator must explicitly claim each record in Guard before local updates are allowed. A holding record has not been written and lists the dependencies that need resolution.'],
      },
      {
        id: 'history',
        title: 'Use receipts for review and recovery',
        paragraphs: ['Package history shows direction, checksum, source system, record and file counts, created and unchanged totals, holding outcomes, and write-lock state. It intentionally excludes package payloads, credentials, signed URLs, and operator identity.'],
      },
    ],
    related: ['vault', 'guard', 'stack'],
  },
  {
    id: 'people',
    group: 'Product areas',
    kicker: 'Users and departments',
    title: 'People',
    summary: 'Organize places, departments, identities, and effective-dated asset assignments without mixing directory ownership into Atlas.',
    appHref: '#workspace-people',
    appLabel: 'Open People',
    searchTerms: ['person', 'identity', 'site', 'building', 'room', 'department', 'assignment', 'location', 'grouper', 'nested group', 'directory import'],
    sections: [
      {
        id: 'directory',
        title: 'Build the directory foundation',
        bullets: ['Sites hold organization locations and postal context.', 'Buildings belong to a site; rooms belong to a building and retain the containing site.', 'Departments can connect to the site context they operate within.', 'Identities represent people, shared accounts, public users, or computer-lab users.'],
      },
      {
        id: 'guided-person',
        title: 'Create a person with location context',
        steps: [
          { title: 'Enter person details', body: 'Provide display name, email, and department as available.' },
          { title: 'Choose or create the place', body: 'Select a visible site, building, or room, or create the missing location when directory write access is available.' },
          { title: 'Review the relationship', body: 'Confirm the exact person and location before submitting. Moving between steps does not clear the draft.' },
        ],
      },
      {
        id: 'assignments',
        title: 'Preserve assignment history',
        paragraphs: ['Asset assignments are effective-dated and support primary user, additional user, and responsible department roles. Ending an assignment preserves the prior stewardship record.'],
      },
      {
        id: 'grouper-sync',
        title: 'Review optional Grouper synchronization',
        paragraphs: ['Administrators can preview a configured, read-only Internet2 Grouper source before applying its normalized groups, nested groups, and memberships. The default local setup does not start or require Grouper.'],
        bullets: ['Use preview counts and item actions to review creates, updates, deactivations, and conflicts before apply.', 'Nested groups and direct subjects appear as semantic member-of relationships in the permission-scoped graph.', 'Provider credentials, endpoints, and raw responses stay server-side and never appear in browser forms or import detail.'],
        callout: { title: 'Source reads remain read-only', body: 'StewardMesh uses GET-only source reads. Fixture create and delete routes exist only in the explicit local integrations profile.', tone: 'info' },
      },
    ],
    related: ['atlas', 'workspace', 'guard'],
  },
  {
    id: 'guard',
    group: 'Administration',
    kicker: 'Authentication and authorization',
    title: 'Guard',
    summary: 'Sign in securely, administer roles and scoped grants, and understand the authorization boundary around every StewardMesh record.',
    appHref: '#workspace-guard',
    appLabel: 'Open Guard',
    searchTerms: ['login', 'bootstrap', 'administrator', 'role', 'permission', 'scope', 'session', 'authorization'],
    sections: [
      {
        id: 'first-admin',
        title: 'Create the first administrator',
        paragraphs: ['A new installation exposes a one-time bootstrap form. The local administrator receives organization-scoped administration access. Shared listeners require a deployment bootstrap token; loopback-only development can use the local setup boundary.'],
        callout: { title: 'Passwords stay server-side', body: 'Local passwords are Argon2id hashes. Sessions use opaque HttpOnly cookies, and CSRF material remains only in application memory.', tone: 'success' },
      },
      {
        id: 'roles',
        title: 'Assign the narrowest useful access',
        bullets: ['Roles group permission strings into reusable policy bundles.', 'Assignments can be organization-, site-, department-, or resource-scoped.', 'Ownership controls protect externally managed records until an authorized user claims them.', 'The interface may hide unavailable actions, but server checks remain authoritative.'],
      },
      {
        id: 'session',
        title: 'Recover safely',
        paragraphs: ['An expired session clears protected browser state and returns to sign-in. Authentication failures do not reveal whether a username exists. When local development has no administrator, use the one-time bootstrap flow rather than placing credentials in environment files.'],
      },
    ],
    related: ['overview', 'workspace', 'guide'],
  },
  {
    id: 'guide',
    group: 'Administration',
    kicker: 'Help and feedback',
    title: 'Guide',
    summary: 'Open contextual help, replay a permission-aware walkthrough, inspect branding accessibility, or prepare a privacy-minimized issue report.',
    appHref: '#workspace-overview',
    appLabel: 'Return to Workspace',
    searchTerms: ['help', 'walkthrough', 'accessibility', 'contrast', 'issue', 'feedback', 'documentation'],
    sections: [
      {
        id: 'context',
        title: 'Get help in context',
        paragraphs: ['Guide opens to the current product topic and provides a plain-language summary, a concrete example, a link to the matching local documentation page, and an in-page destination when that workspace is available.'],
      },
      {
        id: 'walkthrough',
        title: 'Use the optional walkthrough',
        paragraphs: ['Walkthrough steps follow the current session’s readable product areas. You can skip, close with Escape, finish, or replay the tour. Only the new, skipped, or completed preference is saved locally.'],
      },
      {
        id: 'accessibility',
        title: 'Validate branding accessibly',
        paragraphs: ['Guide checks critical color pairs against WCAG contrast requirements before applying optional branding. Unsafe themes fall back as one unit to the verified StewardMesh palette. Status and relationships always require text or shape in addition to color.'],
      },
      {
        id: 'reporting',
        title: 'Review issue context before sending',
        paragraphs: ['The report handoff includes only the page path, component, public version, coarse browser and operating-system family, viewport, and a bounded correlation ID. It excludes identity, role, record values, search terms, cookies, CSRF values, tokens, files, and request bodies.'],
        callout: { title: 'External reporting is explicit', body: 'Local documentation never redirects to GitHub. The separately configured issue destination opens only after you choose to review a sanitized report.', tone: 'warning' },
      },
    ],
    related: ['overview', 'workspace', 'guard'],
  },
]

const pageIDs = new Set(documentationPages.map((page) => page.id))

export const documentationByID = Object.fromEntries(documentationPages.map((page) => [page.id, page])) as Record<DocumentationTopicID, DocumentationPage>

export function documentationHref(topic: DocumentationTopicID = 'overview') {
  return `#docs/${topic}`
}

export function documentationTopicFromHash(hash: string): DocumentationTopicID | null {
  if (hash !== '#docs' && !hash.startsWith('#docs/')) return null
  const candidate = hash.replace(/^#docs\/?/, '').split(/[?#]/, 1)[0]
  return pageIDs.has(candidate as DocumentationTopicID) ? candidate as DocumentationTopicID : 'overview'
}

export function searchDocumentation(query: string) {
  const normalized = query.trim().toLowerCase()
  if (!normalized) return documentationPages
  return documentationPages.filter((page) => [page.title, page.kicker, page.summary, ...page.searchTerms].some((value) => value.toLowerCase().includes(normalized)))
}
