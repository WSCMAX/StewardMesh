// Requirement: REQ-DIRECTORY-EXPANSION-008. Feature: threads.relationships.

export type GraphColorMode = 'type' | 'source' | 'status' | 'campus' | 'model' | 'occupancy'

export type NodePaint = {
  fill: string
  stroke: string
  fills?: readonly string[]
}

export type GraphPaletteColor = {
  id: string
  label: string
  fill: string
  stroke: string
}

// Curated fill/stroke pairs tuned for the dark steward canvas. The stroke is
// what nodes and legend swatches render; fill is the deeper tint for grouping.
export const graphTypePalette: readonly GraphPaletteColor[] = [
  { id: 'teal', label: 'Teal', fill: '#0e3a3d', stroke: '#2dc4b2' },
  { id: 'emerald', label: 'Emerald', fill: '#15342f', stroke: '#3ecf8e' },
  { id: 'sky', label: 'Sky', fill: '#1a2f3d', stroke: '#6bb6ff' },
  { id: 'blue', label: 'Blue', fill: '#1a2332', stroke: '#60a5fa' },
  { id: 'cyan', label: 'Cyan', fill: '#0e2f3a', stroke: '#22d3ee' },
  { id: 'violet', label: 'Violet', fill: '#24183a', stroke: '#c084fc' },
  { id: 'purple', label: 'Purple', fill: '#2a2440', stroke: '#b794f6' },
  { id: 'fuchsia', label: 'Fuchsia', fill: '#2a1a3d', stroke: '#e879f9' },
  { id: 'rose', label: 'Rose', fill: '#3a1f2d', stroke: '#ff8fab' },
  { id: 'amber', label: 'Amber', fill: '#3a2a14', stroke: '#f0b429' },
  { id: 'orange', label: 'Orange', fill: '#3a2410', stroke: '#fb923c' },
  { id: 'gold', label: 'Gold', fill: '#2f2a12', stroke: '#eab308' },
  { id: 'slate', label: 'Slate', fill: '#1f2a3a', stroke: '#94a3b8' },
  { id: 'navy', label: 'Navy', fill: '#123d52', stroke: '#4ea8ff' },
  { id: 'mint', label: 'Mint', fill: '#143322', stroke: '#4ade80' },
  { id: 'rust', label: 'Rust', fill: '#3a2018', stroke: '#f97316' },
] as const

export type GraphPaletteKey = (typeof graphTypePalette)[number]['id']

const paletteByID = new Map(graphTypePalette.map((entry) => [entry.id, entry]))

export function graphPaletteColor(key: string) {
  return paletteByID.get(key) ?? paletteByID.get('slate')!
}

// Assign each mesh record type a palette entry. Edit this map to re-color types
// without touching the palette itself.
export const meshKindColorKeys: Record<string, GraphPaletteKey> = {
  organization: 'navy',
  site: 'teal',
  building: 'emerald',
  room: 'sky',
  department: 'purple',
  person: 'teal',
  shared: 'amber',
  public: 'amber',
  lab: 'rose',
  group: 'purple',
  subject: 'slate',
  asset: 'blue',
  model: 'cyan',
  vendor: 'amber',
  purchase_order: 'orange',
  contract: 'rust',
  budget: 'gold',
  commitment: 'gold',
  product: 'violet',
  version: 'violet',
  license: 'fuchsia',
  label: 'rose',
  goal: 'mint',
  document: 'slate',
  plan: 'cyan',
  source_group: 'navy',
  chart_group: 'gold',
  role_group: 'gold',
}

export type KindMeta = {
  id: string
  label: string
  abbreviation: string
  source: string
  fill: string
  stroke: string
  colorKey: GraphPaletteKey
}

function kindMeta(
  id: string,
  label: string,
  abbreviation: string,
  source: string,
  colorKey: GraphPaletteKey,
): KindMeta {
  const colors = graphPaletteColor(colorKey)
  return { id, label, abbreviation, source, fill: colors.fill, stroke: colors.stroke, colorKey }
}

export const meshNodeKinds: readonly KindMeta[] = [
  kindMeta('organization', 'Organization', 'ORG', '', 'navy'),
  kindMeta('site', 'Site', 'SITE', 'people', 'teal'),
  kindMeta('building', 'Building', 'BLD', 'people', 'emerald'),
  kindMeta('room', 'Room', 'RM', 'people', 'sky'),
  kindMeta('department', 'Department', 'DEPT', 'people', 'purple'),
  kindMeta('person', 'Person', 'PER', 'people', 'teal'),
  kindMeta('shared', 'Shared identity', 'SHR', 'people', 'amber'),
  kindMeta('public', 'Public users', 'PUB', 'people', 'amber'),
  kindMeta('lab', 'Lab users', 'LAB', 'people', 'rose'),
  kindMeta('group', 'Imported group', 'GRP', 'people', 'purple'),
  kindMeta('subject', 'Imported subject', 'SUB', 'people', 'slate'),
  kindMeta('asset', 'Asset', 'AST', 'atlas', 'blue'),
  kindMeta('model', 'Asset model', 'MDL', 'atlas', 'cyan'),
  kindMeta('vendor', 'Vendor', 'VND', 'ledger', 'amber'),
  kindMeta('purchase_order', 'Purchase order', 'PO', 'ledger', 'orange'),
  kindMeta('contract', 'Contract', 'CON', 'ledger', 'rust'),
  kindMeta('budget', 'Budget', 'BGT', 'ledger', 'gold'),
  kindMeta('commitment', 'Commitment', 'CMT', 'ledger', 'gold'),
  kindMeta('product', 'Software product', 'PRD', 'stack', 'violet'),
  kindMeta('version', 'Software version', 'VER', 'stack', 'violet'),
  kindMeta('license', 'License', 'LIC', 'stack', 'fuchsia'),
  kindMeta('label', 'Tag', 'TAG', 'labels', 'rose'),
  kindMeta('goal', 'Goal', 'GOAL', 'goals', 'mint'),
  kindMeta('document', 'Document', 'DOC', 'vault', 'slate'),
  kindMeta('plan', 'Plan', 'PLAN', 'horizon', 'cyan'),
]

export const meshRelationshipKinds = [
  ['contains', 'Contains'],
  ['belongs_to', 'Belongs to'],
  ['located_at', 'Located at'],
  ['member_of', 'Member of'],
  ['assigned_to', 'Assigned to'],
  ['uses_office', 'Uses office'],
  ['teaches_in', 'Teaches in'],
  ['attends_class', 'Attends class'],
  ['resides_in', 'Resides in'],
  ['uses_lab', 'Uses lab'],
  ['tagged_with', 'Tagged with'],
  ['advances', 'Advances'],
  ['purchased_via', 'Purchased via'],
  ['supplied_by', 'Supplied by'],
  ['covered_by', 'Covered by'],
  ['documented_by', 'Documented by'],
  ['modeled_as', 'Modeled as'],
  ['installed_on', 'Installed on'],
  ['planned_for', 'Planned for'],
] as const

export const sourceLabels: Record<string, string> = {
  people: 'People',
  atlas: 'Atlas',
  ledger: 'Ledger',
  stack: 'Stack',
  labels: 'Tags',
  goals: 'Goals',
  vault: 'Vault',
  horizon: 'Horizon',
}

export const sourceColorKeys: Record<string, GraphPaletteKey> = {
  people: 'teal',
  atlas: 'blue',
  ledger: 'amber',
  stack: 'violet',
  labels: 'rose',
  goals: 'mint',
  vault: 'slate',
  horizon: 'cyan',
}

export const statusColorKeys: Record<string, GraphPaletteKey> = {
  active: 'mint',
  inactive: 'slate',
  retired: 'amber',
  draft: 'blue',
  disposed: 'rose',
  ended: 'gold',
}

export const sourceColors: Record<string, { fill: string; stroke: string }> = Object.fromEntries(
  Object.entries(sourceColorKeys).map(([source, key]) => [source, graphPaletteColor(key)]),
)

const defaultPalette = graphPaletteColor('slate')
const defaultColors: NodePaint = { fill: defaultPalette.fill, stroke: defaultPalette.stroke }

export const identityNodeKinds = ['person', 'shared', 'public', 'lab'] as const
export type OccupancyRole = 'instructor' | 'student' | 'resident' | 'office' | 'lab'

export const occupancyRoleMeta: Record<OccupancyRole, { edge: string; label: string; plural: string; colorKey: GraphPaletteKey }> = {
  instructor: { edge: 'teaches_in', label: 'Instructor', plural: 'Instructors', colorKey: 'violet' },
  student: { edge: 'attends_class', label: 'Student', plural: 'Students', colorKey: 'sky' },
  resident: { edge: 'resides_in', label: 'Resident', plural: 'Residents', colorKey: 'amber' },
  office: { edge: 'uses_office', label: 'Office', plural: 'Office users', colorKey: 'teal' },
  lab: { edge: 'uses_lab', label: 'Lab user', plural: 'Lab users', colorKey: 'rose' },
}

export const occupancyRoles = Object.keys(occupancyRoleMeta) as OccupancyRole[]
const occupancyRoleByEdge = new Map(occupancyRoles.map((role) => [occupancyRoleMeta[role].edge, role]))

export const inactiveAssetStatuses = ['retired', 'disposed', 'draft', 'inactive'] as const
export const inactivePersonStatuses = ['inactive', 'retired'] as const

export const maximumGraphNodes = 50_000
export const maximumGraphEdges = 200_000
export const denseGraphNodeThreshold = 2000
export const largeGraphNodeThreshold = 5000

export const graphRecordLimits = ['25', '50', '100', '250', '500', '1000', '2000', '2500', '5000', 'all'] as const

export function graphLimitLabel(value: string) {
  return value === 'all' ? 'All' : value
}

export function atlasInventoryCounts(nodes: readonly { kind: string }[]) {
  let assets = 0
  let models = 0
  for (const node of nodes) {
    if (node.kind === 'asset') assets += 1
    else if (node.kind === 'model') models += 1
  }
  return { assets, models }
}

export function formatAtlasInventorySummary(counts: { assets: number; models: number }) {
  const parts: string[] = []
  if (counts.assets > 0) parts.push(`${counts.assets.toLocaleString()} ${counts.assets === 1 ? 'asset' : 'assets'}`)
  if (counts.models > 0) parts.push(`${counts.models.toLocaleString()} ${counts.models === 1 ? 'model' : 'models'}`)
  return parts.join(', ')
}
const kindByID = new Map(meshNodeKinds.map((kind) => [kind.id, kind]))

export const sourceGroupKind = 'source_group'
export const chartGroupKind = 'chart_group'
export const roleGroupKind = 'role_group'

export function isSyntheticGraphKind(kind: string) {
  return kind === sourceGroupKind || kind === chartGroupKind || kind === roleGroupKind
}

export function graphSlug(value: string) {
  const slug = value.trim().toLowerCase().replace(/[^a-z0-9._:-]+/g, '_').replace(/^_+|_+$/g, '')
  return slug || 'blank'
}

export function sourceHubID(source: string) {
  return `${sourceGroupKind}:${graphSlug(source || 'organization')}`
}

export function chartGroupID(field: string, value: string) {
  return `${chartGroupKind}:${graphSlug(field)}:${graphSlug(value)}`
}

export function displayType(value: string) {
  if (value === sourceGroupKind) return 'Product hub'
  if (value === chartGroupKind) return 'Group'
  if (value === roleGroupKind) return 'User type'
  return kindByID.get(value)?.label ?? value.replaceAll('_', ' ').replace(/\b\w/g, (character) => character.toUpperCase())
}

export function abbreviationForKind(kind: string) {
  return kindByID.get(kind)?.abbreviation ?? kind.slice(0, 3).toUpperCase()
}

export function sourceForKind(kind: string, attributes?: Record<string, string>) {
  const fromAttributes = attributes?.source?.trim()
  if (fromAttributes) return fromAttributes
  return kindByID.get(kind)?.source ?? ''
}

export function defaultKindColorKey(kind: string) {
  return kindByID.get(kind)?.colorKey ?? meshKindColorKeys[kind] ?? 'slate'
}

export function parseKindColorOverrides(raw: unknown): Record<string, GraphPaletteKey> {
  if (!raw || typeof raw !== 'object') return {}
  const paletteIDs = new Set(graphTypePalette.map((entry) => entry.id))
  const result: Record<string, GraphPaletteKey> = {}
  for (const [kind, key] of Object.entries(raw)) {
    if (typeof key === 'string' && paletteIDs.has(key)) result[kind] = key as GraphPaletteKey
  }
  return result
}

export function isIdentityNodeKind(kind: string) {
  return (identityNodeKinds as readonly string[]).includes(kind)
}

export function parseOccupancyRoles(value: string | undefined): OccupancyRole[] {
  if (!value?.trim()) return []
  const known = new Set<OccupancyRole>()
  const roles: OccupancyRole[] = []
  for (const token of value.split(',')) {
    const role = token.trim() as OccupancyRole
    if (!occupancyRoleMeta[role] || known.has(role)) continue
    known.add(role)
    roles.push(role)
  }
  return roles
}

export function occupancyRolesFor(nodeID: string, edges: readonly { from: string; to: string; kind: string }[]): OccupancyRole[] {
  const known = new Set<OccupancyRole>()
  const roles: OccupancyRole[] = []
  for (const edge of edges) {
    if (edge.from !== nodeID && edge.to !== nodeID) continue
    const role = occupancyRoleByEdge.get(edge.kind)
    if (!role || known.has(role)) continue
    known.add(role)
    roles.push(role)
  }
  return roles
}

export function occupancyRoleHubID(role: OccupancyRole) {
  return `${roleGroupKind}:${role}`
}

export function paletteKeyForLabel(value: string): GraphPaletteKey {
  const normalized = value.trim().toLowerCase()
  if (!normalized) return 'slate'
  let hash = 0
  for (let index = 0; index < normalized.length; index += 1) {
    hash = (hash * 33 + normalized.charCodeAt(index)) >>> 0
  }
  return graphTypePalette[hash % graphTypePalette.length].id
}

export function gradientDOMID(nodeID: string) {
  return `mesh-grad-${nodeID.replace(/[^A-Za-z0-9_-]/g, '_')}`
}

export function paintFill(paint: NodePaint, nodeID: string) {
  if (paint.fills && paint.fills.length >= 2) return `url(#${gradientDOMID(nodeID)})`
  return paint.fill
}

export function colorsForNode(
  kind: string,
  attributes: Record<string, string> | undefined,
  mode: GraphColorMode,
  kindColorOverrides?: Readonly<Record<string, GraphPaletteKey>>,
): NodePaint {
  if (mode === 'source') {
    const source = sourceForKind(kind, attributes)
    const key = sourceColorKeys[source]
    return key ? graphPaletteColor(key) : defaultColors
  }
  if (mode === 'status') {
    const status = attributes?.status?.trim().toLowerCase() ?? ''
    const key = statusColorKeys[status]
    return key ? graphPaletteColor(key) : defaultColors
  }
  if (kind === roleGroupKind) {
    const role = parseOccupancyRoles(attributes?.role)[0]
    if (role) return graphPaletteColor(occupancyRoleMeta[role].colorKey)
    const identityKind = attributes?.identityKind?.trim()
    if (identityKind) return graphPaletteColor(defaultKindColorKey(identityKind))
  }
  if (mode === 'occupancy' || (mode === 'campus' && isIdentityNodeKind(kind))) {
    const roles = parseOccupancyRoles(attributes?.roles)
    if (roles.length >= 2) {
      const paints = roles.map((role) => graphPaletteColor(occupancyRoleMeta[role].colorKey))
      return { fill: paints[0].fill, stroke: paints[0].stroke, fills: paints.map((paint) => paint.stroke) }
    }
    if (roles.length === 1) return graphPaletteColor(occupancyRoleMeta[roles[0]].colorKey)
    if (mode === 'occupancy') return defaultColors
  }
  if (mode === 'model' || (mode === 'campus' && kind === 'asset')) {
    const model = attributes?.model?.trim()
    if (model) return graphPaletteColor(paletteKeyForLabel(model))
    if (mode === 'model') return defaultColors
  }
  const key = kindColorOverrides?.[kind] ?? defaultKindColorKey(kind)
  return graphPaletteColor(key)
}

export function kindGraphColor(kind: string, kindColorOverrides?: Readonly<Record<string, GraphPaletteKey>>) {
  return colorsForNode(kind, undefined, 'type', kindColorOverrides)
}

export function kindsBySource() {
  const groups: { source: string; label: string; kinds: KindMeta[] }[] = []
  const index = new Map<string, { source: string; label: string; kinds: KindMeta[] }>()
  for (const kind of meshNodeKinds) {
    const source = kind.source || 'organization'
    const label = source === 'organization' ? 'Organization' : (sourceLabels[source] ?? displayType(source))
    let group = index.get(source)
    if (!group) {
      group = { source, label, kinds: [] }
      index.set(source, group)
      groups.push(group)
    }
    group.kinds.push(kind)
  }
  return groups
}

export type OverlayGroup = {
  id: string
  label: string
  memberIDs: readonly string[]
  attributes?: Record<string, string>
}

type OverlayNode = { id: string; kind: string; label: string; attributes?: Record<string, string> }
type OverlayEdge = { id: string; from: string; to: string; kind: string }

export function sourceHubsFor(nodes: readonly OverlayNode[], sources: readonly string[]): OverlayGroup[] {
  const wanted = new Set(sources)
  if (wanted.size === 0) return []
  const members = new Map<string, string[]>()
  for (const node of nodes) {
    if (isSyntheticGraphKind(node.kind)) continue
    const source = sourceForKind(node.kind, node.attributes) || 'organization'
    if (!wanted.has(source)) continue
    const list = members.get(source)
    if (list) list.push(node.id)
    else members.set(source, [node.id])
  }
  return [...wanted].flatMap((source) => {
    const memberIDs = members.get(source) ?? []
    if (memberIDs.length === 0) return []
    return [{
      id: sourceHubID(source),
      label: source === 'organization' ? 'Organization' : (sourceLabels[source] ?? displayType(source)),
      memberIDs,
    }]
  })
}

export function annotateCampusAttributes<N extends OverlayNode, E extends OverlayEdge>(
  nodes: readonly N[],
  edges: readonly E[],
): N[] {
  const modelLabelByID = new Map<string, string>()
  for (const node of nodes) {
    if (node.kind === 'model') modelLabelByID.set(node.id, node.label)
  }
  const modelByAsset = new Map<string, string>()
  for (const edge of edges) {
    if (edge.kind !== 'modeled_as') continue
    const label = modelLabelByID.get(edge.to) ?? modelLabelByID.get(edge.from)
    if (!label) continue
    if (edge.from.startsWith('asset:')) modelByAsset.set(edge.from, label)
    if (edge.to.startsWith('asset:')) modelByAsset.set(edge.to, label)
  }
  return nodes.map((node) => {
    const next = { ...(node.attributes ?? {}) }
    let changed = false
    if (isIdentityNodeKind(node.kind)) {
      const roles = occupancyRolesFor(node.id, edges)
      if (roles.length > 0) {
        next.roles = roles.join(',')
        changed = true
      }
    }
    if (node.kind === 'asset') {
      const model = modelByAsset.get(node.id)
      if (model) {
        next.model = model
        changed = true
      }
    }
    return changed ? { ...node, attributes: next } : node
  })
}

export function occupancyRoleHubsFor(nodes: readonly OverlayNode[], edges: readonly OverlayEdge[]): OverlayGroup[] {
  const members = new Map<OccupancyRole, string[]>()
  for (const node of nodes) {
    if (!isIdentityNodeKind(node.kind) || isSyntheticGraphKind(node.kind)) continue
    for (const role of occupancyRolesFor(node.id, edges)) {
      const list = members.get(role)
      if (list) list.push(node.id)
      else members.set(role, [node.id])
    }
  }
  return occupancyRoles.flatMap((role) => {
    const memberIDs = members.get(role) ?? []
    if (memberIDs.length === 0) return []
    return [{
      id: occupancyRoleHubID(role),
      label: occupancyRoleMeta[role].plural,
      memberIDs,
      attributes: { role },
    }]
  })
}

export function identityKindHubsFor(nodes: readonly OverlayNode[]): OverlayGroup[] {
  const members = new Map<string, string[]>()
  for (const node of nodes) {
    if (!isIdentityNodeKind(node.kind)) continue
    const list = members.get(node.kind)
    if (list) list.push(node.id)
    else members.set(node.kind, [node.id])
  }
  return identityNodeKinds.flatMap((kind) => {
    const memberIDs = members.get(kind) ?? []
    if (memberIDs.length === 0) return []
    return [{
      id: `${roleGroupKind}:identity_${kind}`,
      label: kindByID.get(kind)?.label ?? displayType(kind),
      memberIDs,
      attributes: { identityKind: kind },
    }]
  })
}

export type CampusStatusFilters = {
  hideInactivePeople?: boolean
  hideInactiveAssets?: boolean
}

export function nodePassesCampusFilters(
  node: { kind: string; attributes?: Record<string, string> },
  filters: CampusStatusFilters,
) {
  const status = node.attributes?.status?.trim().toLowerCase() ?? ''
  if (filters.hideInactivePeople && isIdentityNodeKind(node.kind) && (inactivePersonStatuses as readonly string[]).includes(status)) {
    return false
  }
  if (filters.hideInactiveAssets && node.kind === 'asset' && (inactiveAssetStatuses as readonly string[]).includes(status)) {
    return false
  }
  return true
}

export function withoutUnlinkedNodes<N extends { id: string }, E extends { from: string; to: string }>(
  nodes: readonly N[],
  edges: readonly E[],
): { nodes: N[]; edges: E[] } {
  const linked = new Set<string>()
  for (const edge of edges) {
    linked.add(edge.from)
    linked.add(edge.to)
  }
  const nextNodes = nodes.filter((node) => linked.has(node.id))
  const ids = new Set(nextNodes.map((node) => node.id))
  return { nodes: nextNodes, edges: edges.filter((edge) => ids.has(edge.from) && ids.has(edge.to)) }
}

export function applyOverlayGroups(
  nodes: readonly OverlayNode[],
  edges: readonly OverlayEdge[],
  overlays: readonly OverlayGroup[],
  kind: string,
): { nodes: OverlayNode[]; edges: OverlayEdge[] } {
  if (overlays.length === 0) return { nodes: [...nodes], edges: [...edges] }
  const nextNodes = [...nodes]
  const nextEdges = [...edges]
  const known = new Set(nodes.map((node) => node.id))
  for (const overlay of overlays) {
    if (!known.has(overlay.id)) {
      nextNodes.push({
        id: overlay.id,
        kind,
        label: overlay.label,
        attributes: {
          overlay: kind,
          source: kind === sourceGroupKind ? overlay.id.slice(`${sourceGroupKind}:`.length) : '',
          ...overlay.attributes,
        },
      })
      known.add(overlay.id)
    }
    for (const memberID of overlay.memberIDs) {
      if (!known.has(memberID)) continue
      nextEdges.push({
        id: `belongs_to:${overlay.id}:${memberID}`,
        from: memberID,
        to: overlay.id,
        kind: 'belongs_to',
      })
    }
  }
  return { nodes: nextNodes, edges: nextEdges }
}
