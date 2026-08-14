import { type FormEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ApiRequestError, isRevision, requestJSON, type Revision } from './api'
import AtlasIdentifiers from './AtlasIdentifiers'
import AtlasLabelPrint from './AtlasLabelPrint'
import AtlasScanner from './AtlasScanner'
import { StatusBadge, buttonClass, dangerButtonClass, emptyStateClass, inputClass, labelClass, panelClass, plainButtonClass, secondaryButtonClass, subpanelClass } from './ui'

// Requirements: REQ-ATLAS-001, REQ-ATLAS-CODES-001, REQ-ATLAS-MODELS-001. Features: inventory.assets, inventory.identifiers, inventory.models.

export type Asset = {
  id: string
  organizationId: string
  modelId?: string
  modelContext?: AssetModelContext
  name: string
  kind: string
  assetTag?: string
  serialNumber?: string
  hostname?: string
  deploymentNotes?: string
  siteId?: string
  buildingId?: string
  roomId?: string
  departmentId?: string
  userId?: string
  status: string
  purchaseDate?: string
  revision: Revision
  createdAt: string
  updatedAt: string
}

export type AssetModelContext = {
  manufacturer: string
  name: string
  modelNumber?: string
  kind: string
  vendorIdentifier?: string
  specifications?: Record<string, string>
  supportUrl?: string
  warrantyMonths?: number
  usefulLifeMonths?: number
  sourceSystemId?: string
  sourceRecordId?: string
  modelRevision: number
  defaultsEffectiveAt: string
  appliedAt: string
  overrides: string[]
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
  revision: Revision
  createdAt: string
  updatedAt: string
}

type ModelInventoryGroup = {
  key: string
  count: number
}

type ModelInventory = {
  modelId: string
  totalCount: number
  filteredCount: number
  groupBy?: string
  groups: ModelInventoryGroup[]
  items: Asset[]
}

type ModelInventoryFilters = {
  status: string
  siteId: string
  departmentId: string
  userId: string
  deploymentContext: string
  groupBy: string
}

type LifecycleEvent = {
  id: string
  fromStatus?: string
  toStatus: string
  note?: string
  revision: Revision
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

type BulkAssetRow = {
  key: number
}

type ModelSpecificationRow = {
  key: number
  name: string
  value: string
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
const emptyModelInventoryFilters: ModelInventoryFilters = { status: '', siteId: '', departmentId: '', userId: '', deploymentContext: '', groupBy: '' }

export function isAsset(value: unknown): value is Asset {
  if (typeof value !== 'object' || value === null) return false
  const item = value as Record<string, unknown>
  return typeof item.id === 'string' && typeof item.organizationId === 'string'
    && typeof item.name === 'string' && typeof item.kind === 'string' && typeof item.status === 'string'
    && isRevision(item.revision) && typeof item.createdAt === 'string' && typeof item.updatedAt === 'string'
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
  return typeof item.id === 'string' && typeof item.toStatus === 'string' && isRevision(item.revision)
    && typeof item.actorId === 'string' && typeof item.occurredAt === 'string'
}

function isAssetModel(value: unknown): value is AssetModel {
  if (typeof value !== 'object' || value === null) return false
  const item = value as Record<string, unknown>
  return typeof item.id === 'string' && typeof item.organizationId === 'string'
    && typeof item.manufacturer === 'string' && typeof item.name === 'string'
    && typeof item.kind === 'string' && typeof item.status === 'string'
    && typeof item.instanceCount === 'number' && isRevision(item.revision)
    && typeof item.createdAt === 'string' && typeof item.updatedAt === 'string'
    && ['modelNumber', 'vendorIdentifier', 'supportUrl', 'sourceSystemId', 'sourceRecordId']
      .every((key) => item[key] === undefined || typeof item[key] === 'string')
    && ['warrantyMonths', 'usefulLifeMonths'].every((key) => item[key] === undefined || typeof item[key] === 'number')
    && (item.specifications === undefined || (typeof item.specifications === 'object' && item.specifications !== null
      && !Array.isArray(item.specifications) && Object.values(item.specifications).every((entry) => typeof entry === 'string')))
}

function isModelInventory(value: unknown): value is ModelInventory {
  if (typeof value !== 'object' || value === null) return false
  const item = value as Record<string, unknown>
  return typeof item.modelId === 'string' && typeof item.totalCount === 'number' && typeof item.filteredCount === 'number'
    && Array.isArray(item.groups) && item.groups.every((group) => typeof group === 'object' && group !== null
      && typeof (group as Record<string, unknown>).key === 'string' && typeof (group as Record<string, unknown>).count === 'number')
    && Array.isArray(item.items) && item.items.every(isAsset)
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

function modelContextLabel(context: AssetModelContext) {
  return `${context.manufacturer} ${context.name}${context.modelNumber ? ` ${context.modelNumber}` : ''}`.trim()
}

function formatTimestamp(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.valueOf()) ? value : date.toLocaleString()
}

export default function AtlasInventory({ assets, csrfToken, permissions, onAssetsChange, onOpenHelp }: AtlasInventoryProps) {
  const [search, setSearch] = useState('')
  const [kind, setKind] = useState('')
  const [statusFilter, setStatusFilter] = useState('')
  const [editing, setEditing] = useState<Asset | null>(null)
  const [modelEditing, setModelEditing] = useState<AssetModel | null>(null)
  const [formOpen, setFormOpen] = useState(false)
  const [modelFormOpen, setModelFormOpen] = useState(false)
  const [bulkModel, setBulkModel] = useState<AssetModel | null>(null)
  const [bulkRows, setBulkRows] = useState<BulkAssetRow[]>([{ key: 0 }])
  const [prefillModelID, setPrefillModelID] = useState('')
  const [prefillKind, setPrefillKind] = useState('')
  const [selected, setSelected] = useState<Asset | null>(null)
  const [lifecycle, setLifecycle] = useState<LifecycleEvent[]>([])
  const [models, setModels] = useState<AssetModel[]>([])
  const [modelSearch, setModelSearch] = useState('')
  const [modelKind, setModelKind] = useState('')
  const [modelSpecificationRows, setModelSpecificationRows] = useState<ModelSpecificationRow[]>([])
  const [inventoryModel, setInventoryModel] = useState<AssetModel | null>(null)
  const [modelInventory, setModelInventory] = useState<ModelInventory | null>(null)
  const [modelInventoryFilters, setModelInventoryFilters] = useState<ModelInventoryFilters>(emptyModelInventoryFilters)
  const [references, setReferences] = useState<ReferenceOptions>(emptyReferences)
  const [referencesLoaded, setReferencesLoaded] = useState(false)
  const [busy, setBusy] = useState('')
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')
  const [identifierRefreshVersion, setIdentifierRefreshVersion] = useState(0)
  const errorRef = useRef<HTMLDivElement>(null)
  const nextBulkRowKey = useRef(1)
  const nextModelSpecificationKey = useRef(0)
  const modelLoadVersion = useRef(0)
  const canWrite = permissions.includes('assets.write')
  const canReadDirectory = permissions.includes('directory.read')

  const loadModels = useCallback(async (filters: { search?: string; kind?: string } = {}) => {
    const version = modelLoadVersion.current + 1
    modelLoadVersion.current = version
    const query = new URLSearchParams({ limit: '100' })
    const normalizedSearch = filters.search?.trim()
    if (normalizedSearch) query.set('q', normalizedSearch)
    if (filters.kind) query.set('kind', filters.kind)
    try {
      const response = await requestJSON(`/api/v1/asset-models?${query.toString()}`)
      if (modelLoadVersion.current === version) setModels(readItems(response).filter(isAssetModel))
    } catch {
      if (modelLoadVersion.current === version) setModels([])
    }
  }, [])

  useEffect(() => {
    void loadModels()
  }, [loadModels])

  useEffect(() => {
    if (inventoryModel) document.getElementById('model-inventory-heading')?.focus()
  }, [inventoryModel])

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

  function openBulkCreateFromModel(model: AssetModel) {
    setBulkModel(model)
    setBulkRows([{ key: 0 }])
    nextBulkRowKey.current = 1
    setError('')
    setMessage('')
    void loadReferences()
  }

  function addBulkRow() {
    if (bulkRows.length >= 100) return
    const key = nextBulkRowKey.current
    nextBulkRowKey.current += 1
    setBulkRows((current) => [...current, { key }])
  }

  function removeBulkRow(key: number) {
    setBulkRows((current) => current.length > 1 ? current.filter((row) => row.key !== key) : current)
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
    setModelSpecificationRows([])
    nextModelSpecificationKey.current = 0
    setModelFormOpen(true)
    setError('')
    setMessage('')
  }

  function openModelEdit(model: AssetModel) {
    setModelEditing(model)
    const rows = Object.entries(model.specifications ?? {})
      .sort(([left], [right]) => left.localeCompare(right))
      .map(([name, value], key) => ({ key, name, value }))
    setModelSpecificationRows(rows)
    nextModelSpecificationKey.current = rows.length
    setModelFormOpen(true)
    setError('')
    setMessage('')
  }

  function addModelSpecification() {
    if (modelSpecificationRows.length >= 25) return
    const key = nextModelSpecificationKey.current
    nextModelSpecificationKey.current += 1
    setModelSpecificationRows((current) => [...current, { key, name: '', value: '' }])
  }

  function updateModelSpecification(key: number, field: 'name' | 'value', value: string) {
    setModelSpecificationRows((current) => current.map((row) => row.key === key ? { ...row, [field]: value } : row))
  }

  function removeModelSpecification(key: number) {
    setModelSpecificationRows((current) => current.filter((row) => row.key !== key))
  }

  function handleModelSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    void loadModels({ search: modelSearch, kind: modelKind })
  }

  function clearModelSearch() {
    setModelSearch('')
    setModelKind('')
    void loadModels()
  }

  async function loadModelInventory(model: AssetModel, filters: ModelInventoryFilters) {
    setBusy(`inventory-${model.id}`)
    setError('')
    const query = new URLSearchParams({ limit: '100' })
    Object.entries(filters).forEach(([key, value]) => {
      if (value) query.set(key, value)
    })
    try {
      const response = await requestJSON(`/api/v1/asset-models/${encodeURIComponent(model.id)}/inventory?${query.toString()}`)
      if (!isModelInventory(response) || response.modelId !== model.id) throw new Error('invalid model inventory response')
      setModelInventory(response)
    } catch (requestError) {
      setModelInventory(null)
      setError(requestError instanceof ApiRequestError ? requestError.message : 'The model inventory could not be loaded.')
      queueMicrotask(() => errorRef.current?.focus())
    } finally {
      setBusy('')
    }
  }

  function openModelInventory(model: AssetModel) {
    setInventoryModel(model)
    setModelInventory(null)
    setModelInventoryFilters(emptyModelInventoryFilters)
    setMessage('')
    void loadReferences()
    void loadModelInventory(model, emptyModelInventoryFilters)
  }

  function handleModelInventorySubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (inventoryModel) void loadModelInventory(inventoryModel, modelInventoryFilters)
  }

  function setModelInventoryFilter(name: keyof ModelInventoryFilters, value: string) {
    setModelInventoryFilters((current) => ({ ...current, [name]: value }))
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

  async function resolveScannedAsset(assetID: string) {
    let asset = assets.find((item) => item.id === assetID)
    if (!asset) {
      const response = await requestJSON(`/api/v1/assets/${encodeURIComponent(assetID)}`)
      if (!isAsset(response)) throw new Error('invalid asset response')
      asset = response
      onAssetsChange([...assets, asset].sort((left, right) => left.name.localeCompare(right.name)))
    }
    await selectAsset(asset)
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
      deploymentNotes: String(values.get('deploymentNotes') ?? ''),
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
      void loadModels({ search: modelSearch, kind: modelKind })
      void selectAsset(saved)
    } catch (requestError) {
      setError(requestError instanceof ApiRequestError ? requestError.message : 'The asset could not be saved.')
      queueMicrotask(() => errorRef.current?.focus())
    } finally {
      setBusy('')
    }
  }

  async function handleBulkSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!bulkModel) return
    setError('')
    setMessage('')
    const values = new FormData(event.currentTarget)
    const items = bulkRows.map((row) => {
      const prefix = `bulk-${row.key}-`
      const purchaseDate = String(values.get(`${prefix}purchaseDate`) ?? '')
      const item: Record<string, unknown> = {
        name: String(values.get(`${prefix}name`) ?? ''),
        assetTag: String(values.get(`${prefix}assetTag`) ?? ''),
        serialNumber: String(values.get(`${prefix}serialNumber`) ?? ''),
        hostname: String(values.get(`${prefix}hostname`) ?? ''),
        deploymentNotes: String(values.get(`${prefix}deploymentNotes`) ?? ''),
        siteId: String(values.get(`${prefix}siteId`) ?? ''),
        buildingId: String(values.get(`${prefix}buildingId`) ?? ''),
        roomId: String(values.get(`${prefix}roomId`) ?? ''),
        departmentId: String(values.get(`${prefix}departmentId`) ?? ''),
        userId: String(values.get(`${prefix}userId`) ?? ''),
        status: String(values.get(`${prefix}status`) ?? ''),
      }
      if (purchaseDate) item.purchaseDate = `${purchaseDate}T00:00:00Z`
      return item
    })
    setBusy('save-bulk')
    try {
      const response = await requestJSON(`/api/v1/asset-models/${encodeURIComponent(bulkModel.id)}/assets/bulk`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({ items }),
      })
      const created = readItems(response).filter(isAsset)
      if (created.length !== items.length) throw new Error('invalid bulk asset response')
      onAssetsChange([...assets, ...created].sort((left, right) => left.name.localeCompare(right.name)))
      setBulkModel(null)
      setMessage(`${created.length} asset${created.length === 1 ? '' : 's'} created from ${modelLabel(bulkModel)}.`)
      void loadModels({ search: modelSearch, kind: modelKind })
    } catch (requestError) {
      setError(requestError instanceof ApiRequestError ? requestError.message : 'The asset batch could not be created.')
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
    const specifications: Record<string, string> = Object.create(null) as Record<string, string>
    for (const row of modelSpecificationRows) {
      const name = row.name.trim()
      const value = row.value.trim()
      if (!name || Object.prototype.hasOwnProperty.call(specifications, name)) {
        setError(!name ? 'Every shared specification needs a name.' : `Shared specification names must be unique: ${name}.`)
        queueMicrotask(() => errorRef.current?.focus())
        return
      }
      specifications[name] = value
    }
    const payload: Record<string, unknown> = {
      manufacturer: String(values.get('manufacturer') ?? ''),
      name: String(values.get('modelName') ?? ''),
      modelNumber: String(values.get('modelNumber') ?? ''),
      kind: String(values.get('modelKind') ?? ''),
      vendorIdentifier: String(values.get('vendorIdentifier') ?? ''),
      specifications,
      supportUrl: String(values.get('supportUrl') ?? ''),
      warrantyMonths: Number(values.get('warrantyMonths') || 0),
      usefulLifeMonths: Number(values.get('usefulLifeMonths') || 0),
      sourceSystemId: String(values.get('sourceSystemId') ?? ''),
      sourceRecordId: String(values.get('sourceRecordId') ?? ''),
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
      setInventoryModel((current) => current?.id === saved.id ? saved : current)
      setModelEditing(null)
      setModelSpecificationRows([])
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
      if (inventoryModel?.id === retired.id) {
        setInventoryModel(null)
        setModelInventory(null)
      }
      setMessage('Model retired.')
    } catch (requestError) {
      setError(requestError instanceof ApiRequestError ? requestError.message : 'The model could not be retired.')
      queueMicrotask(() => errorRef.current?.focus())
    } finally {
      setBusy('')
    }
  }

  return (
    <section aria-label="Atlas inventory workflow" className={`${panelClass} p-5 sm:p-6`} data-feature="inventory.assets" data-requirement="REQ-ATLAS-001">
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

      <AtlasScanner canWrite={canWrite} csrfToken={csrfToken} onAssociated={() => setIdentifierRefreshVersion((current) => current + 1)} onResolveAsset={resolveScannedAsset} selectedAsset={selected ? { id: selected.id, name: selected.name } : null} />

      {canWrite && <AtlasLabelPrint assets={filteredAssets} csrfToken={csrfToken} key={`label-print-${identifierRefreshVersion}-${filteredAssets.map((asset) => asset.id).join(',')}`} />}

      <section aria-labelledby="models-heading" className={`${subpanelClass} mt-6 overflow-hidden`} data-feature="inventory.models" data-requirement="REQ-ATLAS-MODELS-001">
        <div aria-hidden="true" className="h-px bg-gradient-to-r from-steward-green/70 via-steward-teal/70 to-steward-blue/70" />
        <div className="p-5 sm:p-6">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <div className="flex flex-wrap items-center gap-2"><h3 className="text-lg font-semibold" id="models-heading">Model catalog</h3><StatusBadge tone="info">{models.length} model{models.length === 1 ? '' : 's'} shown</StatusBadge></div>
            <p className="mt-1 text-sm text-steward-mist-muted">Shared manufacturer and model defaults for repeated assets.</p>
          </div>
          {canWrite && <button className={secondaryButtonClass} onClick={openModelCreate} type="button">Add model</button>}
        </div>
        <form aria-label="Search models" className="mt-5 grid gap-4 sm:grid-cols-[minmax(0,1fr)_minmax(10rem,0.45fr)_auto]" onSubmit={handleModelSearch} role="search">
          <label className={labelClass}>Search models<input className={inputClass} maxLength={200} onChange={(event) => setModelSearch(event.target.value)} placeholder="Manufacturer, model, number, or vendor ID" type="search" value={modelSearch} /></label>
          <label className={labelClass}>Model kind<select className={inputClass} onChange={(event) => setModelKind(event.target.value)} value={modelKind}><option value="">All kinds</option>{kinds.map((value) => <option key={value} value={value}>{value}</option>)}</select></label>
          <div className="flex flex-wrap items-end gap-2"><button className={secondaryButtonClass} type="submit">Search</button><button className={plainButtonClass} onClick={clearModelSearch} type="button">Clear</button></div>
        </form>
        {modelFormOpen && canWrite && (
          <form aria-label={modelEditing ? 'Edit model' : 'Add model'} className={`${subpanelClass} mt-5 border-steward-blue/35 bg-steward-ink-900/75 p-5`} key={modelEditing?.id ?? 'new-model'} onSubmit={handleModelSubmit}>
            <div className="flex flex-wrap items-center justify-between gap-3">
              <h4 className="font-semibold">{modelEditing ? `Edit ${modelLabel(modelEditing)}` : 'Register a model'}</h4>
              <button className={plainButtonClass} onClick={() => { setModelFormOpen(false); setModelEditing(null); setModelSpecificationRows([]) }} type="button">Cancel</button>
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
              <TextField defaultValue={modelEditing?.sourceSystemId ?? ''} help="Optional provider or import system identifier." label="Source system ID" maxLength={120} name="sourceSystemId" />
              <TextField defaultValue={modelEditing?.sourceRecordId ?? ''} help="Optional upstream record identifier." label="Source record ID" maxLength={160} name="sourceRecordId" />
            </div>
            <fieldset className={`${subpanelClass} mt-5 p-4`}>
              <legend className="px-1 font-semibold">Shared specifications</legend>
              <p className="mt-1 text-sm text-steward-mist-muted">Add up to 25 reusable key/value defaults. These are snapshotted when an asset is linked.</p>
              {modelSpecificationRows.length === 0 ? <p className="mt-3 text-sm text-steward-mist-muted">No shared specifications.</p> : <div className="mt-4 space-y-3">{modelSpecificationRows.map((row, index) => <fieldset className="grid gap-3 rounded-lg border border-white/10 p-3 sm:grid-cols-[minmax(0,0.7fr)_minmax(0,1fr)_auto]" key={row.key}>
                <legend className="sr-only">Specification {index + 1}</legend>
                <label className={labelClass}>Specification {index + 1} name<input className={inputClass} maxLength={80} onChange={(event) => updateModelSpecification(row.key, 'name', event.target.value)} required value={row.name} /></label>
                <label className={labelClass}>Specification {index + 1} value<input className={inputClass} maxLength={500} onChange={(event) => updateModelSpecification(row.key, 'value', event.target.value)} value={row.value} /></label>
                <div className="flex items-end"><button className={plainButtonClass} onClick={() => removeModelSpecification(row.key)} type="button">Remove</button></div>
              </fieldset>)}</div>}
              <button className={`${secondaryButtonClass} mt-4`} disabled={modelSpecificationRows.length >= 25} onClick={addModelSpecification} type="button">Add specification</button>
            </fieldset>
            <button className={`${buttonClass} mt-5`} disabled={busy === 'save-model'} type="submit">{busy === 'save-model' ? 'Saving…' : modelEditing ? 'Save model' : 'Create model'}</button>
          </form>
        )}
        {bulkModel && canWrite && (
          <form aria-label="Bulk add assets" className={`${subpanelClass} mt-5 border-steward-blue/35 bg-steward-ink-900/75 p-5`} onSubmit={handleBulkSubmit}>
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div>
                <h4 className="font-semibold">Bulk add from {modelLabel(bulkModel)}</h4>
                <p className="mt-1 text-sm text-steward-mist-muted">Create up to 100 instances atomically. The model supplies the shared kind; every row keeps its own identity, deployment, status, and assignment details.</p>
              </div>
              <button className={plainButtonClass} onClick={() => setBulkModel(null)} type="button">Cancel</button>
            </div>
            <div className="mt-5 space-y-5">
              {bulkRows.map((row, index) => {
                const prefix = `bulk-${row.key}-`
                return <fieldset className={`${subpanelClass} p-4`} key={row.key}>
                  <legend className="px-1 font-semibold">Asset {index + 1}</legend>
                  {bulkRows.length > 1 && <div className="flex justify-end"><button className={plainButtonClass} onClick={() => removeBulkRow(row.key)} type="button">Remove asset {index + 1}</button></div>}
                  <div className="mt-3 grid gap-4 md:grid-cols-2 lg:grid-cols-3">
                    <TextField defaultValue="" label="Asset name" maxLength={200} name={`${prefix}name`} required />
                    <TextField defaultValue="" label="Asset tag" maxLength={128} name={`${prefix}assetTag`} />
                    <TextField defaultValue="" label="Serial number" maxLength={255} name={`${prefix}serialNumber`} />
                    <TextField defaultValue="" label="Hostname" maxLength={253} name={`${prefix}hostname`} />
                    <SelectField defaultValue="draft" label="Status" name={`${prefix}status`} options={statuses} />
                    <label className={labelClass}>Purchase date<input className={inputClass} name={`${prefix}purchaseDate`} type="date" /></label>
                    <ReferenceSelect defaultValue="" label="Site" name={`${prefix}siteId`} options={references.sites} />
                    <ReferenceSelect defaultValue="" label="Building" name={`${prefix}buildingId`} options={references.buildings} />
                    <ReferenceSelect defaultValue="" label="Room" name={`${prefix}roomId`} options={references.rooms} />
                    <ReferenceSelect defaultValue="" label="Department" name={`${prefix}departmentId`} options={references.departments} />
                    <ReferenceSelect defaultValue="" label="Primary user" name={`${prefix}userId`} options={references.identities} />
                    <TextAreaField defaultValue="" label="Deployment notes" maxLength={2000} name={`${prefix}deploymentNotes`} />
                  </div>
                </fieldset>
              })}
            </div>
            {!canReadDirectory && <p className="mt-4 text-sm text-steward-mist-muted">Directory references require directory read access. Core identity and deployment fields remain available.</p>}
            <div className="mt-5 flex flex-wrap gap-3">
              <button className={secondaryButtonClass} disabled={bulkRows.length >= 100} onClick={addBulkRow} type="button">Add another asset</button>
              <button className={buttonClass} disabled={busy === 'save-bulk'} type="submit">{busy === 'save-bulk' ? 'Creating batch…' : `Create ${bulkRows.length} asset${bulkRows.length === 1 ? '' : 's'}`}</button>
            </div>
          </form>
        )}
        {models.length === 0 ? <p className={`${emptyStateClass} mt-4`}>No active models match this search.</p> : (
          <ul className="mt-4 grid gap-3 lg:grid-cols-2">{models.map((model) => (
            <li className={`${subpanelClass} p-4 transition hover:border-white/15 hover:bg-white/[0.025]`} key={model.id}>
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div className="min-w-0">
                  <p className="break-words font-semibold text-steward-mist">{modelLabel(model)}</p>
                  <div className="mt-2 flex flex-wrap gap-2"><StatusBadge>{model.kind}</StatusBadge><StatusBadge tone={model.instanceCount > 0 ? 'success' : 'neutral'}>{model.instanceCount} asset{model.instanceCount === 1 ? '' : 's'}</StatusBadge>{Boolean(model.warrantyMonths) && <StatusBadge tone="info">{model.warrantyMonths} month warranty</StatusBadge>}</div>
                </div>
                <div className="flex flex-wrap gap-2">
                  <a className={secondaryButtonClass} href="#workspace-atlas" onClick={(event) => { event.preventDefault(); openModelInventory(model) }}>View inventory</a>
                {canWrite && <>
                  <button className={secondaryButtonClass} onClick={() => openCreateFromModel(model)} type="button">Use</button>
                  <button className={secondaryButtonClass} onClick={() => openBulkCreateFromModel(model)} type="button">Bulk add</button>
                  <button className={secondaryButtonClass} onClick={() => openModelEdit(model)} type="button">Edit</button>
                  <button className={dangerButtonClass} disabled={busy === `retire-model-${model.id}`} onClick={() => void retireModel(model)} type="button">{busy === `retire-model-${model.id}` ? 'Retiring…' : 'Retire'}</button>
                </>}
                </div>
              </div>
            </li>
          ))}</ul>
        )}
        {inventoryModel && <section aria-labelledby="model-inventory-heading" className={`${subpanelClass} mt-5 border-steward-teal/35 p-4 sm:p-5`}>
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div className="min-w-0">
              <p className="text-sm font-semibold text-steward-teal">Model detail</p>
              <h4 className="mt-1 break-words text-lg font-semibold outline-none focus-visible:ring-2 focus-visible:ring-steward-teal" id="model-inventory-heading" tabIndex={-1}>Inventory for {modelLabel(inventoryModel)}</h4>
              <p className="mt-1 text-sm text-steward-mist-muted">Filter this model's linked Atlas assets, then group the matching instances.</p>
            </div>
            <button className={plainButtonClass} onClick={() => { setInventoryModel(null); setModelInventory(null) }} type="button">Close</button>
          </div>
          <ModelRecordDetails model={inventoryModel} />
          <form aria-label={`Filter inventory for ${modelLabel(inventoryModel)}`} className="mt-5 grid gap-4 sm:grid-cols-2 lg:grid-cols-3" onSubmit={handleModelInventorySubmit}>
            <label className={labelClass}>Lifecycle state<select className={inputClass} onChange={(event) => setModelInventoryFilter('status', event.target.value)} value={modelInventoryFilters.status}><option value="">All states</option>{statuses.map((value) => <option key={value} value={value}>{value}</option>)}</select></label>
            <ModelInventoryReferenceFilter label="Site" onChange={(value) => setModelInventoryFilter('siteId', value)} options={references.sites} value={modelInventoryFilters.siteId} />
            <ModelInventoryReferenceFilter label="Asset department" onChange={(value) => setModelInventoryFilter('departmentId', value)} options={references.departments} value={modelInventoryFilters.departmentId} />
            <ModelInventoryReferenceFilter label="Primary user (asset)" onChange={(value) => setModelInventoryFilter('userId', value)} options={references.identities} value={modelInventoryFilters.userId} />
            <label className={labelClass}>Deployment context<input className={inputClass} maxLength={200} onChange={(event) => setModelInventoryFilter('deploymentContext', event.target.value)} placeholder="Hostname or deployment notes" type="search" value={modelInventoryFilters.deploymentContext} /></label>
            <label className={labelClass}>Group matching assets<select className={inputClass} onChange={(event) => setModelInventoryFilter('groupBy', event.target.value)} value={modelInventoryFilters.groupBy}><option value="">No grouping</option><option value="status">Lifecycle state</option><option value="site">Site</option><option value="department">Asset department</option><option value="user">Primary user (asset)</option><option value="deployment">Deployment context</option></select></label>
            <div className="flex flex-wrap items-end gap-3 sm:col-span-2 lg:col-span-3">
              <button className={buttonClass} disabled={busy === `inventory-${inventoryModel.id}`} type="submit">{busy === `inventory-${inventoryModel.id}` ? 'Applying…' : 'Apply filters'}</button>
              <button className={secondaryButtonClass} onClick={() => { setModelInventoryFilters(emptyModelInventoryFilters); void loadModelInventory(inventoryModel, emptyModelInventoryFilters) }} type="button">Clear filters</button>
            </div>
          </form>
          <p className="mt-3 text-sm text-steward-mist-muted">Asset department and primary user match the current Atlas instance fields. Effective-dated primary, additional-user, and responsible-department history remains in People.</p>
          {!canReadDirectory && <p className="mt-2 text-sm text-steward-mist-muted">Site, asset department, and primary-user choices require directory read access.</p>}
          {busy === `inventory-${inventoryModel.id}` && !modelInventory ? <p className="mt-5 text-sm text-steward-mist-muted" role="status">Loading model inventory…</p> : modelInventory && <>
            <div className="mt-5 flex flex-wrap gap-2" role="status"><StatusBadge tone="info">{modelInventory.filteredCount} matching</StatusBadge><StatusBadge>{modelInventory.totalCount} total</StatusBadge>{modelInventory.items.length < modelInventory.filteredCount && <span className="self-center text-sm text-steward-mist-muted">Showing the first {modelInventory.items.length} assets.</span>}</div>
            {modelInventory.groupBy && <section aria-labelledby="model-inventory-groups-heading" className="mt-5"><h5 className="font-semibold" id="model-inventory-groups-heading">Grouped counts</h5>{modelInventory.groups.length === 0 ? <p className="mt-2 text-sm text-steward-mist-muted">No groups match these filters.</p> : <ul className="mt-3 grid gap-2 sm:grid-cols-2 lg:grid-cols-3">{modelInventory.groups.map((group) => <li className="flex min-w-0 items-center justify-between gap-3 rounded-lg border border-white/10 bg-white/[0.025] p-3 text-sm" key={group.key || 'unassigned'}><span className="min-w-0 break-words">{modelInventoryGroupLabel(modelInventory.groupBy || '', group.key, references)}</span><StatusBadge tone="info">{group.count}</StatusBadge></li>)}</ul>}</section>}
            <section aria-labelledby="model-inventory-assets-heading" className="mt-5"><h5 className="font-semibold" id="model-inventory-assets-heading">Matching assets</h5>{modelInventory.items.length === 0 ? <p className={`${emptyStateClass} mt-3`}>No linked assets match these filters.</p> : <ul className="mt-3 grid gap-3 sm:grid-cols-2">{modelInventory.items.map((asset) => <li className="min-w-0 rounded-xl border border-white/10 bg-white/[0.025] p-4" key={asset.id}><a className="block min-h-11 break-words font-semibold text-steward-blue-light underline-offset-4 hover:underline focus-visible:underline" href="#workspace-atlas" onClick={(event) => { event.preventDefault(); void selectAsset(asset); window.setTimeout(() => document.getElementById('asset-detail-heading')?.focus(), 0) }}>{asset.name}</a><p className="mt-1 break-words text-sm text-steward-mist-muted">{asset.assetTag || asset.serialNumber || asset.hostname || 'No asset identifier'}</p><div className="mt-3 flex flex-wrap gap-2"><StatusBadge>{asset.status}</StatusBadge>{asset.siteId && <StatusBadge tone="info">{modelInventoryGroupLabel('site', asset.siteId, references)}</StatusBadge>}</div>{(asset.hostname || asset.deploymentNotes) && <p className="mt-3 break-words text-sm"><span className="font-semibold">Deployment:</span> {[asset.hostname, asset.deploymentNotes].filter(Boolean).join(' · ')}</p>}</li>)}</ul>}</section>
          </>}
        </section>}
        </div>
      </section>

      <div aria-label="Filter assets" className="mt-6 grid gap-4 md:grid-cols-3" role="search">
        <label className={labelClass}>Search
          <input className={inputClass} onChange={(event) => setSearch(event.target.value)} placeholder="Name, tag, serial, or hostname" type="search" value={search} />
        </label>
        <label className={labelClass}>Kind
          <select className={inputClass} onChange={(event) => setKind(event.target.value)} value={kind}><option value="">All kinds</option>{kinds.map((value) => <option key={value} value={value}>{value}</option>)}</select>
        </label>
        <label className={labelClass}>Status
          <select className={inputClass} onChange={(event) => setStatusFilter(event.target.value)} value={statusFilter}><option value="">All statuses</option>{statuses.map((value) => <option key={value} value={value}>{value}</option>)}</select>
        </label>
      </div>

      {formOpen && canWrite && (
        <form aria-label={editing ? 'Edit asset' : 'Add asset'} className={`${subpanelClass} mt-6 border-steward-blue/35 p-5`} key={editing?.id ?? 'new'} onSubmit={handleSubmit}>
          <div className="flex flex-wrap items-center justify-between gap-3"><h3 className="text-lg font-semibold">{editing ? `Edit ${editing.name}` : 'Register an asset'}</h3><button className={plainButtonClass} onClick={() => { setFormOpen(false); setEditing(null) }} type="button">Cancel</button></div>
          <div className="mt-4 grid gap-4 md:grid-cols-2 lg:grid-cols-3">
            <TextField defaultValue={assetValue(editing, 'name')} label="Asset name" name="name" required />
            <ModelSelect defaultValue={assetValue(editing, 'modelId') || prefillModelID} models={models} />
            <SelectField defaultValue={assetValue(editing, 'kind') || prefillKind || 'server'} label="Kind" name="kind" options={kinds} />
            <SelectField defaultValue={assetValue(editing, 'status') || 'draft'} label="Status" name="status" options={statuses} />
            <TextField defaultValue={assetValue(editing, 'assetTag')} label="Asset tag" maxLength={128} name="assetTag" />
            <TextField defaultValue={assetValue(editing, 'serialNumber')} label="Serial number" maxLength={255} name="serialNumber" />
            <TextField defaultValue={assetValue(editing, 'hostname')} label="Hostname" maxLength={253} name="hostname" />
            <TextAreaField defaultValue={assetValue(editing, 'deploymentNotes')} label="Deployment notes" maxLength={2000} name="deploymentNotes" />
            <label className={labelClass}>Purchase date<input className={inputClass} defaultValue={assetValue(editing, 'purchaseDate').slice(0, 10)} name="purchaseDate" type="date" /></label>
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
          {filteredAssets.length === 0 ? <p className={`${emptyStateClass} mt-4`}>No assets match these filters.</p> : (
            <ul className="mt-3 space-y-2">{filteredAssets.map((asset) => (
              <li className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-transparent px-3 py-3 transition hover:border-white/[0.08] hover:bg-white/[0.035]" key={asset.id}>
                <button className="min-h-11 text-left" onClick={() => void selectAsset(asset)} type="button"><span className="block font-semibold text-steward-mist">{asset.name}</span><span className="text-sm text-steward-mist-muted">{asset.assetTag || asset.serialNumber || 'No asset tag'} · {asset.kind} · {asset.status}</span></button>
                {canWrite && <button className={secondaryButtonClass} onClick={() => openEdit(asset)} type="button">Edit</button>}
              </li>
            ))}</ul>
          )}
        </div>
        <aside aria-labelledby="asset-detail-heading" className={`${subpanelClass} p-5`}>
          <h3 className="text-lg font-semibold outline-none focus-visible:ring-2 focus-visible:ring-steward-teal" id="asset-detail-heading" tabIndex={-1}>Asset details</h3>
          {!selected ? <p className="mt-3 text-sm text-steward-mist-muted">Choose an asset to inspect its current record and lifecycle.</p> : <>
            <h4 className="mt-4 font-semibold">Instance-specific record</h4>
            <dl className="mt-3 grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-2 text-sm"><Detail label="Name" value={selected.name} /><Detail label="Model ID" value={selected.modelId} /><Detail label="Kind" value={selected.kind} /><Detail label="Status" value={selected.status} /><Detail label="Asset tag" value={selected.assetTag} /><Detail label="Serial" value={selected.serialNumber} /><Detail label="Hostname" value={selected.hostname} /><Detail label="Deployment notes" value={selected.deploymentNotes} /><Detail label="Site" value={selected.siteId} /><Detail label="Building" value={selected.buildingId} /><Detail label="Room" value={selected.roomId} /><Detail label="Asset department" value={selected.departmentId} /><Detail label="Primary user" value={selected.userId} /><Detail label="Revision" value={String(selected.revision)} /></dl>
            {selected.modelContext && <ModelContextDetails context={selected.modelContext} instanceKind={selected.kind} />}
            <h4 className="mt-6 font-semibold">Lifecycle history</h4>
            {busy === `history-${selected.id}` ? <p className="mt-2 text-sm text-steward-mist-muted" role="status">Loading lifecycle…</p> : lifecycle.length === 0 ? <p className="mt-2 text-sm text-steward-mist-muted">No lifecycle events loaded.</p> : <ol className="mt-3 space-y-3">{lifecycle.map((event) => <li className="border-l-2 border-steward-blue pl-3 text-sm" key={event.id}><p><strong>{event.fromStatus ? `${event.fromStatus} → ` : ''}{event.toStatus}</strong> · revision {event.revision}</p><p className="text-steward-mist-muted">{event.note || 'Status recorded'} · {new Date(event.occurredAt).toLocaleDateString()}</p></li>)}</ol>}
            <AtlasIdentifiers assetId={selected.id} assetName={selected.name} canWrite={canWrite} csrfToken={csrfToken} key={`${selected.id}-${identifierRefreshVersion}`} onChanged={() => setIdentifierRefreshVersion((current) => current + 1)} />
          </>}
        </aside>
      </div>
    </section>
  )
}

function ModelContextDetails({ context, instanceKind }: { context: AssetModelContext; instanceKind: string }) {
  const specifications = Object.entries(context.specifications ?? {}).sort(([left], [right]) => left.localeCompare(right))
  const kindOverridden = context.overrides.includes('kind')
  return <section aria-labelledby="asset-model-context-heading" className="mt-6 rounded-xl border border-steward-blue/25 bg-steward-blue/[0.06] p-4">
    <h4 className="font-semibold" id="asset-model-context-heading">Model defaults when linked</h4>
    <p className="mt-1 text-sm text-steward-mist-muted">This saved snapshot stays with the asset when the model record changes.</p>
    <dl className="mt-3 grid grid-cols-[minmax(0,0.45fr)_minmax(0,1fr)] gap-x-4 gap-y-2 text-sm">
      <Detail label="Model" value={modelContextLabel(context)} />
      <Detail label="Model revision" value={String(context.modelRevision)} />
      <Detail label="Default kind" value={context.kind} />
      <Detail label="Instance kind" value={`${instanceKind} (${kindOverridden ? `overrides ${context.kind}` : 'uses model default'})`} />
      <Detail label="Vendor ID" value={context.vendorIdentifier} />
      <Detail label="Warranty" value={context.warrantyMonths ? `${context.warrantyMonths} months` : undefined} />
      <Detail label="Useful life" value={context.usefulLifeMonths ? `${context.usefulLifeMonths} months` : undefined} />
      <Detail label="Support" value={context.supportUrl} />
      <Detail label="Source system" value={context.sourceSystemId || 'Manual entry'} />
      <Detail label="Source record" value={context.sourceRecordId} />
      <Detail label="Defaults effective" value={formatTimestamp(context.defaultsEffectiveAt)} />
      <Detail label="Applied to asset" value={formatTimestamp(context.appliedAt)} />
    </dl>
    {specifications.length > 0 && <>
      <h5 className="mt-4 text-sm font-semibold">Shared specifications</h5>
      <dl className="mt-2 grid grid-cols-[minmax(0,0.45fr)_minmax(0,1fr)] gap-x-4 gap-y-2 text-sm">{specifications.map(([key, value]) => <Detail key={key} label={key} value={value} />)}</dl>
    </>}
    <p className="mt-4 text-sm font-semibold">{context.overrides.length === 0 ? 'No instance overrides.' : `Overrides: ${context.overrides.map((value) => value.charAt(0).toUpperCase() + value.slice(1)).join(', ')}`}</p>
  </section>
}

function ModelRecordDetails({ model }: { model: AssetModel }) {
  const specifications = Object.entries(model.specifications ?? {}).sort(([left], [right]) => left.localeCompare(right))
  return <section aria-labelledby="model-record-details-heading" className="mt-5 rounded-xl border border-white/10 bg-white/[0.025] p-4">
    <h5 className="font-semibold" id="model-record-details-heading">Shared model defaults</h5>
    <dl className="mt-3 grid grid-cols-[minmax(0,0.45fr)_minmax(0,1fr)] gap-x-4 gap-y-2 text-sm">
      <Detail label="Manufacturer" value={model.manufacturer} />
      <Detail label="Model name" value={model.name} />
      <Detail label="Model number" value={model.modelNumber} />
      <Detail label="Kind" value={model.kind} />
      <Detail label="Vendor ID" value={model.vendorIdentifier} />
      <Detail label="Support URL" value={model.supportUrl} />
      <Detail label="Warranty" value={model.warrantyMonths ? `${model.warrantyMonths} months` : undefined} />
      <Detail label="Useful life" value={model.usefulLifeMonths ? `${model.usefulLifeMonths} months` : undefined} />
      <Detail label="Source system" value={model.sourceSystemId || 'Manual entry'} />
      <Detail label="Source record" value={model.sourceRecordId} />
      <Detail label="Status" value={model.status} />
      <Detail label="Revision" value={String(model.revision)} />
      <Detail label="Last updated" value={formatTimestamp(model.updatedAt)} />
    </dl>
    {specifications.length > 0 ? <>
      <h6 className="mt-4 text-sm font-semibold">Shared specifications</h6>
      <dl className="mt-2 grid grid-cols-[minmax(0,0.45fr)_minmax(0,1fr)] gap-x-4 gap-y-2 text-sm">{specifications.map(([key, value]) => <Detail key={key} label={key} value={value} />)}</dl>
    </> : <p className="mt-4 text-sm text-steward-mist-muted">No shared specifications.</p>}
  </section>
}

function TextField({ defaultValue, help, label, maxLength, name, required }: { defaultValue: string; help?: string; label: string; maxLength?: number; name: string; required?: boolean }) {
  const helpID = help ? `${name}-help` : undefined
  return <label className={labelClass}>{label}{help && <span className="mt-1 block font-normal leading-5 text-steward-mist-muted" id={helpID}>{help}</span>}<input aria-describedby={helpID} className={inputClass} defaultValue={defaultValue} maxLength={maxLength} name={name} required={required} /></label>
}

function TextAreaField({ defaultValue, label, maxLength, name }: { defaultValue: string; label: string; maxLength: number; name: string }) {
  return <label className={labelClass}>{label}<textarea className={`${inputClass} min-h-24 resize-y`} defaultValue={defaultValue} maxLength={maxLength} name={name} /></label>
}

function SelectField({ defaultValue, label, name, options }: { defaultValue: string; label: string; name: string; options: string[] }) {
  return <label className={labelClass}>{label}<select className={inputClass} defaultValue={defaultValue} name={name}>{options.map((option) => <option key={option} value={option}>{option}</option>)}</select></label>
}

function NumberField({ defaultValue, label, max, name }: { defaultValue: number; label: string; max: number; name: string }) {
  return <label className={labelClass}>{label}<input className={inputClass} defaultValue={defaultValue} max={max} min={0} name={name} type="number" /></label>
}

function ModelSelect({ defaultValue, models }: { defaultValue: string; models: AssetModel[] }) {
  const hasDefault = !defaultValue || models.some((model) => model.id === defaultValue)
  return <label className={labelClass}>Model<select className={inputClass} defaultValue={defaultValue} name="modelId"><option value="">No model</option>{!hasDefault && <option value={defaultValue}>{defaultValue} (current)</option>}{models.map((model) => <option key={model.id} value={model.id}>{modelLabel(model)}</option>)}</select></label>
}

function ReferenceSelect({ defaultValue, label, name, options }: { defaultValue: string; label: string; name: string; options: ReferenceRecord[] }) {
  const hasDefault = !defaultValue || options.some((option) => option.id === defaultValue)
  return <label className={labelClass}>{label}<select className={inputClass} defaultValue={defaultValue} name={name}><option value="">Not assigned</option>{!hasDefault && <option value={defaultValue}>{defaultValue} (current)</option>}{options.map((option) => <option key={option.id} value={option.id}>{referenceLabel(option)}</option>)}</select></label>
}

function ModelInventoryReferenceFilter({ label, onChange, options, value }: { label: string; onChange: (value: string) => void; options: ReferenceRecord[]; value: string }) {
  return <label className={labelClass}>{label}<select className={inputClass} onChange={(event) => onChange(event.target.value)} value={value}><option value="">All {label.toLowerCase()}s</option>{options.map((option) => <option key={option.id} value={option.id}>{referenceLabel(option)}</option>)}</select></label>
}

function modelInventoryGroupLabel(groupBy: string, key: string, references: ReferenceOptions) {
  if (!key) return groupBy === 'deployment' ? 'No deployment context' : 'Not assigned'
  const options = groupBy === 'site' ? references.sites : groupBy === 'department' ? references.departments : groupBy === 'user' ? references.identities : []
  const reference = options.find((option) => option.id === key)
  return reference ? referenceLabel(reference) : key
}

function Detail({ label, value }: { label: string; value: string | undefined }) {
  return <><dt className="min-w-0 break-words font-semibold text-steward-mist-muted">{label}</dt><dd className="min-w-0 break-words">{value || 'Not assigned'}</dd></>
}
