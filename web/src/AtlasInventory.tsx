import { type FormEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { calendarDateText, lifecyclePercent, modelPastLifecycle, usefulLifeMonths } from './lifecyclePlanning'
import { ApiRequestError, isRevision, requestJSON, type Revision } from './api'
import AtlasIdentifiers from './AtlasIdentifiers'
import AtlasLabelPrint from './AtlasLabelPrint'
import AtlasScanner from './AtlasScanner'
import AtlasSectionNav, { type AtlasSection } from './AtlasSectionNav'
import DocumentViewer, { type ViewableDocument } from './DocumentViewer'
import RecordSearchPicker, { type SearchableRecord } from './RecordSearchPicker'
import RecordTags from './RecordTags'
import type { AtlasAssetScope, WorkspaceRecordFocus } from './graphRecord'
import { fiscalMonthInYear, fiscalYearForDate } from './horizonPlanning'
import { ProductHeader, StatusBadge, buttonClass, dangerButtonClass, emptyStateClass, inputClass, labelClass, panelClass, plainButtonClass, secondaryButtonClass, subpanelClass } from './ui'
import DataGrid, { type StagedDraft } from './grid/DataGrid'
import Drawer from './grid/Drawer'
import QueryBuilder from './grid/QueryBuilder'
import { emptyQuery, encodeQuery, isQueryEmpty, matchQuery, parseQuery, type QueryField, type QueryModel } from './grid/queryLanguage'
import { applyCellPayload, calendarText, encodeLookupText, filterLookupOptions, lookupExportText, lookupLabel, parseLookupText, type GridColumn, type LookupConfig, type LookupCreateConfig, type LookupOption } from './grid/columns'
import type { CellEdit } from './grid/useCellEditing'
import {
  buildLabelColumns,
  createAtlasLabelDefinition,
  isLabelColumnKey,
  loadAtlasLabelAssignments,
  loadAtlasLabelDefinitions,
  saveLabelEdits,
  type LabelAssignment,
  type LabelDefinition,
  type LabelValueKind,
} from './labelsGrid'
import { buildPayload, summarizeReport, tasksFromEdits, useWriteQueue, type WriteTransport } from './grid/writeQueue'

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
  additionalUserIds?: string[]
  status: string
  purchaseDate?: string
  lifecycleStartDate?: string
  installedDate?: string
  replacementModelId?: string
  criticalityScore?: number
  attributes?: Record<string, string>
  components?: AssetComponent[]
  unitCostMinor?: number
  currency?: string
  revision: Revision
  createdAt: string
  updatedAt: string
}

export type AssetComponent = {
  id: string
  kind: string
  name: string
  modelNumber?: string
  modelId?: string
  serialNumber?: string
  quantity: number
  unitCostMinor?: number
  currency?: string
  notes?: string
}

export type AssetTemplateField = {
  key: string
  label: string
  kind: string
  required?: boolean
  options?: string[]
  help?: string
  default?: string
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
  lastEffectiveDate?: string
  replacementModelId?: string
  unitCostMinor?: number
  currency?: string
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
  templateFields?: AssetTemplateField[]
  supportUrl?: string
  warrantyMonths?: number
  usefulLifeMonths?: number
  lastEffectiveDate?: string
  replacementModelId?: string
  criticalityScore?: number
  unitCostMinor?: number
  currency?: string
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
  email?: string
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

type TemplateFieldRow = {
  key: number
  fieldKey: string
  label: string
  kind: string
  required: boolean
  options: string
  help: string
  defaultValue: string
}

type AccessoryRow = {
  key: number
  kind: string
  name: string
  modelNumber: string
  unitCost: string
}

const accessoryKinds = ['monitor', 'mouse', 'keyboard', 'combo', 'dock', 'other']
const templateFieldKinds = ['text', 'number', 'select']

type AtlasInventoryProps = {
  assets: readonly Asset[]
  assetNextCursor?: string
  assetsLoading?: boolean
  assetScope?: AtlasAssetScope | null
  csrfToken: string
  permissions: readonly string[]
  onAssetsChange: (assets: Asset[]) => void
  onClearAssetScope?: () => void
  onLoadMoreAssets?: () => Promise<void>
  onLoadAllAssets?: () => Promise<Asset[]>
  onOpenHelp?: () => void
  identity?: { subject: string; organizationId: string } | null
  focusRecord?: WorkspaceRecordFocus | null
}

const kinds = ['server', 'computer', 'desktop', 'laptop', 'tablet', 'phone', 'network', 'peripheral', 'virtual', 'other']
const statuses = ['draft', 'active', 'inactive', 'retired', 'disposed']
// Matches atlas.MaximumBulkAssets, the atomic batch size the API accepts.
const maximumBulkAssets = 100
const emptyReferences: ReferenceOptions = { sites: [], buildings: [], rooms: [], departments: [], identities: [] }
const emptyModelInventoryFilters: ModelInventoryFilters = { status: '', siteId: '', departmentId: '', userId: '', deploymentContext: '', groupBy: '' }

const modelInventoryQueryFields: QueryField[] = [
  { key: 'name', header: 'Name', kind: 'text' },
  { key: 'status', header: 'Lifecycle state', kind: 'enum', options: statuses },
  { key: 'kind', header: 'Kind', kind: 'enum', options: kinds },
  { key: 'siteId', header: 'Site', kind: 'text' },
  { key: 'departmentId', header: 'Asset department', kind: 'text' },
  { key: 'userId', header: 'Primary user (asset)', kind: 'text' },
  { key: 'deploymentContext', header: 'Deployment context', kind: 'text' },
]

function modelInventoryQueryValue(asset: Asset, field: string, references: ReferenceOptions) {
  if (field === 'name') return asset.name
  if (field === 'status') return asset.status
  if (field === 'kind') return asset.kind
  if (field === 'siteId') return `${asset.siteId ?? ''} ${modelInventoryGroupLabel('site', asset.siteId ?? '', references)}`.trim()
  if (field === 'departmentId') return `${asset.departmentId ?? ''} ${modelInventoryGroupLabel('department', asset.departmentId ?? '', references)}`.trim()
  if (field === 'userId') return `${asset.userId ?? ''} ${modelInventoryGroupLabel('user', asset.userId ?? '', references)}`.trim()
  if (field === 'deploymentContext') return [asset.hostname, asset.deploymentNotes].filter(Boolean).join(' ')
  return ''
}

function modelInventoryGroupKey(asset: Asset, groupBy: string) {
  if (groupBy === 'status') return asset.status
  if (groupBy === 'site') return asset.siteId ?? ''
  if (groupBy === 'department') return asset.departmentId ?? ''
  if (groupBy === 'user') return asset.userId ?? ''
  if (groupBy === 'deployment') return asset.hostname || asset.deploymentNotes || ''
  return ''
}

function queryErrorText(source: string) {
  const parsed = parseQuery(source, modelInventoryQueryFields)
  return parsed.ok ? '' : parsed.error
}

function applyModelInventoryQuery(inventory: ModelInventory, queryModel: QueryModel, references: ReferenceOptions): ModelInventory {
  if (isQueryEmpty(queryModel)) return inventory
  const items = inventory.items.filter((asset) => matchQuery(asset, modelInventoryQueryFields, queryModel, (row, field) => modelInventoryQueryValue(row, field, references)))
  const groupBy = inventory.groupBy
  const counts = new Map<string, number>()
  for (const asset of items) {
    const key = modelInventoryGroupKey(asset, groupBy ?? '')
    counts.set(key, (counts.get(key) ?? 0) + 1)
  }
  return {
    ...inventory,
    filteredCount: items.length,
    items,
    groups: groupBy ? [...counts.entries()].map(([key, count]) => ({ key, count })) : inventory.groups,
  }
}

export function isAsset(value: unknown): value is Asset {
  if (typeof value !== 'object' || value === null) return false
  const item = value as Record<string, unknown>
  return typeof item.id === 'string' && typeof item.organizationId === 'string'
    && typeof item.name === 'string' && typeof item.kind === 'string' && typeof item.status === 'string'
    && isRevision(item.revision) && typeof item.createdAt === 'string' && typeof item.updatedAt === 'string'
}

// Matches atlas.maximumListLimit, the largest page the API accepts.
export const assetPageLimit = 100

export type AssetPage = {
  items: Asset[]
  nextCursor: string
}

export function parseAssetPage(value: unknown): AssetPage {
  if (typeof value !== 'object' || value === null) return { items: [], nextCursor: '' }
  const body = value as Record<string, unknown>
  const nextCursor = body.nextCursor === undefined ? '' : body.nextCursor
  if (typeof nextCursor !== 'string') throw new Error('invalid asset page cursor')
  return { items: readItems(body).filter(isAsset), nextCursor }
}

export async function fetchAssetPage(params: URLSearchParams = new URLSearchParams()): Promise<AssetPage> {
  const query = new URLSearchParams(params)
  if (!query.has('limit')) query.set('limit', String(assetPageLimit))
  return parseAssetPage(await requestJSON(`/api/v1/assets?${query.toString()}`))
}

export function mergeAssets(current: readonly Asset[], added: readonly Asset[]): Asset[] {
  const byID = new Map(current.map((asset) => [asset.id, asset]))
  for (const asset of added) byID.set(asset.id, asset)
  return [...byID.values()].sort((left, right) => left.name.localeCompare(right.name, undefined, { numeric: true, sensitivity: 'base' }))
}

function readItems(value: unknown): unknown[] {
  if (typeof value !== 'object' || value === null) return []
  const items = (value as Record<string, unknown>).items
  return Array.isArray(items) ? items : []
}

function identityRecord(value: unknown): ReferenceRecord | null {
  if (typeof value !== 'object' || value === null) return null
  const item = value as Record<string, unknown>
  if (typeof item.id !== 'string') return null
  return {
    id: item.id,
    displayName: typeof item.displayName === 'string' ? item.displayName : undefined,
    name: typeof item.name === 'string' ? item.name : undefined,
    email: typeof item.email === 'string' ? item.email : undefined,
  }
}

function mergeIdentityReferences(current: readonly ReferenceRecord[], extra: readonly ReferenceRecord[]) {
  const byID = new Map(current.map((item) => [item.id, item]))
  for (const item of extra) byID.set(item.id, item)
  return [...byID.values()].sort((left, right) => referenceLabel(left).localeCompare(referenceLabel(right), undefined, { numeric: true, sensitivity: 'base' }))
}

function collectAssetUserIds(assets: readonly Asset[]) {
  const ids = new Set<string>()
  for (const asset of assets) {
    if (asset.userId) ids.add(asset.userId)
    for (const id of asset.additionalUserIds ?? []) if (id) ids.add(id)
  }
  return [...ids]
}

async function resolveIdentityReferences(ids: readonly string[]) {
  if (ids.length === 0) return [] as ReferenceRecord[]
  const resolved: ReferenceRecord[] = []
  for (let start = 0; start < ids.length; start += 100) {
    const chunk = ids.slice(start, start + 100)
    const params = new URLSearchParams({ limit: String(chunk.length) })
    for (const id of chunk) params.append('id', id)
    const response = await requestJSON(`/api/v1/identities?${params.toString()}`)
    resolved.push(...readItems(response).flatMap((item) => {
      const identity = identityRecord(item)
      return identity ? [identity] : []
    }))
  }
  return resolved
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

export function isAssetModel(value: unknown): value is AssetModel {
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
    && (item.lastEffectiveDate === undefined || typeof item.lastEffectiveDate === 'string')
    && (item.replacementModelId === undefined || typeof item.replacementModelId === 'string')
    && (item.criticalityScore === undefined || (typeof item.criticalityScore === 'number' && item.criticalityScore >= 0 && item.criticalityScore <= 5))
    && (item.specifications === undefined || (typeof item.specifications === 'object' && item.specifications !== null
      && !Array.isArray(item.specifications) && Object.values(item.specifications).every((entry) => typeof entry === 'string')))
    && (item.unitCostMinor === undefined || typeof item.unitCostMinor === 'number')
    && (item.templateFields === undefined || Array.isArray(item.templateFields))
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

function referenceExportLabel(list: ReferenceRecord[], id: string) {
  if (!id) return ''
  const match = list.find((option) => option.id === id)
  return match ? referenceLabel(match) : id
}

function roomsForBuilding(rooms: ReferenceRecord[], buildingId: string) {
  if (!buildingId) return []
  return rooms.filter((room) => room.buildingId === buildingId)
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

function formatMoney(minor?: number, currency = 'USD') {
  if (!minor) return ''
  try { return new Intl.NumberFormat(undefined, { style: 'currency', currency }).format(minor / 100) } catch { return `${currency} ${(minor / 100).toFixed(2)}` }
}

function minorFromDollars(value: string) {
  const normalized = value.trim()
  if (!normalized) return 0
  if (!/^\d+(?:\.\d{1,2})?$/.test(normalized)) return 0
  const [whole, fraction = ''] = normalized.split('.')
  return Number(whole) * 100 + Number(fraction.padEnd(2, '0'))
}

function dollarsFromMinor(minor?: number) {
  return minor ? (minor / 100).toFixed(2) : ''
}

function formatTimestamp(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.valueOf()) ? value : date.toLocaleString()
}

// Every asset write is a whole-record PUT, so the payload always starts from
// the stored record and pending cell edits are layered on top. Omitting a field
// would clear it.
export function assetPayload(asset: Asset): Record<string, unknown> {
  const payload: Record<string, unknown> = {
    name: asset.name,
    modelId: asset.modelId ?? '',
    kind: asset.kind,
    assetTag: asset.assetTag ?? '',
    serialNumber: asset.serialNumber ?? '',
    hostname: asset.hostname ?? '',
    deploymentNotes: asset.deploymentNotes ?? '',
    siteId: asset.siteId ?? '',
    buildingId: asset.buildingId ?? '',
    roomId: asset.roomId ?? '',
    departmentId: asset.departmentId ?? '',
    userId: asset.userId ?? '',
    additionalUserIds: asset.additionalUserIds ?? [],
    status: asset.status,
    unitCostMinor: asset.unitCostMinor ?? 0,
    currency: asset.currency ?? 'USD',
    attributes: asset.attributes ?? {},
    components: asset.components ?? [],
    revision: asset.revision,
  }
  if (asset.purchaseDate) payload.purchaseDate = asset.purchaseDate
  if (asset.lifecycleStartDate) payload.lifecycleStartDate = asset.lifecycleStartDate
  if (asset.installedDate) payload.installedDate = asset.installedDate
  payload.replacementModelId = asset.replacementModelId ?? ''
  return payload
}

/**
 * Directory columns hold opaque identifiers because that is what the API
 * accepts, so copy and paste round-trip exactly. The resolved name is shown
 * in the grid and carried into exports; raw IDs stay in optional columns that
 * are hidden until someone turns them on in the column chooser.
 */
function usersText(asset: Asset) {
  const primary = asset.userId ? [{ id: asset.userId, primary: true }] : []
  const extra = (asset.additionalUserIds ?? []).filter((id) => id && id !== asset.userId).map((id) => ({ id, primary: false }))
  return encodeLookupText([...primary, ...extra])
}

function identityOption(reference: ReferenceRecord): LookupOption {
  return { id: reference.id, label: referenceLabel(reference), detail: reference.email || undefined }
}

function toLookupOption(reference: ReferenceRecord): LookupOption {
  const label = referenceLabel(reference)
  return { id: reference.id, label, detail: reference.number && reference.number !== label ? reference.number : reference.id }
}

function directorySearch(path: string, filter?: (record: ReferenceRecord) => boolean) {
  return async (query: string): Promise<readonly LookupOption[]> => {
    const response = await requestJSON(path)
    const needle = query.trim().toLowerCase()
    return readItems(response).flatMap((item) => {
      if (!isReference(item)) return []
      if (filter && !filter(item)) return []
      const option = toLookupOption(item)
      if (!needle) return [option]
      if (option.label.toLowerCase().includes(needle) || option.id.toLowerCase().includes(needle) || (option.detail ?? '').toLowerCase().includes(needle)) return [option]
      return []
    })
  }
}

type AssetColumnContext = {
  references: ReferenceOptions
  models: AssetModel[]
  csrfToken: string
  canWriteDirectory: boolean
  canWriteModels: boolean
  canWriteLabels: boolean
  tagDefinitions: readonly LabelDefinition[]
  tagAssignments: ReadonlyMap<string, ReadonlyMap<string, LabelAssignment>>
  onTagDefinitionUpdated: (definition: LabelDefinition) => void
  onOpenModels: () => void
  onReferenceCreated: (kind: keyof ReferenceOptions, record: ReferenceRecord) => void
  onModelCreated: (model: AssetModel) => void
  replacementDates?: ReadonlyMap<string, string>
  fiscalYearStartMonth?: number
}

async function postDirectoryRecord(path: string, csrfToken: string, body: Record<string, unknown>): Promise<ReferenceRecord> {
  const saved = await requestJSON(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
    body: JSON.stringify({ ...body, status: 'active' }),
  })
  if (!isReference(saved)) throw new Error('invalid directory response')
  return saved
}

function directoryCreate(kind: 'site' | 'building' | 'room' | 'department', context: AssetColumnContext): LookupCreateConfig | undefined {
  if (!context.canWriteDirectory) return undefined
  const siteOptions = context.references.sites.map(toLookupOption)
  const buildingOptions = context.references.buildings.map((building) => ({
    id: building.id,
    label: `${referenceLabel(building)}${building.siteId ? ` · ${context.references.sites.find((site) => site.id === building.siteId)?.name ?? ''}` : ''}`.trim(),
  }))
  if (kind === 'site') {
    return {
      label: 'Add site',
      fields: [{ key: 'name', label: 'Site name', required: true, placeholder: 'Campus or office' }],
      submit: async (values) => {
        const saved = await postDirectoryRecord('/api/v1/sites', context.csrfToken, { name: values.name })
        context.onReferenceCreated('sites', saved)
        return toLookupOption(saved)
      },
    }
  }
  if (kind === 'building') {
    return {
      label: 'Add building',
      fields: [
        { key: 'siteId', label: 'Site', required: true, options: siteOptions, placeholder: 'Select a site' },
        { key: 'name', label: 'Building name', required: true },
      ],
      submit: async (values) => {
        const saved = await postDirectoryRecord('/api/v1/buildings', context.csrfToken, { siteId: values.siteId, name: values.name })
        context.onReferenceCreated('buildings', saved)
        return toLookupOption(saved)
      },
    }
  }
  if (kind === 'room') {
    return {
      label: 'Add room',
      fields: [
        { key: 'buildingId', label: 'Building', required: true, options: buildingOptions, placeholder: 'Select a building' },
        { key: 'number', label: 'Room number', required: true },
        { key: 'name', label: 'Room name' },
      ],
      submit: async (values) => {
        const building = context.references.buildings.find((candidate) => candidate.id === values.buildingId)
        const saved = await postDirectoryRecord('/api/v1/rooms', context.csrfToken, {
          siteId: building?.siteId ?? '', buildingId: values.buildingId, number: values.number, name: values.name,
        })
        context.onReferenceCreated('rooms', saved)
        return toLookupOption(saved)
      },
    }
  }
  return {
    label: 'Add department',
    fields: [
      { key: 'name', label: 'Department name', required: true },
      { key: 'siteId', label: 'Site', options: siteOptions, placeholder: 'No site' },
    ],
    submit: async (values) => {
      const body: Record<string, unknown> = { name: values.name }
      if (values.siteId) body.siteId = values.siteId
      const saved = await postDirectoryRecord('/api/v1/departments', context.csrfToken, body)
      context.onReferenceCreated('departments', saved)
      return toLookupOption(saved)
    },
  }
}

function referenceColumn(
  key: 'siteId' | 'buildingId' | 'roomId' | 'departmentId',
  header: string,
  kind: 'site' | 'building' | 'room' | 'department',
  list: ReferenceRecord[],
  context: AssetColumnContext,
  editable: boolean,
  width: number,
): GridColumn<Asset> {
  const path = kind === 'site' ? '/api/v1/sites' : kind === 'building' ? '/api/v1/buildings' : kind === 'room' ? '/api/v1/rooms' : '/api/v1/departments'
  return {
    key, header, kind: 'lookup', editable, width,
    lookup: {
      options: list.map(toLookupOption),
      search: directorySearch(path),
      create: directoryCreate(kind, context),
      browseHref: '#workspace-people',
      browseLabel: kind === 'department' ? 'Open departments' : kind === 'site' ? 'Open sites' : kind === 'building' ? 'Open buildings' : 'Open rooms',
    },
    text: (asset) => asset[key] ?? '',
    exportText: (asset) => referenceExportLabel(list, asset[key] ?? ''),
    display: (asset) => {
      const id = asset[key] ?? ''
      if (!id) return ''
      const match = list.find((option) => option.id === id)
      return match ? referenceLabel(match) : id
    },
  }
}

function roomReferenceColumn(context: AssetColumnContext, editable: boolean): GridColumn<Asset> {
  const { references } = context
  const allRooms = references.rooms
  const baseLookup: LookupConfig = {
    options: allRooms.map(toLookupOption),
    search: directorySearch('/api/v1/rooms'),
    create: directoryCreate('room', context),
    browseHref: '#workspace-people',
    browseLabel: 'Open rooms',
  }
  return {
    key: 'roomId',
    header: 'Room',
    kind: 'lookup',
    editable,
    width: 9,
    lookup: baseLookup,
    resolveLookup: ({ row, values }) => {
      const buildingId = values.buildingId || row?.buildingId || ''
      const rooms = roomsForBuilding(allRooms, buildingId)
      return {
        ...baseLookup,
        options: rooms.map(toLookupOption),
        search: directorySearch('/api/v1/rooms', (room) => room.buildingId === buildingId),
        create: buildingId ? directoryCreate('room', context) : undefined,
      }
    },
    text: (asset) => asset.roomId ?? '',
    exportText: (asset) => referenceExportLabel(allRooms, asset.roomId ?? ''),
    display: (asset) => {
      const id = asset.roomId ?? ''
      if (!id) return ''
      const match = allRooms.find((option) => option.id === id)
      return match ? referenceLabel(match) : id
    },
  }
}

function referenceIdColumn(key: 'siteId' | 'buildingId' | 'roomId' | 'departmentId', header: string): GridColumn<Asset> {
  return {
    key: `${key}.recordId`,
    header: `${header} ID`,
    kind: 'text',
    width: 12,
    hiddenByDefault: true,
    text: (asset) => asset[key] ?? '',
  }
}

function buildAssetColumns(
  context: AssetColumnContext,
  assetEditable: boolean,
  labelEditable: boolean,
  rowId: (asset: Asset) => string,
): GridColumn<Asset>[] {
  const { references, models } = context
  const labelColumns = context.tagDefinitions.length > 0
    ? buildLabelColumns({
      csrfToken: context.csrfToken,
      canWriteLabels: context.canWriteLabels,
      definitions: context.tagDefinitions,
      assignments: context.tagAssignments,
      onDefinitionUpdated: context.onTagDefinitionUpdated,
      rowId,
    }, labelEditable)
    : []
  return [
    { key: 'name', header: 'Asset name', kind: 'text', editable: assetEditable, required: true, maxLength: 200, width: 15, text: (asset) => asset.name },
    { key: 'assetTag', header: 'Asset tag', kind: 'text', editable: assetEditable, maxLength: 128, width: 10, text: (asset) => asset.assetTag ?? '' },
    { key: 'serialNumber', header: 'Serial number', kind: 'text', editable: assetEditable, maxLength: 255, width: 12, scannable: true, text: (asset) => asset.serialNumber ?? '' },
    { key: 'kind', header: 'Kind', kind: 'enum', options: kinds, editable: assetEditable, required: true, width: 8, text: (asset) => asset.kind },
    {
      key: 'status', header: 'Status', kind: 'enum', options: statuses, editable: assetEditable, required: true, width: 8,
      text: (asset) => asset.status,
      display: (asset) => <StatusBadge tone={asset.status === 'active' ? 'success' : asset.status === 'retired' || asset.status === 'disposed' ? 'warning' : 'neutral'}>{asset.status}</StatusBadge>,
    },
    { key: 'hostname', header: 'Hostname', kind: 'text', editable: assetEditable, maxLength: 253, width: 12, text: (asset) => asset.hostname ?? '' },
    referenceColumn('siteId', 'Site', 'site', references.sites, context, assetEditable, 10),
    referenceColumn('buildingId', 'Building', 'building', references.buildings, context, assetEditable, 10),
    roomReferenceColumn(context, assetEditable),
    referenceColumn('departmentId', 'Department', 'department', references.departments, context, assetEditable, 11),
    {
      key: 'users', header: 'Users', kind: 'lookup', editable: assetEditable, width: 16,
      lookup: {
        multiple: true,
        allowPrimary: true,
        options: references.identities.map(identityOption),
        search: async (query) => {
          const encoded = encodeURIComponent(query.trim())
          const response = await requestJSON(`/api/v1/identities?q=${encoded}&kind=person&limit=20`)
          const items = Array.isArray((response as { items?: unknown }).items) ? (response as { items: unknown[] }).items : []
          return items.flatMap((item) => {
            const identity = identityRecord(item)
            return identity ? [identityOption(identity)] : []
          })
        },
        browseHref: '#workspace-people',
        browseLabel: 'Open directory',
      },
      text: usersText,
      exportText: (asset) => lookupExportText(usersText(asset), references.identities.map(identityOption), true),
      display: (asset) => {
        const selected = parseLookupText(usersText(asset))
        if (selected.length === 0) return ''
        return selected.map((item) => {
          const label = lookupLabel(item.id, references.identities.map(identityOption))
          return item.primary ? `${label} (primary)` : label
        }).join(', ')
      },
      toPayload: (draft, text) => {
        const selected = parseLookupText(text)
        const primary = selected.find((item) => item.primary) ?? selected[0]
        draft.userId = primary?.id ?? ''
        draft.additionalUserIds = selected.filter((item) => item.id !== primary?.id).map((item) => item.id)
      },
    },
    { key: 'purchaseDate', header: 'Purchase date', kind: 'date', editable: assetEditable, width: 9, text: (asset) => calendarText(asset.purchaseDate) },
    { key: 'lifecycleStartDate', header: 'Lifecycle start', kind: 'date', editable: assetEditable, width: 9, text: (asset) => calendarText(asset.lifecycleStartDate) },
    { key: 'installedDate', header: 'Installed date', kind: 'date', editable: assetEditable, width: 9, text: (asset) => calendarText(asset.installedDate) },
    ...(context.replacementDates ? [
      {
        key: 'replacementFiscalYear',
        header: 'Replacement FY',
        kind: 'text' as const,
        width: 8,
        text: (asset: Asset) => {
          const replacement = context.replacementDates?.get(asset.id)
          if (!replacement) return ''
          return `FY${fiscalYearForDate(replacement, context.fiscalYearStartMonth ?? 1)}`
        },
      },
      {
        key: 'replacementFiscalMonth',
        header: 'Month in FY',
        kind: 'text' as const,
        width: 8,
        text: (asset: Asset) => {
          const replacement = context.replacementDates?.get(asset.id)
          if (!replacement) return ''
          return String(fiscalMonthInYear(replacement, context.fiscalYearStartMonth ?? 1))
        },
      },
    ] : []),
    {
      key: 'replacementModelId', header: 'Replacement model', kind: 'lookup', editable: assetEditable, width: 12,
      lookup: {
        options: models.map((model) => ({ id: model.id, label: modelLabel(model), detail: model.kind })),
        search: async (query) => filterLookupOptions(models.map((model) => ({ id: model.id, label: modelLabel(model), detail: model.kind })), query),
        browseLabel: 'Open models',
        onBrowse: context.onOpenModels,
      },
      text: (asset) => asset.replacementModelId ?? '',
      exportText: (asset) => {
        const model = models.find((item) => item.id === asset.replacementModelId)
        return model ? modelLabel(model) : asset.replacementModelId ?? ''
      },
      display: (asset) => {
        const model = models.find((item) => item.id === asset.replacementModelId)
        return model ? modelLabel(model) : asset.replacementModelId ?? ''
      },
    },
    { key: 'unitCostMinor', header: 'Unit cost', kind: 'money', editable: assetEditable, align: 'right' as const, width: 8, text: (asset) => dollarsFromMinor(asset.unitCostMinor) },
    { key: 'deploymentNotes', header: 'Deployment notes', kind: 'text', editable: assetEditable, maxLength: 2000, width: 16, text: (asset) => asset.deploymentNotes ?? '' },
    ...labelColumns,
    {
      key: 'modelId', header: 'Model', kind: 'lookup', editable: assetEditable, width: 12,
      lookup: {
        options: models.map((model) => ({ id: model.id, label: modelLabel(model), detail: model.kind })),
        search: async (query) => {
          const params = new URLSearchParams({ limit: '20' })
          if (query.trim()) params.set('q', query.trim())
          const response = await requestJSON(`/api/v1/asset-models?${params.toString()}`)
          return readItems(response).flatMap((item) => isAssetModel(item) ? [{ id: item.id, label: modelLabel(item), detail: item.kind }] : [])
        },
        create: context.canWriteModels ? {
          label: 'Add model',
          fields: [
            { key: 'manufacturer', label: 'Manufacturer', required: true },
            { key: 'name', label: 'Model name', required: true },
            { key: 'kind', label: 'Kind', required: true, options: kinds.map((kind) => ({ id: kind, label: kind })) },
          ],
          submit: async (values) => {
            const saved = await requestJSON('/api/v1/asset-models', {
              method: 'POST',
              headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': context.csrfToken },
              body: JSON.stringify({ manufacturer: values.manufacturer, name: values.name, kind: values.kind || 'other', status: 'active', currency: 'USD' }),
            })
            if (!isAssetModel(saved)) throw new Error('invalid model response')
            context.onModelCreated(saved)
            return { id: saved.id, label: modelLabel(saved), detail: saved.kind }
          },
        } : undefined,
        browseLabel: 'Open models',
        onBrowse: context.onOpenModels,
      },
      text: (asset) => asset.modelId ?? '',
      exportText: (asset) => {
        if (asset.modelContext) return modelContextLabel(asset.modelContext)
        const model = models.find((item) => item.id === asset.modelId)
        return model ? modelLabel(model) : asset.modelId ?? ''
      },
      display: (asset) => asset.modelContext ? modelContextLabel(asset.modelContext) : asset.modelId ?? '',
    },
    referenceIdColumn('siteId', 'Site'),
    referenceIdColumn('buildingId', 'Building'),
    referenceIdColumn('roomId', 'Room'),
    referenceIdColumn('departmentId', 'Department'),
    { key: 'modelId.recordId', header: 'Model ID', kind: 'text', width: 12, hiddenByDefault: true, text: (asset) => asset.modelId ?? '' },
    { key: 'userId.recordId', header: 'Primary user ID', kind: 'text', width: 12, hiddenByDefault: true, text: (asset) => asset.userId ?? '' },
    { key: 'revision', header: 'Revision', kind: 'number', align: 'right', width: 6, text: (asset) => String(asset.revision) },
    { key: 'updatedAt', header: 'Updated', kind: 'instant', width: 11, text: (asset) => asset.updatedAt, exportText: (asset) => formatTimestamp(asset.updatedAt), display: (asset) => formatTimestamp(asset.updatedAt) },
  ]
}

/** Reports duplicate asset tags or serial numbers, which the API rejects per organization. */
function duplicateIdentityValues(assets: readonly Asset[], edits: readonly CellEdit[]) {
  const conflicts: string[] = []
  for (const key of ['assetTag', 'serialNumber'] as const) {
    const changed = edits.filter((edit) => edit.columnKey === key && edit.text.trim().length > 0)
    if (changed.length === 0) continue
    const seen = new Map<string, string>()
    for (const asset of assets) {
      const value = (asset[key] ?? '').trim().toLowerCase()
      if (value) seen.set(value, asset.id)
    }
    for (const edit of changed) {
      const value = edit.text.trim().toLowerCase()
      const owner = seen.get(value)
      if (owner && owner !== edit.rowId) conflicts.push(edit.text.trim())
      else seen.set(value, edit.rowId)
    }
  }
  return conflicts
}

export default function AtlasInventory({
  assets, assetNextCursor = '', assetsLoading = false, assetScope = null, csrfToken, permissions, onAssetsChange,
  onClearAssetScope, onLoadMoreAssets = async () => undefined, onLoadAllAssets = async () => [...assets],
  onOpenHelp, identity, focusRecord = null,
}: AtlasInventoryProps) {
  const [editing, setEditing] = useState<Asset | null>(null)
  const [detailOpen, setDetailOpen] = useState(false)
  const [lifecycleNoteOpen, setLifecycleNoteOpen] = useState(false)
  const [lifecycleNoteCount, setLifecycleNoteCount] = useState(0)
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
  const [modelIncludeRetired, setModelIncludeRetired] = useState(false)
  const [modelReplacementId, setModelReplacementId] = useState('')
  const [modelSpecificationRows, setModelSpecificationRows] = useState<ModelSpecificationRow[]>([])
  const [templateFieldRows, setTemplateFieldRows] = useState<TemplateFieldRow[]>([])
  const [formModelId, setFormModelId] = useState('')
  const [assetReplacementId, setAssetReplacementId] = useState('')
  const [formBuildingId, setFormBuildingId] = useState('')
  const [bulkBuildingIds, setBulkBuildingIds] = useState<Record<number, string>>({})
  const [accessoryRows, setAccessoryRows] = useState<AccessoryRow[]>([])
  const [inventoryModel, setInventoryModel] = useState<AssetModel | null>(null)
  const [modelInventory, setModelInventory] = useState<ModelInventory | null>(null)
  const [modelInventoryFilters, setModelInventoryFilters] = useState<ModelInventoryFilters>(emptyModelInventoryFilters)
  const [modelInventoryQuery, setModelInventoryQuery] = useState<QueryModel>(emptyQuery)
  const [modelInventoryQueryText, setModelInventoryQueryText] = useState('')
  const [references, setReferences] = useState<ReferenceOptions>(emptyReferences)
  const [referencesLoaded, setReferencesLoaded] = useState(false)
  const [resolvedIdentities, setResolvedIdentities] = useState<ReferenceRecord[]>([])
  const requestedIdentityIds = useRef(new Set<string>())
  const [busy, setBusy] = useState('')
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')
  const [identifierRefreshVersion, setIdentifierRefreshVersion] = useState(0)
  const [activeSection, setActiveSection] = useState<AtlasSection>('assets')
  const [replacementDates, setReplacementDates] = useState<Map<string, string>>(() => new Map())
  const [tagDefinitions, setTagDefinitions] = useState<LabelDefinition[]>([])
  const [tagAssignments, setTagAssignments] = useState<Map<string, Map<string, LabelAssignment>>>(() => new Map())
  const [tagColumnFormOpen, setTagColumnFormOpen] = useState(false)
  const [tagColumnBusy, setTagColumnBusy] = useState(false)
  const errorRef = useRef<HTMLDivElement>(null)
  const lifecycleNoteResolver = useRef<((note: string | null) => void) | null>(null)
  const assetWrites = useWriteQueue()
  const nextBulkRowKey = useRef(1)
  const nextModelSpecificationKey = useRef(0)
  const nextTemplateFieldKey = useRef(0)
  const nextAccessoryKey = useRef(0)
  const modelLoadVersion = useRef(0)
  const canWrite = permissions.includes('assets.write')
  const canReadPlanning = permissions.includes('planning.read')
  const canReadDirectory = permissions.includes('directory.read')
  const canWriteDirectory = permissions.includes('directory.write')
  const canReadLabels = permissions.includes('labels.read')
  const canWriteLabels = permissions.includes('labels.write')
  const fiscalYearStartMonth = assetScope?.fiscalYearStartMonth ?? 1

  const scopedAssets = useMemo(() => {
    if (!assetScope) return [...assets]
    const allowed = new Set(assetScope.assetIds)
    return assets.filter((asset) => allowed.has(asset.id))
  }, [assetScope, assets])

  const missingScopedAssetCount = useMemo(() => {
    if (!assetScope) return 0
    const loaded = new Set(assets.map((asset) => asset.id))
    return assetScope.assetIds.filter((id) => !loaded.has(id)).length
  }, [assetScope, assets])

  const loadModels = useCallback(async (filters: { search?: string; kind?: string; includeRetired?: boolean } = {}) => {
    const version = modelLoadVersion.current + 1
    modelLoadVersion.current = version
    const query = new URLSearchParams({ limit: '100' })
    const normalizedSearch = filters.search?.trim()
    if (normalizedSearch) query.set('q', normalizedSearch)
    if (filters.kind) query.set('kind', filters.kind)
    if (filters.includeRetired) query.set('includeRetired', 'true')
    else if (filters.search?.trim()) query.set('includeRetired', 'true')
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
    if (!canReadPlanning) {
      setReplacementDates(new Map())
      return
    }
    let active = true
    requestJSON('/api/v1/horizon/plans?scenario=baseline')
      .then((value) => {
        if (!active) return
        const dates = new Map<string, string>()
        for (const item of readItems(value)) {
          if (typeof item !== 'object' || item === null) continue
          const plan = item as Record<string, unknown>
          if (typeof plan.assetId !== 'string') continue
          const replacement = typeof plan.replacementDate === 'string' && plan.replacementDate
            ? plan.replacementDate
            : typeof plan.derivedReplacementDate === 'string' ? plan.derivedReplacementDate : ''
          if (replacement) dates.set(plan.assetId, replacement)
        }
        setReplacementDates(dates)
      })
      .catch(() => {
        if (active) setReplacementDates(new Map())
      })
    return () => { active = false }
  }, [canReadPlanning])

  useEffect(() => {
    if (!canReadLabels) {
      setTagDefinitions([])
      setTagAssignments(new Map())
      return
    }
    let active = true
    void loadAtlasLabelDefinitions()
      .then(async (definitions) => {
        if (!active) return
        setTagDefinitions(definitions)
        setTagAssignments(await loadAtlasLabelAssignments(definitions))
      })
      .catch(() => {
        if (active) {
          setTagDefinitions([])
          setTagAssignments(new Map())
        }
      })
    return () => { active = false }
  }, [canReadLabels])

  const handleTagDefinitionUpdated = useCallback((definition: LabelDefinition) => {
    setTagDefinitions((current) => current.map((item) => (item.id === definition.id ? definition : item)))
  }, [])

  const handleTagAssignmentUpdated = useCallback((definitionId: string, assetId: string, assignment: LabelAssignment | null) => {
    setTagAssignments((current) => {
      const next = new Map(current)
      const byAsset = new Map(next.get(definitionId) ?? [])
      if (assignment) byAsset.set(assetId, assignment)
      else byAsset.delete(assetId)
      next.set(definitionId, byAsset)
      return next
    })
  }, [])

  useEffect(() => {
    if (!assetScope) return
    setActiveSection('assets')
    if (missingScopedAssetCount > 0 && (assetNextCursor || assetsLoading)) {
      void onLoadAllAssets()
    }
  }, [assetScope?.nonce, assetNextCursor, assetsLoading, missingScopedAssetCount, onLoadAllAssets])

  useEffect(() => {
    if (inventoryModel) document.getElementById('model-inventory-heading')?.focus()
  }, [inventoryModel])

  useEffect(() => {
    if (activeSection === 'labels' && !canWrite) setActiveSection('assets')
  }, [activeSection, canWrite])

  useEffect(() => {
    if (formOpen) {
      setFormBuildingId(editing?.buildingId ?? '')
      setAssetReplacementId(editing?.replacementModelId ?? '')
    }
  }, [formOpen, editing?.id, editing?.buildingId, editing?.replacementModelId])

  useEffect(() => {
    if (bulkModel) setBulkBuildingIds({})
  }, [bulkModel?.id])

  const directoryReferences = useMemo(
    () => ({ ...references, identities: mergeIdentityReferences(references.identities, resolvedIdentities) }),
    [references, resolvedIdentities],
  )

  const formPickerContext = useMemo((): AssetColumnContext => ({
    references: directoryReferences,
    models,
    csrfToken,
    canWriteDirectory,
    canWriteModels: canWrite,
    canWriteLabels,
    tagDefinitions,
    tagAssignments,
    onTagDefinitionUpdated: handleTagDefinitionUpdated,
    onOpenModels: () => setActiveSection('models'),
    onReferenceCreated: (kind, record) => {
      setReferences((current) => current[kind].some((item) => item.id === record.id)
        ? current
        : { ...current, [kind]: [...current[kind], record] })
    },
    onModelCreated: (model) => {
      setModels((current) => current.some((item) => item.id === model.id) ? current : [...current, model].sort((left, right) => modelLabel(left).localeCompare(modelLabel(right))))
    },
    replacementDates: canReadPlanning ? replacementDates : undefined,
    fiscalYearStartMonth,
  }), [canReadPlanning, canWrite, canWriteDirectory, canWriteLabels, csrfToken, directoryReferences, fiscalYearStartMonth, handleTagDefinitionUpdated, models, replacementDates, tagAssignments, tagDefinitions])

  const assetRowId = useCallback((asset: Asset) => asset.id, [])
  const assetColumns = useMemo(
    () => buildAssetColumns(formPickerContext, canWrite, canWriteLabels, assetRowId),
    [assetRowId, canWrite, canWriteLabels, formPickerContext],
  )

  const identityLabel = useCallback((id?: string) => {
    if (!id) return undefined
    const match = directoryReferences.identities.find((item) => item.id === id)
    return match ? referenceLabel(match) : id
  }, [directoryReferences.identities])

  const selectedFormModel = models.find((model) => model.id === formModelId)
  const selectedModelTemplateFields = selectedFormModel?.templateFields ?? []

  const loadReferences = useCallback(async () => {
    if (!canReadDirectory || referencesLoaded) return
    const paths = ['/api/v1/sites', '/api/v1/buildings', '/api/v1/rooms', '/api/v1/departments', '/api/v1/identities?limit=100']
    try {
      const responses = await Promise.all(paths.map((path) => requestJSON(path)))
      const [sites, buildings, rooms, departments, identityResponse] = responses
      const identities = readItems(identityResponse).flatMap((item) => {
        const identity = identityRecord(item)
        return identity ? [identity] : []
      })
      setReferences({
        sites: readItems(sites).filter(isReference),
        buildings: readItems(buildings).filter(isReference),
        rooms: readItems(rooms).filter(isReference),
        departments: readItems(departments).filter(isReference),
        identities,
      })
      setReferencesLoaded(true)
    } catch {
      setReferencesLoaded(true)
    }
  }, [canReadDirectory, referencesLoaded])

  useEffect(() => {
    if (!canReadDirectory) return
    const known = new Set(directoryReferences.identities.map((item) => item.id))
    const needed = collectAssetUserIds(assets).filter((id) => !known.has(id) && !requestedIdentityIds.current.has(id))
    if (needed.length === 0) return
    for (const id of needed) requestedIdentityIds.current.add(id)
    let cancelled = false
    void resolveIdentityReferences(needed)
      .then((items) => {
        if (!cancelled && items.length > 0) setResolvedIdentities((current) => mergeIdentityReferences(current, items))
      })
      .catch(() => {
        for (const id of needed) requestedIdentityIds.current.delete(id)
      })
    return () => { cancelled = true }
  }, [assets, canReadDirectory, directoryReferences.identities])

  // The grid resolves directory identifiers to names, so load references up
  // front rather than waiting for a form to open.
  useEffect(() => {
    void loadReferences()
  }, [loadReferences])

  function openCreate() {
    setEditing(null)
    setPrefillModelID('')
    setPrefillKind('')
    setFormModelId('')
    setAccessoryRows([])
    setFormOpen(true)
    setActiveSection('assets')
    setError('')
    setMessage('')
    void loadReferences()
  }

  function openCreateFromModel(model: AssetModel) {
    setPrefillModelID(model.id)
    setPrefillKind(model.kind)
    setFormModelId(model.id)
    setAccessoryRows(defaultAccessoriesForKind(model.kind))
    setEditing(null)
    setFormOpen(true)
    setActiveSection('assets')
    setError('')
    setMessage('')
    void loadReferences()
  }

  function openBulkCreateFromModel(model: AssetModel) {
    setBulkModel(model)
    setBulkRows([{ key: 0 }])
    nextBulkRowKey.current = 1
    setActiveSection('models')
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
    setFormModelId(asset.modelId ?? '')
    setAccessoryRows((asset.components ?? []).map((component, key) => ({
      key, kind: component.kind, name: component.name, modelNumber: component.modelNumber ?? '', unitCost: dollarsFromMinor(component.unitCostMinor),
    })))
    nextAccessoryKey.current = (asset.components ?? []).length
    setFormOpen(true)
    setSelected(asset)
    setActiveSection('assets')
    setError('')
    setMessage('')
    void loadReferences()
  }

  function openModelCreate() {
    setModelEditing(null)
    setModelReplacementId('')
    setModelSpecificationRows([])
    setTemplateFieldRows([])
    nextModelSpecificationKey.current = 0
    nextTemplateFieldKey.current = 0
    setModelFormOpen(true)
    setActiveSection('models')
    setError('')
    setMessage('')
  }

  function openModelEdit(model: AssetModel) {
    setModelEditing(model)
    setModelReplacementId(model.replacementModelId ?? '')
    const rows = Object.entries(model.specifications ?? {})
      .sort(([left], [right]) => left.localeCompare(right))
      .map(([name, value], key) => ({ key, name, value }))
    setModelSpecificationRows(rows)
    nextModelSpecificationKey.current = rows.length
    const fields = (model.templateFields ?? []).map((field, key) => ({
      key, fieldKey: field.key, label: field.label, kind: field.kind, required: Boolean(field.required),
      options: (field.options ?? []).join(', '), help: field.help ?? '', defaultValue: field.default ?? '',
    }))
    setTemplateFieldRows(fields)
    nextTemplateFieldKey.current = fields.length
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

  function addTemplateField() {
    if (templateFieldRows.length >= 25) return
    const key = nextTemplateFieldKey.current
    nextTemplateFieldKey.current += 1
    setTemplateFieldRows((current) => [...current, { key, fieldKey: '', label: '', kind: 'text', required: false, options: '', help: '', defaultValue: '' }])
  }

  function addAccessory() {
    if (accessoryRows.length >= 40) return
    const key = nextAccessoryKey.current
    nextAccessoryKey.current += 1
    setAccessoryRows((current) => [...current, { key, kind: 'monitor', name: '', modelNumber: '', unitCost: '' }])
  }

  function updateModelSpecification(key: number, field: 'name' | 'value', value: string) {
    setModelSpecificationRows((current) => current.map((row) => row.key === key ? { ...row, [field]: value } : row))
  }

  function removeModelSpecification(key: number) {
    setModelSpecificationRows((current) => current.filter((row) => row.key !== key))
  }

  function handleModelSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    void loadModels({ search: modelSearch, kind: modelKind, includeRetired: modelIncludeRetired || Boolean(modelSearch.trim()) })
  }

  function clearModelSearch() {
    setModelSearch('')
    setModelKind('')
    setModelIncludeRetired(false)
    void loadModels()
  }

  async function loadModelInventory(model: AssetModel, filters: ModelInventoryFilters, queryModel = modelInventoryQuery) {
    setBusy(`inventory-${model.id}`)
    setError('')
    const query = new URLSearchParams({ limit: '100' })
    const complexQuery = !isQueryEmpty(queryModel) && (queryModel.groupJoin === 'OR' || queryModel.groups.some((group) => group.join === 'OR' && group.conditions.length > 1))
    Object.entries(filters).forEach(([key, value]) => {
      if (value && (key === 'groupBy' || !complexQuery)) query.set(key, value)
    })
    try {
      const response = await requestJSON(`/api/v1/asset-models/${encodeURIComponent(model.id)}/inventory?${query.toString()}`)
      if (!isModelInventory(response) || response.modelId !== model.id) throw new Error('invalid model inventory response')
      setModelInventory(applyModelInventoryQuery(response, queryModel, directoryReferences))
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
    setModelInventoryQuery(emptyQuery())
    setModelInventoryQueryText('')
    setActiveSection('models')
    setMessage('')
    void loadReferences()
    void loadModelInventory(model, emptyModelInventoryFilters, emptyQuery())
  }

  function handleModelInventorySubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (inventoryModel) void loadModelInventory(inventoryModel, modelInventoryFilters, modelInventoryQuery)
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

  function openAssetDetail(asset: Asset) {
    setDetailOpen(true)
    void selectAsset(asset)
  }

  useEffect(() => {
    if (!focusRecord || (focusRecord.kind !== 'asset' && focusRecord.kind !== 'model')) return
    const focused = focusRecord
    let cancelled = false
    async function openFocusedRecord() {
      if (focused.kind === 'asset') {
        let asset = assets.find((item) => item.id === focused.recordId)
        if (!asset) {
          try {
            const response = await requestJSON(`/api/v1/assets/${encodeURIComponent(focused.recordId)}`)
            if (!isAsset(response)) return
            asset = response
            if (!cancelled) onAssetsChange([...assets, asset].sort((left, right) => left.name.localeCompare(right.name)))
          } catch (requestError) {
            if (!cancelled) {
              setError(requestError instanceof ApiRequestError ? requestError.message : 'The asset could not be opened.')
              queueMicrotask(() => errorRef.current?.focus())
            }
            return
          }
        }
        if (cancelled || !asset) return
        if (canWrite) openEdit(asset)
        else openAssetDetail(asset)
        queueMicrotask(() => document.getElementById(canWrite ? 'assets-heading' : 'asset-detail-heading')?.focus())
        return
      }
      let model = models.find((item) => item.id === focused.recordId)
      if (!model) {
        try {
          const response = await requestJSON(`/api/v1/asset-models/${encodeURIComponent(focused.recordId)}`)
          if (!isAssetModel(response)) return
          model = response
          if (!cancelled) setModels((current) => current.some((item) => item.id === response.id) ? current : [...current, response])
        } catch (requestError) {
          if (!cancelled) {
            setError(requestError instanceof ApiRequestError ? requestError.message : 'The asset model could not be opened.')
            queueMicrotask(() => errorRef.current?.focus())
          }
          return
        }
      }
      if (cancelled || !model) return
      openModelInventory(model)
    }
    void openFocusedRecord()
    return () => { cancelled = true }
    // Open once per Mesh/People inspector navigation, not when inventory arrays refresh.
  }, [focusRecord?.kind, focusRecord?.nonce, focusRecord?.recordId])

  function requestLifecycleNote(count: number) {
    setLifecycleNoteCount(count)
    setLifecycleNoteOpen(true)
    return new Promise<string | null>((resolve) => { lifecycleNoteResolver.current = resolve })
  }

  function resolveLifecycleNote(note: string | null) {
    setLifecycleNoteOpen(false)
    const resolver = lifecycleNoteResolver.current
    lifecycleNoteResolver.current = null
    resolver?.(note)
  }

  function reportGridError(reason: string): never {
    setError(reason)
    queueMicrotask(() => errorRef.current?.focus())
    throw new Error(reason)
  }

  async function saveAssetEdits(edits: readonly CellEdit[]) {
    setError('')
    setMessage('')
    const assetFieldEdits = edits.filter((edit) => !isLabelColumnKey(edit.columnKey))
    const duplicates = [...new Set(duplicateIdentityValues(assets, assetFieldEdits))]
    if (duplicates.length > 0) {
      reportGridError(`Asset tags and serial numbers must stay unique for the organization. Already in use: ${duplicates.join(', ')}.`)
    }
    // A status change must carry a lifecycle note, so ask once for the batch
    // instead of interrupting every row.
    const statusEdits = assetFieldEdits.filter((edit) => edit.columnKey === 'status')
    let lifecycleNote = ''
    if (statusEdits.length > 0) {
      const provided = await requestLifecycleNote(statusEdits.length)
      if (provided === null) throw new Error('Lifecycle note was not provided.')
      lifecycleNote = provided
    }
    const stored = new Map(assets.map((asset) => [asset.id, asset]))
    const saved = new Map<string, Asset>()
    const transport: WriteTransport = {
      concurrency: 4,
      writeRecord: async (task) => {
        const asset = stored.get(task.rowId)
        if (!asset) throw new Error('The asset is no longer loaded.')
        const rowAssetEdits = task.edits.filter((edit) => !isLabelColumnKey(edit.columnKey))
        const rowLabelEdits = task.edits.filter((edit) => isLabelColumnKey(edit.columnKey))
        if (rowAssetEdits.length > 0) {
          const payload = buildPayload(rowAssetEdits, assetColumns, assetPayload(asset))
          if (rowAssetEdits.some((edit) => edit.columnKey === 'status')) payload.lifecycleNote = lifecycleNote
          const response = await requestJSON(`/api/v1/assets/${encodeURIComponent(asset.id)}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
            body: JSON.stringify(payload),
          })
          if (!isAsset(response)) throw new Error('invalid asset response')
          saved.set(response.id, response)
        }
        if (rowLabelEdits.length > 0) {
          await saveLabelEdits(
            rowLabelEdits,
            tagDefinitions,
            tagAssignments,
            csrfToken,
            canWriteLabels,
            handleTagDefinitionUpdated,
            handleTagAssignmentUpdated,
          )
        }
      },
    }
    const tasks = tasksFromEdits(edits)
    const report = await assetWrites.run(tasks, transport)
    if (saved.size > 0) {
      onAssetsChange(assets.map((asset) => saved.get(asset.id) ?? asset).sort((left, right) => left.name.localeCompare(right.name)))
    }
    if (report.failed > 0) reportGridError(summarizeReport(report))
    setMessage(summarizeReport(report))
  }

  async function handleCreateTagColumn(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!canWriteLabels) return
    const form = event.currentTarget
    const values = new FormData(form)
    const name = String(values.get('name') ?? '').trim()
    const id = String(values.get('id') ?? '').trim()
    const valueKind = String(values.get('valueKind') ?? 'multiselect') as LabelValueKind
    const options = String(values.get('options') ?? '').split(',').map((option) => option.trim()).filter(Boolean)
    if (!name) {
      setError('Tag name is required.')
      return
    }
    if ((valueKind === 'select' || valueKind === 'multiselect') && options.length === 0) {
      setError('Add at least one allowed value for select tags.')
      return
    }
    setTagColumnBusy(true)
    setError('')
    setMessage('')
    try {
      const created = await createAtlasLabelDefinition({ id: id || undefined, name, valueKind, options }, csrfToken)
      setTagDefinitions((current) => [...current, created].sort((left, right) => left.name.localeCompare(right.name)))
      setTagAssignments((current) => {
        const next = new Map(current)
        next.set(created.id, new Map())
        return next
      })
      setTagColumnFormOpen(false)
      form.reset()
      setMessage(`Added tag column “${created.name}”.`)
    } catch (requestError) {
      setError(requestError instanceof ApiRequestError ? requestError.message : 'The tag column could not be created.')
      queueMicrotask(() => errorRef.current?.focus())
    } finally {
      setTagColumnBusy(false)
    }
  }

  async function createStagedAssets(drafts: readonly StagedDraft[]) {
    setError('')
    setMessage('')
    const items = new Map(drafts.map((draft) => {
      const item: Record<string, unknown> = {}
      for (const [key, value] of Object.entries(draft.values)) {
        const column = assetColumns.find((candidate) => candidate.key === key)
        if (column) applyCellPayload(column, item, value)
      }
      return [draft.id, item] as const
    }))
    const incomplete = [...items.values()].filter((item) => !String(item.name ?? '').trim() || !String(item.kind ?? '').trim())
    if (incomplete.length > 0) reportGridError('Every new row needs an asset name and a kind before it can be created.')

    // A batch sharing one model can be created atomically. Mixed models fall
    // back to one request per row through the same queue.
    const modelIds = new Set([...items.values()].map((item) => String(item.modelId ?? '').trim()))
    const sharedModel = modelIds.size === 1 ? [...modelIds][0] : ''
    if (sharedModel && items.size <= maximumBulkAssets) {
      const response = await requestJSON(`/api/v1/asset-models/${encodeURIComponent(sharedModel)}/assets/bulk`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({ items: [...items.values()] }),
      })
      const created = readItems(response).filter(isAsset)
      if (created.length !== items.size) reportGridError('The asset batch could not be created.')
      onAssetsChange([...assets, ...created].sort((left, right) => left.name.localeCompare(right.name)))
      setMessage(`${created.length} ${created.length === 1 ? 'asset' : 'assets'} created.`)
      void loadModels({ search: modelSearch, kind: modelKind })
      return
    }

    const created: Asset[] = []
    const transport: WriteTransport = {
      concurrency: 4,
      writeRecord: async (task) => {
        const response = await requestJSON('/api/v1/assets', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
          body: JSON.stringify(items.get(task.rowId) ?? {}),
        })
        if (!isAsset(response)) throw new Error('invalid asset response')
        created.push(response)
      },
    }
    const report = await assetWrites.run(drafts.map((draft) => ({ rowId: draft.id, edits: [] })), transport)
    if (created.length > 0) onAssetsChange([...assets, ...created].sort((left, right) => left.name.localeCompare(right.name)))
    if (report.failed > 0) reportGridError(summarizeReport(report))
    setMessage(`${created.length} ${created.length === 1 ? 'asset' : 'assets'} created.`)
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
    queueMicrotask(() => document.getElementById('asset-detail-heading')?.focus())
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError('')
    setMessage('')
    const form = event.currentTarget
    const values = new FormData(form)
    const purchaseDate = String(values.get('purchaseDate') ?? '')
    const lifecycleStartDate = String(values.get('lifecycleStartDate') ?? '')
    const installedDate = String(values.get('installedDate') ?? '')
    const status = String(values.get('status') ?? '')
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
      status,
      unitCostMinor: minorFromDollars(String(values.get('unitCost') ?? '')),
      currency: 'USD',
      attributes: selectedModelTemplateFields.reduce<Record<string, string>>((attributes, field) => {
        const value = String(values.get(`attr-${field.key}`) ?? '').trim()
        if (value) attributes[field.key] = value
        return attributes
      }, {}),
      components: accessoryRows.filter((row) => row.name.trim()).map((row, index) => ({
        id: `component-${index + 1}`, kind: row.kind, name: row.name.trim(), modelNumber: row.modelNumber.trim(),
        quantity: 1, unitCostMinor: minorFromDollars(row.unitCost), currency: 'USD',
      })),
    }
    if (purchaseDate) payload.purchaseDate = `${purchaseDate}T00:00:00Z`
    if (lifecycleStartDate) payload.lifecycleStartDate = `${lifecycleStartDate}T00:00:00Z`
    if (installedDate) payload.installedDate = `${installedDate}T00:00:00Z`
    payload.replacementModelId = assetReplacementId
    if (editing) {
      payload.revision = editing.revision
      if (status !== editing.status) {
        payload.lifecycleNote = String(values.get('lifecycleNote') ?? '')
      }
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
      lastEffectiveDate: String(values.get('lastEffectiveDate') ?? '').trim() ? `${String(values.get('lastEffectiveDate'))}T00:00:00Z` : undefined,
      replacementModelId: modelReplacementId,
      unitCostMinor: minorFromDollars(String(values.get('modelUnitCost') ?? '')),
      currency: 'USD',
      templateFields: templateFieldRows.filter((row) => row.fieldKey.trim() && row.label.trim()).map((row) => ({
        key: row.fieldKey.trim(), label: row.label.trim(), kind: row.kind, required: row.required,
        options: row.options.split(',').map((item) => item.trim()).filter(Boolean), help: row.help.trim(), default: row.defaultValue.trim(),
      })),
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
      setTemplateFieldRows([])
      setModelFormOpen(false)
      setModelReplacementId('')
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
      setModels((current) => modelIncludeRetired || modelSearch.trim()
        ? current.map((item) => item.id === retired.id ? retired : item)
        : current.filter((item) => item.id !== retired.id))
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

  async function reactivateModel(model: AssetModel) {
    setError('')
    setMessage('')
    setBusy(`reactivate-model-${model.id}`)
    try {
      const reactivated = await requestJSON(`/api/v1/asset-models/${encodeURIComponent(model.id)}/reactivate?revision=${model.revision}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken },
      })
      if (!isAssetModel(reactivated)) throw new Error('invalid model response')
      setModels((current) => {
        const next = current.map((item) => item.id === reactivated.id ? reactivated : item)
        return next.some((item) => item.id === reactivated.id) ? next : [...next, reactivated].sort((left, right) => modelLabel(left).localeCompare(modelLabel(right)))
      })
      setInventoryModel((current) => current?.id === reactivated.id ? reactivated : current)
      setMessage('Model reactivated.')
    } catch (requestError) {
      setError(requestError instanceof ApiRequestError ? requestError.message : 'The model could not be reactivated.')
      queueMicrotask(() => errorRef.current?.focus())
    } finally {
      setBusy('')
    }
  }

  return (
    <section aria-label="Atlas inventory workflow" className={`${panelClass} space-y-5 p-4 sm:p-5`} data-feature="inventory.assets" data-requirement="REQ-ATLAS-001">
      <ProductHeader
        actions={<>
          {onOpenHelp && <button className={secondaryButtonClass} onClick={onOpenHelp} type="button">Atlas help</button>}
          <a className={plainButtonClass} href="#workspace-mesh">Open Mesh graph</a>
          {canWrite && <button className={buttonClass} onClick={openCreate} type="button">Add asset</button>}
        </>}
        description="Search and inspect organization-owned assets, then use Scan, Labels, or Models when you need codes or shared product defaults."
        headingId="assets-heading"
        kicker="Organization asset registry"
        title="Atlas — Asset inventory"
      />

      {error && <div className="rounded-lg border border-red-400/50 bg-red-950/50 p-3 text-sm" ref={errorRef} role="alert" tabIndex={-1}>{error}</div>}
      {message && <p className="rounded-lg border border-steward-green/40 bg-steward-green/10 p-3 text-sm" role="status">{message}</p>}

      <AtlasSectionNav active={activeSection} canWrite={canWrite} onChange={setActiveSection} />

      <div aria-labelledby="atlas-tab-assets" hidden={activeSection !== 'assets'} id="atlas-panel-assets" role="tabpanel">
      <p className="text-sm leading-6 text-steward-mist-muted">
        {canWrite || canWriteLabels
          ? `Edit cells directly: type to replace, Enter or Tab to commit, Escape to revert. Ctrl+C and Ctrl+V move a block to and from a spreadsheet, Ctrl+D fills down, and Ctrl+Z undoes.${canWrite ? ' Use the + on a row to insert a new asset below it.' : ''}${canReadLabels ? ' Tag columns use configured labels; add values from the cell picker when you have tag write access.' : ''}`
          : 'Sort, filter, and copy asset records. Editing requires asset or tag write access.'}
      </p>
      {assetScope && <div className="mt-4 flex flex-wrap items-center justify-between gap-3 rounded-xl border border-steward-teal/35 bg-steward-teal/10 px-4 py-3">
        <p className="text-sm leading-6 text-steward-mist">Showing {scopedAssets.length} of {assetScope.assetIds.length} assets from Horizon: <strong>{assetScope.label}</strong>{missingScopedAssetCount > 0 && assetsLoading ? ' · loading remaining assets…' : missingScopedAssetCount > 0 ? ` · ${missingScopedAssetCount} assets not loaded yet` : ''}</p>
        {onClearAssetScope && <button className={secondaryButtonClass} onClick={onClearAssetScope} type="button">Show all assets</button>}
      </div>}
      <div className="mt-4">
        <DataGrid
          columns={assetColumns}
          editable={canWrite || canWriteLabels}
          emptyMessage={assetScope ? 'No scoped assets are loaded in Atlas yet.' : 'No assets match these filters.'}
          exportAllRows={async () => (assetScope ? scopedAssets : assetNextCursor ? onLoadAllAssets() : [...assets])}
          label={assetScope ? `Horizon scope · ${assetScope.label}` : 'Asset inventory'}
          identity={identity}
          viewDefaults={assetScope ? { filters: {} } : { filters: { status: 'active' } }}
          viewId={assetScope ? `atlas-assets-scope-${assetScope.nonce}` : 'atlas-assets'}
          onCreateRows={canWrite && !assetScope ? createStagedAssets : undefined}
          onEditRow={canWrite ? openEdit : undefined}
          onOpenRow={openAssetDetail}
          onSaveEdits={canWrite || canWriteLabels ? saveAssetEdits : undefined}
          rowId={(asset) => asset.id}
          rowLabel={(asset) => asset.name}
          rowMessage={(asset) => assetWrites.rowMessage(asset.id)}
          rowState={(asset) => assetWrites.rowState(asset.id)}
          rows={scopedAssets}
          selectable
          toolbar={!assetScope && (canWrite || canWriteLabels) ? <div className="flex flex-wrap items-center gap-2">
            {canWrite && <button className={plainButtonClass + ' min-h-8 px-2 py-1 text-xs'} onClick={openCreate} type="button">Full form</button>}
            {canWriteLabels && <button className={plainButtonClass + ' min-h-8 px-2 py-1 text-xs'} onClick={() => setTagColumnFormOpen(true)} type="button">Add tag column</button>}
            {canReadLabels && <a className={plainButtonClass + ' min-h-8 px-2 py-1 text-xs'} href="#workspace-threads">Manage tags</a>}
          </div> : undefined}
        />
        {!assetScope && <div className="mt-3 flex flex-wrap items-center justify-between gap-3 rounded-xl border border-white/10 bg-white/[0.025] px-4 py-3">
          <p className="text-sm text-steward-mist-muted" role="status">
            {assetsLoading
              ? 'Loading assets…'
              : assetNextCursor
                ? `Showing ${assets.length} assets. More records are available in Atlas.`
                : assets.length > 0
                  ? `Showing all ${assets.length} loaded assets.`
                  : 'No assets loaded yet.'}
          </p>
          {assetNextCursor && <div className="flex flex-wrap gap-2">
            <button className={secondaryButtonClass} disabled={assetsLoading} onClick={() => void onLoadMoreAssets()} type="button">
              {assetsLoading ? 'Loading…' : 'Load more'}
            </button>
            <button className={buttonClass} disabled={assetsLoading} onClick={() => void onLoadAllAssets()} type="button">
              {assetsLoading ? 'Loading…' : 'Load all'}
            </button>
          </div>}
        </div>}
      </div>
      </div>

      <Drawer
        description={selected ? undefined : 'Choose an asset to inspect its current record and lifecycle.'}
        kicker="Atlas"
        onClose={() => setDetailOpen(false)}
        open={detailOpen}
        title={selected ? selected.name : 'Asset details'}
        wide
      >
        <AssetDetailPanel busy={busy} canWrite={canWrite} csrfToken={csrfToken} emptyPrompt="Choose an asset to inspect its current record and lifecycle." identifierRefreshVersion={identifierRefreshVersion} identityLabel={identityLabel} lifecycle={lifecycle} models={models} onIdentifierChanged={() => setIdentifierRefreshVersion((current) => current + 1)} permissions={permissions} references={directoryReferences} selected={selected} />
        {canWrite && selected && <button className={`${secondaryButtonClass} mt-4`} onClick={() => { setDetailOpen(false); openEdit(selected) }} type="button">Edit in full form</button>}
      </Drawer>

      <Drawer
        description={`${lifecycleNoteCount} ${lifecycleNoteCount === 1 ? 'asset changes' : 'assets change'} status. Atlas stores the note with each lifecycle event.`}
        kicker="Lifecycle"
        onClose={() => resolveLifecycleNote(null)}
        open={lifecycleNoteOpen}
        title="Add a lifecycle note"
      >
        <form
          aria-label="Lifecycle note"
          onSubmit={(event) => {
            event.preventDefault()
            resolveLifecycleNote(String(new FormData(event.currentTarget).get('lifecycleNote') ?? ''))
          }}
        >
          <label className={labelClass} htmlFor="atlas-batch-lifecycle-note">Lifecycle note</label>
          <textarea className={`${inputClass} min-h-24`} id="atlas-batch-lifecycle-note" maxLength={1000} name="lifecycleNote" required />
          <div className="mt-4 flex flex-wrap gap-3">
            <button className={buttonClass} type="submit">Save status changes</button>
            <button className={secondaryButtonClass} onClick={() => resolveLifecycleNote(null)} type="button">Cancel</button>
          </div>
        </form>
      </Drawer>

      <Drawer
        kicker="Atlas tags"
        onClose={() => setTagColumnFormOpen(false)}
        open={tagColumnFormOpen && canWriteLabels}
        title="Add tag column"
      >
        <form aria-label="Add tag column" className="grid gap-4" onSubmit={handleCreateTagColumn}>
          <p className="text-sm text-steward-mist-muted">Create a configurable tag column for the asset spreadsheet. Use multi-select for tags like deployment groups with several allowed values.</p>
          <TextField label="Tag name" name="name" required />
          <TextField help="Optional stable id such as deployment-group." label="Tag id" maxLength={64} name="id" />
          <SelectField defaultValue="multiselect" label="Value type" name="valueKind" options={['flag', 'text', 'select', 'multiselect']} />
          <TextField help="Required for select and multi-select tags. Comma separated." label="Allowed values" name="options" placeholder="Lab A rollout, Office refresh, Summer 2026" />
          <div className="flex flex-wrap gap-3">
            <button className={buttonClass} disabled={tagColumnBusy} type="submit">{tagColumnBusy ? 'Creating…' : 'Create tag column'}</button>
            <button className={secondaryButtonClass} onClick={() => setTagColumnFormOpen(false)} type="button">Cancel</button>
          </div>
        </form>
      </Drawer>

      <Drawer
        kicker="Atlas"
        onClose={() => { setFormOpen(false); setEditing(null) }}
        open={formOpen && canWrite}
        title={editing ? `Edit ${editing.name}` : 'Register an asset'}
        wide
      >
        <form aria-label={editing ? 'Edit asset' : 'Add asset'} key={editing?.id ?? 'new'} onSubmit={handleSubmit}>
          {error && formOpen && <div className="mb-4 rounded-lg border border-red-400/50 bg-red-950/50 p-3 text-sm" role="alert">{error}</div>}
          {message && formOpen && <p className="mb-4 rounded-lg border border-steward-green/40 bg-steward-green/10 p-3 text-sm" role="status">{message}</p>}
          <div className="grid gap-4 md:grid-cols-2">
            <TextField defaultValue={assetValue(editing, 'name')} label="Asset name" name="name" required />
            <ModelSelect
              canWrite={canWrite}
              csrfToken={csrfToken}
              models={models}
              onChange={(value) => { setFormModelId(value); const model = models.find((item) => item.id === value); if (model && accessoryRows.length === 0) setAccessoryRows(defaultAccessoriesForKind(model.kind)) }}
              onCreated={(model) => setModels((current) => current.some((item) => item.id === model.id) ? current : [...current, model])}
              onOpenModels={() => { setFormOpen(false); setActiveSection('models') }}
              value={formModelId || prefillModelID}
            />
            <SelectField defaultValue={assetValue(editing, 'kind') || prefillKind || selectedFormModel?.kind || 'server'} label="Kind" name="kind" options={kinds} />
            <SelectField defaultValue={assetValue(editing, 'status') || 'draft'} label="Status" name="status" options={statuses} />
            <TextField defaultValue={assetValue(editing, 'assetTag')} label="Asset tag" maxLength={128} name="assetTag" />
            <TextField defaultValue={assetValue(editing, 'serialNumber')} label="Serial number" maxLength={255} name="serialNumber" />
            <TextField defaultValue={assetValue(editing, 'hostname')} label="Hostname" maxLength={253} name="hostname" />
            <TextAreaField defaultValue={assetValue(editing, 'deploymentNotes')} label="Deployment notes" maxLength={2000} name="deploymentNotes" />
            <label className={labelClass}>Purchase date<input className={inputClass} defaultValue={assetValue(editing, 'purchaseDate').slice(0, 10)} name="purchaseDate" type="date" /></label>
            <label className={labelClass}>Lifecycle start date<input className={inputClass} defaultValue={assetValue(editing, 'lifecycleStartDate').slice(0, 10)} name="lifecycleStartDate" type="date" /></label>
            <label className={labelClass}>Installed date<input className={inputClass} defaultValue={assetValue(editing, 'installedDate').slice(0, 10)} name="installedDate" type="date" /></label>
            <div className="md:col-span-2">
              <RecordSearchPicker
                help="Optional override. When empty, Atlas inherits the linked model's replacement lineage for Horizon planning."
                kind="model"
                label="Replacement model"
                multiple={false}
                onChange={(records) => setAssetReplacementId(records[0]?.id ?? '')}
                options={models.map((item) => ({ id: item.id, label: modelLabel(item), detail: item.status === 'retired' ? `${item.kind} · retired` : item.kind }))}
                selected={assetReplacementId ? [{ id: assetReplacementId, label: models.find((item) => item.id === assetReplacementId) ? modelLabel(models.find((item) => item.id === assetReplacementId) as AssetModel) : assetReplacementId }] : []}
              />
            </div>
            <TextField defaultValue={dollarsFromMinor(editing?.unitCostMinor ?? selectedFormModel?.unitCostMinor)} help="Copied from the model when left empty on create." label="Unit cost" name="unitCost" />
            <FormReferencePicker browseLabel="Open sites" create={canWriteDirectory ? directoryCreate('site', formPickerContext) : undefined} defaultValue={assetValue(editing, 'siteId')} kind="site" label="Site" name="siteId" options={references.sites} />
            <FormReferencePicker browseLabel="Open buildings" create={canWriteDirectory ? directoryCreate('building', formPickerContext) : undefined} defaultValue={assetValue(editing, 'buildingId')} kind="building" label="Building" name="buildingId" onSelectedChange={(records) => setFormBuildingId(records[0]?.id ?? '')} options={references.buildings} />
            <FormReferencePicker browseLabel="Open rooms" create={canWriteDirectory ? directoryCreate('room', formPickerContext) : undefined} defaultValue={formBuildingId === assetValue(editing, 'buildingId') ? assetValue(editing, 'roomId') : ''} help={formBuildingId ? undefined : 'Select a building to choose a room.'} key={`form-room-${formBuildingId}`} kind="room" label="Room" name="roomId" options={roomsForBuilding(references.rooms, formBuildingId)} />
            <FormReferencePicker browseLabel="Open departments" create={canWriteDirectory ? directoryCreate('department', formPickerContext) : undefined} defaultValue={assetValue(editing, 'departmentId')} kind="department" label="Department" name="departmentId" options={references.departments} />
            <FormReferencePicker browseHref="#workspace-people" browseLabel="Open directory" defaultValue={assetValue(editing, 'userId')} kind="identity" label="Primary user" name="userId" options={directoryReferences.identities} />
            {editing && <TextField defaultValue="" help="Required only when changing status; stored with lifecycle history." label="Lifecycle note" maxLength={1000} name="lifecycleNote" />}
          </div>
          {selectedModelTemplateFields.length > 0 && (
            <fieldset className={`${subpanelClass} mt-5 p-4`}>
              <legend className="px-1 font-semibold">Model template fields</legend>
              <p className="mt-1 text-sm text-steward-mist-muted">These fields come from the selected model so each asset captures the same intake details.</p>
              <div className="mt-4 grid gap-4 md:grid-cols-2 lg:grid-cols-3">
                {selectedModelTemplateFields.map((field) => (
                  <label className={labelClass} key={field.key}>{field.label}
                    {field.help && <span className="mt-1 block font-normal text-xs text-steward-mist-muted">{field.help}</span>}
                    {field.kind === 'select'
                      ? <select className={inputClass} defaultValue={editing?.attributes?.[field.key] || field.default || ''} name={`attr-${field.key}`} required={field.required}><option value="">Select</option>{(field.options ?? []).map((option) => <option key={option} value={option}>{option}</option>)}</select>
                      : <input className={inputClass} defaultValue={editing?.attributes?.[field.key] || field.default || ''} name={`attr-${field.key}`} required={field.required} type={field.kind === 'number' ? 'number' : 'text'} />}
                  </label>
                ))}
              </div>
            </fieldset>
          )}
          <fieldset className={`${subpanelClass} mt-5 p-4`}>
            <legend className="px-1 font-semibold">Related accessories</legend>
            <p className="mt-1 text-sm text-steward-mist-muted">Monitors, mice, keyboard combos, and docks can stay as model numbers related to this asset instead of becoming their own assets.</p>
            {accessoryRows.length === 0 ? <p className="mt-3 text-sm text-steward-mist-muted">No accessories yet.</p> : <div className="mt-4 space-y-3">{accessoryRows.map((row, index) => (
              <fieldset className="grid gap-3 rounded-lg border border-white/10 p-3 md:grid-cols-4" key={row.key}>
                <legend className="sr-only">Accessory {index + 1}</legend>
                <label className={labelClass}>Accessory kind<select className={inputClass} onChange={(event) => setAccessoryRows((current) => current.map((item) => item.key === row.key ? { ...item, kind: event.target.value } : item))} value={row.kind}>{accessoryKinds.map((kindValue) => <option key={kindValue} value={kindValue}>{kindValue}</option>)}</select></label>
                <label className={labelClass}>Name<input className={inputClass} onChange={(event) => setAccessoryRows((current) => current.map((item) => item.key === row.key ? { ...item, name: event.target.value } : item))} value={row.name} /></label>
                <label className={labelClass}>Model number<input className={inputClass} onChange={(event) => setAccessoryRows((current) => current.map((item) => item.key === row.key ? { ...item, modelNumber: event.target.value } : item))} value={row.modelNumber} /></label>
                <div className="flex items-end gap-2">
                  <label className={`${labelClass} min-w-0 flex-1`}>Unit cost<input className={inputClass} onChange={(event) => setAccessoryRows((current) => current.map((item) => item.key === row.key ? { ...item, unitCost: event.target.value } : item))} value={row.unitCost} /></label>
                  <button className={plainButtonClass} onClick={() => setAccessoryRows((current) => current.filter((item) => item.key !== row.key))} type="button">Remove</button>
                </div>
              </fieldset>
            ))}</div>}
            <button className={`${secondaryButtonClass} mt-4`} onClick={addAccessory} type="button">Add accessory</button>
          </fieldset>
          {!canReadDirectory && <p className="mt-4 text-sm text-steward-mist-muted">Directory references require directory read access. You can still maintain core asset identity fields.</p>}
          <button className={`${buttonClass} mt-5`} disabled={busy === 'save'} type="submit">{busy === 'save' ? 'Saving…' : editing ? 'Save changes' : 'Create asset'}</button>
        </form>
      </Drawer>

      <div aria-labelledby="atlas-tab-scan" hidden={activeSection !== 'scan'} id="atlas-panel-scan" role="tabpanel">
        <div className="grid gap-6 lg:grid-cols-[minmax(0,1.15fr)_minmax(18rem,0.85fr)]">
          <div>
            <p className="mb-4 text-sm text-steward-mist-muted">{selected ? 'Scan another code to switch assets, or associate a code with the record shown here.' : 'Scan to find an asset. The matching record opens here so you can inspect it without leaving Scan.'}</p>
            <AtlasScanner active={activeSection === 'scan'} canWrite={canWrite} csrfToken={csrfToken} onAssociated={() => setIdentifierRefreshVersion((current) => current + 1)} onResolveAsset={resolveScannedAsset} selectedAsset={selected ? { id: selected.id, name: selected.name } : null} />
          </div>
          {activeSection === 'scan' && (
            <AssetDetailPanel busy={busy} canWrite={canWrite} csrfToken={csrfToken} emptyPrompt="Scan a code to open the matching asset here." identifierRefreshVersion={identifierRefreshVersion} identityLabel={identityLabel} lifecycle={lifecycle} models={models} onIdentifierChanged={() => setIdentifierRefreshVersion((current) => current + 1)} permissions={permissions} references={directoryReferences} selected={selected} />
          )}
        </div>
      </div>

      {canWrite && (
        <div aria-labelledby="atlas-tab-labels" hidden={activeSection !== 'labels'} id="atlas-panel-labels" role="tabpanel">
          {assets.length === 0
            ? <p className={emptyStateClass}>No assets are loaded for label printing. Register an asset under Assets first.</p>
            : <AtlasLabelPrint assets={assets} csrfToken={csrfToken} key={`label-print-${identifierRefreshVersion}-${assets.map((asset) => asset.id).join(',')}`} />}
        </div>
      )}

      <div aria-labelledby="atlas-tab-models" hidden={activeSection !== 'models'} id="atlas-panel-models" role="tabpanel">
      <section aria-labelledby="models-heading" className="overflow-hidden" data-feature="inventory.models" data-requirement="REQ-ATLAS-MODELS-001">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <div className="flex flex-wrap items-center gap-2"><h3 className="text-lg font-semibold" id="models-heading">Model catalog</h3><StatusBadge tone="info">{models.length} model{models.length === 1 ? '' : 's'} shown</StatusBadge></div>
            <p className="mt-1 text-sm text-steward-mist-muted">Shared manufacturer and model defaults for repeated assets.</p>
          </div>
          {canWrite && <button className={secondaryButtonClass} onClick={openModelCreate} type="button">Add model</button>}
        </div>
        <form aria-label="Search models" className="mt-5 grid gap-4 sm:grid-cols-[minmax(0,1fr)_minmax(10rem,0.45fr)_minmax(10rem,0.45fr)_auto]" onSubmit={handleModelSearch} role="search">
          <label className={labelClass}>Search models<input className={inputClass} maxLength={200} onChange={(event) => setModelSearch(event.target.value)} placeholder="Manufacturer, model, number, or vendor ID" type="search" value={modelSearch} /></label>
          <label className={labelClass}>Model kind<select className={inputClass} onChange={(event) => setModelKind(event.target.value)} value={modelKind}><option value="">All kinds</option>{kinds.map((value) => <option key={value} value={value}>{value}</option>)}</select></label>
          <label className={`${labelClass} flex items-end gap-2 pb-2`}><input checked={modelIncludeRetired} onChange={(event) => setModelIncludeRetired(event.target.checked)} type="checkbox" /><span>Include retired</span></label>
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
              <label className={labelClass}>Last effective date<input className={inputClass} defaultValue={modelEditing?.lastEffectiveDate?.slice(0, 10) ?? ''} name="lastEffectiveDate" type="date" /></label>
              <TextField defaultValue={dollarsFromMinor(modelEditing?.unitCostMinor)} help="Default purchase cost copied onto new assets." label="Unit cost" name="modelUnitCost" />
            </div>
            <div className="mt-4">
              <RecordSearchPicker
                help="Horizon uses this successor when assets linked to this model do not specify their own replacement."
                kind="model"
                label="Replacement model (lineage)"
                multiple={false}
                onChange={(records) => setModelReplacementId(records[0]?.id ?? '')}
                options={models.filter((item) => item.id !== modelEditing?.id).map((item) => ({ id: item.id, label: modelLabel(item), detail: item.status === 'retired' ? `${item.kind} · retired` : item.kind }))}
                selected={modelReplacementId ? [{ id: modelReplacementId, label: models.find((item) => item.id === modelReplacementId) ? modelLabel(models.find((item) => item.id === modelReplacementId) as AssetModel) : modelReplacementId }] : []}
              />
            </div>
            <div className="mt-4 grid gap-4 md:grid-cols-2 lg:grid-cols-3">
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
            <fieldset className={`${subpanelClass} mt-5 p-4`}>
              <legend className="px-1 font-semibold">Asset template fields</legend>
              <p className="mt-1 text-sm text-steward-mist-muted">These fields appear when someone adds an asset from this model.</p>
              {templateFieldRows.length === 0 ? <p className="mt-3 text-sm text-steward-mist-muted">No template fields.</p> : <div className="mt-4 space-y-3">{templateFieldRows.map((row, index) => (
                <fieldset className="grid gap-3 rounded-lg border border-white/10 p-3 md:grid-cols-2" key={row.key}>
                  <legend className="sr-only">Template field {index + 1}</legend>
                  <label className={labelClass}>Field key<input className={inputClass} onChange={(event) => setTemplateFieldRows((current) => current.map((item) => item.key === row.key ? { ...item, fieldKey: event.target.value } : item))} required value={row.fieldKey} /></label>
                  <label className={labelClass}>Label<input className={inputClass} onChange={(event) => setTemplateFieldRows((current) => current.map((item) => item.key === row.key ? { ...item, label: event.target.value } : item))} required value={row.label} /></label>
                  <label className={labelClass}>Kind<select className={inputClass} onChange={(event) => setTemplateFieldRows((current) => current.map((item) => item.key === row.key ? { ...item, kind: event.target.value } : item))} value={row.kind}>{templateFieldKinds.map((kindValue) => <option key={kindValue} value={kindValue}>{kindValue}</option>)}</select></label>
                  <label className={labelClass}>Default<input className={inputClass} onChange={(event) => setTemplateFieldRows((current) => current.map((item) => item.key === row.key ? { ...item, defaultValue: event.target.value } : item))} value={row.defaultValue} /></label>
                  <label className={labelClass}>Options<input className={inputClass} onChange={(event) => setTemplateFieldRows((current) => current.map((item) => item.key === row.key ? { ...item, options: event.target.value } : item))} placeholder="Comma-separated for select fields" value={row.options} /></label>
                  <label className={labelClass}>Help<input className={inputClass} onChange={(event) => setTemplateFieldRows((current) => current.map((item) => item.key === row.key ? { ...item, help: event.target.value } : item))} value={row.help} /></label>
                  <label className="flex items-center gap-2 text-sm"><input checked={row.required} onChange={(event) => setTemplateFieldRows((current) => current.map((item) => item.key === row.key ? { ...item, required: event.target.checked } : item))} type="checkbox" /> Required</label>
                  <div className="flex items-end"><button className={plainButtonClass} onClick={() => setTemplateFieldRows((current) => current.filter((item) => item.key !== row.key))} type="button">Remove</button></div>
                </fieldset>
              ))}</div>}
              <button className={`${secondaryButtonClass} mt-4`} disabled={templateFieldRows.length >= 25} onClick={addTemplateField} type="button">Add template field</button>
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
                    <FormReferencePicker browseLabel="Open sites" create={canWriteDirectory ? directoryCreate('site', formPickerContext) : undefined} defaultValue="" kind="site" label="Site" name={`${prefix}siteId`} options={references.sites} />
                    <FormReferencePicker browseLabel="Open buildings" create={canWriteDirectory ? directoryCreate('building', formPickerContext) : undefined} defaultValue="" kind="building" label="Building" name={`${prefix}buildingId`} onSelectedChange={(records) => setBulkBuildingIds((current) => ({ ...current, [row.key]: records[0]?.id ?? '' }))} options={references.buildings} />
                    <FormReferencePicker browseLabel="Open rooms" create={canWriteDirectory ? directoryCreate('room', formPickerContext) : undefined} defaultValue="" help={bulkBuildingIds[row.key] ? undefined : 'Select a building to choose a room.'} key={`bulk-room-${row.key}-${bulkBuildingIds[row.key] ?? ''}`} kind="room" label="Room" name={`${prefix}roomId`} options={roomsForBuilding(references.rooms, bulkBuildingIds[row.key] ?? '')} />
                    <FormReferencePicker browseLabel="Open departments" create={canWriteDirectory ? directoryCreate('department', formPickerContext) : undefined} defaultValue="" kind="department" label="Department" name={`${prefix}departmentId`} options={references.departments} />
                    <FormReferencePicker browseHref="#workspace-people" browseLabel="Open directory" defaultValue="" kind="identity" label="Primary user" name={`${prefix}userId`} options={directoryReferences.identities} />
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
        {models.length === 0 ? <p className={`${emptyStateClass} mt-4`}>{modelIncludeRetired || modelSearch.trim() ? 'No models match this search.' : 'No active models match this search.'}</p> : (
          <ul className="mt-4 grid gap-3 lg:grid-cols-2">{models.map((model) => (
            <li className={`${subpanelClass} p-4 transition hover:border-white/15 hover:bg-white/[0.025]`} key={model.id}>
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div className="min-w-0">
                  <p className="break-words font-semibold text-steward-mist">{modelLabel(model)}</p>
                  <div className="mt-2 flex flex-wrap gap-2"><StatusBadge>{model.kind}</StatusBadge>{model.status === 'retired' && <StatusBadge tone="warning">retired</StatusBadge>}<StatusBadge tone={model.instanceCount > 0 ? 'success' : 'neutral'}>{model.instanceCount} asset{model.instanceCount === 1 ? '' : 's'}</StatusBadge>{Boolean(model.warrantyMonths) && <StatusBadge tone="info">{model.warrantyMonths} month warranty</StatusBadge>}{Boolean(model.usefulLifeMonths) && <StatusBadge tone="info">{model.usefulLifeMonths} month life</StatusBadge>}{Boolean(model.unitCostMinor) && <StatusBadge tone="info">{formatMoney(model.unitCostMinor, model.currency)}</StatusBadge>}</div>
                  {model.replacementModelId && <p className="mt-2 text-sm text-steward-mist-muted">Replacement lineage: {models.find((item) => item.id === model.replacementModelId) ? modelLabel(models.find((item) => item.id === model.replacementModelId) as AssetModel) : model.replacementModelId}</p>}
                </div>
                <div className="flex flex-wrap gap-2">
                  <a className={secondaryButtonClass} href="#workspace-atlas" onClick={(event) => { event.preventDefault(); openModelInventory(model) }}>View inventory</a>
                {canWrite && <>
                  {model.status !== 'retired' && <button className={secondaryButtonClass} onClick={() => openCreateFromModel(model)} type="button">Use</button>}
                  {model.status !== 'retired' && <button className={secondaryButtonClass} onClick={() => openBulkCreateFromModel(model)} type="button">Bulk add</button>}
                  <button className={secondaryButtonClass} onClick={() => openModelEdit(model)} type="button">Edit</button>
                  {model.status === 'retired'
                    ? <button className={secondaryButtonClass} disabled={busy === `reactivate-model-${model.id}`} onClick={() => void reactivateModel(model)} type="button">{busy === `reactivate-model-${model.id}` ? 'Reactivating…' : 'Un-retire'}</button>
                    : <button className={dangerButtonClass} disabled={busy === `retire-model-${model.id}`} onClick={() => void retireModel(model)} type="button">{busy === `retire-model-${model.id}` ? 'Retiring…' : 'Retire'}</button>}
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
          <ModelRecordDetails model={inventoryModel} models={models} />
          <RecordTags csrfToken={csrfToken} permissions={permissions} recordId={inventoryModel.id} recordName={modelLabel(inventoryModel)} recordType="atlas.model" />
          <form aria-label={`Filter inventory for ${modelLabel(inventoryModel)}`} className="mt-5 grid gap-4 sm:grid-cols-2 lg:grid-cols-3" onSubmit={handleModelInventorySubmit}>
            <label className={labelClass}>Lifecycle state<select className={inputClass} onChange={(event) => setModelInventoryFilter('status', event.target.value)} value={modelInventoryFilters.status}><option value="">All states</option>{statuses.map((value) => <option key={value} value={value}>{value}</option>)}</select></label>
            <ModelInventoryReferenceFilter label="Site" onChange={(value) => setModelInventoryFilter('siteId', value)} options={references.sites} value={modelInventoryFilters.siteId} />
            <ModelInventoryReferenceFilter label="Asset department" onChange={(value) => setModelInventoryFilter('departmentId', value)} options={references.departments} value={modelInventoryFilters.departmentId} />
            <ModelInventoryReferenceFilter label="Primary user (asset)" onChange={(value) => setModelInventoryFilter('userId', value)} options={directoryReferences.identities} value={modelInventoryFilters.userId} />
            <label className={labelClass}>Deployment context<input className={inputClass} maxLength={200} onChange={(event) => setModelInventoryFilter('deploymentContext', event.target.value)} placeholder="Hostname or deployment notes" type="search" value={modelInventoryFilters.deploymentContext} /></label>
            <label className={labelClass}>Group matching assets<select className={inputClass} onChange={(event) => setModelInventoryFilter('groupBy', event.target.value)} value={modelInventoryFilters.groupBy}><option value="">No grouping</option><option value="status">Lifecycle state</option><option value="site">Site</option><option value="department">Asset department</option><option value="user">Primary user (asset)</option><option value="deployment">Deployment context</option></select></label>
            <div className="sm:col-span-2 lg:col-span-3">
              <p className="mb-2 text-sm font-medium text-steward-mist">AND / OR query</p>
              <QueryBuilder
                encoded={modelInventoryQueryText}
                error={queryErrorText(modelInventoryQueryText)}
                fields={modelInventoryQueryFields}
                model={modelInventoryQuery}
                onEncodedChange={(value) => {
                  setModelInventoryQueryText(value)
                  const parsed = parseQuery(value, modelInventoryQueryFields)
                  if (parsed.ok) setModelInventoryQuery(isQueryEmpty(parsed.model) ? emptyQuery() : parsed.model)
                }}
                onModelChange={(model) => {
                  setModelInventoryQuery(model)
                  setModelInventoryQueryText(encodeQuery(model))
                }}
              />
            </div>
            <div className="flex flex-wrap items-end gap-3 sm:col-span-2 lg:col-span-3">
              <button className={buttonClass} disabled={busy === `inventory-${inventoryModel.id}`} type="submit">{busy === `inventory-${inventoryModel.id}` ? 'Applying…' : 'Apply filters'}</button>
              <button className={secondaryButtonClass} onClick={() => { setModelInventoryFilters(emptyModelInventoryFilters); setModelInventoryQuery(emptyQuery()); setModelInventoryQueryText(''); void loadModelInventory(inventoryModel, emptyModelInventoryFilters, emptyQuery()) }} type="button">Clear filters</button>
            </div>
          </form>
          <p className="mt-3 text-sm text-steward-mist-muted">Asset department and primary user match the current Atlas instance fields. Effective-dated primary, additional-user, and responsible-department history remains in People.</p>
          {!canReadDirectory && <p className="mt-2 text-sm text-steward-mist-muted">Site, asset department, and primary-user choices require directory read access.</p>}
          {busy === `inventory-${inventoryModel.id}` && !modelInventory ? <p className="mt-5 text-sm text-steward-mist-muted" role="status">Loading model inventory…</p> : modelInventory && <>
            <div className="mt-5 flex flex-wrap gap-2" role="status"><StatusBadge tone="info">{modelInventory.filteredCount} matching</StatusBadge><StatusBadge>{modelInventory.totalCount} total</StatusBadge>{modelInventory.items.length < modelInventory.filteredCount && <span className="self-center text-sm text-steward-mist-muted">Showing the first {modelInventory.items.length} assets.</span>}</div>
            {modelInventory.groupBy && <section aria-labelledby="model-inventory-groups-heading" className="mt-5"><h5 className="font-semibold" id="model-inventory-groups-heading">Grouped counts</h5>{modelInventory.groups.length === 0 ? <p className="mt-2 text-sm text-steward-mist-muted">No groups match these filters.</p> : <ul className="mt-3 grid gap-2 sm:grid-cols-2 lg:grid-cols-3">{modelInventory.groups.map((group) => <li className="flex min-w-0 items-center justify-between gap-3 rounded-lg border border-white/10 bg-white/[0.025] p-3 text-sm" key={group.key || 'unassigned'}><span className="min-w-0 break-words">{modelInventoryGroupLabel(modelInventory.groupBy || '', group.key, directoryReferences)}</span><StatusBadge tone="info">{group.count}</StatusBadge></li>)}</ul>}</section>}
            <section aria-labelledby="model-inventory-assets-heading" className="mt-5"><h5 className="font-semibold" id="model-inventory-assets-heading">Matching assets</h5>{modelInventory.items.length === 0 ? <p className={`${emptyStateClass} mt-3`}>No linked assets match these filters.</p> : <ul className="mt-3 grid gap-3 sm:grid-cols-2">{modelInventory.items.map((asset) => <li className="min-w-0 rounded-xl border border-white/10 bg-white/[0.025] p-4" key={asset.id}><a className="block min-h-11 break-words font-semibold text-steward-blue underline-offset-4 hover:underline focus-visible:underline" href="#workspace-atlas" onClick={(event) => { event.preventDefault(); openAssetDetail(asset) }}>{asset.name}</a><p className="mt-1 break-words text-sm text-steward-mist-muted">{asset.assetTag || asset.serialNumber || asset.hostname || 'No asset identifier'}</p><div className="mt-3 flex flex-wrap gap-2"><StatusBadge>{asset.status}</StatusBadge>{asset.siteId && <StatusBadge tone="info">{modelInventoryGroupLabel('site', asset.siteId, references)}</StatusBadge>}</div>{(asset.hostname || asset.deploymentNotes) && <p className="mt-3 break-words text-sm"><span className="font-semibold">Deployment:</span> {[asset.hostname, asset.deploymentNotes].filter(Boolean).join(' · ')}</p>}</li>)}</ul>}</section>
          </>}
        </section>}
      </section>
      </div>
    </section>
  )
}

function AssetDetailPanel({
  busy, canWrite, csrfToken, emptyPrompt, identifierRefreshVersion, identityLabel, lifecycle, models, onIdentifierChanged, permissions, references, selected,
}: {
  busy: string
  canWrite: boolean
  csrfToken: string
  emptyPrompt: string
  identifierRefreshVersion: number
  identityLabel: (id?: string) => string | undefined
  lifecycle: readonly LifecycleEvent[]
  models: readonly AssetModel[]
  onIdentifierChanged: () => void
  permissions: readonly string[]
  references: ReferenceOptions
  selected: Asset | null
}) {
  const [related, setRelated] = useState<AssetRelated | null>(null)
  const [preview, setPreview] = useState<ViewableDocument | null>(null)

  useEffect(() => {
    if (!selected) {
      setRelated(null)
      setPreview(null)
      return
    }
    let active = true
    requestJSON(`/api/v1/assets/${encodeURIComponent(selected.id)}/related`)
      .then((value) => { if (active) setRelated(parseAssetRelated(value)) })
      .catch(() => { if (active) setRelated(emptyRelated) })
    return () => { active = false }
  }, [selected])

  const linkedModel = selected?.modelId ? models.find((model) => model.id === selected.modelId) : undefined
  const effectiveReplacementId = selected?.replacementModelId || linkedModel?.replacementModelId
  const replacementModel = effectiveReplacementId ? models.find((model) => model.id === effectiveReplacementId) : undefined
  const replacementSource = selected?.replacementModelId ? 'asset override' : linkedModel?.replacementModelId ? 'model lineage' : undefined
  const percent = selected ? lifecyclePercent(selected, new Date(), linkedModel) : null
  const currentModelPastLifecycle = linkedModel ? modelPastLifecycle(linkedModel) : false

  return (
    <aside aria-labelledby="asset-detail-heading" className={`${subpanelClass} p-5`}>
      <h3 className="text-lg font-semibold outline-none focus-visible:ring-2 focus-visible:ring-steward-teal" id="asset-detail-heading" tabIndex={-1}>Asset details</h3>
      {!selected ? <p className="mt-3 text-sm text-steward-mist-muted">{emptyPrompt}</p> : <>
        <h4 className="mt-4 font-semibold">Instance-specific record</h4>
        <dl className="mt-3 grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-2 text-sm"><Detail label="Name" value={selected.name} /><Detail label="Model ID" value={selected.modelId} /><Detail label="Kind" value={selected.kind} /><Detail label="Status" value={selected.status} /><Detail label="Criticality" value={selected.criticalityScore ? `${selected.criticalityScore} / 5` : undefined} /><Detail label="Asset tag" value={selected.assetTag} /><Detail label="Serial" value={selected.serialNumber} /><Detail label="Hostname" value={selected.hostname} /><Detail label="Deployment notes" value={selected.deploymentNotes} /><Detail label="Unit cost" value={formatMoney(selected.unitCostMinor, selected.currency) || undefined} /><Detail label="Purchase date" value={calendarDateText(selected.purchaseDate) || undefined} /><Detail label="Installed date" value={calendarDateText(selected.installedDate) || undefined} /><Detail label="Lifecycle start" value={calendarDateText(selected.lifecycleStartDate) || undefined} /><Detail label="Replacement model" value={replacementModel ? `${modelLabel(replacementModel)}${replacementSource ? ` (${replacementSource})` : ''}` : effectiveReplacementId} /><Detail label="Site" value={referenceExportLabel(references.sites, selected.siteId ?? '') || selected.siteId} /><Detail label="Building" value={referenceExportLabel(references.buildings, selected.buildingId ?? '') || selected.buildingId} /><Detail label="Room" value={referenceExportLabel(references.rooms, selected.roomId ?? '') || selected.roomId} /><Detail label="Asset department" value={referenceExportLabel(references.departments, selected.departmentId ?? '') || selected.departmentId} /><Detail label="Primary user" value={identityLabel(selected.userId)} /><Detail label="Revision" value={String(selected.revision)} /></dl>
        {(percent !== null || currentModelPastLifecycle) && <section aria-labelledby="asset-lifecycle-progress-heading" className="mt-6 rounded-xl border border-steward-teal/30 bg-steward-teal/[0.06] p-4">
          <h4 className="font-semibold" id="asset-lifecycle-progress-heading">Lifecycle planning</h4>
          {percent !== null && <>
            <p className="mt-2 text-sm text-steward-mist-muted">{usefulLifeMonths(selected, linkedModel)} month expected useful life · measured from {selected.lifecycleStartDate ? 'lifecycle start' : selected.installedDate ? 'installed date' : 'purchase date'}.</p>
            <div className="mt-3">
              <div className="flex items-center justify-between gap-3 text-sm"><span>Through lifecycle</span><strong>{percent}%</strong></div>
              <div aria-hidden="true" className="mt-2 h-3 overflow-hidden rounded-sm bg-steward-ink-800"><div className={`h-full rounded-sm ${percent >= 100 ? 'bg-steward-warning' : 'bg-steward-teal'}`} style={{ width: `${Math.max(percent === 0 ? 0 : 2, percent)}%` }} /></div>
            </div>
          </>}
          {currentModelPastLifecycle && linkedModel && <p className="mt-3 rounded-lg border border-steward-warning/60 bg-steward-warning/10 p-3 text-sm text-[#ffd596]"><strong>Past model lifecycle:</strong> {modelLabel(linkedModel)} reached its last effective date on {calendarDateText(linkedModel.lastEffectiveDate)}.</p>}
        </section>}
        {selected.attributes && Object.keys(selected.attributes).length > 0 && <>
          <h4 className="mt-6 font-semibold">Template values</h4>
          <dl className="mt-3 grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-2 text-sm">{Object.entries(selected.attributes).map(([key, value]) => <Detail key={key} label={key} value={value} />)}</dl>
        </>}
        {selected.modelContext && <ModelContextDetails context={selected.modelContext} instanceKind={selected.kind} />}
        <RelatedRecords onPreview={setPreview} related={related} />
        {preview && <div className="mt-4"><DocumentViewer csrfToken={csrfToken} document={preview} onClose={() => setPreview(null)} /></div>}
        <RecordTags csrfToken={csrfToken} permissions={permissions} recordId={selected.id} recordName={selected.name} recordType="atlas.asset" />
        <h4 className="mt-6 font-semibold">Lifecycle history</h4>
        {busy === `history-${selected.id}` ? <p className="mt-2 text-sm text-steward-mist-muted" role="status">Loading lifecycle…</p> : lifecycle.length === 0 ? <p className="mt-2 text-sm text-steward-mist-muted">No lifecycle events loaded.</p> : <ol className="mt-3 space-y-3">{lifecycle.map((event) => <li className="border-l-2 border-steward-blue pl-3 text-sm" key={event.id}><p><strong>{event.fromStatus ? `${event.fromStatus} → ` : ''}{event.toStatus}</strong> · revision {event.revision}</p><p className="text-steward-mist-muted">{event.note || 'Status recorded'} · {new Date(event.occurredAt).toLocaleDateString()}</p></li>)}</ol>}
        <AtlasIdentifiers assetId={selected.id} assetName={selected.name} canWrite={canWrite} csrfToken={csrfToken} key={`${selected.id}-${identifierRefreshVersion}`} onChanged={onIdentifierChanged} />
      </>}
    </aside>
  )
}

type AssetRelated = {
  components: AssetComponent[]
  purchaseOrders: { id: string; number: string; totalMinor: number; currency: string; lines?: { description: string; quantity: number; amountMinor: number }[] }[]
  costs: { id: string; description: string; amountMinor: number; currency: string; kind: string }[]
  installations: { id: string; versionId: string; status: string }[]
  assignments: { id: string; licenseId: string; assigneeKind: string; seats: number }[]
  licenses: { id: string; name: string; quantity: number; entitlementMetric: string; purchaseOrderId?: string; costRecordId?: string; documentIds?: string[] }[]
  documents: ViewableDocument[]
}

const emptyRelated: AssetRelated = { components: [], purchaseOrders: [], costs: [], installations: [], assignments: [], licenses: [], documents: [] }

function parseAssetRelated(value: unknown): AssetRelated {
  if (typeof value !== 'object' || value === null) return emptyRelated
  const item = value as Record<string, unknown>
  const documents = Array.isArray(item.documents) ? item.documents.flatMap((entry) => {
    if (typeof entry !== 'object' || entry === null) return []
    const document = entry as Record<string, unknown>
    if (typeof document.id !== 'string' || typeof document.name !== 'string' || typeof document.mediaType !== 'string') return []
    return [{ id: document.id, name: document.name, mediaType: document.mediaType }]
  }) : []
  return {
    components: Array.isArray(item.components) ? item.components.filter((entry): entry is AssetComponent => typeof entry === 'object' && entry !== null && typeof (entry as AssetComponent).name === 'string') : [],
    purchaseOrders: Array.isArray(item.purchaseOrders) ? item.purchaseOrders as AssetRelated['purchaseOrders'] : [],
    costs: Array.isArray(item.costs) ? item.costs as AssetRelated['costs'] : [],
    installations: Array.isArray(item.installations) ? item.installations as AssetRelated['installations'] : [],
    assignments: Array.isArray(item.assignments) ? item.assignments as AssetRelated['assignments'] : [],
    licenses: Array.isArray(item.licenses) ? item.licenses as AssetRelated['licenses'] : [],
    documents,
  }
}

function RelatedRecords({ onPreview, related }: { onPreview: (document: ViewableDocument) => void; related: AssetRelated | null }) {
  if (!related) return <p className="mt-6 text-sm text-steward-mist-muted" role="status">Loading purchase orders, software, and documents…</p>
  return (
    <section aria-labelledby="asset-related-heading" className="mt-6">
      <h4 className="font-semibold" id="asset-related-heading">Linked procurement, software, and documents</h4>
      {related.components.length > 0 && <RelatedList title="Accessories" items={related.components.map((item) => `${item.name}${item.modelNumber ? ` · ${item.modelNumber}` : ''}${item.unitCostMinor ? ` · ${formatMoney(item.unitCostMinor, item.currency)}` : ''}`)} />}
      {related.purchaseOrders.length > 0 && <RelatedList title="Purchase orders" items={related.purchaseOrders.map((item) => `${item.number} · ${formatMoney(item.totalMinor, item.currency) || 'No total'}${item.lines?.length ? ` · ${item.lines.length} lines` : ''}`)} />}
      {related.costs.length > 0 && <RelatedList title="Costs" items={related.costs.map((item) => `${item.description} · ${item.kind} · ${formatMoney(item.amountMinor, item.currency)}`)} />}
      {related.licenses.length > 0 && <RelatedList title="Software and volume licenses" items={related.licenses.map((item) => `${item.name} · ${item.quantity} ${item.entitlementMetric} seats${item.purchaseOrderId ? ` · PO ${item.purchaseOrderId}` : ''}${item.costRecordId ? ` · cost ${item.costRecordId}` : ''}`)} />}
      {related.installations.length > 0 && <RelatedList title="Installations" items={related.installations.map((item) => `${item.versionId} · ${item.status}`)} />}
      {related.assignments.length > 0 && <RelatedList title="License assignments" items={related.assignments.map((item) => `${item.assigneeKind} · ${item.seats} seats · license ${item.licenseId}`)} />}
      {related.documents.length > 0 && (
        <div className="mt-4">
          <h5 className="text-sm font-semibold">Documents</h5>
          <ul className="mt-2 space-y-2">{related.documents.map((document) => (
            <li key={document.id}><button className={plainButtonClass} onClick={() => onPreview(document)} type="button">View {document.name}</button></li>
          ))}</ul>
        </div>
      )}
      {related.components.length + related.purchaseOrders.length + related.costs.length + related.licenses.length + related.documents.length === 0 && <p className="mt-2 text-sm text-steward-mist-muted">No purchase orders, software, or documents are linked yet.</p>}
    </section>
  )
}

function RelatedList({ title, items }: { title: string; items: string[] }) {
  return <div className="mt-4"><h5 className="text-sm font-semibold">{title}</h5><ul className="mt-2 list-disc space-y-1 pl-5 text-sm text-steward-mist-muted">{items.map((item) => <li key={item}>{item}</li>)}</ul></div>
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
      <Detail label="Model unit cost" value={formatMoney(context.unitCostMinor, context.currency) || undefined} />
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

function ModelRecordDetails({ model, models }: { model: AssetModel; models: readonly AssetModel[] }) {
  const specifications = Object.entries(model.specifications ?? {}).sort(([left], [right]) => left.localeCompare(right))
  const replacement = model.replacementModelId ? models.find((item) => item.id === model.replacementModelId) : undefined
  return <section aria-labelledby="model-record-details-heading" className="mt-5 rounded-xl border border-white/10 bg-white/[0.025] p-4">
    <h5 className="font-semibold" id="model-record-details-heading">Shared model defaults</h5>
    <dl className="mt-3 grid grid-cols-[minmax(0,0.45fr)_minmax(0,1fr)] gap-x-4 gap-y-2 text-sm">
      <Detail label="Manufacturer" value={model.manufacturer} />
      <Detail label="Model name" value={model.name} />
      <Detail label="Model number" value={model.modelNumber} />
      <Detail label="Kind" value={model.kind} />
      <Detail label="Status" value={model.status} />
      <Detail label="Vendor ID" value={model.vendorIdentifier} />
      <Detail label="Support URL" value={model.supportUrl} />
      <Detail label="Warranty" value={model.warrantyMonths ? `${model.warrantyMonths} months` : undefined} />
      <Detail label="Useful life" value={model.usefulLifeMonths ? `${model.usefulLifeMonths} months` : undefined} />
      <Detail label="Last effective date" value={model.lastEffectiveDate ? calendarDateText(model.lastEffectiveDate) : undefined} />
      <Detail label="Replacement lineage" value={replacement ? modelLabel(replacement) : model.replacementModelId} />
      <Detail label="Unit cost" value={formatMoney(model.unitCostMinor, model.currency) || undefined} />
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

function TextField({ defaultValue = '', help, label, maxLength, name, placeholder, required }: { defaultValue?: string; help?: string; label: string; maxLength?: number; name: string; placeholder?: string; required?: boolean }) {
  const helpID = help ? `${name}-help` : undefined
  return <label className={labelClass}>{label}{help && <span className="mt-1 block font-normal leading-5 text-steward-mist-muted" id={helpID}>{help}</span>}<input aria-describedby={helpID} className={inputClass} defaultValue={defaultValue} maxLength={maxLength} name={name} placeholder={placeholder} required={required} /></label>
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

function ModelSelect({ canWrite, csrfToken, models, onChange, onCreated, onOpenModels, value }: {
  canWrite: boolean
  csrfToken: string
  models: AssetModel[]
  onChange: (value: string) => void
  onCreated: (model: AssetModel) => void
  onOpenModels: () => void
  value: string
}) {
  const selected = value ? [{ id: value, label: models.find((model) => model.id === value) ? modelLabel(models.find((model) => model.id === value) as AssetModel) : value }] : []
  return <RecordSearchPicker
    browseLabel="Open models"
    create={canWrite ? {
      label: 'Add model',
      fields: [
        { key: 'manufacturer', label: 'Manufacturer', required: true },
        { key: 'name', label: 'Model name', required: true },
        { key: 'kind', label: 'Kind', required: true, options: kinds.map((kind) => ({ id: kind, label: kind })) },
      ],
      submit: async (values) => {
        const saved = await requestJSON('/api/v1/asset-models', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
          body: JSON.stringify({ manufacturer: values.manufacturer, name: values.name, kind: values.kind || 'other', status: 'active', currency: 'USD' }),
        })
        if (!isAssetModel(saved)) throw new Error('invalid model response')
        onCreated(saved)
        return { id: saved.id, label: modelLabel(saved), detail: saved.kind }
      },
    } : undefined}
    kind="model"
    label="Model"
    multiple={false}
    name="modelId"
    onBrowse={onOpenModels}
    onChange={(records) => onChange(records[0]?.id ?? '')}
    options={models.map((model) => ({ id: model.id, label: modelLabel(model), detail: model.kind }))}
    selected={selected}
  />
}

function FormReferencePicker({ browseHref = '#workspace-people', browseLabel, create, defaultValue, help, kind, label, name, onSelectedChange, options }: {
  browseHref?: string
  browseLabel: string
  create?: LookupCreateConfig
  defaultValue: string
  help?: string
  kind: 'site' | 'building' | 'room' | 'department' | 'identity'
  label: string
  name: string
  onSelectedChange?: (records: SearchableRecord[]) => void
  options: ReferenceRecord[]
}) {
  const [selected, setSelected] = useState<SearchableRecord[]>(() => {
    if (!defaultValue) return []
    const match = options.find((option) => option.id === defaultValue)
    return [{ id: defaultValue, label: match ? referenceLabel(match) : defaultValue, detail: defaultValue }]
  })
  return <RecordSearchPicker
    browseHref={browseHref}
    browseLabel={browseLabel}
    create={create}
    help={help}
    kind={kind}
    label={label}
    multiple={false}
    name={name}
    onChange={(records) => {
      setSelected(records)
      onSelectedChange?.(records)
    }}
    options={options.map((option) => ({ id: option.id, label: referenceLabel(option), detail: option.id }))}
    selected={selected}
  />
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

function defaultAccessoriesForKind(kind: string): AccessoryRow[] {
  const rows: AccessoryRow[] = [
    { key: 0, kind: 'monitor', name: 'Dell P2422H', modelNumber: 'P2422H', unitCost: '219.00' },
    { key: 1, kind: 'combo', name: 'Logitech MK270 mouse and keyboard', modelNumber: 'MK270', unitCost: '29.99' },
  ]
  if (kind === 'laptop') rows.push({ key: 2, kind: 'dock', name: 'Dell WD19 docking station', modelNumber: 'WD19', unitCost: '189.00' })
  return rows
}
