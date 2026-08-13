export type DocumentationTopicID = 'overview' | 'workspace' | 'atlas' | 'horizon' | 'ledger' | 'threads' | 'vault' | 'people' | 'guard' | 'guide'

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
        paragraphs: ['StewardMesh organizes inventory, planning, finance, relationships, files, people, and access into focused product areas. Begin in Workspace, choose the area that owns the task, and use Guide when you need contextual help.'],
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
          'Threads owns hierarchical tags, strategic goals, links, and inheritance provenance.',
          'Vault owns private evidence metadata, integrity checks, and authorized downloads.',
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
    id: 'people',
    group: 'Product areas',
    kicker: 'Users and departments',
    title: 'People',
    summary: 'Organize places, departments, identities, and effective-dated asset assignments without mixing directory ownership into Atlas.',
    appHref: '#workspace-people',
    appLabel: 'Open People',
    searchTerms: ['person', 'identity', 'site', 'building', 'room', 'department', 'assignment', 'location'],
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
