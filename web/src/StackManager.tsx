import { type FormEvent, type InputHTMLAttributes, type ReactNode, useEffect, useId, useMemo, useRef, useState } from 'react'
import { ApiRequestError, isRevision, requestJSON, type Revision } from './api'
import type { Asset } from './AtlasInventory'
import DocumentViewer, { type ViewableDocument } from './DocumentViewer'
import RecordSearchPicker, { type SearchableRecord } from './RecordSearchPicker'
import { ProductHeader, StatusBadge, buttonClass, inputClass, panelClass, plainButtonClass, secondaryButtonClass, subpanelClass, tableWrapClass } from './ui'
import DataGrid from './grid/DataGrid'
import Drawer from './grid/Drawer'
import type { GridColumn } from './grid/columns'
import type { CellEdit } from './grid/useCellEditing'
import { buildPayload, summarizeReport, tasksFromEdits, useWriteQueue, type WriteTransport } from './grid/writeQueue'

// Requirement: REQ-STACK-001. Feature: software.licenses.

type Product = { id: string; name: string; publisher: string; category?: string; status: string; revision: Revision }
type Version = { id: string; productId: string; name: string; releasedOn?: string; status: string; revision: Revision }
type Installation = { id: string; versionId: string; assetId: string; status: string; usageState: string; installedAt: string; lastUsedAt?: string; removedAt?: string; revision: Revision }
type License = { id: string; productId: string; versionId?: string; name: string; entitlementMetric: string; quantity: number; status: string; startsOn?: string; expiresOn?: string; vendorId?: string; purchaseOrderId?: string; contractId?: string; costRecordId?: string; documentIds: string[]; revision: Revision }
type Assignment = { id: string; licenseId: string; assigneeKind: string; assigneeId: string; seats: number; usageState: string; assignedAt: string; lastUsedAt?: string; endedAt?: string; revision: Revision }
type Snapshot = { products: Product[]; versions: Version[]; installations: Installation[]; licenses: License[]; assignments: Assignment[]; assignmentTotal?: number; installationTotal?: number }
type Condition = { code: string; severity: string; productId: string; versionId?: string; licenseId?: string; assetId?: string; entitledQuantity?: number; assignedQuantity?: number; underusedQuantity?: number; daysUntilExpiry?: number; humanReadableState: string }
type Analytics = { asOf: string; expiringWithinDays: number; products: number; activeInstallations: number; activeLicenses: number; entitledQuantity: number; assignedQuantity: number; underusedAssignments: number; complianceConditions: Condition[] }
type ImportReference = { type: string; id: string }
type ImportOutcome = ImportReference & { revision: Revision; checksum: string; status: 'created' | 'unchanged' | 'holding'; missingDependencies: ImportReference[]; writeLocked: boolean }
type ImportOwnership = ImportReference & { writeLocked: boolean }
export type StackImportResult = { packageId: string; status: 'processing' | 'completed' | 'holding' | 'failed'; created: number; unchanged: number; holding: number; replay: boolean; errorCode?: string; records: ImportOutcome[]; pendingOwnership: ImportOwnership[] }
type StackManagerProps = { assets: readonly Asset[]; csrfToken: string; permissions: readonly string[]; onOpenHelp?: () => void; identity?: { subject: string; organizationId: string } | null }

const emptySnapshot: Snapshot = { products: [], versions: [], installations: [], licenses: [], assignments: [] }
const emptyAnalytics: Analytics = { asOf: '', expiringWithinDays: 90, products: 0, activeInstallations: 0, activeLicenses: 0, entitledQuantity: 0, assignedQuantity: 0, underusedAssignments: 0, complianceConditions: [] }
const stableIDPattern = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/
const sha256Pattern = /^[a-f0-9]{64}$/

function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null }
function hasID(value: unknown): value is Record<string, unknown> { return isObject(value) && typeof value.id === 'string' && value.id.length > 0 }
function validItems(value: unknown, validate: (item: unknown) => boolean) { return Array.isArray(value) && value.every(validate) }
function validCount(value: unknown): value is number { return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0 }
function validPositive(value: unknown): value is number { return typeof value === 'number' && Number.isSafeInteger(value) && value > 0 }
function optionalString(value: unknown) { return value === undefined || value === null || typeof value === 'string' }
function optionalCount(value: unknown) { return value === undefined || validCount(value) }
function validStringItems(value: unknown) { return Array.isArray(value) && value.every((item) => typeof item === 'string') }
function optionalStringItems(value: unknown) { return value === undefined || value === null || validStringItems(value) }
function optionalInstantValue(value: unknown) { return value === undefined || value === null || validInstantValue(value) }
function importReferenceKey(value: ImportReference) { return `${value.type}\u0000${value.id}` }
function hasExactKeys(value: Record<string, unknown>, expected: readonly string[]) {
  const keys = Object.keys(value).sort()
  return keys.length === expected.length && keys.every((key, index) => key === expected[index])
}
function validInstantValue(value: unknown) { return typeof value === 'string' && !Number.isNaN(Date.parse(value)) }

export function parseStackSnapshot(value: unknown): Snapshot {
  if (!isObject(value)
    || !validItems(value.products, (item) => hasID(item) && typeof item.name === 'string' && typeof item.publisher === 'string' && optionalString(item.category) && typeof item.status === 'string' && isRevision(item.revision))
    || !validItems(value.versions, (item) => hasID(item) && typeof item.productId === 'string' && typeof item.name === 'string' && optionalInstantValue(item.releasedOn) && typeof item.status === 'string' && isRevision(item.revision))
    || !validItems(value.installations, (item) => hasID(item) && typeof item.versionId === 'string' && typeof item.assetId === 'string' && typeof item.status === 'string' && typeof item.usageState === 'string' && validInstantValue(item.installedAt) && optionalInstantValue(item.lastUsedAt) && optionalInstantValue(item.removedAt) && isRevision(item.revision))
    || !validItems(value.licenses, (item) => hasID(item) && typeof item.productId === 'string' && optionalString(item.versionId) && typeof item.name === 'string' && typeof item.entitlementMetric === 'string' && validPositive(item.quantity) && typeof item.status === 'string' && optionalInstantValue(item.startsOn) && optionalInstantValue(item.expiresOn) && optionalStringItems(item.documentIds) && isRevision(item.revision))
    || !validItems(value.assignments, (item) => hasID(item) && typeof item.licenseId === 'string' && typeof item.assigneeKind === 'string' && typeof item.assigneeId === 'string' && validPositive(item.seats) && typeof item.usageState === 'string' && validInstantValue(item.assignedAt) && optionalInstantValue(item.lastUsedAt) && optionalInstantValue(item.endedAt) && isRevision(item.revision))) {
    throw new Error('invalid Stack response')
  }
  const snapshot = value as Snapshot
  snapshot.licenses = snapshot.licenses.map((item) => ({ ...item, documentIds: item.documentIds ?? [] }))
  return snapshot
}

export function parseStackAnalytics(value: unknown): Analytics {
  if (!isObject(value) || !validInstantValue(value.asOf) || !validPositive(value.expiringWithinDays) || !validCount(value.products)
    || !validCount(value.activeInstallations) || !validCount(value.activeLicenses) || !validCount(value.entitledQuantity)
    || !validCount(value.assignedQuantity) || !validCount(value.underusedAssignments)
    || !validItems(value.complianceConditions, (item) => isObject(item) && typeof item.code === 'string' && typeof item.severity === 'string'
      && typeof item.productId === 'string' && optionalString(item.versionId) && optionalString(item.licenseId) && optionalString(item.assetId)
      && optionalCount(item.entitledQuantity) && optionalCount(item.assignedQuantity) && optionalCount(item.underusedQuantity)
      && (item.daysUntilExpiry === undefined || typeof item.daysUntilExpiry === 'number' && Number.isSafeInteger(item.daysUntilExpiry))
      && typeof item.humanReadableState === 'string')) {
    throw new Error('invalid Stack analytics response')
  }
  return value as Analytics
}

export function parseStackImportResult(value: unknown): StackImportResult {
  if (!isObject(value) || typeof value.packageId !== 'string' || !stableIDPattern.test(value.packageId)
    || typeof value.status !== 'string' || !['processing', 'completed', 'holding', 'failed'].includes(value.status)
    || !validCount(value.created) || value.created > 10_000 || !validCount(value.unchanged) || value.unchanged > 10_000
    || !validCount(value.holding) || value.holding > 10_000 || typeof value.replay !== 'boolean'
    || !(value.errorCode === undefined || typeof value.errorCode === 'string' && /^[a-z][a-z0-9_]{0,63}$/.test(value.errorCode))
    || !validItems(value.records, (item) => isObject(item) && typeof item.type === 'string' && /^[a-z][a-z0-9_.-]{0,63}$/.test(item.type)
      && typeof item.id === 'string' && stableIDPattern.test(item.id) && isRevision(item.revision)
      && typeof item.checksum === 'string' && sha256Pattern.test(item.checksum)
      && typeof item.status === 'string' && ['created', 'unchanged', 'holding'].includes(item.status) && typeof item.writeLocked === 'boolean'
      && Array.isArray(item.missingDependencies) && item.missingDependencies.length <= 130
      && item.missingDependencies.every((dependency) => isObject(dependency) && typeof dependency.type === 'string'
        && /^[a-z][a-z0-9_.-]{0,63}$/.test(dependency.type) && typeof dependency.id === 'string' && stableIDPattern.test(dependency.id)))
    || !validItems(value.pendingOwnership, (item) => isObject(item)
      && hasExactKeys(item, ['id', 'type', 'writeLocked'])
      && typeof item.type === 'string' && /^[a-z][a-z0-9_.-]{0,63}$/.test(item.type)
      && typeof item.id === 'string' && stableIDPattern.test(item.id) && typeof item.writeLocked === 'boolean')) {
    throw new Error('invalid Stack import response')
  }
  const candidate = value as unknown as StackImportResult
  const recordKeys = new Set(candidate.records.map(importReferenceKey))
  const pendingKeys = candidate.pendingOwnership.map(importReferenceKey)
  if (candidate.records.length > 10_000 || candidate.records.length !== candidate.created + candidate.unchanged + candidate.holding
    || candidate.status === 'completed' && candidate.holding !== 0 || candidate.status === 'holding' && candidate.holding === 0
    || (candidate.status === 'processing' || candidate.status === 'failed') && candidate.holding !== 0
    || candidate.status === 'failed' && typeof candidate.errorCode !== 'string'
    || (candidate.status === 'completed' || candidate.status === 'holding') && candidate.errorCode !== undefined
    || candidate.pendingOwnership.length > 10_000
    || (candidate.status === 'completed' || candidate.status === 'holding') && candidate.pendingOwnership.length !== 0
    || new Set(pendingKeys).size !== pendingKeys.length || pendingKeys.some((key) => recordKeys.has(key))) {
    throw new Error('invalid Stack import response')
  }
  return candidate
}

function importResultFromError(cause: ApiRequestError): StackImportResult | undefined {
  if (!isObject(cause.body) || !('import' in cause.body)) return undefined
  try { return parseStackImportResult(cause.body.import) } catch { return undefined }
}

function text(values: FormData, key: string) { return String(values.get(key) ?? '').trim() }
function date(value: string) { return value ? `${value}T00:00:00Z` : undefined }
function instant(value: string) { return value ? new Date(value).toISOString() : undefined }
function label(value: string) { return value.replaceAll('_', ' ').replace(/\b\w/g, (letter) => letter.toUpperCase()) }
function displayCalendarDate(value?: string) { return value ? new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeZone: 'UTC' }).format(new Date(value)) : 'Not set' }
function displayInstantDate(value?: string) { return value ? new Intl.DateTimeFormat(undefined, { dateStyle: 'medium' }).format(new Date(value)) : 'Not set' }
function inputInstant(value?: string) {
  if (!value) return undefined
  const parsed = new Date(value)
  return new Date(parsed.getTime() - parsed.getTimezoneOffset() * 60000).toISOString().slice(0, 16)
}

const productStatuses = ['active', 'retired']
const versionStatuses = ['active', 'unsupported', 'retired']
const installationStatuses = ['installed', 'removed']
const usageStates = ['unknown', 'used', 'unused']
const licenseStatuses = ['active', 'expired', 'retired']

function statusTone(status: string) {
  if (status === 'active' || status === 'installed') return 'success' as const
  if (status === 'retired' || status === 'expired' || status === 'removed') return 'warning' as const
  return 'neutral' as const
}

function revisionOf(value: unknown) {
  return isObject(value) && isRevision(value.revision) ? value.revision : undefined
}

// Stack exposes narrow lifecycle endpoints rather than whole-record updates, so
// each grid marks only the fields its endpoint accepts as editable. Everything
// else is immutable after creation and renders read-only.

function productColumns(canWrite: boolean): GridColumn<Product>[] {
  return [
    { key: 'name', header: 'Product', kind: 'text', width: 16, text: (item) => item.name },
    { key: 'publisher', header: 'Publisher', kind: 'text', width: 14, text: (item) => item.publisher },
    { key: 'category', header: 'Category', kind: 'text', width: 12, text: (item) => item.category ?? '' },
    {
      key: 'status', header: 'Status', kind: 'enum', options: productStatuses, editable: canWrite, required: true, width: 9,
      text: (item) => item.status,
      display: (item) => <StatusBadge tone={statusTone(item.status)}>{label(item.status)}</StatusBadge>,
    },
    { key: 'id', header: 'Product ID', kind: 'text', width: 12, text: (item) => item.id },
  ]
}

function versionColumns(products: readonly Product[], canWrite: boolean): GridColumn<Version>[] {
  return [
    { key: 'name', header: 'Version', kind: 'text', width: 12, text: (item) => item.name },
    { key: 'productId', header: 'Product', kind: 'text', width: 16, text: (item) => item.productId, display: (item) => products.find((product) => product.id === item.productId)?.name ?? item.productId },
    { key: 'releasedOn', header: 'Released on', kind: 'date', width: 11, text: (item) => (item.releasedOn ?? '').slice(0, 10), display: (item) => displayCalendarDate(item.releasedOn) },
    {
      key: 'status', header: 'Status', kind: 'enum', options: versionStatuses, editable: canWrite, required: true, width: 11,
      text: (item) => item.status,
      display: (item) => <StatusBadge tone={statusTone(item.status)}>{label(item.status)}</StatusBadge>,
    },
    { key: 'id', header: 'Version ID', kind: 'text', width: 12, text: (item) => item.id },
  ]
}

function installationColumns(versions: readonly Version[], assets: readonly Asset[], canWrite: boolean): GridColumn<Installation>[] {
  return [
    { key: 'assetId', header: 'Asset', kind: 'text', width: 15, text: (item) => item.assetId, display: (item) => assets.find((asset) => asset.id === item.assetId)?.name ?? item.assetId },
    { key: 'versionId', header: 'Version', kind: 'text', width: 14, text: (item) => item.versionId, display: (item) => versions.find((version) => version.id === item.versionId)?.name ?? item.versionId },
    {
      key: 'status', header: 'State', kind: 'enum', options: installationStatuses, editable: canWrite, required: true, width: 10,
      text: (item) => item.status,
      display: (item) => <StatusBadge tone={statusTone(item.status)}>{label(item.status)}</StatusBadge>,
    },
    { key: 'usageState', header: 'Usage', kind: 'enum', options: usageStates, editable: canWrite, required: true, width: 9, text: (item) => item.usageState },
    { key: 'installedAt', header: 'Installed', kind: 'instant', width: 11, text: (item) => inputInstant(item.installedAt) ?? '', display: (item) => displayInstantDate(item.installedAt) },
    { key: 'lastUsedAt', header: 'Last used', kind: 'instant', editable: canWrite, width: 12, text: (item) => inputInstant(item.lastUsedAt) ?? '' },
    { key: 'removedAt', header: 'Removed', kind: 'instant', editable: canWrite, width: 12, text: (item) => inputInstant(item.removedAt) ?? '' },
  ]
}

function licenseColumns(products: readonly Product[], canWrite: boolean): GridColumn<License>[] {
  return [
    { key: 'name', header: 'License', kind: 'text', width: 16, text: (item) => item.name },
    { key: 'productId', header: 'Product', kind: 'text', width: 14, text: (item) => item.productId, display: (item) => products.find((product) => product.id === item.productId)?.name ?? item.productId },
    { key: 'versionId', header: 'Version scope', kind: 'text', width: 12, text: (item) => item.versionId ?? '', display: (item) => item.versionId ?? 'All versions' },
    { key: 'entitlementMetric', header: 'Metric', kind: 'text', width: 10, text: (item) => item.entitlementMetric, display: (item) => label(item.entitlementMetric) },
    { key: 'quantity', header: 'Seats', kind: 'number', minimum: 1, editable: canWrite, required: true, align: 'right', width: 7, text: (item) => String(item.quantity) },
    {
      key: 'status', header: 'Status', kind: 'enum', options: licenseStatuses, editable: canWrite, required: true, width: 9,
      text: (item) => item.status,
      display: (item) => <StatusBadge tone={statusTone(item.status)}>{label(item.status)}</StatusBadge>,
    },
    { key: 'startsOn', header: 'Starts on', kind: 'date', editable: canWrite, width: 10, text: (item) => (item.startsOn ?? '').slice(0, 10) },
    { key: 'expiresOn', header: 'Expires on', kind: 'date', editable: canWrite, width: 10, text: (item) => (item.expiresOn ?? '').slice(0, 10) },
    { key: 'documentIds', header: 'Documents', kind: 'number', align: 'right', width: 9, text: (item) => String(item.documentIds.length) },
  ]
}

function assignmentColumns(licenses: readonly License[], canWrite: boolean): GridColumn<Assignment>[] {
  return [
    { key: 'assigneeId', header: 'Assignee', kind: 'text', width: 15, text: (item) => item.assigneeId },
    { key: 'assigneeKind', header: 'Assignee type', kind: 'text', width: 11, text: (item) => item.assigneeKind, display: (item) => label(item.assigneeKind) },
    { key: 'licenseId', header: 'License', kind: 'text', width: 15, text: (item) => item.licenseId, display: (item) => licenses.find((license) => license.id === item.licenseId)?.name ?? item.licenseId },
    { key: 'seats', header: 'Seats', kind: 'number', align: 'right', width: 7, text: (item) => String(item.seats) },
    { key: 'usageState', header: 'Usage', kind: 'enum', options: usageStates, editable: canWrite, required: true, width: 9, text: (item) => item.usageState },
    { key: 'assignedAt', header: 'Assigned', kind: 'instant', width: 11, text: (item) => inputInstant(item.assignedAt) ?? '', display: (item) => displayInstantDate(item.assignedAt) },
    { key: 'lastUsedAt', header: 'Last used', kind: 'instant', editable: canWrite, width: 12, text: (item) => inputInstant(item.lastUsedAt) ?? '' },
    { key: 'endedAt', header: 'Ended', kind: 'instant', editable: canWrite, width: 12, text: (item) => inputInstant(item.endedAt) ?? '' },
  ]
}

type CreateKind = 'product' | 'version' | 'installation' | 'license' | 'assignment' | 'import'

const createTitles: Record<CreateKind, string> = {
  product: 'Define product',
  version: 'Define version',
  installation: 'Associate installation',
  license: 'Record license entitlement',
  assignment: 'Assign license seats',
  import: 'Import portable records',
}

export default function StackManager({ assets, csrfToken, permissions, onOpenHelp, identity }: StackManagerProps) {
  const canRead = permissions.includes('software.read')
  const canWrite = permissions.includes('software.write')
  const [snapshot, setSnapshot] = useState<Snapshot>(emptySnapshot)
  const [analytics, setAnalytics] = useState<Analytics>(emptyAnalytics)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')
  const [lastImport, setLastImport] = useState<StackImportResult | null>(null)
  const [installationAsset, setInstallationAsset] = useState<SearchableRecord[]>([])
  const [licenseDocuments, setLicenseDocuments] = useState<SearchableRecord[]>([])
  const [licensePurchaseOrder, setLicensePurchaseOrder] = useState<SearchableRecord[]>([])
  const [licenseVendor, setLicenseVendor] = useState<SearchableRecord[]>([])
  const [assignmentTarget, setAssignmentTarget] = useState<SearchableRecord[]>([])
  const [assignmentKind, setAssignmentKind] = useState('asset')
  const [preview, setPreview] = useState<ViewableDocument | null>(null)
  const [createKind, setCreateKind] = useState<CreateKind | null>(null)
  const errorRef = useRef<HTMLDivElement>(null)
  const productWrites = useWriteQueue()
  const versionWrites = useWriteQueue()
  const installationWrites = useWriteQueue()
  const licenseWrites = useWriteQueue()
  const assignmentWrites = useWriteQueue()

  const columns = useMemo(() => ({
    products: productColumns(canWrite),
    versions: versionColumns(snapshot.products, canWrite),
    installations: installationColumns(snapshot.versions, assets, canWrite),
    licenses: licenseColumns(snapshot.products, canWrite),
    assignments: assignmentColumns(snapshot.licenses, canWrite),
  }), [snapshot.products, snapshot.versions, snapshot.licenses, assets, canWrite])

  useEffect(() => { if (error) errorRef.current?.focus() }, [error])
  useEffect(() => {
    if (!canRead) return
    let active = true
    load().catch(() => { if (active) showError('Stack software and license records could not be loaded.') })
    return () => { active = false }
  // load is intentionally tied to the permission transition, not local form state.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [canRead])

  function showError(value: string) { setError(value); setMessage(''); queueMicrotask(() => errorRef.current?.focus()) }

  async function openDocument(id: string) {
    setError('')
    try {
      const value = await requestJSON(`/api/v1/blobs/${encodeURIComponent(id)}`)
      if (!isObject(value) || typeof value.id !== 'string' || typeof value.name !== 'string' || typeof value.mediaType !== 'string') throw new Error('invalid Vault blob')
      setPreview({ id: value.id, name: value.name, mediaType: value.mediaType })
    } catch (cause) {
      showError(cause instanceof ApiRequestError ? cause.message : 'The document could not be opened.')
    }
  }

  async function load() {
    const [snapshotResult, analyticsResult] = await Promise.allSettled([
      requestJSON('/api/v1/stack'),
      requestJSON('/api/v1/stack/analytics'),
    ])
    if (analyticsResult.status === 'fulfilled') setAnalytics(parseStackAnalytics(analyticsResult.value))
    if (snapshotResult.status === 'fulfilled') {
      setSnapshot(parseStackSnapshot(snapshotResult.value))
      return
    }
    throw snapshotResult.reason instanceof Error ? snapshotResult.reason : new Error('invalid Stack response')
  }

  async function create(event: FormEvent<HTMLFormElement>, kind: 'product' | 'version' | 'installation' | 'license' | 'assignment') {
    event.preventDefault()
    const form = event.currentTarget
    const values = new FormData(form)
    const configurations = {
      product: {
        path: '/api/v1/stack/products', message: 'Software product created.', body: () => ({
          name: text(values, 'name'), publisher: text(values, 'publisher'), category: text(values, 'category'), status: text(values, 'status'),
        }),
      },
      version: {
        path: '/api/v1/stack/versions', message: 'Software version created.', body: () => ({
          productId: text(values, 'productId'), name: text(values, 'name'), releasedOn: date(text(values, 'releasedOn')), status: text(values, 'status'),
        }),
      },
      installation: {
        path: '/api/v1/stack/installations', message: 'Software installation associated with the asset.', body: () => ({
          versionId: text(values, 'versionId'), assetId: installationAsset[0]?.id || text(values, 'assetId'), usageState: text(values, 'usageState'), installedAt: instant(text(values, 'installedAt')),
        }),
      },
      license: {
        path: '/api/v1/stack/licenses', message: 'License entitlement created.', body: () => ({
          productId: text(values, 'productId'), versionId: text(values, 'versionId'), name: text(values, 'name'), entitlementMetric: text(values, 'entitlementMetric'),
          quantity: Number(text(values, 'quantity')), startsOn: date(text(values, 'startsOn')), expiresOn: date(text(values, 'expiresOn')),
          vendorId: licenseVendor[0]?.id || text(values, 'vendorId'), purchaseOrderId: licensePurchaseOrder[0]?.id || text(values, 'purchaseOrderId'), contractId: text(values, 'contractId'),
          costRecordId: text(values, 'costRecordId'), documentIds: licenseDocuments.map((item) => item.id),
        }),
      },
      assignment: {
        path: '/api/v1/stack/assignments', message: 'License seats assigned.', body: () => ({
          licenseId: text(values, 'licenseId'), assigneeKind: text(values, 'assigneeKind') || assignmentKind, assigneeId: assignmentTarget[0]?.id || text(values, 'assigneeId'),
          seats: Number(text(values, 'seats')), usageState: text(values, 'usageState'), assignedAt: instant(text(values, 'assignedAt')),
        }),
      },
    } as const
    setBusy(kind); setError(''); setMessage('')
    try {
      const configuration = configurations[kind]
      await requestJSON(configuration.path, { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken }, body: JSON.stringify(configuration.body()) })
      await load(); form.reset(); setInstallationAsset([]); setLicenseDocuments([]); setLicensePurchaseOrder([]); setLicenseVendor([]); setAssignmentTarget([]); setMessage(configuration.message)
    } catch (cause) {
      showError(cause instanceof ApiRequestError || cause instanceof Error ? cause.message : 'The Stack record could not be saved.')
    } finally { setBusy('') }
  }

  function put(body: Record<string, unknown>): RequestInit {
    return { method: 'PUT', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken }, body: JSON.stringify(body) }
  }

  // Every grid shares this path: fan the pending edits out through the queue,
  // reload so the view reflects what the server actually stored, then report
  // per-record failures rather than a single opaque error.
  async function runGridWrites(queue: ReturnType<typeof useWriteQueue>, edits: readonly CellEdit[], transport: WriteTransport, success: string) {
    setError('')
    setMessage('')
    const report = await queue.run(tasksFromEdits(edits), transport)
    await load()
    if (report.failed > 0) {
      showError(summarizeReport(report))
      throw new Error(summarizeReport(report))
    }
    setMessage(success)
  }

  function saveProductEdits(edits: readonly CellEdit[]) {
    return runGridWrites(productWrites, edits, {
      writeRecord: async (task) => {
        const item = snapshot.products.find((candidate) => candidate.id === task.rowId)
        if (!item) throw new Error('The product is no longer loaded.')
        await requestJSON(`/api/v1/stack/products/${encodeURIComponent(item.id)}/status`, put(buildPayload(task.edits, columns.products, { status: item.status, revision: item.revision })))
      },
    }, 'Product status updated.')
  }

  function saveVersionEdits(edits: readonly CellEdit[]) {
    return runGridWrites(versionWrites, edits, {
      writeRecord: async (task) => {
        const item = snapshot.versions.find((candidate) => candidate.id === task.rowId)
        if (!item) throw new Error('The version is no longer loaded.')
        await requestJSON(`/api/v1/stack/versions/${encodeURIComponent(item.id)}/status`, put(buildPayload(task.edits, columns.versions, { status: item.status, revision: item.revision })))
      },
    }, 'Version status updated.')
  }

  function saveInstallationEdits(edits: readonly CellEdit[]) {
    return runGridWrites(installationWrites, edits, {
      writeRecord: async (task) => {
        const item = snapshot.installations.find((candidate) => candidate.id === task.rowId)
        if (!item) throw new Error('The installation is no longer loaded.')
        const payload = buildPayload(task.edits, columns.installations, {
          status: item.status, usageState: item.usageState, lastUsedAt: item.lastUsedAt, removedAt: item.removedAt, revision: item.revision,
        })
        if (payload.status !== 'removed') payload.removedAt = undefined
        await requestJSON(`/api/v1/stack/installations/${encodeURIComponent(item.id)}`, put(payload))
      },
    }, 'Installation state updated.')
  }

  function saveLicenseEdits(edits: readonly CellEdit[]) {
    return runGridWrites(licenseWrites, edits, {
      writeRecord: async (task) => {
        const item = snapshot.licenses.find((candidate) => candidate.id === task.rowId)
        if (!item) throw new Error('The license is no longer loaded.')
        const payload = buildPayload(task.edits, columns.licenses, {
          quantity: item.quantity, status: item.status, startsOn: item.startsOn, expiresOn: item.expiresOn, revision: item.revision,
        })
        await requestJSON(`/api/v1/stack/licenses/${encodeURIComponent(item.id)}/entitlement`, put(payload))
      },
    }, 'License entitlement updated.')
  }

  function saveAssignmentEdits(edits: readonly CellEdit[]) {
    return runGridWrites(assignmentWrites, edits, {
      // Usage and ending an assignment are separate endpoints, so a row that
      // changes both writes twice and carries the new revision forward.
      writeRecord: async (task) => {
        const item = snapshot.assignments.find((candidate) => candidate.id === task.rowId)
        if (!item) throw new Error('The assignment is no longer loaded.')
        const usageEdits = task.edits.filter((edit) => edit.columnKey === 'usageState' || edit.columnKey === 'lastUsedAt')
        const endEdits = task.edits.filter((edit) => edit.columnKey === 'endedAt')
        let revision = item.revision
        if (usageEdits.length > 0) {
          const payload = buildPayload(usageEdits, columns.assignments, { usageState: item.usageState, lastUsedAt: item.lastUsedAt, revision })
          revision = revisionOf(await requestJSON(`/api/v1/stack/assignments/${encodeURIComponent(item.id)}/usage`, put(payload))) ?? revision
        }
        if (endEdits.length > 0) {
          const payload = buildPayload(endEdits, columns.assignments, { revision })
          await requestJSON(`/api/v1/stack/assignments/${encodeURIComponent(item.id)}/end`, put(payload))
        }
      },
    }, 'Assignment updated.')
  }

  async function importRecords(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const values = new FormData(event.currentTarget)
    setBusy('import'); setError(''); setMessage(''); setLastImport(null)
    try {
      const parsed: unknown = JSON.parse(text(values, 'records'))
      const records = Array.isArray(parsed) ? parsed : isObject(parsed) && Array.isArray(parsed.records) ? parsed.records : null
      if (!records) throw new Error('Paste an exported records array or an object containing records.')
      const result = parseStackImportResult(await requestJSON('/api/v1/stack/exchange/import', {
        method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({ sourceSystemId: text(values, 'sourceSystemId'), records }),
      }))
      setLastImport(result)
      await load()
      const prefix = result.replay ? 'Import replay complete' : result.status === 'holding' ? 'Import is holding for dependencies' : 'Import complete'
      setMessage(`${prefix}: ${result.created} created, ${result.unchanged} unchanged, and ${result.holding} holding. Receipt ${result.packageId}.`)
    } catch (cause) {
      if (cause instanceof ApiRequestError) {
        const result = importResultFromError(cause)
        if (result) {
          setLastImport(result)
          showError(`${cause.message} Receipt ${result.packageId} records ${result.created} created and ${result.unchanged} unchanged. Retry the exact JSON to resume.`)
        } else showError(cause.message)
      } else showError(cause instanceof Error ? cause.message : 'The portable records could not be imported.')
    }
    finally { setBusy('') }
  }

  if (!canRead) return <section aria-labelledby="stack-heading" className={`${panelClass} p-5 sm:p-6`} data-feature="software.licenses" data-requirement="REQ-STACK-001"><div className="flex flex-wrap items-start justify-between gap-4"><div><h2 className="text-2xl font-semibold" id="stack-heading">Stack — Software and licenses</h2><p className="mt-2 text-steward-mist-muted">Your role does not include permission to view software inventory.</p></div>{onOpenHelp && <button className={secondaryButtonClass} onClick={onOpenHelp} type="button">Stack help</button>}</div></section>

  return <section aria-labelledby="stack-heading" className={`${panelClass} min-w-0 max-w-full p-4 sm:p-5`} data-feature="software.licenses" data-requirement="REQ-STACK-001">
    <ProductHeader
      actions={onOpenHelp ? <button className={secondaryButtonClass} onClick={onOpenHelp} type="button">Stack help</button> : undefined}
      description="Connect installed versions to Atlas assets, preserve purchased entitlement references, assign seats, and review explicit license conditions."
      headingId="stack-heading"
      kicker="Stack"
      title="Software inventory and license management"
    />
    {error && <div className="mt-4 rounded-lg border border-steward-danger/50 bg-steward-danger/15 p-3 text-[#ffccd1]" ref={errorRef} role="alert" tabIndex={-1}>{error}</div>}
    {message && <p className="mt-4 rounded-lg border border-steward-success/50 bg-steward-success/15 p-3 text-[#aaf0c6]" role="status">{message}</p>}
    {lastImport && <section aria-labelledby="stack-import-receipt-heading" className={`${subpanelClass} mt-4 min-w-0 p-4`}>
      <h3 className="font-semibold" id="stack-import-receipt-heading">Latest import receipt</h3>
      <p className="mt-2 break-all font-mono text-xs text-steward-mist-muted">{lastImport.packageId}</p>
      <p className="mt-2 text-sm text-steward-mist-muted">{label(lastImport.status)} · {lastImport.created} created · {lastImport.unchanged} unchanged · {lastImport.holding} holding{lastImport.replay ? ' · exact replay' : ''}</p>
      {lastImport.errorCode && <p className="mt-2 text-sm text-[#ffccd1]">Failure code: <code>{lastImport.errorCode}</code></p>}
      {lastImport.records.length > 0 && <div aria-label="Latest Stack import outcomes" className={`${tableWrapClass} mt-3`} role="region" tabIndex={0}><table className="min-w-[40rem] w-full text-left text-sm"><thead><tr><th className="p-3" scope="col">Record</th><th className="p-3" scope="col">Outcome</th><th className="p-3" scope="col">Ownership</th></tr></thead><tbody>{lastImport.records.map((outcome) => <tr className="border-t border-white/[0.07]" key={`${outcome.type}:${outcome.id}`}><th className="p-3 break-all font-mono text-xs" scope="row">{outcome.type}:{outcome.id}</th><td className="p-3">{label(outcome.status)}</td><td className="p-3">{outcome.writeLocked ? 'Write locked until claimed' : outcome.status === 'holding' ? 'Not imported' : 'No import lock'}</td></tr>)}</tbody></table></div>}
      {lastImport.pendingOwnership.length > 0 && <details className="mt-3 rounded-lg border border-steward-warning/40 bg-steward-warning/10 p-3" open>
        <summary className="cursor-pointer font-semibold">Pending Guard ownership locks ({lastImport.pendingOwnership.length})</summary>
        <p className="mt-2 text-sm text-steward-mist-muted">Guard recorded these ownership states before the provider outcome became durable. Review them, then retry the exact import JSON to resume safely.</p>
        <div aria-label="Pending Stack import ownership locks" className={`${tableWrapClass} mt-3`} role="region" tabIndex={0}><table className="min-w-[32rem] w-full text-left text-sm"><thead><tr><th className="p-3" scope="col">Record</th><th className="p-3" scope="col">Guard state</th></tr></thead><tbody>{lastImport.pendingOwnership.map((ownership) => <tr className="border-t border-white/[0.07]" key={`${ownership.type}:${ownership.id}`}><th className="p-3 break-all font-mono text-xs" scope="row">{ownership.type}:{ownership.id}</th><td className="p-3">{ownership.writeLocked ? 'Write locked until claimed' : 'Ownership recorded; writes are not locked'}</td></tr>)}</tbody></table></div>
      </details>}
    </section>}

    <section aria-labelledby="stack-analytics-heading" className="mt-6">
      <div className="flex flex-wrap items-end justify-between gap-3"><div><p className="text-sm font-semibold text-steward-teal">Live calculation</p><h3 className="mt-1 text-xl font-semibold" id="stack-analytics-heading">Entitlement and compliance summary</h3></div><p className="text-sm text-steward-mist-muted">Expiration window: {analytics.expiringWithinDays} days</p></div>
      <dl className="mt-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
        <Metric label="Products" value={analytics.products} />
        <Metric label="Active installations" value={analytics.activeInstallations} />
        <Metric label="Active licenses" value={analytics.activeLicenses} />
        <Metric label="Entitled seats" value={analytics.entitledQuantity} />
        <Metric label="Assigned seats" value={analytics.assignedQuantity} />
      </dl>
      {analytics.complianceConditions.length === 0 ? <p className={`${subpanelClass} mt-4 p-4 text-sm text-steward-mist-muted`} role="status">No expiration, assignment, usage, or missing-license conditions are active.</p> : <ul aria-label="Stack compliance conditions" className="mt-4 grid gap-3">
        {analytics.complianceConditions.map((condition, index) => <li className={`${subpanelClass} min-w-0 p-4`} key={`${condition.code}:${condition.licenseId ?? condition.assetId ?? condition.productId}:${index}`}>
          <div className="flex flex-wrap items-center gap-2"><strong>{label(condition.code)}</strong><span className={condition.severity === 'critical' ? 'text-[#ffccd1]' : condition.severity === 'warning' ? 'text-[#ffd08a]' : 'text-[#a9c7ff]'}>{label(condition.severity)}</span></div>
          <p className="mt-2 text-sm leading-6 text-steward-mist-muted">{condition.humanReadableState}</p>
          <p className="mt-2 break-all text-xs text-steward-slate">Product {condition.productId}{condition.licenseId ? ` · License ${condition.licenseId}` : ''}{condition.assetId ? ` · Asset ${condition.assetId}` : ''}</p>
        </li>)}
      </ul>}
    </section>

    {canWrite && <section aria-labelledby="stack-create-heading" className="mt-8">
      <h3 className="text-xl font-semibold" id="stack-create-heading">Add and connect records</h3>
      <p className="mt-2 text-sm leading-6 text-steward-mist-muted">Create dependencies first. Every reference is revalidated by its owning feature before Stack persists the relationship.</p>
      <div className="mt-4 flex min-w-0 flex-wrap gap-2">
        {(Object.keys(createTitles) as CreateKind[]).map((kind) => <button className={secondaryButtonClass} key={kind} onClick={() => setCreateKind(kind)} type="button">{createTitles[kind]}</button>)}
      </div>
    </section>}

    {canWrite && <Drawer kicker="Stack" onClose={() => setCreateKind(null)} open={createKind !== null} title={createKind ? createTitles[createKind] : ''} wide>
      {createKind === 'product' && <form className="grid gap-3" onSubmit={(event) => create(event, 'product')}><Input label="Product name" name="name" required /><Input label="Publisher" name="publisher" required /><Input label="Category" name="category" /><Select label="Status" name="status" options={['active', 'retired']} /><Submit busy={busy === 'product'} label="Create product" /></form>}
      {createKind === 'version' && <form className="grid gap-3" onSubmit={(event) => create(event, 'version')}><Select label="Product" name="productId" options={snapshot.products.filter((item) => item.status !== 'retired').map((item) => [item.id, `${item.publisher} · ${item.name}`])} required /><Input label="Version name" name="name" required /><Input label="Released on" name="releasedOn" type="date" /><Select label="Status" name="status" options={['active', 'unsupported', 'retired']} /><Submit busy={busy === 'version'} label="Create version" /></form>}
      {createKind === 'installation' && <form className="grid gap-3" onSubmit={(event) => create(event, 'installation')}><Select label="Software version" name="versionId" options={snapshot.versions.filter((item) => item.status !== 'retired').map((item) => [item.id, versionName(item, snapshot.products)])} required /><RecordSearchPicker help="Search Atlas computers and lab stations." kind="asset" label="Atlas asset" multiple={false} onChange={setInstallationAsset} selected={installationAsset} />{installationAsset.length === 0 && assets.length > 0 ? <Select label="Or choose a loaded asset" name="assetId" options={assets.slice(0, 80).map((item) => [item.id, item.name])} /> : null}<Select label="Usage state" name="usageState" options={['unknown', 'used', 'unused']} /><Input label="Installed at" name="installedAt" type="datetime-local" required /><Submit busy={busy === 'installation'} label="Associate installation" /></form>}
      {createKind === 'license' && <form className="grid gap-3" onSubmit={(event) => create(event, 'license')}><Select label="Product" name="productId" options={snapshot.products.filter((item) => item.status === 'active').map((item) => [item.id, `${item.publisher} · ${item.name}`])} required /><Select emptyLabel="All product versions" label="Version scope" name="versionId" options={snapshot.versions.filter((item) => item.status !== 'retired').map((item) => [item.id, versionName(item, snapshot.products)])} /><Input label="License name" name="name" required /><Select label="Entitlement metric" name="entitlementMetric" options={['device', 'user', 'concurrent', 'site', 'enterprise']} /><Input help="Volume quantity for the purchased entitlement." label="Quantity" min="1" name="quantity" required type="number" /><div className="grid gap-3 sm:grid-cols-2"><Input label="Starts on" name="startsOn" type="date" /><Input label="Expires on" name="expiresOn" type="date" /></div><RecordSearchPicker browseHref="#workspace-ledger" browseLabel="Open vendors" kind="vendor" label="Vendor" multiple={false} name="vendorId" onChange={setLicenseVendor} selected={licenseVendor} /><RecordSearchPicker browseHref="#workspace-ledger" browseLabel="Open purchase orders" kind="purchase-order" label="Purchase order" multiple={false} name="purchaseOrderId" onChange={setLicensePurchaseOrder} selected={licensePurchaseOrder} /><Input help="Optional Ledger reference." label="Contract ID" name="contractId" /><Input help="Optional Ledger current-cost reference." label="Cost record ID" name="costRecordId" /><RecordSearchPicker help="Search Vault contracts and quotes." kind="document" label="License documents" onChange={setLicenseDocuments} selected={licenseDocuments} /><Submit busy={busy === 'license'} label="Create license" /></form>}
      {createKind === 'assignment' && <form className="grid gap-3" onSubmit={(event) => create(event, 'assignment')}><Select label="License" name="licenseId" options={snapshot.licenses.filter((item) => item.status === 'active').map((item) => [item.id, `${item.name} · ${item.quantity} ${label(item.entitlementMetric)}`])} required /><Select label="Assignee type" name="assigneeKind" onChange={(value) => { setAssignmentKind(value); setAssignmentTarget([]) }} options={['asset', 'identity', 'department', 'site', 'room']} value={assignmentKind} />{assignmentKind === 'asset' ? <RecordSearchPicker kind="asset" label="Assignee asset" multiple={false} onChange={setAssignmentTarget} selected={assignmentTarget} /> : assignmentKind === 'room' ? <RecordSearchPicker browseHref="#workspace-people" browseLabel="Open rooms" kind="room" label="Assignee lab or room" multiple={false} onChange={setAssignmentTarget} selected={assignmentTarget} /> : assignmentKind === 'department' ? <RecordSearchPicker browseHref="#workspace-people" browseLabel="Open departments" kind="department" label="Assignee department" multiple={false} onChange={setAssignmentTarget} selected={assignmentTarget} /> : assignmentKind === 'site' ? <RecordSearchPicker browseHref="#workspace-people" browseLabel="Open sites" kind="site" label="Assignee site" multiple={false} onChange={setAssignmentTarget} selected={assignmentTarget} /> : assignmentKind === 'identity' ? <RecordSearchPicker browseHref="#workspace-people" browseLabel="Open directory" kind="identity" label="Assignee person" multiple={false} onChange={setAssignmentTarget} selected={assignmentTarget} /> : <Input help="Device licenses use assets, user licenses use identities, site licenses use sites, and lab packs can use rooms." label="Assignee ID" name="assigneeId" required />}<Input label="Seats" min="1" name="seats" required type="number" /><Select label="Usage state" name="usageState" options={['unknown', 'used', 'unused']} /><Input label="Assigned at" name="assignedAt" type="datetime-local" required /><Submit busy={busy === 'assignment'} label="Assign seats" /></form>}
      {createKind === 'import' && <form className="grid gap-3" onSubmit={importRecords}><Input help="Stable source identity used for idempotency." label="Source system ID" name="sourceSystemId" required /><TextArea help="Paste the records array or the complete object returned by Stack export. Review content before importing." label="Exported JSON" maxLength={10000000} name="records" required /><Submit busy={busy === 'import'} label="Import records" /></form>}
    </Drawer>}

    <section aria-labelledby="stack-records-heading" className="mt-8 min-w-0">
      <div className="flex flex-wrap items-center justify-between gap-3"><div><h3 className="text-xl font-semibold" id="stack-records-heading">Current software and entitlements</h3><p className="mt-1 text-sm text-steward-mist-muted">Showing {snapshot.assignments.length} of {snapshot.assignmentTotal ?? snapshot.assignments.length} assignments and {snapshot.installations.length} of {snapshot.installationTotal ?? snapshot.installations.length} installations.</p></div><a className={secondaryButtonClass} href="/api/v1/stack/exchange">Export portable JSON</a></div>
      {canWrite && <p className="mt-3 text-sm leading-6 text-steward-mist-muted">Each grid edits only the fields its Stack endpoint accepts. Names, publishers, entitlement metrics, and seat counts stay immutable after creation, so those columns are read-only. Ctrl+C and Ctrl+V move a block to and from a spreadsheet, Ctrl+D fills down, and Ctrl+Z undoes.</p>}
      <div className="mt-4 grid min-w-0 gap-6">
        <div className="min-w-0">
          <h4 className="mb-2 font-semibold" id="stack-products-heading">Products</h4>
          <DataGrid
            columns={columns.products}
            editable={canWrite}
            emptyMessage="No software products yet."
            identity={identity}
            isRowEditable={(item) => item.status !== 'retired'}
            label="Software products"
            maximumBodyHeight="24rem"
            onSaveEdits={canWrite ? saveProductEdits : undefined}
            rowId={(item) => item.id}
            rowLabel={(item) => item.name}
            rowMessage={(item) => productWrites.rowMessage(item.id)}
            rowState={(item) => productWrites.rowState(item.id)}
            rows={snapshot.products}
            viewId="stack-products"
          />
        </div>
        <div className="min-w-0">
          <h4 className="mb-2 font-semibold" id="stack-versions-heading">Versions</h4>
          <DataGrid
            columns={columns.versions}
            editable={canWrite}
            emptyMessage="No software versions yet."
            identity={identity}
            isRowEditable={(item) => item.status !== 'retired'}
            label="Software versions"
            maximumBodyHeight="24rem"
            onSaveEdits={canWrite ? saveVersionEdits : undefined}
            rowId={(item) => item.id}
            rowLabel={(item) => item.name}
            rowMessage={(item) => versionWrites.rowMessage(item.id)}
            rowState={(item) => versionWrites.rowState(item.id)}
            rows={snapshot.versions}
            viewId="stack-versions"
          />
        </div>
        <div className="min-w-0">
          <h4 className="mb-2 font-semibold" id="stack-installations-heading">Installations</h4>
          <DataGrid
            columns={columns.installations}
            editable={canWrite}
            emptyMessage="No installations are associated with assets yet."
            identity={identity}
            isRowEditable={(item) => item.status === 'installed'}
            label="Software installations"
            maximumBodyHeight="24rem"
            onSaveEdits={canWrite ? saveInstallationEdits : undefined}
            rowId={(item) => item.id}
            rowLabel={(item) => item.assetId}
            rowMessage={(item) => installationWrites.rowMessage(item.id)}
            rowState={(item) => installationWrites.rowState(item.id)}
            rows={snapshot.installations}
            viewId="stack-installations"
          />
        </div>
        <div className="min-w-0">
          <h4 className="mb-2 font-semibold" id="stack-licenses-heading">Licenses</h4>
          <DataGrid
            bulkActions={(selected) => {
              const documentIds = [...new Set(selected.flatMap((item) => item.documentIds))]
              if (documentIds.length === 0) return null
              return <button className={`${plainButtonClass} min-h-8 px-2 py-1 text-xs`} onClick={() => void openDocument(documentIds[0])} type="button">View first document</button>
            }}
            columns={columns.licenses}
            editable={canWrite}
            emptyMessage="No license entitlements yet."
            identity={identity}
            isRowEditable={(item) => item.status !== 'retired'}
            label="License entitlements"
            maximumBodyHeight="24rem"
            onSaveEdits={canWrite ? saveLicenseEdits : undefined}
            rowId={(item) => item.id}
            rowLabel={(item) => item.name}
            rowMessage={(item) => licenseWrites.rowMessage(item.id)}
            rowState={(item) => licenseWrites.rowState(item.id)}
            rows={snapshot.licenses}
            selectable
            viewId="stack-licenses"
          />
        </div>
        <div className="min-w-0">
          <h4 className="mb-2 font-semibold" id="stack-assignments-heading">Seat assignments</h4>
          <DataGrid
            columns={columns.assignments}
            editable={canWrite}
            emptyMessage="No license seats are assigned yet."
            identity={identity}
            isRowEditable={(item) => !item.endedAt}
            label="Seat assignments"
            maximumBodyHeight="24rem"
            onSaveEdits={canWrite ? saveAssignmentEdits : undefined}
            rowId={(item) => item.id}
            rowLabel={(item) => item.assigneeId}
            rowMessage={(item) => assignmentWrites.rowMessage(item.id)}
            rowState={(item) => assignmentWrites.rowState(item.id)}
            rows={snapshot.assignments}
            viewId="stack-assignments"
          />
        </div>
      </div>
    </section>
    {preview && <div className="mt-6"><DocumentViewer csrfToken={csrfToken} document={preview} onClose={() => setPreview(null)} /></div>}
  </section>
}

function versionName(version: Version, products: Product[]) { return `${products.find((item) => item.id === version.productId)?.name ?? version.productId} · ${version.name}` }
function Metric({ label: valueLabel, value }: { label: string; value: number }) { return <div className={`${subpanelClass} p-4`}><dt className="text-sm text-steward-mist-muted">{valueLabel}</dt><dd className="mt-1 text-2xl font-semibold">{value}</dd></div> }
function Submit({ busy, label: value }: { busy: boolean; label: string }) { return <button className={buttonClass} disabled={busy} type="submit">{busy ? 'Saving…' : value}</button> }

function Field({ children, help, id, label: value }: { children: ReactNode; help?: string; id: string; label: string }) {
  const helpID = `${id}-help`
  return <div className="min-w-0"><label className="text-sm font-semibold text-steward-mist" htmlFor={id}>{value}</label>{children}{help && <span className="mt-1 block text-xs font-normal leading-5 text-steward-mist-muted" id={helpID}>{help}</span>}</div>
}
function Input({ help, label: value, name, ...props }: { help?: string; label: string; name: string } & InputHTMLAttributes<HTMLInputElement>) {
  const generated = useId(); const id = `stack-${name}-${generated}`; const helpID = help ? `${id}-help` : undefined
  return <Field help={help} id={id} label={value}><input {...props} aria-describedby={helpID} className={inputClass} id={id} key={String(props.defaultValue ?? '')} name={name} /></Field>
}
function Select({ emptyLabel, label: value, name, onChange, options, required, value: selected }: { emptyLabel?: string; label: string; name: string; onChange?: (value: string) => void; options: readonly (string | readonly [string, string])[]; required?: boolean; value?: string }) {
  const generated = useId(); const id = `stack-${name}-${generated}`
  return <Field id={id} label={value}><select className={inputClass} defaultValue={onChange ? undefined : selected} id={id} key={selected ?? ''} name={name} onChange={onChange ? (event) => onChange(event.target.value) : undefined} required={required} value={onChange ? selected : undefined}>{emptyLabel !== undefined && <option value="">{emptyLabel}</option>}{options.map((option) => { const [optionValue, optionLabel] = typeof option === 'string' ? [option, label(option)] : option; return <option key={optionValue} value={optionValue}>{optionLabel}</option> })}</select></Field>
}
function TextArea({ help, label: value, maxLength, name, required }: { help?: string; label: string; maxLength: number; name: string; required?: boolean }) {
  const generated = useId(); const id = `stack-${name}-${generated}`; const helpID = help ? `${id}-help` : undefined
  return <Field help={help} id={id} label={value}><textarea aria-describedby={helpID} className={`${inputClass} min-h-36 font-mono text-xs`} id={id} maxLength={maxLength} name={name} required={required} /></Field>
}