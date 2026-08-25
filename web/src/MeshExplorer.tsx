import { type FormEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ApiRequestError, requestJSON } from './api'
import GraphNodeBlurb from './GraphNodeBlurb'
import InteractiveRelationshipGraph, { type GraphEdge, type GraphNode } from './InteractiveRelationshipGraph'
import MeshSectionNav, { type MeshSection } from './MeshSectionNav'
import { atlasInventoryCounts, applyOverlayGroups, annotateCampusAttributes, chartGroupID, chartGroupKind, displayType, defaultKindColorKey, formatAtlasInventorySummary, graphLimitLabel, graphRecordLimits, identityKindHubsFor, isSyntheticGraphKind, kindsBySource, meshNodeKinds, meshRelationshipKinds, nodePassesCampusFilters, occupancyRoleHubsFor, parseKindColorOverrides, roleGroupKind, sourceForKind, sourceGroupKind, sourceHubsFor, sourceLabels, withoutUnlinkedNodes, type GraphColorMode, type GraphPaletteKey } from './graphModel'
import { meshNodeFromHash } from './graphRecord'
import { defaultMeshQuickView, matchingQuickView, MeshViewPills, type MeshQuickView } from './MeshViewPills'
import { readMeshGraph } from './RelationshipGraphView'
import DataGrid, { type GridListing } from './grid/DataGrid'
import type { GridColumn } from './grid/columns'
import type { GridIdentity } from './grid/viewState'
import { ProductHeader, buttonClass, compactInputClass, cx, emptyStateClass, labelClass, panelClass, plainButtonClass, secondaryButtonClass, subpanelClass } from './ui'
import { meshReadPermissions } from './workspaceAccess'

// Requirement: REQ-DIRECTORY-EXPANSION-008. Feature: threads.relationships.

type MeshGraphState = { nodes: GraphNode[]; edges: GraphEdge[]; sources: string[] }
type Filters = { search: string; relationship: string; limit: string; node: string }
type RecordRow = { id: string; label: string; type: string; source: string; status: string; connections: number }
type RelationshipRow = { id: string; from: string; fromType: string; relationship: string; to: string; toType: string }

const emptyGraph: MeshGraphState = { nodes: [], edges: [], sources: [] }
const emptyFilters: Filters = { search: '', relationship: '', limit: '100', node: '' }
const allKindIDs = meshNodeKinds.map((kind) => kind.id)
const sourceGroups = kindsBySource()
const meshKindColorStorageKey = 'stewardmesh:mesh-kind-colors'

function readStoredKindColors() {
  try {
    return parseKindColorOverrides(JSON.parse(localStorage.getItem(meshKindColorStorageKey) ?? '{}'))
  } catch {
    return {}
  }
}

const recordColumns: GridColumn<RecordRow>[] = [
  { key: 'label', header: 'Record', kind: 'text', width: 18, text: (row) => row.label },
  { key: 'type', header: 'Type', kind: 'enum', width: 12, options: meshNodeKinds.map((kind) => kind.label), text: (row) => row.type },
  { key: 'source', header: 'Product', kind: 'enum', width: 10, options: [...new Set(Object.values(sourceLabels))], text: (row) => row.source },
  { key: 'status', header: 'Status', kind: 'enum', width: 8, options: ['Active', 'Inactive', 'Retired', 'Draft', 'Disposed', 'Ended'], text: (row) => row.status },
  { key: 'connections', header: 'Connections', kind: 'number', width: 8, align: 'right', text: (row) => String(row.connections) },
]

const relationshipColumns: GridColumn<RelationshipRow>[] = [
  { key: 'from', header: 'From', kind: 'text', width: 16, text: (row) => row.from },
  { key: 'fromType', header: 'From type', kind: 'text', width: 10, text: (row) => row.fromType },
  { key: 'relationship', header: 'Relationship', kind: 'text', width: 12, text: (row) => row.relationship },
  { key: 'to', header: 'To', kind: 'text', width: 16, text: (row) => row.to },
  { key: 'toType', header: 'To type', kind: 'text', width: 10, text: (row) => row.toType },
]

function meshQuery(filters: Filters, kinds: readonly string[]) {
  const query = new URLSearchParams()
  if (filters.search.trim()) query.set('search', filters.search.trim())
  if (filters.node.trim()) query.set('node', filters.node.trim())
  if (filters.relationship) query.set('relationship', filters.relationship)
  if (kinds.length > 0 && kinds.length < allKindIDs.length) query.set('kinds', kinds.join(','))
  query.set('limit', filters.limit)
  return query.toString()
}

function filtersFromHash(): Filters {
  const node = meshNodeFromHash(window.location.hash)
  return node ? { ...emptyFilters, node } : emptyFilters
}

export default function MeshExplorer({ csrfToken = '', identity = null, onOpenRecord, permissions }: { csrfToken?: string; identity?: GridIdentity | null; onOpenRecord?: (node: GraphNode) => void; permissions: readonly string[] }) {
  const [section, setSection] = useState<MeshSection>('graph')
  const [filters, setFilters] = useState<Filters>(filtersFromHash)
  const [selectedKinds, setSelectedKinds] = useState<string[]>(allKindIDs)
  const [groupedKinds, setGroupedKinds] = useState<string[]>([])
  const [colorMode, setColorMode] = useState<GraphColorMode>(defaultMeshQuickView.colorMode)
  const [kindColorOverrides, setKindColorOverrides] = useState<Record<string, GraphPaletteKey>>(readStoredKindColors)
  const [hideInactivePeople, setHideInactivePeople] = useState(defaultMeshQuickView.hideInactivePeople)
  const [hideInactiveAssets, setHideInactiveAssets] = useState(defaultMeshQuickView.hideInactiveAssets)
  const [occupancyHubs, setOccupancyHubs] = useState(defaultMeshQuickView.occupancyHubs)
  const [identityHubs, setIdentityHubs] = useState(defaultMeshQuickView.identityHubs)
  const [graph, setGraph] = useState<MeshGraphState>(emptyGraph)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [status, setStatus] = useState('')
  const [selectedNodeID, setSelectedNodeID] = useState('')
  const [focusNodeID, setFocusNodeID] = useState('')
  const [optionsOpen, setOptionsOpen] = useState(false)
  const [enabledSourceHubs, setEnabledSourceHubs] = useState<string[]>([])
  const [chartGroupBy, setChartGroupBy] = useState<string | null>(null)
  const [showUnlinked, setShowUnlinked] = useState(false)
  const [isolatedOnly, setIsolatedOnly] = useState(false)
  const [listing, setListing] = useState<GridListing | null>(null)
  const errorRef = useRef<HTMLDivElement>(null)
  const canRead = meshReadPermissions.some((permission) => permissions.includes(permission))

  const loadGraph = useCallback(async (activeFilters: Filters, kinds: readonly string[], signal?: AbortSignal) => {
    setLoading(true)
    setError('')
    try {
      const response = await requestJSON(`/api/v1/mesh/graph?${meshQuery(activeFilters, kinds)}`, { signal })
      const next = readMeshGraph(response)
      setGraph(next)
      const anchor = activeFilters.node.trim()
      setSelectedNodeID(anchor && next.nodes.some((node) => node.id === anchor) ? anchor : '')
      setFocusNodeID(anchor && next.nodes.some((node) => node.id === anchor) ? anchor : '')
      const sourceSummary = next.sources.length > 0 ? ` from ${next.sources.map((source) => sourceLabels[source] ?? source).join(', ')}` : ''
      const inventorySummary = formatAtlasInventorySummary(atlasInventoryCounts(next.nodes))
      const inventoryText = inventorySummary ? ` Includes ${inventorySummary}.` : ''
      setStatus(`Mesh graph loaded with ${next.nodes.length} ${next.nodes.length === 1 ? 'record' : 'records'} and ${next.edges.length} ${next.edges.length === 1 ? 'relationship' : 'relationships'}${sourceSummary}.${inventoryText}`)
    } catch (loadError) {
      if (loadError instanceof DOMException && loadError.name === 'AbortError') return
      setGraph(emptyGraph)
      setError(loadError instanceof ApiRequestError ? loadError.message : 'The mesh graph could not be loaded.')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    function syncHash() {
      const node = meshNodeFromHash(window.location.hash)
      setFilters((current) => current.node === node ? current : { ...current, node })
    }
    window.addEventListener('hashchange', syncHash)
    return () => window.removeEventListener('hashchange', syncHash)
  }, [])

  useEffect(() => {
    if (!canRead) return
    const controller = new AbortController()
    void loadGraph(filters.node ? { ...emptyFilters, node: filters.node } : emptyFilters, allKindIDs, controller.signal)
    return () => controller.abort()
  }, [canRead, filters.node, loadGraph])

  useEffect(() => {
    if (error) errorRef.current?.focus()
  }, [error])

  useEffect(() => {
    localStorage.setItem(meshKindColorStorageKey, JSON.stringify(kindColorOverrides))
  }, [kindColorOverrides])

  const selectedKindSet = useMemo(() => new Set(selectedKinds), [selectedKinds])
  const kindFiltered = useMemo(() => {
    const nodes = graph.nodes.filter((node) => selectedKindSet.has(node.kind))
    const ids = new Set(nodes.map((node) => node.id))
    return { nodes, edges: graph.edges.filter((edge) => ids.has(edge.from) && ids.has(edge.to)), sources: graph.sources }
  }, [graph, selectedKindSet])

  const statusFiltered = useMemo(() => {
    const keep = filters.node.trim()
    const nodes = kindFiltered.nodes.filter((node) => node.id === keep || nodePassesCampusFilters(node, { hideInactivePeople, hideInactiveAssets }))
    const ids = new Set(nodes.map((node) => node.id))
    return { nodes, edges: kindFiltered.edges.filter((edge) => ids.has(edge.from) && ids.has(edge.to)), sources: kindFiltered.sources }
  }, [filters.node, hideInactiveAssets, hideInactivePeople, kindFiltered])

  const campusGraph = useMemo(() => ({
    nodes: annotateCampusAttributes(statusFiltered.nodes, statusFiltered.edges),
    edges: statusFiltered.edges,
    sources: statusFiltered.sources,
  }), [statusFiltered])

  const listingIDs = useMemo(() => {
    if (!listing || listing.total === 0 || listing.rowIds.length >= listing.total) return null
    return new Set(listing.rowIds)
  }, [listing])
  const plottedGraph = useMemo(() => {
    let nodes = listingIDs ? campusGraph.nodes.filter((node) => listingIDs.has(node.id)) : campusGraph.nodes
    if (isolatedOnly) {
      const linked = new Set<string>()
      for (const edge of campusGraph.edges) {
        linked.add(edge.from)
        linked.add(edge.to)
      }
      nodes = nodes.filter((node) => !linked.has(node.id))
    }
    const ids = new Set(nodes.map((node) => node.id))
    return { nodes, edges: campusGraph.edges.filter((edge) => ids.has(edge.from) && ids.has(edge.to)) }
  }, [campusGraph, isolatedOnly, listingIDs])
  const unlinkedCount = useMemo(() => {
    const linked = new Set<string>()
    for (const edge of plottedGraph.edges) {
      linked.add(edge.from)
      linked.add(edge.to)
    }
    return plottedGraph.nodes.reduce((count, node) => count + (linked.has(node.id) ? 0 : 1), 0)
  }, [plottedGraph])
  const visibleGraph = useMemo(() => {
    const includeUnlinked = showUnlinked || isolatedOnly
    let { nodes, edges } = includeUnlinked ? plottedGraph : withoutUnlinkedNodes(plottedGraph.nodes, plottedGraph.edges)
    const anchor = filters.node.trim()
    if (anchor && !nodes.some((node) => node.id === anchor)) {
      const kept = plottedGraph.nodes.find((node) => node.id === anchor)
      if (kept) nodes = [...nodes, kept]
    }
    const ids = new Set(nodes.map((node) => node.id))
    const chartOverlays = listing?.groupBy && listing.groups.length > 0
      ? listing.groups.map((group) => ({
        id: chartGroupID(listing.groupBy ?? 'group', group.value),
        label: group.value,
        memberIDs: group.rowIds.filter((id) => ids.has(id)),
      })).filter((group) => group.memberIDs.length > 0)
      : []
    const grouped = applyOverlayGroups(nodes, edges, chartOverlays, chartGroupKind)
    const withRoles = occupancyHubs
      ? applyOverlayGroups(grouped.nodes, grouped.edges, occupancyRoleHubsFor(grouped.nodes, grouped.edges), roleGroupKind)
      : grouped
    const withIdentities = identityHubs
      ? applyOverlayGroups(withRoles.nodes, withRoles.edges, identityKindHubsFor(withRoles.nodes), roleGroupKind)
      : withRoles
    const hubSources = listing?.groupBy === 'source' ? [] : enabledSourceHubs
    return applyOverlayGroups(withIdentities.nodes, withIdentities.edges, sourceHubsFor(withIdentities.nodes, hubSources), sourceGroupKind)
  }, [enabledSourceHubs, filters.node, identityHubs, isolatedOnly, listing, occupancyHubs, plottedGraph, showUnlinked])

  const nodesByID = useMemo(() => new Map(visibleGraph.nodes.map((node) => [node.id, node])), [visibleGraph.nodes])
  const selectedNode = selectedNodeID ? nodesByID.get(selectedNodeID) : undefined
  const selectedEdges = useMemo(() => {
    if (!selectedNodeID) return []
    return visibleGraph.edges.filter((edge) => edge.from === selectedNodeID || edge.to === selectedNodeID)
  }, [selectedNodeID, visibleGraph.edges])

  const loadedInventory = useMemo(() => atlasInventoryCounts(graph.nodes), [graph.nodes])
  const visibleInventory = useMemo(() => atlasInventoryCounts(visibleGraph.nodes), [visibleGraph.nodes])

  const recordRows = useMemo<RecordRow[]>(() => {
    const degree = new Map<string, number>()
    for (const node of campusGraph.nodes) degree.set(node.id, 0)
    for (const edge of campusGraph.edges) {
      degree.set(edge.from, (degree.get(edge.from) ?? 0) + 1)
      degree.set(edge.to, (degree.get(edge.to) ?? 0) + 1)
    }
    return campusGraph.nodes.map((node) => ({
      id: node.id,
      label: node.label,
      type: displayType(node.kind),
      source: sourceLabels[sourceForKind(node.kind, node.attributes)] ?? 'Organization',
      status: node.attributes?.status ? displayType(node.attributes.status) : '',
      connections: degree.get(node.id) ?? 0,
    }))
  }, [campusGraph])

  const displayedRecordRows = useMemo(
    () => (isolatedOnly ? recordRows.filter((row) => row.connections === 0) : recordRows),
    [isolatedOnly, recordRows],
  )

  const relationshipRows = useMemo<RelationshipRow[]>(() => campusGraph.edges.map((edge) => ({
    id: edge.id,
    from: nodesByID.get(edge.from)?.label ?? edge.from,
    fromType: displayType(nodesByID.get(edge.from)?.kind ?? ''),
    relationship: displayType(edge.kind),
    to: nodesByID.get(edge.to)?.label ?? edge.to,
    toType: displayType(nodesByID.get(edge.to)?.kind ?? ''),
  })), [campusGraph.edges, nodesByID])

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    void loadGraph(filters, selectedKinds)
  }

  function applyQuickView(view: MeshQuickView) {
    setColorMode(view.colorMode)
    setHideInactivePeople(view.hideInactivePeople)
    setHideInactiveAssets(view.hideInactiveAssets)
    setOccupancyHubs(view.occupancyHubs)
    setIdentityHubs(view.identityHubs)
  }

  function reset() {
    setFilters(emptyFilters)
    setSelectedKinds(allKindIDs)
    setGroupedKinds([])
    applyQuickView(defaultMeshQuickView)
    setKindColorOverrides({})
    setOptionsOpen(false)
    setEnabledSourceHubs([])
    setChartGroupBy(null)
    setShowUnlinked(false)
    setIsolatedOnly(false)
    setListing(null)
    if (meshNodeFromHash(window.location.hash)) {
      window.history.replaceState(null, '', `${window.location.pathname}${window.location.search}#workspace-mesh`)
    }
    void loadGraph(emptyFilters, allKindIDs)
  }

  function setKindColor(kind: string, colorKey: GraphPaletteKey) {
    setKindColorOverrides((current) => {
      const next = { ...current }
      if (colorKey === defaultKindColorKey(kind)) delete next[kind]
      else next[kind] = colorKey
      return next
    })
  }

  function toggleKind(kind: string) {
    setSelectedKinds((current) => {
      const next = current.includes(kind) ? current.filter((value) => value !== kind) : [...current, kind]
      setGroupedKinds((grouped) => grouped.filter((value) => next.includes(value)))
      return next
    })
  }

  function toggleGrouped(kind: string) {
    setGroupedKinds((current) => current.includes(kind) ? current.filter((value) => value !== kind) : [...current, kind])
  }

  function toggleSourceHub(source: string) {
    setEnabledSourceHubs((current) => current.includes(source) ? current.filter((value) => value !== source) : [...current, source])
  }

  function hideKind(kind: string) {
    if (kind === sourceGroupKind) {
      setEnabledSourceHubs([])
      return
    }
    if (kind === chartGroupKind) {
      setChartGroupBy(null)
      return
    }
    setSelectedKinds((current) => current.filter((value) => value !== kind))
    setGroupedKinds((grouped) => grouped.filter((value) => value !== kind))
  }

  function showKind(kind: string) {
    if (kind === sourceGroupKind || kind === chartGroupKind) return
    setSelectedKinds((current) => current.includes(kind) ? current : [...current, kind])
  }

  function rememberListing(next: GridListing) {
    setListing((current) => {
      if (
        current
        && current.total === next.total
        && current.groupBy === next.groupBy
        && current.rowIds.length === next.rowIds.length
        && current.groups.length === next.groups.length
        && current.rowIds.every((id, index) => id === next.rowIds[index])
      ) return current
      return next
    })
  }

  function openRecord(row: RecordRow) {
    setSelectedNodeID(row.id)
    setSection('graph')
  }

  function applyNodeUpdate(next: GraphNode) {
    setGraph((current) => ({
      ...current,
      nodes: current.nodes.map((node) => node.id === next.id ? { ...node, label: next.label, attributes: { ...node.attributes, ...next.attributes } } : node),
    }))
  }

  const colorModeLabel = colorMode === 'campus'
    ? 'occupancy and model'
    : colorMode === 'occupancy'
      ? 'occupancy role'
      : colorMode === 'model'
        ? 'asset model'
        : colorMode === 'source'
          ? 'product'
          : colorMode === 'status' ? 'status' : 'record type'
  const quickView = matchingQuickView({ colorMode, hideInactivePeople, hideInactiveAssets, occupancyHubs, identityHubs })
  const typeSummary = `${selectedKinds.length} of ${allKindIDs.length} types`
  const hiddenKinds = useMemo(() => {
    const present = new Set(graph.nodes.map((node) => node.kind))
    return meshNodeKinds.filter((kind) => present.has(kind.id) && !selectedKindSet.has(kind.id)).map((kind) => ({ kind: kind.id, label: kind.label }))
  }, [graph.nodes, selectedKindSet])
  const availableSources = useMemo(() => {
    const sources = new Set<string>()
    for (const node of visibleGraph.nodes) {
      if (isSyntheticGraphKind(node.kind)) continue
      sources.add(sourceForKind(node.kind, node.attributes) || 'organization')
    }
    return [...sources]
  }, [visibleGraph.nodes])

  return (
    <section aria-labelledby="mesh-heading" className={`${panelClass} space-y-3 p-3 sm:p-4`} data-feature="threads.relationships" data-requirement="REQ-DIRECTORY-EXPANSION-008">
      <ProductHeader
        description="Search, then apply filters. Open graph options only when you need occupancy colors or product hubs."
        headingId="mesh-heading"
        title="See how records connect across StewardMesh"
      />

      <MeshSectionNav active={section} onChange={setSection} />

      {!canRead ? (
        <p className={emptyStateClass}>A product read permission is required to load the mesh graph.</p>
      ) : (
        <>
          <form className="space-y-3" onSubmit={submit}>
            <div className="grid gap-3 lg:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)_9.5rem_auto] lg:items-end">
              <div>
                <label className={labelClass} htmlFor="mesh-search">Search record names</label>
                <input className={`${compactInputClass} mt-1.5 w-full`} id="mesh-search" maxLength={200} onChange={(event) => setFilters((current) => ({ ...current, search: event.target.value }))} type="search" value={filters.search} />
              </div>
              <div>
                <label className={labelClass} htmlFor="mesh-relationship-kind">Relationship type</label>
                <select className={`${compactInputClass} mt-1.5 w-full`} id="mesh-relationship-kind" onChange={(event) => setFilters((current) => ({ ...current, relationship: event.target.value }))} value={filters.relationship}>
                  <option value="">All relationships</option>
                  {meshRelationshipKinds.map(([value, label]) => <option key={value} value={value}>{label}</option>)}
                </select>
              </div>
              <div>
                <label className={labelClass} htmlFor="mesh-limit">Maximum records</label>
                <select className={`${compactInputClass} mt-1.5 w-full`} id="mesh-limit" onChange={(event) => setFilters((current) => ({ ...current, limit: event.target.value }))} value={filters.limit}>
                  {graphRecordLimits.map((value) => <option key={value} value={value}>{graphLimitLabel(value)}</option>)}
                </select>
              </div>
              <div className="flex flex-wrap gap-2">
                <button className={buttonClass} disabled={loading} type="submit">{loading ? 'Loading graph…' : 'Apply graph filters'}</button>
                <button className={secondaryButtonClass} disabled={loading} onClick={reset} type="button">Reset graph filters</button>
              </div>
            </div>

            <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
              <button
                aria-controls="mesh-graph-options"
                aria-expanded={optionsOpen}
                className={plainButtonClass}
                onClick={() => setOptionsOpen((current) => !current)}
                type="button"
              >
                {optionsOpen ? 'Hide graph options' : 'Show graph options'}
              </button>
              <p className="text-sm text-steward-mist-muted">
                {typeSummary} visible · Colored by {colorModeLabel}
                {!showUnlinked && unlinkedCount > 0
                  ? ` · ${unlinkedCount} unlinked ${unlinkedCount === 1 ? 'record' : 'records'} hidden`
                  : showUnlinked ? ' · Showing unlinked records' : ''}
                {graph.sources.length > 0 ? ` · Included products: ${graph.sources.map((source) => sourceLabels[source] ?? source).join(', ')}` : ''}
                {graph.nodes.length > 0 && (loadedInventory.assets > 0 || loadedInventory.models > 0)
                  ? ` · Atlas inventory in this graph: ${formatAtlasInventorySummary(loadedInventory)}.`
                  : ''}
              </p>
            </div>

            {optionsOpen && (
              <div className={`${subpanelClass} grid gap-5 p-4`} id="mesh-graph-options">
                <fieldset>
                  <legend className={labelClass}>Record types to graph</legend>
                  <div className="mt-2 flex flex-wrap gap-2">
                    <button className={plainButtonClass} onClick={() => setSelectedKinds(allKindIDs)} type="button">Select all types</button>
                    <button className={plainButtonClass} onClick={() => { setSelectedKinds([]); setGroupedKinds([]) }} type="button">Clear types</button>
                  </div>
                  <div className="mt-3 grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
                    {sourceGroups.map((group) => (
                      <div key={group.source}>
                        <p className="text-xs font-medium text-steward-mist-muted">{group.label}</p>
                        <ul className="mt-1.5 columns-2 gap-x-3">
                          {group.kinds.map((kind) => (
                            <li key={kind.id}>
                              <label className="flex min-h-9 items-center gap-2 text-sm text-steward-mist">
                                <input checked={selectedKindSet.has(kind.id)} onChange={() => toggleKind(kind.id)} type="checkbox" />
                                {kind.label}
                              </label>
                            </li>
                          ))}
                        </ul>
                      </div>
                    ))}
                  </div>
                </fieldset>

                <fieldset>
                  <legend className={labelClass}>Cluster these types together</legend>
                  <p className="mt-1 text-sm text-steward-mist-muted">Checked types pull into their own grouping. Unchecked types stay in the shared layout.</p>
                  <ul className="mt-2 flex flex-wrap gap-x-4 gap-y-1">
                    {meshNodeKinds.filter((kind) => selectedKindSet.has(kind.id)).map((kind) => (
                      <li key={kind.id}>
                        <label className="flex min-h-9 items-center gap-2 text-sm text-steward-mist">
                          <input checked={groupedKinds.includes(kind.id)} onChange={() => toggleGrouped(kind.id)} type="checkbox" />
                          {kind.label}
                        </label>
                      </li>
                    ))}
                  </ul>
                </fieldset>

                <fieldset>
                  <legend className={labelClass}>Color records by</legend>
                  <div className="mt-2 flex flex-wrap gap-4">
                    {([['campus', 'Campus: occupancy + model'], ['occupancy', 'Occupancy role'], ['model', 'Asset model'], ['type', 'Record type'], ['source', 'Product'], ['status', 'Status']] as const).map(([value, label]) => (
                      <label className="flex min-h-9 items-center gap-2 text-sm text-steward-mist" key={value}>
                        <input checked={colorMode === value} name="mesh-color-mode" onChange={() => setColorMode(value)} type="radio" value={value} />
                        {label}
                      </label>
                    ))}
                  </div>
                </fieldset>
              </div>
            )}
          </form>

          <MeshViewPills
            colorMode={colorMode}
            hideInactiveAssets={hideInactiveAssets}
            hideInactivePeople={hideInactivePeople}
            identityHubs={identityHubs}
            occupancyHubs={occupancyHubs}
            onColorModeChange={setColorMode}
            onHideInactiveAssetsChange={setHideInactiveAssets}
            onHideInactivePeopleChange={setHideInactivePeople}
            onIdentityHubsChange={setIdentityHubs}
            onOccupancyHubsChange={setOccupancyHubs}
            onQuickViewChange={applyQuickView}
            quickView={quickView}
          />

          <div className="flex flex-wrap items-end gap-4">
            {availableSources.length > 0 && (
              <div className="min-w-0 flex-1">
                <p className={labelClass}>Product hubs</p>
                <ul className="mt-1.5 flex flex-wrap gap-1.5">
                  {availableSources.map((source) => {
                    const label = source === 'organization' ? 'Organization' : (sourceLabels[source] ?? displayType(source))
                    const pressed = enabledSourceHubs.includes(source)
                    return (
                      <li key={source}>
                        <button
                          aria-pressed={pressed}
                          className={cx(
                            'rounded-full border px-2.5 py-1 text-xs font-medium transition',
                            pressed ? 'border-steward-teal/60 bg-steward-teal/15 text-steward-mist' : 'border-white/12 text-steward-mist-muted hover:border-white/25 hover:text-steward-mist',
                          )}
                          onClick={() => toggleSourceHub(source)}
                          type="button"
                        >
                          {pressed ? `Hide ${label}` : `Show ${label}`}
                        </button>
                      </li>
                    )
                  })}
                </ul>
              </div>
            )}
            <div>
              <label className={labelClass} htmlFor="mesh-chart-group">Group as nodes</label>
              <select
                className={`${compactInputClass} mt-1.5`}
                id="mesh-chart-group"
                onChange={(event) => setChartGroupBy(event.target.value || null)}
                value={chartGroupBy ?? ''}
              >
                <option value="">No extra group nodes</option>
                {recordColumns.map((column) => <option key={column.key} value={column.key}>{column.header}</option>)}
              </select>
            </div>
            <label className="flex min-h-10 items-center gap-2 text-sm text-steward-mist" htmlFor="mesh-show-unlinked">
              <input checked={showUnlinked} id="mesh-show-unlinked" onChange={(event) => setShowUnlinked(event.target.checked)} type="checkbox" />
              Show unlinked records
            </label>
            <label className="flex min-h-10 items-center gap-2 text-sm text-steward-mist" htmlFor="mesh-isolated-only">
              <input checked={isolatedOnly} id="mesh-isolated-only" onChange={(event) => setIsolatedOnly(event.target.checked)} type="checkbox" />
              Isolated records only
            </label>
          </div>

          {filters.node ? (
            <p className="text-sm text-steward-mist-muted">
              Showing the neighborhood of <strong className="font-medium text-steward-mist">{nodesByID.get(filters.node)?.label ?? filters.node}</strong>. Reset graph filters to return to the full graph.
            </p>
          ) : null}

          {error && <div className="rounded-xl border border-steward-danger/50 bg-steward-danger/15 p-4 text-[#ffccd1]" ref={errorRef} role="alert" tabIndex={-1}>{error}</div>}
          <p aria-live="polite" className="sr-only" role="status">{status}</p>
          {graph.nodes.length > 0 && (loadedInventory.assets > 0 || loadedInventory.models > 0) && (visibleInventory.assets !== loadedInventory.assets || visibleInventory.models !== loadedInventory.models) && formatAtlasInventorySummary(visibleInventory) !== '' && (
            <p className="text-sm text-steward-mist-muted">{formatAtlasInventorySummary(visibleInventory)} visible with the current graph filters.</p>
          )}

          <div aria-labelledby="mesh-tab-graph" hidden={section !== 'graph'} id="mesh-panel-graph" role="tabpanel">
            {!error && !loading && visibleGraph.nodes.length === 0 && hiddenKinds.length === 0 ? (
              <p className={`${emptyStateClass} mt-2`}>
                {!showUnlinked && unlinkedCount > 0
                  ? `No connected records match these graph filters. Turn on Show unlinked records to include ${unlinkedCount} isolated ${unlinkedCount === 1 ? 'record' : 'records'}.`
                  : 'No visible records match these graph filters.'}
              </p>
            ) : !error ? (
              <div aria-busy={loading} className={cx(subpanelClass, 'p-3 sm:p-4')}>
                <InteractiveRelationshipGraph
                  colorMode={colorMode}
                  edges={visibleGraph.edges}
                  focusNodeID={focusNodeID}
                  groupedKinds={groupedKinds}
                  hiddenKinds={hiddenKinds}
                  kindColorOverrides={kindColorOverrides}
                  onHideKind={hideKind}
                  onKindColorChange={setKindColor}
                  onShowKind={showKind}
                  inspector={selectedNode ? (
                    <GraphNodeBlurb
                      csrfToken={csrfToken}
                      edges={selectedEdges}
                      focusNodeID={focusNodeID}
                      node={selectedNode}
                      nodesByID={nodesByID}
                      onClearFocus={() => setFocusNodeID('')}
                      onClose={() => setSelectedNodeID('')}
                      onFocusNode={setFocusNodeID}
                      onNodeUpdated={applyNodeUpdate}
                      onOpenRecord={onOpenRecord}
                      onSelectNode={setSelectedNodeID}
                      permissions={permissions}
                    />
                  ) : null}
                  nodes={visibleGraph.nodes}
                  onSelectNode={(nodeID) => setSelectedNodeID((current) => current === nodeID ? '' : nodeID)}
                  selectedNodeID={selectedNodeID}
                />
              </div>
            ) : null}
          </div>

          <div aria-labelledby="mesh-tab-data" hidden={section !== 'data'} id="mesh-panel-data" role="tabpanel">
            {section === 'data' && !error && !loading && displayedRecordRows.length === 0 ? (
              <p className={`${emptyStateClass} mt-2`}>
                {isolatedOnly
                  ? 'No isolated records match these graph filters. Isolated means no operational relationships in the current graph.'
                  : 'No visible records match these graph filters.'}
              </p>
            ) : !error ? (
              <div aria-busy={loading} className="grid min-w-0 gap-6" hidden={section !== 'data'}>
                <p className="text-sm leading-6 text-steward-mist-muted">
                  Filter with the query editor, group rows from the dropdown, and export the current view to Excel. Isolated records have no operational relationships; organization membership is not counted. Grouping and visible rows also become extra nodes on the graph.
                </p>
                <DataGrid
                  columns={recordColumns}
                  emptyMessage={isolatedOnly ? 'No isolated records match these graph filters.' : 'No records match these graph filters.'}
                  focusRowId={selectedNodeID}
                  groupBy={chartGroupBy}
                  identity={identity}
                  label="Mesh records"
                  onGroupByChange={setChartGroupBy}
                  onListingChange={rememberListing}
                  onOpenRow={openRecord}
                  rowId={(row) => row.id}
                  rowLabel={(row) => row.label}
                  rows={displayedRecordRows}
                  viewId="mesh-records"
                />
                <DataGrid
                  columns={relationshipColumns}
                  emptyMessage="No relationships connect the matching records."
                  identity={identity}
                  label="Mesh relationships"
                  maximumBodyHeight="24rem"
                  rowId={(row) => row.id}
                  rowLabel={(row) => `${row.from} ${row.relationship} ${row.to}`}
                  rows={relationshipRows}
                  viewId="mesh-relationships"
                />
              </div>
            ) : null}
          </div>
        </>
      )}
    </section>
  )
}
