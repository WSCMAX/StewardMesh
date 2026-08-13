import { type FormEvent, type InputHTMLAttributes, type ReactNode, useEffect, useId, useRef, useState } from 'react'
import { ApiRequestError, requestJSON } from './api'
import type { Asset } from './AtlasInventory'
import { buttonClass, inputClass, panelClass, secondaryButtonClass, subpanelClass, tableWrapClass } from './ui'

// Requirement: REQ-STACK-001. Feature: software.licenses.

type Product = { id: string; name: string; publisher: string; category?: string; status: string; revision: number }
type Version = { id: string; productId: string; name: string; releasedOn?: string; status: string; revision: number }
type Installation = { id: string; versionId: string; assetId: string; status: string; usageState: string; installedAt: string; lastUsedAt?: string; removedAt?: string; revision: number }
type License = { id: string; productId: string; versionId?: string; name: string; entitlementMetric: string; quantity: number; status: string; startsOn?: string; expiresOn?: string; vendorId?: string; purchaseOrderId?: string; contractId?: string; costRecordId?: string; documentIds: string[]; revision: number }
type Assignment = { id: string; licenseId: string; assigneeKind: string; assigneeId: string; seats: number; usageState: string; assignedAt: string; lastUsedAt?: string; endedAt?: string; revision: number }
type Snapshot = { products: Product[]; versions: Version[]; installations: Installation[]; licenses: License[]; assignments: Assignment[] }
type Condition = { code: string; severity: string; productId: string; versionId?: string; licenseId?: string; assetId?: string; entitledQuantity?: number; assignedQuantity?: number; underusedQuantity?: number; daysUntilExpiry?: number; humanReadableState: string }
type Analytics = { asOf: string; expiringWithinDays: number; products: number; activeInstallations: number; activeLicenses: number; entitledQuantity: number; assignedQuantity: number; underusedAssignments: number; complianceConditions: Condition[] }
type StackManagerProps = { assets: readonly Asset[]; csrfToken: string; permissions: readonly string[]; onOpenHelp?: () => void }

const emptySnapshot: Snapshot = { products: [], versions: [], installations: [], licenses: [], assignments: [] }
const emptyAnalytics: Analytics = { asOf: '', expiringWithinDays: 90, products: 0, activeInstallations: 0, activeLicenses: 0, entitledQuantity: 0, assignedQuantity: 0, underusedAssignments: 0, complianceConditions: [] }

function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null }
function hasID(value: unknown): value is Record<string, unknown> { return isObject(value) && typeof value.id === 'string' && value.id.length > 0 }
function validItems(value: unknown, validate: (item: unknown) => boolean) { return Array.isArray(value) && value.every(validate) }
function validCount(value: unknown) { return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0 }
function validPositive(value: unknown) { return typeof value === 'number' && Number.isSafeInteger(value) && value > 0 }
function optionalString(value: unknown) { return value === undefined || typeof value === 'string' }
function optionalCount(value: unknown) { return value === undefined || validCount(value) }
function validStringItems(value: unknown) { return Array.isArray(value) && value.every((item) => typeof item === 'string') }
function validInstantValue(value: unknown) { return typeof value === 'string' && !Number.isNaN(Date.parse(value)) }
function optionalInstantValue(value: unknown) { return value === undefined || validInstantValue(value) }

export function parseStackSnapshot(value: unknown): Snapshot {
  if (!isObject(value)
    || !validItems(value.products, (item) => hasID(item) && typeof item.name === 'string' && typeof item.publisher === 'string' && optionalString(item.category) && typeof item.status === 'string' && validPositive(item.revision))
    || !validItems(value.versions, (item) => hasID(item) && typeof item.productId === 'string' && typeof item.name === 'string' && optionalInstantValue(item.releasedOn) && typeof item.status === 'string' && validPositive(item.revision))
    || !validItems(value.installations, (item) => hasID(item) && typeof item.versionId === 'string' && typeof item.assetId === 'string' && typeof item.status === 'string' && typeof item.usageState === 'string' && validInstantValue(item.installedAt) && optionalInstantValue(item.lastUsedAt) && optionalInstantValue(item.removedAt) && validPositive(item.revision))
    || !validItems(value.licenses, (item) => hasID(item) && typeof item.productId === 'string' && optionalString(item.versionId) && typeof item.name === 'string' && typeof item.entitlementMetric === 'string' && validPositive(item.quantity) && typeof item.status === 'string' && optionalInstantValue(item.startsOn) && optionalInstantValue(item.expiresOn) && validStringItems(item.documentIds) && validPositive(item.revision))
    || !validItems(value.assignments, (item) => hasID(item) && typeof item.licenseId === 'string' && typeof item.assigneeKind === 'string' && typeof item.assigneeId === 'string' && validPositive(item.seats) && typeof item.usageState === 'string' && validInstantValue(item.assignedAt) && optionalInstantValue(item.lastUsedAt) && optionalInstantValue(item.endedAt) && validPositive(item.revision))) {
    throw new Error('invalid Stack response')
  }
  return value as Snapshot
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

function text(values: FormData, key: string) { return String(values.get(key) ?? '').trim() }
function date(value: string) { return value ? `${value}T00:00:00Z` : undefined }
function instant(value: string) { return value ? new Date(value).toISOString() : undefined }
function list(value: string) { return value.split(',').map((item) => item.trim()).filter(Boolean) }
function label(value: string) { return value.replaceAll('_', ' ').replace(/\b\w/g, (letter) => letter.toUpperCase()) }
function displayDate(value?: string) { return value ? new Intl.DateTimeFormat(undefined, { dateStyle: 'medium' }).format(new Date(value)) : 'Not set' }
function inputInstant(value?: string) {
  if (!value) return undefined
  const parsed = new Date(value)
  return new Date(parsed.getTime() - parsed.getTimezoneOffset() * 60000).toISOString().slice(0, 16)
}

export default function StackManager({ assets, csrfToken, permissions, onOpenHelp }: StackManagerProps) {
  const canRead = permissions.includes('software.read')
  const canWrite = permissions.includes('software.write')
  const [snapshot, setSnapshot] = useState<Snapshot>(emptySnapshot)
  const [analytics, setAnalytics] = useState<Analytics>(emptyAnalytics)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')
  const errorRef = useRef<HTMLDivElement>(null)

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

  async function load() {
    const [snapshotValue, analyticsValue] = await Promise.all([
      requestJSON('/api/v1/stack'), requestJSON('/api/v1/stack/analytics'),
    ])
    setSnapshot(parseStackSnapshot(snapshotValue))
    setAnalytics(parseStackAnalytics(analyticsValue))
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
          versionId: text(values, 'versionId'), assetId: text(values, 'assetId'), usageState: text(values, 'usageState'), installedAt: instant(text(values, 'installedAt')),
        }),
      },
      license: {
        path: '/api/v1/stack/licenses', message: 'License entitlement created.', body: () => ({
          productId: text(values, 'productId'), versionId: text(values, 'versionId'), name: text(values, 'name'), entitlementMetric: text(values, 'entitlementMetric'),
          quantity: Number(text(values, 'quantity')), startsOn: date(text(values, 'startsOn')), expiresOn: date(text(values, 'expiresOn')),
          vendorId: text(values, 'vendorId'), purchaseOrderId: text(values, 'purchaseOrderId'), contractId: text(values, 'contractId'),
          costRecordId: text(values, 'costRecordId'), documentIds: list(text(values, 'documentIds')),
        }),
      },
      assignment: {
        path: '/api/v1/stack/assignments', message: 'License seats assigned.', body: () => ({
          licenseId: text(values, 'licenseId'), assigneeKind: text(values, 'assigneeKind'), assigneeId: text(values, 'assigneeId'),
          seats: Number(text(values, 'seats')), usageState: text(values, 'usageState'), assignedAt: instant(text(values, 'assignedAt')),
        }),
      },
    } as const
    setBusy(kind); setError(''); setMessage('')
    try {
      const configuration = configurations[kind]
      await requestJSON(configuration.path, { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken }, body: JSON.stringify(configuration.body()) })
      await load(); form.reset(); setMessage(configuration.message)
    } catch (cause) {
      showError(cause instanceof ApiRequestError || cause instanceof Error ? cause.message : 'The Stack record could not be saved.')
    } finally { setBusy('') }
  }

  async function updateUsage(event: FormEvent<HTMLFormElement>, assignment: Assignment) {
    event.preventDefault()
    const values = new FormData(event.currentTarget)
    setBusy(`usage:${assignment.id}`); setError(''); setMessage('')
    try {
      await requestJSON(`/api/v1/stack/assignments/${encodeURIComponent(assignment.id)}/usage`, {
        method: 'PUT', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken }, body: JSON.stringify({
          usageState: text(values, 'usageState'), lastUsedAt: instant(text(values, 'lastUsedAt')), revision: assignment.revision,
        }),
      })
      await load(); setMessage('Assignment usage updated.')
    } catch (cause) { showError(cause instanceof ApiRequestError || cause instanceof Error ? cause.message : 'Usage could not be updated.') }
    finally { setBusy('') }
  }

  async function updateRecord(event: FormEvent<HTMLFormElement>, path: string, body: (values: FormData) => object, success: string, key: string) {
    event.preventDefault()
    const values = new FormData(event.currentTarget)
    setBusy(key); setError(''); setMessage('')
    try {
      await requestJSON(path, { method: 'PUT', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken }, body: JSON.stringify(body(values)) })
      await load(); setMessage(success)
    } catch (cause) { showError(cause instanceof ApiRequestError || cause instanceof Error ? cause.message : 'The Stack record could not be updated.') }
    finally { setBusy('') }
  }

  async function importRecords(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const values = new FormData(event.currentTarget)
    setBusy('import'); setError(''); setMessage('')
    try {
      const parsed: unknown = JSON.parse(text(values, 'records'))
      const records = Array.isArray(parsed) ? parsed : isObject(parsed) && Array.isArray(parsed.records) ? parsed.records : null
      if (!records) throw new Error('Paste an exported records array or an object containing records.')
      const result = await requestJSON('/api/v1/stack/exchange/import', {
        method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({ sourceSystemId: text(values, 'sourceSystemId'), records }),
      })
      if (!isObject(result) || !validCount(result.created) || !validCount(result.unchanged)) throw new Error('The import response was invalid.')
      await load(); setMessage(`Import complete: ${result.created} created and ${result.unchanged} unchanged.`)
    } catch (cause) { showError(cause instanceof ApiRequestError || cause instanceof Error ? cause.message : 'The portable records could not be imported.') }
    finally { setBusy('') }
  }

  if (!canRead) return <section aria-labelledby="stack-heading" className={`${panelClass} p-5 sm:p-6`} data-feature="software.licenses" data-requirement="REQ-STACK-001"><div className="flex flex-wrap items-start justify-between gap-4"><div><h2 className="text-2xl font-semibold" id="stack-heading">Stack — Software and licenses</h2><p className="mt-2 text-steward-mist-muted">Your role does not include permission to view software inventory.</p></div>{onOpenHelp && <button className={secondaryButtonClass} onClick={onOpenHelp} type="button">Stack help</button>}</div></section>

  return <section aria-labelledby="stack-heading" className={`${panelClass} min-w-0 max-w-full p-4 sm:p-6`} data-feature="software.licenses" data-requirement="REQ-STACK-001">
    <div className="flex flex-wrap items-start justify-between gap-4">
      <div><p className="text-sm font-semibold text-steward-teal">Stack</p><h2 className="mt-2 text-2xl font-semibold" id="stack-heading">Software inventory and license management</h2><p className="mt-2 max-w-4xl leading-7 text-steward-mist-muted">Connect installed versions to Atlas assets, preserve purchased entitlement references, assign seats, and review explicit license conditions.</p></div>
      {onOpenHelp && <button className={secondaryButtonClass} onClick={onOpenHelp} type="button">Stack help</button>}
    </div>
    {error && <div className="mt-4 rounded-lg border border-steward-danger/50 bg-steward-danger/15 p-3 text-[#ffccd1]" ref={errorRef} role="alert" tabIndex={-1}>{error}</div>}
    {message && <p className="mt-4 rounded-lg border border-steward-success/50 bg-steward-success/15 p-3 text-[#aaf0c6]" role="status">{message}</p>}

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
      <div className="mt-4 grid min-w-0 gap-3 lg:grid-cols-2">
        <CreatePanel title="Define product"><form className="grid gap-3" onSubmit={(event) => create(event, 'product')}><Input label="Product name" name="name" required /><Input label="Publisher" name="publisher" required /><Input label="Category" name="category" /><Select label="Status" name="status" options={['active', 'retired']} /><Submit busy={busy === 'product'} label="Create product" /></form></CreatePanel>
        <CreatePanel title="Define version"><form className="grid gap-3" onSubmit={(event) => create(event, 'version')}><Select label="Product" name="productId" options={snapshot.products.filter((item) => item.status !== 'retired').map((item) => [item.id, `${item.publisher} · ${item.name}`])} required /><Input label="Version name" name="name" required /><Input label="Released on" name="releasedOn" type="date" /><Select label="Status" name="status" options={['active', 'unsupported', 'retired']} /><Submit busy={busy === 'version'} label="Create version" /></form></CreatePanel>
        <CreatePanel title="Associate installation"><form className="grid gap-3" onSubmit={(event) => create(event, 'installation')}><Select label="Software version" name="versionId" options={snapshot.versions.filter((item) => item.status !== 'retired').map((item) => [item.id, versionName(item, snapshot.products)])} required />{assets.length > 0 ? <Select label="Atlas asset" name="assetId" options={assets.map((item) => [item.id, item.name])} required /> : <Input help="Enter an organization-visible Atlas asset ID." label="Atlas asset ID" name="assetId" required />}<Select label="Usage state" name="usageState" options={['unknown', 'used', 'unused']} /><Input label="Installed at" name="installedAt" type="datetime-local" required /><Submit busy={busy === 'installation'} label="Associate installation" /></form></CreatePanel>
        <CreatePanel title="Record license entitlement"><form className="grid gap-3" onSubmit={(event) => create(event, 'license')}><Select label="Product" name="productId" options={snapshot.products.filter((item) => item.status === 'active').map((item) => [item.id, `${item.publisher} · ${item.name}`])} required /><Select emptyLabel="All product versions" label="Version scope" name="versionId" options={snapshot.versions.filter((item) => item.status !== 'retired').map((item) => [item.id, versionName(item, snapshot.products)])} /><Input label="License name" name="name" required /><Select label="Entitlement metric" name="entitlementMetric" options={['device', 'user', 'concurrent', 'site', 'enterprise']} /><Input label="Quantity" min="1" name="quantity" required type="number" /><div className="grid gap-3 sm:grid-cols-2"><Input label="Starts on" name="startsOn" type="date" /><Input label="Expires on" name="expiresOn" type="date" /></div><Input help="Optional Ledger reference." label="Vendor ID" name="vendorId" /><Input help="Optional Ledger reference." label="Purchase order ID" name="purchaseOrderId" /><Input help="Optional Ledger reference." label="Contract ID" name="contractId" /><Input help="Optional Ledger current-cost reference." label="Cost record ID" name="costRecordId" /><Input help="Comma-separated Vault IDs." label="Document IDs" name="documentIds" /><Submit busy={busy === 'license'} label="Create license" /></form></CreatePanel>
        <CreatePanel title="Assign license seats"><form className="grid gap-3" onSubmit={(event) => create(event, 'assignment')}><Select label="License" name="licenseId" options={snapshot.licenses.filter((item) => item.status === 'active').map((item) => [item.id, `${item.name} · ${item.quantity} ${label(item.entitlementMetric)}`])} required /><Select label="Assignee type" name="assigneeKind" options={['asset', 'identity', 'department', 'site']} /><Input help="Device licenses use assets, user licenses use identities, and site licenses use sites. Concurrent and enterprise licenses may use any listed scope." label="Assignee ID" name="assigneeId" required /><Input label="Seats" min="1" name="seats" required type="number" /><Select label="Usage state" name="usageState" options={['unknown', 'used', 'unused']} /><Input label="Assigned at" name="assignedAt" type="datetime-local" required /><Submit busy={busy === 'assignment'} label="Assign seats" /></form></CreatePanel>
        <CreatePanel title="Import portable records"><form className="grid gap-3" onSubmit={importRecords}><Input help="Stable source identity used for idempotency." label="Source system ID" name="sourceSystemId" required /><TextArea help="Paste the records array or the complete object returned by Stack export. Review content before importing." label="Exported JSON" maxLength={10000000} name="records" required /><Submit busy={busy === 'import'} label="Import records" /></form></CreatePanel>
      </div>
    </section>}

    <section aria-labelledby="stack-records-heading" className="mt-8 min-w-0">
      <div className="flex flex-wrap items-center justify-between gap-3"><div><h3 className="text-xl font-semibold" id="stack-records-heading">Current software and entitlements</h3><p className="mt-1 text-sm text-steward-mist-muted">Wide details scroll inside the labelled record region.</p></div><a className={secondaryButtonClass} href="/api/v1/stack/exchange">Export portable JSON</a></div>
      <div aria-label="Stack software records" className={`${tableWrapClass} mt-4 max-w-full`} role="region" tabIndex={0}>
        <table className="min-w-[48rem] w-full text-left text-sm"><thead className="border-b border-white/10 text-steward-mist-muted"><tr><th className="p-3" scope="col">Record</th><th className="p-3" scope="col">Name or target</th><th className="p-3" scope="col">State</th><th className="p-3" scope="col">Relationship</th><th className="p-3" scope="col">Timing or quantity</th></tr></thead><tbody>
          {snapshot.products.map((item) => <RecordRow key={`product:${item.id}`} relationship={item.publisher} state={canWrite && item.status !== 'retired' ? <form className="flex min-w-56 flex-wrap items-end gap-2" onSubmit={(event) => updateRecord(event, `/api/v1/stack/products/${encodeURIComponent(item.id)}/status`, (values) => ({ status: text(values, 'status'), revision: item.revision }), 'Product status updated.', `product:${item.id}`)}><Select compact label={`Product status for ${item.name}`} name="status" options={['active', 'retired']} value={item.status} /><button className={buttonClass} disabled={busy === `product:${item.id}`} type="submit">Update product</button></form> : item.status} timing={item.category || 'No category'} title={item.name} type="Product" />)}
          {snapshot.versions.map((item) => <RecordRow key={`version:${item.id}`} relationship={`Product ${item.productId}`} state={canWrite && item.status !== 'retired' ? <form className="flex min-w-56 flex-wrap items-end gap-2" onSubmit={(event) => updateRecord(event, `/api/v1/stack/versions/${encodeURIComponent(item.id)}/status`, (values) => ({ status: text(values, 'status'), revision: item.revision }), 'Version status updated.', `version:${item.id}`)}><Select compact label={`Version status for ${item.name}`} name="status" options={['active', 'unsupported', 'retired']} value={item.status} /><button className={buttonClass} disabled={busy === `version:${item.id}`} type="submit">Update version</button></form> : item.status} timing={displayDate(item.releasedOn)} title={item.name} type="Version" />)}
          {snapshot.installations.map((item) => <RecordRow key={`installation:${item.id}`} relationship={`Version ${item.versionId}`} state={canWrite && item.status === 'installed' ? <form className="grid min-w-64 gap-2" onSubmit={(event) => updateRecord(event, `/api/v1/stack/installations/${encodeURIComponent(item.id)}`, (values) => ({ status: text(values, 'status'), usageState: text(values, 'usageState'), lastUsedAt: instant(text(values, 'lastUsedAt')), removedAt: text(values, 'status') === 'removed' ? instant(text(values, 'removedAt')) : undefined, revision: item.revision }), 'Installation state updated.', `installation:${item.id}`)}><div className="flex flex-wrap items-end gap-2"><Select compact label={`Installation state for ${item.assetId}`} name="status" options={['installed', 'removed']} value={item.status} /><Select compact label={`Installation usage for ${item.assetId}`} name="usageState" options={['unknown', 'used', 'unused']} value={item.usageState} /></div><div className="flex flex-wrap items-end gap-2"><Input compact defaultValue={inputInstant(item.lastUsedAt)} label={`Last used for ${item.assetId}`} name="lastUsedAt" type="datetime-local" /><Input compact label={`Removed at for ${item.assetId}`} name="removedAt" type="datetime-local" /><button className={buttonClass} disabled={busy === `installation:${item.id}`} type="submit">Update installation</button></div></form> : `${label(item.status)} · ${label(item.usageState)}`} timing={displayDate(item.installedAt)} title={`Asset ${item.assetId}`} type="Installation" />)}
          {snapshot.licenses.map((item) => <RecordRow key={`license:${item.id}`} relationship={`Product ${item.productId}${item.versionId ? ` · Version ${item.versionId}` : ''}`} state={canWrite && item.status !== 'retired' ? <form className="grid min-w-64 gap-2" onSubmit={(event) => updateRecord(event, `/api/v1/stack/licenses/${encodeURIComponent(item.id)}/entitlement`, (values) => ({ quantity: Number(text(values, 'quantity')), status: text(values, 'status'), startsOn: date(text(values, 'startsOn')), expiresOn: date(text(values, 'expiresOn')), revision: item.revision }), 'License entitlement updated.', `license:${item.id}`)}><div className="flex flex-wrap items-end gap-2"><Input compact defaultValue={item.quantity} label={`License quantity for ${item.name}`} min="1" name="quantity" required type="number" /><Select compact label={`License status for ${item.name}`} name="status" options={['active', 'expired', 'retired']} value={item.status} /></div><div className="flex flex-wrap items-end gap-2"><Input compact defaultValue={item.startsOn?.slice(0, 10)} label={`License starts for ${item.name}`} name="startsOn" type="date" /><Input compact defaultValue={item.expiresOn?.slice(0, 10)} label={`License expires for ${item.name}`} name="expiresOn" type="date" /><button className={buttonClass} disabled={busy === `license:${item.id}`} type="submit">Update license</button></div></form> : item.status} timing={`${item.quantity} ${label(item.entitlementMetric)} · expires ${displayDate(item.expiresOn)}`} title={item.name} type="License" />)}
          {snapshot.assignments.map((item) => <tr className="border-b border-white/[0.06] last:border-0" key={`assignment:${item.id}`}><th className="p-3 text-left align-top" scope="row"><strong>Assignment</strong><span className="mt-1 block break-all text-xs font-normal text-steward-slate">{item.id}</span></th><td className="p-3 break-all">{label(item.assigneeKind)} {item.assigneeId}</td><td className="p-3">{canWrite && !item.endedAt ? <div className="grid min-w-64 gap-2"><form className="flex flex-wrap items-end gap-2" onSubmit={(event) => updateUsage(event, item)}><Select compact label={`Assignment usage for ${item.assigneeId}`} name="usageState" options={['unknown', 'used', 'unused']} value={item.usageState} /><Input compact defaultValue={inputInstant(item.lastUsedAt)} label={`Last used for assignment ${item.assigneeId}`} name="lastUsedAt" type="datetime-local" /><button className={buttonClass} disabled={busy === `usage:${item.id}`} type="submit">{busy === `usage:${item.id}` ? 'Saving…' : 'Update assignment usage'}</button></form><form className="flex flex-wrap items-end gap-2" onSubmit={(event) => updateRecord(event, `/api/v1/stack/assignments/${encodeURIComponent(item.id)}/end`, (values) => ({ endedAt: instant(text(values, 'endedAt')), revision: item.revision }), 'License assignment ended.', `end:${item.id}`)}><Input compact label={`Assignment ends at for ${item.assigneeId}`} name="endedAt" required type="datetime-local" /><button className={secondaryButtonClass} disabled={busy === `end:${item.id}`} type="submit">End assignment</button></form></div> : `${label(item.usageState)}${item.endedAt ? ` · ended ${displayDate(item.endedAt)}` : ''}`}</td><td className="p-3 break-all">License {item.licenseId}</td><td className="p-3">{item.seats} seats · {displayDate(item.assignedAt)}</td></tr>)}
          {snapshot.products.length + snapshot.versions.length + snapshot.installations.length + snapshot.licenses.length + snapshot.assignments.length === 0 && <tr><td className="p-6 text-center text-steward-mist-muted" colSpan={5}>No software or license records yet.</td></tr>}
        </tbody></table>
      </div>
    </section>
  </section>
}

function versionName(version: Version, products: Product[]) { return `${products.find((item) => item.id === version.productId)?.name ?? version.productId} · ${version.name}` }
function Metric({ label: valueLabel, value }: { label: string; value: number }) { return <div className={`${subpanelClass} p-4`}><dt className="text-sm text-steward-mist-muted">{valueLabel}</dt><dd className="mt-1 text-2xl font-semibold">{value}</dd></div> }
function CreatePanel({ children, title }: { children: ReactNode; title: string }) { return <details className={`${subpanelClass} min-w-0 p-4`}><summary className="min-h-11 cursor-pointer content-center font-semibold text-steward-teal">{title}</summary><div className="mt-4 border-t border-white/[0.08] pt-4">{children}</div></details> }
function Submit({ busy, label: value }: { busy: boolean; label: string }) { return <button className={buttonClass} disabled={busy} type="submit">{busy ? 'Saving…' : value}</button> }

function Field({ children, help, id, label: value }: { children: ReactNode; help?: string; id: string; label: string }) {
  const helpID = `${id}-help`
  return <div className="min-w-0"><label className="text-sm font-semibold text-steward-mist" htmlFor={id}>{value}</label>{children}{help && <span className="mt-1 block text-xs font-normal leading-5 text-steward-mist-muted" id={helpID}>{help}</span>}</div>
}
function Input({ compact = false, help, label: value, name, ...props }: { compact?: boolean; help?: string; label: string; name: string } & InputHTMLAttributes<HTMLInputElement>) {
  const generated = useId(); const id = `stack-${name}-${generated}`; const helpID = help ? `${id}-help` : undefined
  return <Field help={help} id={id} label={value}><input {...props} aria-describedby={helpID} className={compact ? 'mt-1 min-h-11 min-w-0 rounded-xl border-0 bg-steward-ink-950/75 px-3 py-2 text-sm ring-1 ring-inset ring-white/10 focus:ring-2 focus:ring-steward-teal' : inputClass} id={id} key={String(props.defaultValue ?? '')} name={name} /></Field>
}
function Select({ compact = false, emptyLabel, label: value, name, options, required, value: selected }: { compact?: boolean; emptyLabel?: string; label: string; name: string; options: readonly (string | readonly [string, string])[]; required?: boolean; value?: string }) {
  const generated = useId(); const id = `stack-${name}-${generated}`
  return <Field id={id} label={value}><select className={compact ? 'mt-1 min-h-11 min-w-0 rounded-xl border-0 bg-steward-ink-950/75 px-3 py-2 text-sm ring-1 ring-inset ring-white/10 focus:ring-2 focus:ring-steward-teal' : inputClass} defaultValue={selected} id={id} key={selected ?? ''} name={name} required={required}>{emptyLabel !== undefined && <option value="">{emptyLabel}</option>}{options.map((option) => { const [optionValue, optionLabel] = typeof option === 'string' ? [option, label(option)] : option; return <option key={optionValue} value={optionValue}>{optionLabel}</option> })}</select></Field>
}
function TextArea({ help, label: value, maxLength, name, required }: { help?: string; label: string; maxLength: number; name: string; required?: boolean }) {
  const generated = useId(); const id = `stack-${name}-${generated}`; const helpID = help ? `${id}-help` : undefined
  return <Field help={help} id={id} label={value}><textarea aria-describedby={helpID} className={`${inputClass} min-h-36 font-mono text-xs`} id={id} maxLength={maxLength} name={name} required={required} /></Field>
}
function RecordRow({ relationship, state, timing, title, type }: { relationship: string; state: ReactNode; timing: ReactNode; title: string; type: string }) { return <tr className="border-b border-white/[0.06] last:border-0"><th className="p-3 text-left align-top" scope="row"><strong>{type}</strong></th><td className="p-3 break-words">{title}</td><td className="p-3">{typeof state === 'string' ? label(state) : state}</td><td className="p-3 break-all">{relationship}</td><td className="p-3">{timing}</td></tr> }
