import { type FormEvent, useEffect, useRef, useState } from 'react'
import { ApiRequestError, requestJSON } from './api'

// Requirement: REQ-ATLAS-CODES-001. Feature: inventory.identifiers.

export type AssetIdentifier = {
  id: string
  organizationId: string
  assetId: string
  symbology: 'code128' | 'qr'
  normalizedValue: string
  displayValue: string
  source: 'imported' | 'user_entered' | 'generated'
  primary: boolean
  status: 'active' | 'replaced' | 'deactivated'
  supersedesId?: string
  replacedById?: string
  revision: number
  createdBy: string
  createdAt: string
  updatedAt: string
  deactivatedAt?: string
}

type OperationEnvelope = {
  identifier: AssetIdentifier
  created?: boolean
  changed?: boolean
}

type AtlasIdentifiersProps = {
  assetId: string
  assetName: string
  canWrite: boolean
  csrfToken: string
}

const inputClass = 'mt-2 min-h-11 w-full rounded-lg border border-steward-ink-800 bg-steward-ink-950 px-3 py-2 text-steward-mist shadow-inner shadow-black/20'
const buttonClass = 'min-h-11 rounded-lg bg-steward-teal px-4 py-2 font-semibold text-steward-ink-950 transition hover:bg-[#29cfb9] disabled:cursor-wait disabled:opacity-60'
const secondaryButtonClass = 'min-h-11 rounded-lg border border-steward-teal px-4 py-2 font-semibold text-steward-teal transition hover:bg-steward-teal/10 disabled:cursor-wait disabled:opacity-60'

function isIdentifier(value: unknown): value is AssetIdentifier {
  if (typeof value !== 'object' || value === null) return false
  const item = value as Record<string, unknown>
  return typeof item.id === 'string' && typeof item.organizationId === 'string'
    && typeof item.assetId === 'string' && (item.symbology === 'code128' || item.symbology === 'qr')
    && typeof item.normalizedValue === 'string' && typeof item.displayValue === 'string'
    && typeof item.source === 'string' && typeof item.primary === 'boolean'
    && typeof item.status === 'string' && typeof item.revision === 'number'
    && typeof item.createdBy === 'string' && typeof item.createdAt === 'string'
    && typeof item.updatedAt === 'string'
}

function readItems(value: unknown): AssetIdentifier[] {
  if (typeof value !== 'object' || value === null) return []
  const items = (value as Record<string, unknown>).items
  return Array.isArray(items) ? items.filter(isIdentifier) : []
}

function readOperation(value: unknown): OperationEnvelope | null {
  if (isIdentifier(value)) return { identifier: value, changed: true }
  if (typeof value !== 'object' || value === null) return null
  const item = value as Record<string, unknown>
  if (!isIdentifier(item.identifier)) return null
  return {
    identifier: item.identifier,
    created: typeof item.created === 'boolean' ? item.created : undefined,
    changed: typeof item.changed === 'boolean' ? item.changed : undefined,
  }
}

function requestMessage(error: unknown, fallback: string) {
  return error instanceof ApiRequestError ? error.message : fallback
}

export default function AtlasIdentifiers({ assetId, assetName, canWrite, csrfToken }: AtlasIdentifiersProps) {
  const [identifiers, setIdentifiers] = useState<AssetIdentifier[]>([])
  const [busy, setBusy] = useState('loading')
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')
  const [createOpen, setCreateOpen] = useState(false)
  const [replaceTarget, setReplaceTarget] = useState<AssetIdentifier | null>(null)
  const [deactivateTarget, setDeactivateTarget] = useState<AssetIdentifier | null>(null)
  const errorRef = useRef<HTMLDivElement>(null)
  const loadGenerationRef = useRef(0)
  const currentAssetIdRef = useRef(assetId)
  currentAssetIdRef.current = assetId

  async function loadIdentifiers() {
    const requestedAssetId = assetId
    const generation = ++loadGenerationRef.current
    setBusy('loading')
    try {
      const response = await requestJSON(`/api/v1/assets/${encodeURIComponent(assetId)}/identifiers`)
      if (currentAssetIdRef.current !== requestedAssetId || loadGenerationRef.current !== generation) return
      setIdentifiers(readItems(response))
    } catch (requestError) {
      if (currentAssetIdRef.current !== requestedAssetId || loadGenerationRef.current !== generation) return
      setError(requestMessage(requestError, 'Asset identifiers could not be loaded.'))
      queueMicrotask(() => errorRef.current?.focus())
    } finally {
      if (currentAssetIdRef.current === requestedAssetId && loadGenerationRef.current === generation) setBusy('')
    }
  }

  useEffect(() => {
    setIdentifiers([])
    setMessage('')
    setError('')
    setCreateOpen(false)
    setReplaceTarget(null)
    setDeactivateTarget(null)
    void loadIdentifiers()
    return () => { loadGenerationRef.current += 1 }
    // The selected asset ID is the complete load boundary.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [assetId])

  async function createIdentifier(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const requestedAssetId = assetId
    const form = event.currentTarget
    const values = new FormData(form)
    setBusy('create')
    setError('')
    setMessage('')
    try {
      const response = await requestJSON(`/api/v1/assets/${encodeURIComponent(assetId)}/identifiers`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({
          symbology: String(values.get('symbology') ?? ''),
          value: String(values.get('value') ?? ''),
          displayValue: String(values.get('displayValue') ?? ''),
          source: 'user_entered',
          primary: values.get('primary') === 'on',
        }),
      })
      const operation = readOperation(response)
      if (!operation) throw new Error('invalid identifier response')
      if (currentAssetIdRef.current !== requestedAssetId) return
      setMessage(operation.created === false ? 'That identifier is already associated with this asset.' : 'Identifier associated.')
      setCreateOpen(false)
      form.reset()
      await loadIdentifiers()
    } catch (requestError) {
      if (currentAssetIdRef.current !== requestedAssetId) return
      setError(requestMessage(requestError, 'The identifier could not be associated.'))
      queueMicrotask(() => errorRef.current?.focus())
    } finally {
      if (currentAssetIdRef.current === requestedAssetId) setBusy('')
    }
  }

  async function replaceIdentifier(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!replaceTarget) return
    const requestedAssetId = assetId
    const values = new FormData(event.currentTarget)
    setBusy(`replace-${replaceTarget.id}`)
    setError('')
    setMessage('')
    try {
      const response = await requestJSON(`/api/v1/assets/${encodeURIComponent(assetId)}/identifiers/${encodeURIComponent(replaceTarget.id)}/replace`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({
          revision: replaceTarget.revision,
          symbology: String(values.get('symbology') ?? ''),
          value: String(values.get('value') ?? ''),
          displayValue: String(values.get('displayValue') ?? ''),
          source: 'user_entered',
        }),
      })
      if (!readOperation(response)) throw new Error('invalid replacement response')
      if (currentAssetIdRef.current !== requestedAssetId) return
      setMessage('Identifier replaced; the previous association remains in history.')
      setReplaceTarget(null)
      await loadIdentifiers()
    } catch (requestError) {
      if (currentAssetIdRef.current !== requestedAssetId) return
      setError(requestMessage(requestError, 'The identifier could not be replaced.'))
      queueMicrotask(() => errorRef.current?.focus())
    } finally {
      if (currentAssetIdRef.current === requestedAssetId) setBusy('')
    }
  }

  async function deactivateIdentifier() {
    if (!deactivateTarget) return
    const requestedAssetId = assetId
    setBusy(`deactivate-${deactivateTarget.id}`)
    setError('')
    setMessage('')
    try {
      const response = await requestJSON(`/api/v1/assets/${encodeURIComponent(assetId)}/identifiers/${encodeURIComponent(deactivateTarget.id)}/deactivate`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({ revision: deactivateTarget.revision }),
      })
      if (!readOperation(response)) throw new Error('invalid deactivation response')
      if (currentAssetIdRef.current !== requestedAssetId) return
      setMessage('Identifier deactivated; its history is preserved.')
      setDeactivateTarget(null)
      await loadIdentifiers()
    } catch (requestError) {
      if (currentAssetIdRef.current !== requestedAssetId) return
      setError(requestMessage(requestError, 'The identifier could not be deactivated.'))
      queueMicrotask(() => errorRef.current?.focus())
    } finally {
      if (currentAssetIdRef.current === requestedAssetId) setBusy('')
    }
  }

  return (
    <section aria-labelledby="asset-identifiers-heading" className="mt-6 border-t border-steward-ink-800 pt-5" data-feature="inventory.identifiers" data-requirement="REQ-ATLAS-CODES-001">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h4 className="font-semibold" id="asset-identifiers-heading">Atlas Codes — Identifiers</h4>
          <p className="mt-1 text-sm leading-5 text-steward-mist-muted">Manage Code 128 and QR associations for {assetName}. Scanning and label printing arrive in later Atlas Codes slices.</p>
        </div>
        {canWrite && <button className={secondaryButtonClass} onClick={() => { setCreateOpen((open) => !open); setReplaceTarget(null); setDeactivateTarget(null) }} type="button">{createOpen ? 'Cancel association' : 'Associate identifier'}</button>}
      </div>

      {error && <div className="mt-3 rounded-lg border border-red-400/50 bg-red-950/50 p-3 text-sm" ref={errorRef} role="alert" tabIndex={-1}>{error}</div>}
      {message && <p className="mt-3 rounded-lg border border-steward-green/40 bg-steward-green/10 p-3 text-sm" role="status">{message}</p>}

      {createOpen && canWrite && <IdentifierForm busy={busy === 'create'} legend="Associate an identifier" onSubmit={createIdentifier} submitLabel="Associate identifier" />}

      {busy === 'loading' ? <p className="mt-3 text-sm text-steward-mist-muted" role="status">Loading identifiers…</p> : identifiers.length === 0 ? (
        <p className="mt-3 rounded-lg border border-dashed border-steward-ink-800 p-3 text-sm text-steward-mist-muted">No identifiers are associated with this asset.</p>
      ) : (
        <ul className="mt-3 space-y-3">
          {identifiers.map((identifier) => <li className="rounded-lg border border-steward-ink-800 p-3 text-sm" key={identifier.id}>
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div className="min-w-0">
                <p className="break-all font-mono text-steward-mist">{identifier.displayValue || identifier.normalizedValue}</p>
                <p className="mt-1 text-steward-mist-muted">{identifier.symbology === 'code128' ? 'Code 128' : 'QR'} · {identifier.status} · {identifier.source.replace('_', ' ')}{identifier.primary ? ' · primary' : ''} · revision {identifier.revision}</p>
                <p className="mt-1 break-all text-steward-mist-muted">Created by {identifier.createdBy} on {new Date(identifier.createdAt).toLocaleString()}</p>
              </div>
              {canWrite && identifier.status === 'active' && <div className="flex flex-wrap gap-2">
                <button className={secondaryButtonClass} onClick={() => { setReplaceTarget(identifier); setCreateOpen(false); setDeactivateTarget(null) }} type="button">Replace</button>
                <button className={secondaryButtonClass} onClick={() => { setDeactivateTarget(identifier); setCreateOpen(false); setReplaceTarget(null) }} type="button">Deactivate</button>
              </div>}
            </div>
            {replaceTarget?.id === identifier.id && <IdentifierForm busy={busy === `replace-${identifier.id}`} initialSymbology={identifier.symbology} legend={`Replace ${identifier.displayValue || identifier.normalizedValue}`} onCancel={() => setReplaceTarget(null)} onSubmit={replaceIdentifier} showPrimary={false} submitLabel="Confirm replacement" />}
            {deactivateTarget?.id === identifier.id && <div className="mt-3 rounded-lg border border-amber-300/40 bg-amber-950/30 p-3">
              <p>Deactivate this association? The value remains in history and will no longer resolve to this asset.</p>
              <div className="mt-3 flex flex-wrap gap-2">
                <button className={buttonClass} disabled={busy === `deactivate-${identifier.id}`} onClick={() => void deactivateIdentifier()} type="button">{busy === `deactivate-${identifier.id}` ? 'Deactivating…' : 'Confirm deactivation'}</button>
                <button className={secondaryButtonClass} onClick={() => setDeactivateTarget(null)} type="button">Cancel</button>
              </div>
            </div>}
          </li>)}
        </ul>
      )}
    </section>
  )
}

function IdentifierForm({ busy, initialPrimary = false, initialSymbology = 'code128', legend, onCancel, onSubmit, showPrimary = true, submitLabel }: {
  busy: boolean
  initialPrimary?: boolean
  initialSymbology?: AssetIdentifier['symbology']
  legend: string
  onCancel?: () => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
  showPrimary?: boolean
  submitLabel: string
}) {
  return <form aria-label={legend} className="mt-3 rounded-lg border border-steward-blue/40 bg-steward-ink-950/55 p-4" onSubmit={onSubmit}>
    <fieldset>
      <legend className="font-semibold">{legend}</legend>
      <div className="mt-3 grid gap-3">
        <label className="text-sm font-semibold text-steward-mist-muted">Symbology
          <select className={inputClass} defaultValue={initialSymbology} name="symbology"><option value="code128">Code 128</option><option value="qr">QR</option></select>
        </label>
        <label className="text-sm font-semibold text-steward-mist-muted">Encoded value
          <input autoCapitalize="none" autoComplete="off" autoCorrect="off" className={inputClass} maxLength={512} name="value" required spellCheck={false} />
        </label>
        <label className="text-sm font-semibold text-steward-mist-muted">Display value <span className="font-normal">(optional)</span>
          <input className={inputClass} maxLength={512} name="displayValue" />
        </label>
        {showPrimary && <label className="flex min-h-11 items-center gap-3 text-sm font-semibold text-steward-mist-muted"><input className="h-5 w-5 accent-steward-teal" defaultChecked={initialPrimary} name="primary" type="checkbox" />Primary identifier</label>}
      </div>
      <div className="mt-4 flex flex-wrap gap-2">
        <button className={buttonClass} disabled={busy} type="submit">{busy ? 'Saving…' : submitLabel}</button>
        {onCancel && <button className={secondaryButtonClass} onClick={onCancel} type="button">Cancel</button>}
      </div>
    </fieldset>
  </form>
}
