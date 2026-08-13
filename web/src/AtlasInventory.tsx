import { type FormEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ApiRequestError, requestJSON } from './api'
import AtlasIdentifiers from './AtlasIdentifiers'

// Requirements: REQ-ATLAS-001, REQ-ATLAS-CODES-001. Features: inventory.assets, inventory.identifiers.

export type Asset = {
  id: string
  organizationId: string
  modelId?: string
  name: string
  kind: string
  assetTag?: string
  serialNumber?: string
  hostname?: string
  siteId?: string
  buildingId?: string
  roomId?: string
  departmentId?: string
  userId?: string
  status: string
  purchaseDate?: string
  revision: number
  createdAt: string
  updatedAt: string
}

export type AssetModel = {
  id: string
  organizationId: string
  manufacturer: string
  name: string
  modelNumber?: string
  kind: string
  vendorIdentifier?: string
  specifications?: Record<string, string>
  supportUrl?: string
  warrantyMonths?: number
  usefulLifeMonths?: number
  status: string
  sourceSystemId?: string
  sourceRecordId?: string
  instanceCount: number
  revision: number
  createdAt: string
  updatedAt: string
}

type LifecycleEvent = {
  id: string
  fromStatus?: string
  toStatus: string
  note?: string
  revision: number
  actorId: string
  occurredAt: string
}

type ReferenceRecord = {
  id: string
  name?: string
  displayName?: string
  number?: string
  siteId?: string
  buildingId?: string
}

type ReferenceOptions = {
  sites: ReferenceRecord[]
  buildings: ReferenceRecord[]
  rooms: ReferenceRecord[]
  departments: ReferenceRecord[]
  identities: ReferenceRecord[]
}

type AtlasInventoryProps = {
  assets: readonly Asset[]
  csrfToken: string
  permissions: readonly string[]
  onAssetsChange: (assets: Asset[]) => void
  onOpenHelp?: () => void
}

const kinds = ['server', 'computer', 'desktop', 'laptop', 'tablet', 'phone', 'network', 'peripheral', 'virtual', 'other']
const statuses = ['draft', 'active', 'inactive', 'retired', 'disposed']
const emptyReferences: ReferenceOptions = { sites: [], buildings: [], rooms: [], departments: [], identities: [] }
const inputClass = 'mt-2 min-h-11 w-full rounded-lg border border-steward-ink-800 bg-steward-ink-950 px-3 py-2 text-steward-mist shadow-inner shadow-black/20'
const buttonClass = 'min-h-11 rounded-lg bg-steward-teal px-4 py-2 font-semibold text-steward-ink-950 transition hover:bg-[#29cfb9] disabled:cursor-wait disabled:opacity-60'
const secondaryButtonClass = 'min-h-11 rounded-lg border border-steward-teal px-4 py-2 font-semibold text-steward-teal transition hover:bg-steward-teal/10 disabled:cursor-wait disabled:opacity-60'

export function isAsset(value: unknown): value is Asset {
  if (typeof value !== 'object' || value === null) return false
  const item = value as Record<string, unknown>
  return typeof item.id === 'string' && typeof item.organizationId === 'string'
    && typeof item.name === 'string' && typeof item.kind === 'string' && typeof item.status === 'string'
    && typeof item.revision === 'number' && typeof item.createdAt === 'string' && typeof item.updatedAt === 'string'
}

function readItems(value: unknown): unknown[] {
  if (typeof value !== 'object' || value === null) return []
  const items = (value as Record<string, unknown>).items
  return Array.isArray(items) ? items : []
}

function isReference(value: unknown): value is ReferenceRecord {
  if (typeof value !== 'object' || value === null) return false
  return typeof (value as Record<string, unknown>).id === 'string'
}

function isLifecycleEvent(value: unknown): value is LifecycleEvent {
  if (typeof value !== 'object' || value === null) return false
  const item = value as Record<string, unknown>
  return typeof item.id === 'string' && typeof item.toStatus === 'string' && typeof item.revision === 'number'
    && typeof item.actorId === 'string' && typeof item.occurredAt === 'string'
}

function isAssetModel(value: unknown): value is AssetModel {
  if (typeof value !== 'object' || value === null) return false
  const item = value as Record<string, unknown>
  return typeof item.id === 'string' && typeof item.organizationId === 'string'
    && typeof item.manufacturer === 'string' && typeof item.name === 'string'
    && typeof item.kind === 'string' && typeof item.status === 'string'
    && typeof item.instanceCount === 'number' && typeof item.revision === 'number'
    && typeof item.createdAt === 'string' && typeof item.updatedAt === 'string'
}

function referenceLabel(reference: ReferenceRecord) {
  return reference.displayName || reference.name || reference.number || reference.id
}

function assetValue(asset: Asset | null, key: keyof Asset) {
  const value = asset?.[key]
  return typeof value === 'string' ? value : ''
}

function modelLabel(model: AssetModel) {
  return `${model.manufacturer} ${model.name}${model.modelNumber ? ` ${model.modelNumber}` : ''}`.trim()
}

export default function AtlasInventory({ assets, csrfToken, permissions, onAssetsChange, onOpenHelp }: AtlasInventoryProps) {
  const [search, setSearch] = useState('')
  const [kind, setKind] = useState('')
  const [statusFilter, setStatusFilter] = useState('')
  const [editing, setEditing] = useState<Asset | null>(null)
  const [modelEditing, setModelEditing] = useState<AssetModel | null>(null)
  const [formOpen, setFormOpen] = useState(false)
  const [modelFormOpen, setModelFormOpen] = useState(false)
  const [prefillModelID, setPrefillModelID] = useState('')
  const [prefillKind, setPrefillKind] = useState('')
  const [selected, setSelected] = useState<Asset | null>(null)
  const [lifecycle, setLifecycle] = useState<LifecycleEvent[]>([])
  const [models, setModels] = useState<AssetModel[]>([])
  const [references, setReferences] = useState<ReferenceOptions>(emptyReferences)
  const [referencesLoaded, setReferencesLoaded] = useState(false)
  const [busy, setBusy] = useState('')
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')
  const errorRef = useRef<HTMLDivElement>(null)
  const canWrite = permissions.includes('assets.write')
  const canReadDirectory = permissions.includes('directory.read')

  const loadModels = useCallback(async () => {
    try {
      const response = await requestJSON('/api/v1/asset-models?limit=100')
      setModels(readItems(response).filter(isAssetModel))
    } catch {
      setModels([])
    }
  }, [])

  useEffect(() => {
    void loadModels()
  }, [loadModels])

  const filteredAssets = useMemo(() => {
    const normalizedSearch = search.trim().toLowerCase()
    return assets.filter((asset) => {
      if (kind && asset.kind !== kind) return false
      if (statusFilter && asset.status !== statusFilter) return false
      if (!normalizedSearch) return true
      return [asset.name, asset.assetTag, asset.serialNumber, asset.hostname]
        .some((value) => value?.toLowerCase().includes(normalizedSearch))
    })
  }, [assets, kind, search, statusFilter])

  async function loadReferences() {
    if (!canReadDirectory || referencesLoaded) return
    const paths = ['/api/v1/sites', '/api/v1/buildings', '/api/v1/rooms', '/api/v1/departments', '/api/v1/identities?limit=100']
    try {
      const responses = await Promise.all(paths.map((path) => requestJSON(path)))
      const [sites, buildings, rooms, departments, identities] = responses.map((response) => readItems(response).filter(isReference))
      setReferences({ sites, buildings, rooms, departments, identities })
      setReferencesLoaded(true)
    } catch {
      setReferencesLoaded(true)
    }
  }

  function openCreate() {
    setEditing(null)
    setPrefillModelID('')
    setPrefillKind('')
    setFormOpen(true)
    setError('')
    setMessage('')
    void loadReferences()
  }

  function openCreateFromModel(model: AssetModel) {
    setPrefillModelID(model.id)
    setPrefillKind(model.kind)
    setEditing(null)
    setFormOpen(true)
    setError('')
    setMessage('')
    void loadReferences()
  }

  function openEdit(asset: Asset) {
    setEditing(asset)
    setPrefillModelID('')
    setPrefillKind('')
    setFormOpen(true)
    setSelected(asset)
    setError('')
    setMessage('')
    void loadReferences()
  }

  function openModelCreate() {
    setModelEditing(null)
    setModelFormOpen(true)
    setError('')
    setMessage('')
  }

  function openModelEdit(model: AssetModel) {
    setModelEditing(model)
    setModelFormOpen(true)
    setError('')
    setMessage('')
  }

  async function selectAsset(asset: Asset) {
    setSelected(asset)
    setLifecycle([])
    setBusy(`history-${asset.id}`)
    setError('')
    try {
      const value = await requestJSON(`/api/v1/assets/${encodeURIComponent(asset.id)}/lifecycle`)
      setLifecycle(readItems(value).filter(isLifecycleEvent))
    } catch (requestError) {
      setError(requestError instanceof ApiRequestError ? requestError.message : 'Lifecycle history could not be loaded.')
      queueMicrotask(() => errorRef.current?.focus())
    } finally {
      setBusy('')
    }
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError('')
    setMessage('')
    const form = event.currentTarget
    const values = new FormData(form)
    const purchaseDate = String(values.get('purchaseDate') ?? '')
    const payload: Record<string, unknown> = {
      name: String(values.get('name') ?? ''),
      modelId: String(values.get('modelId') ?? ''),
      kind: String(values.get('kind') ?? ''),
      assetTag: String(values.get('assetTag') ?? ''),
      serialNumber: String(values.get('serialNumber') ?? ''),
      hostname: String(values.get('hostname') ?? ''),
      siteId: String(values.get('siteId') ?? ''),
      buildingId: String(values.get('buildingId') ?? ''),
      roomId: String(values.get('roomId') ?? ''),
      departmentId: String(values.get('departmentId') ?? ''),
      userId: String(values.get('userId') ?? ''),
      status: String(values.get('status') ?? ''),
    }
    if (purchaseDate) payload.purchaseDate = `${purchaseDate}T00:00:00Z`
    if (editing) {
      payload.revision = editing.revision
      payload.lifecycleNote = String(values.get('lifecycleNote') ?? '')
    }
    setBusy('save')
    try {
      const saved = await requestJSON(editing ? `/api/v1/assets/${encodeURIComponent(editing.id)}` : '/api/v1/assets', {
        method: editing ? 'PUT' : 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify(payload),
      })
      if (!isAsset(saved)) throw new Error('invalid asset response')
      const nextAssets = editing
        ? assets.map((asset) => asset.id === saved.id ? saved : asset)
        : [...assets, saved]
      onAssetsChange([...nextAssets].sort((left, right) => left.name.localeCompare(right.name)))
      setSelected(saved)
      setEditing(null)
      setFormOpen(false)
      setMessage(editing ? 'Asset updated.' : 'Asset created.')
      void loadModels()
      void selectAsset(saved)
    } catch (requestError) {
      setError(requestError instanceof ApiRequestError ? requestError.message : 'The asset could not be saved.')
      queueMicrotask(() => errorRef.current?.focus())
    } finally {
      setBusy('')
    }
  }

  async function handleModelSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError('')
    setMessage('')
    const values = new FormData(event.currentTarget)
    const payload: Record<string, unknown> = {
      manufacturer: String(values.get('manufacturer') ?? ''),
      name: String(values.get('modelName') ?? ''),
      modelNumber: String(values.get('modelNumber') ?? ''),
      kind: String(values.get('modelKind') ?? ''),
      vendorIdentifier: String(values.get('vendorIdentifier') ?? ''),
      supportUrl: String(values.get('supportUrl') ?? ''),
      warrantyMonths: Number(values.get('warrantyMonths') || 0),
      usefulLifeMonths: Number(values.get('usefulLifeMonths') || 0),
    }
    if (modelEditing) payload.revision = modelEditing.revision
    setBusy('save-model')
    try {
      const saved = await requestJSON(modelEditing ? `/api/v1/asset-models/${encodeURIComponent(modelEditing.id)}` : '/api/v1/asset-models', {
        method: modelEditing ? 'PUT' : 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify(payload),
      })
      if (!isAssetModel(saved)) throw new Error('invalid model response')
      setModels((current) => {
        const next = modelEditing ? current.map((model) => model.id === saved.id ? saved : model) : [...current, saved]
        return next.sort((left, right) => modelLabel(left).localeCompare(modelLabel(right)))
      })
      setModelEditing(null)
      setModelFormOpen(false)
      setMessage(modelEditing ? 'Model updated.' : 'Model created.')
    } catch (requestError) {
      setError(requestError instanceof ApiRequestError ? requestError.message : 'The model could not be saved.')
      queueMicrotask(() => errorRef.current?.focus())
    } finally {
      setBusy('')
    }
  }

  async function retireModel(model: AssetModel) {
    setError('')
    setMessage('')
    setBusy(`retire-model-${model.id}`)
    try {
      const retired = await requestJSON(`/api/v1/asset-models/${encodeURIComponent(model.id)}/retire?revision=${model.revision}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken },
      })
      if (!isAssetModel(retired)) throw new Error('invalid model response')
      setModels((current) => current.filter((item) => item.id !== retired.id))
      setMessage('Model retired.')
    } catch (requestError) {
      setError(requestError instanceof ApiRequestError ? requestError.message : 'The model could not be retired.')
      queueMicrotask(() => errorRef.current?.focus())
    } finally {
      setBusy('')
    }
  }

  return (
    <section aria-labelledby="assets-heading" className="rounded-xl border border-steward-ink-800 bg-steward-ink-900 p-6" data-feature="inventory.assets" data-requirement="REQ-ATLAS-001">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <p className="text-sm font-semibold text-steward-teal">Organization asset registry</p>
          <h2 id="assets-heading" className="mt-1 text-2xl font-semibold">Atlas — Asset inventory</h2>
          <p className="mt-2 max-w-3xl text-sm leading-6 text-steward-mist-muted">Know every asset and where it is in its lifecycle. Search organization-owned servers and devices, maintain identity and location details, and preserve every status transition.</p>
        </div>
        <div className="flex flex-wrap gap-3">
          {onOpenHelp && <button className={secondaryButtonClass} onClick={onOpenHelp} type="button">Atlas help</button>}
          {canWrite && <button className={buttonClass} onClick={openCreate} type="button">Add asset</button>}
        </div>
      </div>

      {error && <div className="mt-4 rounded-lg border border-red-400/50 bg-red-950/50 p-3 text-sm" ref={errorRef} role="alert" tabIndex={-1}>{error}</div>}
      {message && <p className="mt-4 rounded-lg border border-steward-green/40 bg-steward-green/10 p-3 text-sm" role="status">{message}</p>}

      <section aria-labelledby="models-heading" className="mt-6 rounded-xl border border-steward-ink-800 bg-steward-ink-950/45 p-5" data-feature="inventory.models" data-requirement="REQ-ATLAS-MODELS-001">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h3 className="text-lg font-semibold" id="models-heading">Model catalog <span className="text-sm font-normal text-steward-mist-muted">({models.length})</span></h3>
            <p className="mt-1 text-sm text-steward-mist-muted">Shared manufacturer and model defaults for repeated assets.</p>
          </div>
          {canWrite && <button className={secondaryButtonClass} onClick={openModelCreate} type="button">Add model</button>}
        </div>
        {modelFormOpen && canWrite && (
          <form aria-label={modelEditing ? 'Edit model' : 'Add model'} className="mt-5 rounded-xl border border-steward-blue/40 bg-steward-ink-900 p-5" onSubmit={handleModelSubmit}>
            <div className="flex flex-wrap items-center justify-between gap-3">
              <h4 className="font-semibold">{modelEditing ? `Edit ${modelLabel(modelEditing)}` : 'Register a model'}</h4>
              <button className="text-sm text-steward-teal underline underline-offset-4" onClick={() => { setModelFormOpen(false); setModelEditing(null) }} type="button">Cancel</button>
            </div>
            <div className="mt-4 grid gap-4 md:grid-cols-2 lg:grid-cols-3">
              <TextField defaultValue={modelEditing?.manufacturer ?? ''} label="Manufacturer" maxLength={120} name="manufacturer" required />
              <TextField defaultValue={modelEditing?.name ?? ''} label="Model name" maxLength={160} name="modelName" required />
              <TextField defaultValue={modelEditing?.modelNumber ?? ''} label="Model number" maxLength={120} name="modelNumber" />
              <SelectField defaultValue={modelEditing?.kind ?? 'server'} label="Kind" name="modelKind" options={kinds} />
              <TextField defaultValue={modelEditing?.vendorIdentifier ?? ''} label="Vendor identifier" maxLength={160} name="vendorIdentifier" />
              <TextField defaultValue={modelEditing?.supportUrl ?? ''} label="Support URL" maxLength={500} name="supportUrl" />
              <NumberField defaultValue={modelEditing?.warrantyMonths ?? 0} label="Warranty months" max={1200} name="warrantyMonths" />
              <NumberField defaultValue={modelEditing?.usefulLifeMonths ?? 0} label="Useful life months" max={1200} name="usefulLifeMonths" />
            </div>
            <button className={`${buttonClass} mt-5`} disabled={busy === 'save-model'} type="submit">{busy === 'save-model' ? 'Saving…' : modelEditing ? 'Save model' : 'Create model'}</button>
          </form>
        )}
        {models.length === 0 ? <p className="mt-4 rounded-xl border border-dashed border-steward-ink-800 p-4 text-sm text-steward-mist-muted">No active models have been registered.</p> : (
          <ul className="mt-4 grid gap-3 lg:grid-cols-2">{models.map((model) => (
            <li className="rounded-lg border border-steward-ink-800 p-4" key={model.id}>
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div>
                  <p className="font-semibold text-steward-mist">{modelLabel(model)}</p>
                  <p className="mt-1 text-sm text-steward-mist-muted">{model.kind} · {model.instanceCount} asset{model.instanceCount === 1 ? '' : 's'} · {model.warrantyMonths || 0} warranty months</p>
                </div>
                {canWrite && <div className="flex flex-wrap gap-2">
                  <button className={secondaryButtonClass} onClick={() => openCreateFromModel(model)} type="button">Use</button>
                  <button className={secondaryButtonClass} onClick={() => openModelEdit(model)} type="button">Edit</button>
                  <button className={secondaryButtonClass} disabled={busy === `retire-model-${model.id}`} onClick={() => void retireModel(model)} type="button">Retire</button>
                </div>}
              </div>
            </li>
          ))}</ul>
        )}
      </section>

      <div aria-label="Filter assets" className="mt-6 grid gap-4 md:grid-cols-3" role="search">
        <label className="text-sm font-semibold text-steward-mist-muted">Search
          <input className={inputClass} onChange={(event) => setSearch(event.target.value)} placeholder="Name, tag, serial, or hostname" type="search" value={search} />
        </label>
        <label className="text-sm font-semibold text-steward-mist-muted">Kind
          <select className={inputClass} onChange={(event) => setKind(event.target.value)} value={kind}><option value="">All kinds</option>{kinds.map((value) => <option key={value} value={value}>{value}</option>)}</select>
        </label>
        <label className="text-sm font-semibold text-steward-mist-muted">Status
          <select className={inputClass} onChange={(event) => setStatusFilter(event.target.value)} value={statusFilter}><option value="">All statuses</option>{statuses.map((value) => <option key={value} value={value}>{value}</option>)}</select>
        </label>
      </div>

      {formOpen && canWrite && (
        <form aria-label={editing ? 'Edit asset' : 'Add asset'} className="mt-6 rounded-xl border border-steward-blue/40 bg-steward-ink-950/55 p-5" key={editing?.id ?? 'new'} onSubmit={handleSubmit}>
          <div className="flex flex-wrap items-center justify-between gap-3"><h3 className="text-lg font-semibold">{editing ? `Edit ${editing.name}` : 'Register an asset'}</h3><button className="text-sm text-steward-teal underline underline-offset-4" onClick={() => { setFormOpen(false); setEditing(null) }} type="button">Cancel</button></div>
          <div className="mt-4 grid gap-4 md:grid-cols-2 lg:grid-cols-3">
            <TextField defaultValue={assetValue(editing, 'name')} label="Asset name" name="name" required />
            <ModelSelect defaultValue={assetValue(editing, 'modelId') || prefillModelID} models={models} />
            <SelectField defaultValue={assetValue(editing, 'kind') || prefillKind || 'server'} label="Kind" name="kind" options={kinds} />
            <SelectField defaultValue={assetValue(editing, 'status') || 'draft'} label="Status" name="status" options={statuses} />
            <TextField defaultValue={assetValue(editing, 'assetTag')} label="Asset tag" maxLength={128} name="assetTag" />
            <TextField defaultValue={assetValue(editing, 'serialNumber')} label="Serial number" maxLength={255} name="serialNumber" />
            <TextField defaultValue={assetValue(editing, 'hostname')} label="Hostname" maxLength={253} name="hostname" />
            <label className="text-sm font-semibold text-steward-mist-muted">Purchase date<input className={inputClass} defaultValue={assetValue(editing, 'purchaseDate').slice(0, 10)} name="purchaseDate" type="date" /></label>
            <ReferenceSelect defaultValue={assetValue(editing, 'siteId')} label="Site" name="siteId" options={references.sites} />
            <ReferenceSelect defaultValue={assetValue(editing, 'buildingId')} label="Building" name="buildingId" options={references.buildings} />
            <ReferenceSelect defaultValue={assetValue(editing, 'roomId')} label="Room" name="roomId" options={references.rooms} />
            <ReferenceSelect defaultValue={assetValue(editing, 'departmentId')} label="Department" name="departmentId" options={references.departments} />
            <ReferenceSelect defaultValue={assetValue(editing, 'userId')} label="Primary user" name="userId" options={references.identities} />
            {editing && <TextField defaultValue="" help="Required only when changing status; stored with lifecycle history." label="Lifecycle note" maxLength={1000} name="lifecycleNote" />}
          </div>
          {!canReadDirectory && <p className="mt-4 text-sm text-steward-mist-muted">Directory references require directory read access. You can still maintain core asset identity fields.</p>}
          <button className={`${buttonClass} mt-5`} disabled={busy === 'save'} type="submit">{busy === 'save' ? 'Saving…' : editing ? 'Save changes' : 'Create asset'}</button>
        </form>
      )}

      <div className="mt-6 grid gap-6 lg:grid-cols-[minmax(0,1.4fr)_minmax(18rem,0.8fr)]">
        <div>
          <h3 className="text-lg font-semibold">Assets <span className="text-sm font-normal text-steward-mist-muted">({filteredAssets.length})</span></h3>
          {filteredAssets.length === 0 ? <p className="mt-4 rounded-xl border border-dashed border-steward-ink-800 p-5 text-sm text-steward-mist-muted">No assets match these filters.</p> : (
            <ul className="mt-3 divide-y divide-steward-ink-800">{filteredAssets.map((asset) => (
              <li className="flex flex-wrap items-center justify-between gap-3 py-4" key={asset.id}>
                <button className="min-h-11 text-left" onClick={() => void selectAsset(asset)} type="button"><span className="block font-semibold text-steward-mist">{asset.name}</span><span className="text-sm text-steward-mist-muted">{asset.assetTag || asset.serialNumber || 'No asset tag'} · {asset.kind} · {asset.status}</span></button>
                {canWrite && <button className={secondaryButtonClass} onClick={() => openEdit(asset)} type="button">Edit</button>}
              </li>
            ))}</ul>
          )}
        </div>
        <aside aria-labelledby="asset-detail-heading" className="rounded-xl border border-steward-ink-800 bg-steward-ink-950/45 p-5">
          <h3 className="text-lg font-semibold" id="asset-detail-heading">Asset details</h3>
          {!selected ? <p className="mt-3 text-sm text-steward-mist-muted">Choose an asset to inspect its current record and lifecycle.</p> : <>
            <dl className="mt-4 grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm"><Detail label="Name" value={selected.name} /><Detail label="Model" value={models.find((model) => model.id === selected.modelId) ? modelLabel(models.find((model) => model.id === selected.modelId) as AssetModel) : selected.modelId} /><Detail label="Kind" value={selected.kind} /><Detail label="Status" value={selected.status} /><Detail label="Asset tag" value={selected.assetTag} /><Detail label="Serial" value={selected.serialNumber} /><Detail label="Hostname" value={selected.hostname} /><Detail label="Site" value={selected.siteId} /><Detail label="Building" value={selected.buildingId} /><Detail label="Room" value={selected.roomId} /><Detail label="Department" value={selected.departmentId} /><Detail label="User" value={selected.userId} /><Detail label="Revision" value={String(selected.revision)} /></dl>
            <h4 className="mt-6 font-semibold">Lifecycle history</h4>
            {busy === `history-${selected.id}` ? <p className="mt-2 text-sm text-steward-mist-muted" role="status">Loading lifecycle…</p> : lifecycle.length === 0 ? <p className="mt-2 text-sm text-steward-mist-muted">No lifecycle events loaded.</p> : <ol className="mt-3 space-y-3">{lifecycle.map((event) => <li className="border-l-2 border-steward-blue pl-3 text-sm" key={event.id}><p><strong>{event.fromStatus ? `${event.fromStatus} → ` : ''}{event.toStatus}</strong> · revision {event.revision}</p><p className="text-steward-mist-muted">{event.note || 'Status recorded'} · {new Date(event.occurredAt).toLocaleDateString()}</p></li>)}</ol>}
            <AtlasIdentifiers assetId={selected.id} assetName={selected.name} canWrite={canWrite} csrfToken={csrfToken} />
          </>}
        </aside>
      </div>
    </section>
  )
}

function TextField({ defaultValue, help, label, maxLength, name, required }: { defaultValue: string; help?: string; label: string; maxLength?: number; name: string; required?: boolean }) {
  const helpID = help ? `${name}-help` : undefined
  return <label className="text-sm font-semibold text-steward-mist-muted">{label}{help && <span className="mt-1 block font-normal leading-5" id={helpID}>{help}</span>}<input aria-describedby={helpID} className={inputClass} defaultValue={defaultValue} maxLength={maxLength} name={name} required={required} /></label>
}

function SelectField({ defaultValue, label, name, options }: { defaultValue: string; label: string; name: string; options: string[] }) {
  return <label className="text-sm font-semibold text-steward-mist-muted">{label}<select className={inputClass} defaultValue={defaultValue} name={name}>{options.map((option) => <option key={option} value={option}>{option}</option>)}</select></label>
}

function NumberField({ defaultValue, label, max, name }: { defaultValue: number; label: string; max: number; name: string }) {
  return <label className="text-sm font-semibold text-steward-mist-muted">{label}<input className={inputClass} defaultValue={defaultValue} max={max} min={0} name={name} type="number" /></label>
}

function ModelSelect({ defaultValue, models }: { defaultValue: string; models: AssetModel[] }) {
  const hasDefault = !defaultValue || models.some((model) => model.id === defaultValue)
  return <label className="text-sm font-semibold text-steward-mist-muted">Model<select className={inputClass} defaultValue={defaultValue} name="modelId"><option value="">No model</option>{!hasDefault && <option value={defaultValue}>{defaultValue} (current)</option>}{models.map((model) => <option key={model.id} value={model.id}>{modelLabel(model)}</option>)}</select></label>
}

function ReferenceSelect({ defaultValue, label, name, options }: { defaultValue: string; label: string; name: string; options: ReferenceRecord[] }) {
  const hasDefault = !defaultValue || options.some((option) => option.id === defaultValue)
  return <label className="text-sm font-semibold text-steward-mist-muted">{label}<select className={inputClass} defaultValue={defaultValue} name={name}><option value="">Not assigned</option>{!hasDefault && <option value={defaultValue}>{defaultValue} (current)</option>}{options.map((option) => <option key={option.id} value={option.id}>{referenceLabel(option)}</option>)}</select></label>
}

function Detail({ label, value }: { label: string; value: string | undefined }) {
  return <><dt className="font-semibold text-steward-mist-muted">{label}</dt><dd className="min-w-0 break-words">{value || 'Not assigned'}</dd></>
}
