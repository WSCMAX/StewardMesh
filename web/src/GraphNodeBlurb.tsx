import { type FormEvent, useEffect, useMemo, useState } from 'react'
import { ApiRequestError, requestJSON } from './api'
import { assetPayload, isAsset, type Asset } from './AtlasInventory'
import { displayType, sourceForKind, sourceLabels } from './graphModel'
import { openRecordLabel, recordIDFromNode, workspaceRecordHref, workspaceRecordTarget } from './graphRecord'
import type { GraphEdge, GraphNode } from './InteractiveRelationshipGraph'
import { buttonClass, inputClass, labelClass, plainButtonClass, secondaryButtonClass } from './ui'

// Requirement: REQ-DIRECTORY-EXPANSION-008. Feature: threads.relationships.

const assetStatuses = ['draft', 'active', 'inactive', 'retired', 'disposed'] as const

type AssetDraft = {
  name: string
  assetTag: string
  serialNumber: string
  hostname: string
  status: string
  lifecycleNote: string
}

type GraphNodeBlurbProps = {
  node: GraphNode
  edges: readonly GraphEdge[]
  nodesByID: Map<string, GraphNode>
  disconnected?: boolean
  focusNodeID: string
  csrfToken?: string
  permissions?: readonly string[]
  onSelectNode: (nodeID: string) => void
  onFocusNode: (nodeID: string) => void
  onClearFocus: () => void
  onClose: () => void
  onOpenRecord?: (node: GraphNode) => void
  onNodeUpdated?: (node: GraphNode) => void
}

function emptyDraft(node: GraphNode): AssetDraft {
  return {
    name: node.label,
    assetTag: '',
    serialNumber: '',
    hostname: '',
    status: node.attributes?.status ?? 'active',
    lifecycleNote: '',
  }
}

export default function GraphNodeBlurb({
  node,
  edges,
  nodesByID,
  disconnected = false,
  focusNodeID,
  csrfToken = '',
  permissions = [],
  onSelectNode,
  onFocusNode,
  onClearFocus,
  onClose,
  onOpenRecord,
  onNodeUpdated,
}: GraphNodeBlurbProps) {
  const canReadAssets = permissions.includes('assets.read')
  const canWriteAssets = permissions.includes('assets.write')
  const target = workspaceRecordTarget(node.kind)
  const href = workspaceRecordHref(node.kind)
  const linkLabel = openRecordLabel(node.kind, canWriteAssets)
  const [asset, setAsset] = useState<Asset | null>(null)
  const [draft, setDraft] = useState<AssetDraft>(() => emptyDraft(node))
  const [loadState, setLoadState] = useState<'idle' | 'loading' | 'ready' | 'unavailable'>('idle')
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')

  const related = useMemo(() => edges.filter((edge) => edge.from === node.id || edge.to === node.id), [edges, node.id])
  const showAssetFields = node.kind === 'asset' && canReadAssets
  const canEditAsset = Boolean(asset) && canWriteAssets && csrfToken.length > 0
  const statusChanged = Boolean(asset) && draft.status !== asset?.status

  useEffect(() => {
    setDraft(emptyDraft(node))
    setAsset(null)
    setMessage('')
    setError('')
    if (node.kind !== 'asset' || !canReadAssets) {
      setLoadState('idle')
      return
    }
    const recordID = recordIDFromNode(node)
    let active = true
    setLoadState('loading')
    requestJSON(`/api/v1/assets/${encodeURIComponent(recordID)}`)
      .then((value) => {
        if (!active) return
        if (!isAsset(value)) throw new Error('invalid asset response')
        setAsset(value)
        setDraft({
          name: value.name,
          assetTag: value.assetTag ?? '',
          serialNumber: value.serialNumber ?? '',
          hostname: value.hostname ?? '',
          status: value.status,
          lifecycleNote: '',
        })
        setLoadState('ready')
      })
      .catch(() => {
        if (!active) return
        setAsset(null)
        setLoadState('unavailable')
      })
    return () => {
      active = false
    }
  }, [canReadAssets, node.id, node.kind])

  async function saveAsset(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!asset || !canEditAsset) return
    if (statusChanged && draft.lifecycleNote.trim() === '') {
      setError('Add a short lifecycle note when status changes.')
      return
    }
    setBusy(true)
    setError('')
    setMessage('')
    try {
      const payload = assetPayload(asset)
      payload.name = draft.name.trim()
      payload.assetTag = draft.assetTag.trim()
      payload.serialNumber = draft.serialNumber.trim()
      payload.hostname = draft.hostname.trim()
      payload.status = draft.status
      if (statusChanged) payload.lifecycleNote = draft.lifecycleNote.trim()
      const saved = await requestJSON(`/api/v1/assets/${encodeURIComponent(asset.id)}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify(payload),
      })
      if (!isAsset(saved)) throw new Error('invalid asset response')
      setAsset(saved)
      setDraft({
        name: saved.name,
        assetTag: saved.assetTag ?? '',
        serialNumber: saved.serialNumber ?? '',
        hostname: saved.hostname ?? '',
        status: saved.status,
        lifecycleNote: '',
      })
      setMessage('Asset updated.')
      onNodeUpdated?.({
        ...node,
        label: saved.name,
        attributes: { ...node.attributes, status: saved.status, source: node.attributes?.source ?? 'atlas' },
      })
    } catch (saveError) {
      setError(saveError instanceof ApiRequestError ? saveError.message : 'The asset could not be saved.')
    } finally {
      setBusy(false)
    }
  }

  function openRecord() {
    if (onOpenRecord) onOpenRecord(node)
  }

  const product = sourceLabels[sourceForKind(node.kind, node.attributes)] ?? 'Organization'

  return (
    <aside
      aria-labelledby="graph-node-blurb-heading"
      className="max-h-[32rem] w-[min(100%,22rem)] overflow-y-auto rounded-md border border-white/12 bg-steward-ink-900 p-4 shadow-lg shadow-black/40"
      role="region"
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="text-xs font-medium text-steward-slate">{displayType(node.kind)}</p>
          <h3 className="mt-1 truncate text-base font-semibold text-steward-mist" id="graph-node-blurb-heading">{node.label}</h3>
          <p className="mt-1 text-sm text-steward-mist-muted">
            {product}
            {node.attributes?.status ? ` · ${displayType(node.attributes.status)}` : ''}
            {` · ${related.length} ${related.length === 1 ? 'connection' : 'connections'}`}
            {disconnected ? ' · Disconnected' : ''}
          </p>
        </div>
        <button className={`${plainButtonClass} shrink-0 px-2 text-steward-mist-muted`} onClick={onClose} type="button">Close</button>
      </div>

      {showAssetFields && loadState === 'loading' && <p className="mt-3 text-sm text-steward-mist-muted" role="status">Loading asset fields…</p>}
      {showAssetFields && loadState === 'unavailable' && <p className="mt-3 text-sm text-steward-mist-muted">Asset fields could not be loaded here. Open Atlas to inspect the full record.</p>}

      {showAssetFields && loadState === 'ready' && (
        <form className="mt-4 space-y-3" onSubmit={saveAsset}>
          <div>
            <label className={labelClass} htmlFor="graph-node-name">Name</label>
            <input className={inputClass} disabled={!canEditAsset || busy} id="graph-node-name" maxLength={200} onChange={(event) => setDraft((current) => ({ ...current, name: event.target.value }))} required value={draft.name} />
          </div>
          <div>
            <label className={labelClass} htmlFor="graph-node-asset-tag">Asset tag</label>
            <input className={inputClass} disabled={!canEditAsset || busy} id="graph-node-asset-tag" maxLength={64} onChange={(event) => setDraft((current) => ({ ...current, assetTag: event.target.value }))} value={draft.assetTag} />
          </div>
          <div>
            <label className={labelClass} htmlFor="graph-node-serial">Serial number</label>
            <input className={inputClass} disabled={!canEditAsset || busy} id="graph-node-serial" maxLength={128} onChange={(event) => setDraft((current) => ({ ...current, serialNumber: event.target.value }))} value={draft.serialNumber} />
          </div>
          <div>
            <label className={labelClass} htmlFor="graph-node-hostname">Hostname</label>
            <input className={inputClass} disabled={!canEditAsset || busy} id="graph-node-hostname" maxLength={253} onChange={(event) => setDraft((current) => ({ ...current, hostname: event.target.value }))} value={draft.hostname} />
          </div>
          <div>
            <label className={labelClass} htmlFor="graph-node-status">Status</label>
            <select className={inputClass} disabled={!canEditAsset || busy} id="graph-node-status" onChange={(event) => setDraft((current) => ({ ...current, status: event.target.value }))} value={draft.status}>
              {assetStatuses.map((status) => <option key={status} value={status}>{displayType(status)}</option>)}
            </select>
          </div>
          {canEditAsset && statusChanged && (
            <div>
              <label className={labelClass} htmlFor="graph-node-lifecycle-note">Lifecycle note</label>
              <textarea className={`${inputClass} min-h-20`} id="graph-node-lifecycle-note" maxLength={1000} onChange={(event) => setDraft((current) => ({ ...current, lifecycleNote: event.target.value }))} required value={draft.lifecycleNote} />
            </div>
          )}
          {canEditAsset && (
            <button className={buttonClass} disabled={busy} type="submit">{busy ? 'Saving…' : 'Save asset'}</button>
          )}
        </form>
      )}

      {!showAssetFields && node.attributes && Object.keys(node.attributes).length > 0 && (
        <dl className="mt-4 grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-sm">
          {Object.entries(node.attributes).map(([key, value]) => (
            <div className="contents" key={key}>
              <dt className="text-steward-mist-muted">{displayType(key)}</dt>
              <dd className="truncate text-steward-mist">{displayType(value)}</dd>
            </div>
          ))}
        </dl>
      )}

      {error && <p className="mt-3 text-sm text-[#ffccd1]" role="alert">{error}</p>}
      {message && <p className="mt-3 text-sm text-steward-mist-muted" role="status">{message}</p>}

      {related.length > 0 ? (
        <ul className="mt-4 space-y-2 text-sm">
          {related.slice(0, 8).map((edge) => {
            const outward = edge.from === node.id
            const other = nodesByID.get(outward ? edge.to : edge.from)
            return (
              <li className="rounded-lg border border-white/10 bg-steward-ink-900/70 px-3 py-2" key={`${edge.from}-${edge.kind}-${edge.to}`}>
                <button className="text-left text-steward-teal underline-offset-2 hover:underline" onClick={() => other && onSelectNode(other.id)} type="button">
                  {outward ? `${displayType(edge.kind)} ${other?.label ?? 'Unknown record'}` : `${other?.label ?? 'Unknown record'} ${displayType(edge.kind)} this record`}
                </button>
              </li>
            )
          })}
        </ul>
      ) : (
        <p className="mt-4 text-sm text-steward-mist-muted">This record has no direct relationships in the current graph view.</p>
      )}
      {related.length > 8 && <p className="mt-2 text-xs text-steward-mist-muted">{related.length - 8} more connections are in the data table.</p>}

      <div className="mt-4 flex flex-wrap gap-2">
        {target && href && (
          <a
            className={secondaryButtonClass}
            href={href}
            onClick={(event) => {
              if (!onOpenRecord) return
              event.preventDefault()
              openRecord()
            }}
          >
            {linkLabel}
          </a>
        )}
        <button className={plainButtonClass} onClick={() => onFocusNode(node.id)} type="button">Focus connections</button>
        {focusNodeID && <button className={plainButtonClass} onClick={onClearFocus} type="button">Show full graph</button>}
      </div>
    </aside>
  )
}
