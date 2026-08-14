import { type FormEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ApiRequestError, requestJSON } from './api'
import { buttonClass, emptyStateClass, inputClass, labelClass, secondaryButtonClass, subpanelClass, tableWrapClass } from './ui'

// Requirement: REQ-DIRECTORY-EXPANSION-008. Feature: threads.relationships.

type GraphNode = {
  id: string
  kind: string
  label: string
  attributes?: Record<string, string>
}

type GraphEdge = {
  id: string
  from: string
  to: string
  kind: string
  attributes?: Record<string, string>
}

type Graph = { nodes: GraphNode[]; edges: GraphEdge[] }
type Filters = { search: string; kind: string; relationship: string; limit: string }

const emptyGraph: Graph = { nodes: [], edges: [] }
const emptyFilters: Filters = { search: '', kind: '', relationship: '', limit: '100' }
const graphIDPattern = /^[a-z][a-z0-9_-]{0,63}:[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$/
const graphTypePattern = /^[a-z][a-z0-9_-]{0,63}$/

const nodeKinds = [
  ['organization', 'Organization'], ['site', 'Site'], ['building', 'Building'], ['room', 'Room'],
  ['department', 'Department'], ['person', 'Person'], ['shared', 'Shared identity'], ['public', 'Public users'],
  ['lab', 'Lab users'], ['group', 'Imported group'], ['subject', 'Imported subject'], ['asset', 'Asset'],
] as const
const relationshipKinds = [
  ['contains', 'Contains'], ['belongs_to', 'Belongs to'], ['located_at', 'Located at'],
  ['member_of', 'Member of'], ['assigned_to', 'Assigned to'],
] as const

const runeLength = (value: string) => [...value].length

function isAttributes(value: unknown): value is Record<string, string> | undefined {
  if (value === undefined) return true
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return false
  const entries = Object.entries(value)
  return entries.length <= 8 && entries.every(([key, entry]) => graphTypePattern.test(key) && typeof entry === 'string' && runeLength(entry) <= 200)
}

function isGraphNode(value: unknown): value is GraphNode {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return false
  const node = value as Record<string, unknown>
  return typeof node.id === 'string' && graphIDPattern.test(node.id)
    && typeof node.kind === 'string' && graphTypePattern.test(node.kind)
    && node.id.startsWith(`${node.kind}:`)
    && typeof node.label === 'string' && node.label.trim().length > 0 && runeLength(node.label) <= 320
    && isAttributes(node.attributes)
}

function isGraphEdge(value: unknown): value is GraphEdge {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return false
  const edge = value as Record<string, unknown>
  return typeof edge.id === 'string' && edge.id.length > 0 && edge.id.length <= 320
    && typeof edge.from === 'string' && graphIDPattern.test(edge.from)
    && typeof edge.to === 'string' && graphIDPattern.test(edge.to)
    && typeof edge.kind === 'string' && graphTypePattern.test(edge.kind)
    && isAttributes(edge.attributes)
}

export function readRelationshipGraph(value: unknown): Graph {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error('invalid relationship graph response')
  const candidate = value as Record<string, unknown>
  if (!Array.isArray(candidate.nodes) || candidate.nodes.length > 500 || !candidate.nodes.every(isGraphNode)
    || !Array.isArray(candidate.edges) || candidate.edges.length > 2000 || !candidate.edges.every(isGraphEdge)) {
    throw new Error('invalid relationship graph response')
  }
  const nodesByID = new Map<string, GraphNode>()
  for (const node of candidate.nodes) if (!nodesByID.has(node.id)) nodesByID.set(node.id, node)
  const edgesByRelationship = new Map<string, GraphEdge>()
  for (const edge of candidate.edges) {
    if (!nodesByID.has(edge.from) || !nodesByID.has(edge.to)) throw new Error('invalid relationship graph response')
    const key = `${edge.from}\u0000${edge.kind}\u0000${edge.to}`
    if (!edgesByRelationship.has(key)) edgesByRelationship.set(key, edge)
  }
  return { nodes: [...nodesByID.values()], edges: [...edgesByRelationship.values()] }
}

function graphQuery(filters: Filters) {
  const query = new URLSearchParams()
  if (filters.search.trim()) query.set('search', filters.search.trim())
  if (filters.kind) query.set('kind', filters.kind)
  if (filters.relationship) query.set('relationship', filters.relationship)
  query.set('limit', filters.limit)
  return query.toString()
}

function displayType(value: string) {
  return value.replaceAll('_', ' ').replace(/\b\w/g, (character) => character.toUpperCase())
}

export default function RelationshipGraphView({ permissions }: { permissions: readonly string[] }) {
  const [filters, setFilters] = useState<Filters>(emptyFilters)
  const [graph, setGraph] = useState<Graph>(emptyGraph)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [status, setStatus] = useState('')
  const errorRef = useRef<HTMLDivElement>(null)
  const canRead = permissions.includes('directory.read')

  const loadGraph = useCallback(async (activeFilters: Filters, signal?: AbortSignal) => {
    setLoading(true)
    setError('')
    try {
      const response = await requestJSON(`/api/v1/graph?${graphQuery(activeFilters)}`, { signal })
      const next = readRelationshipGraph(response)
      setGraph(next)
      setStatus(`Relationship graph loaded with ${next.nodes.length} ${next.nodes.length === 1 ? 'record' : 'records'} and ${next.edges.length} ${next.edges.length === 1 ? 'relationship' : 'relationships'}.`)
    } catch (loadError) {
      if (loadError instanceof DOMException && loadError.name === 'AbortError') return
      setGraph(emptyGraph)
      setError(loadError instanceof ApiRequestError ? loadError.message : 'The relationship graph could not be loaded.')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (!canRead) return
    const controller = new AbortController()
    void loadGraph(emptyFilters, controller.signal)
    return () => controller.abort()
  }, [canRead, loadGraph])

  useEffect(() => {
    if (error) errorRef.current?.focus()
  }, [error])

  const nodesByID = useMemo(() => new Map(graph.nodes.map((node) => [node.id, node])), [graph.nodes])
  const disconnected = useMemo(() => {
    const connected = new Set(graph.edges.flatMap((edge) => [edge.from, edge.to]))
    return graph.nodes.filter((node) => !connected.has(node.id))
  }, [graph])
  const plottedNodes = graph.nodes.slice(0, 40)
  const plottedIDs = new Set(plottedNodes.map((node) => node.id))
  const positions = new Map(plottedNodes.map((node, index) => {
    const angle = (Math.PI * 2 * index) / Math.max(plottedNodes.length, 1) - Math.PI / 2
    return [node.id, { x: 320 + Math.cos(angle) * 225, y: 210 + Math.sin(angle) * 155 }] as const
  }))

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    void loadGraph(filters)
  }

  function reset() {
    setFilters(emptyFilters)
    void loadGraph(emptyFilters)
  }

  return (
    <section aria-labelledby="relationship-graph-heading" className={`${subpanelClass} p-4 sm:p-5`} data-feature="threads.relationships" data-requirement="REQ-DIRECTORY-EXPANSION-008">
      <div className="max-w-3xl">
        <p className="text-sm font-semibold text-steward-teal">Relationship graph</p>
        <h3 className="mt-2 text-xl font-semibold text-steward-mist" id="relationship-graph-heading">Explore connected records in your scope</h3>
        <p className="mt-2 text-sm leading-6 text-steward-mist-muted">See how visible people, locations, departments, imported groups, and permitted assets relate. The table remains the complete keyboard and screen-reader view.</p>
      </div>

      {!canRead ? (
        <p className={`${emptyStateClass} mt-4`}>Directory read permission is required to load the relationship graph.</p>
      ) : (
        <>
          <form className="mt-5" onSubmit={submit} role="search">
            <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
              <div><label className={labelClass} htmlFor="graph-search">Search record names</label><input className={inputClass} id="graph-search" maxLength={200} onChange={(event) => setFilters((current) => ({ ...current, search: event.target.value }))} type="search" value={filters.search} /></div>
              <div><label className={labelClass} htmlFor="graph-node-kind">Record type</label><select className={inputClass} id="graph-node-kind" onChange={(event) => setFilters((current) => ({ ...current, kind: event.target.value }))} value={filters.kind}><option value="">All record types</option>{nodeKinds.map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></div>
              <div><label className={labelClass} htmlFor="graph-relationship-kind">Relationship type</label><select className={inputClass} id="graph-relationship-kind" onChange={(event) => setFilters((current) => ({ ...current, relationship: event.target.value }))} value={filters.relationship}><option value="">All relationships</option>{relationshipKinds.map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></div>
              <div><label className={labelClass} htmlFor="graph-limit">Maximum records</label><select className={inputClass} id="graph-limit" onChange={(event) => setFilters((current) => ({ ...current, limit: event.target.value }))} value={filters.limit}>{['25', '50', '100', '250', '500'].map((value) => <option key={value} value={value}>{value}</option>)}</select></div>
            </div>
            <div className="mt-4 flex flex-wrap gap-3"><button className={buttonClass} disabled={loading} type="submit">{loading ? 'Loading graph…' : 'Apply graph filters'}</button><button className={secondaryButtonClass} disabled={loading} onClick={reset} type="button">Reset graph filters</button></div>
          </form>

          {error && <div className="mt-4 rounded-xl border border-steward-danger/50 bg-steward-danger/15 p-4 text-[#ffccd1]" ref={errorRef} role="alert" tabIndex={-1}>{error}</div>}
          <p aria-live="polite" className="sr-only" role="status">{status}</p>

          {!error && !loading && graph.nodes.length === 0 ? (
            <p className={`${emptyStateClass} mt-5`}>No visible records match these graph filters.</p>
          ) : !error ? (
            <div aria-busy={loading} className="mt-5 space-y-5">
              <div className="overflow-x-auto rounded-xl border border-white/[0.08] bg-steward-ink-950/30" role="region" aria-label="Relationship graph visualization" tabIndex={0}>
                <svg aria-labelledby="relationship-graph-title relationship-graph-description" className="h-auto min-w-[640px]" role="img" viewBox="0 0 640 420">
                  <title id="relationship-graph-title">Visible relationship graph</title>
                  <desc id="relationship-graph-description">A visual overview of up to forty visible records. Use the relationship and disconnected-record tables for the complete text view.</desc>
                  {graph.edges.filter((edge) => plottedIDs.has(edge.from) && plottedIDs.has(edge.to)).map((edge) => {
                    const from = positions.get(edge.from)!
                    const to = positions.get(edge.to)!
                    return <line key={`${edge.from}-${edge.kind}-${edge.to}`} stroke="#5f7288" strokeWidth="1.5" x1={from.x} x2={to.x} y1={from.y} y2={to.y}><title>{`${nodesByID.get(edge.from)?.label} ${displayType(edge.kind)} ${nodesByID.get(edge.to)?.label}`}</title></line>
                  })}
                  {plottedNodes.map((node) => {
                    const point = positions.get(node.id)!
                    return <g key={node.id} transform={`translate(${point.x} ${point.y})`}><circle fill="#0e3a3d" r="18" stroke="#2dc4b2" strokeWidth="2" /><text fill="#e7edf3" fontSize="11" textAnchor="middle" y="32">{[...node.label].slice(0, 22).join('')}</text><title>{`${node.label}, ${displayType(node.kind)}`}</title></g>
                  })}
                </svg>
              </div>
              {graph.nodes.length > plottedNodes.length && <p className="text-sm text-steward-mist-muted">The visual overview shows the first 40 records. The tables below include all {graph.nodes.length} visible records.</p>}

              <div aria-label="Visible relationships" className={tableWrapClass} role="region" tabIndex={0}>
                <table className="w-full min-w-[680px] border-collapse text-left text-sm"><caption className="sr-only">Visible record relationships</caption><thead><tr className="border-b border-white/10 text-steward-mist-muted"><th className="px-4 py-3" scope="col">From</th><th className="px-4 py-3" scope="col">Relationship</th><th className="px-4 py-3" scope="col">To</th></tr></thead><tbody>{graph.edges.map((edge) => <tr className="border-b border-white/[0.06]" key={`${edge.from}-${edge.kind}-${edge.to}`}><td className="px-4 py-3"><span className="font-medium text-steward-mist">{nodesByID.get(edge.from)?.label}</span><span className="block text-xs text-steward-mist-muted">{displayType(nodesByID.get(edge.from)?.kind ?? '')}</span></td><td className="px-4 py-3 text-steward-teal">{displayType(edge.kind)}</td><td className="px-4 py-3"><span className="font-medium text-steward-mist">{nodesByID.get(edge.to)?.label}</span><span className="block text-xs text-steward-mist-muted">{displayType(nodesByID.get(edge.to)?.kind ?? '')}</span></td></tr>)}{graph.edges.length === 0 && <tr><td className="px-4 py-6 text-steward-mist-muted" colSpan={3}>No relationships connect the matching records.</td></tr>}</tbody></table>
              </div>

              {disconnected.length > 0 && <div aria-label="Disconnected visible records" className={tableWrapClass} role="region" tabIndex={0}><table className="w-full min-w-[480px] border-collapse text-left text-sm"><caption className="sr-only">Visible records without a matching relationship</caption><thead><tr className="border-b border-white/10 text-steward-mist-muted"><th className="px-4 py-3" scope="col">Disconnected record</th><th className="px-4 py-3" scope="col">Type</th></tr></thead><tbody>{disconnected.map((node) => <tr className="border-b border-white/[0.06]" key={node.id}><td className="px-4 py-3 font-medium text-steward-mist">{node.label}</td><td className="px-4 py-3 text-steward-mist-muted">{displayType(node.kind)}</td></tr>)}</tbody></table></div>}
            </div>
          ) : null}
        </>
      )}
    </section>
  )
}
