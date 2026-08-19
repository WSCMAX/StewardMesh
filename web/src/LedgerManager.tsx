import { type FormEvent, type ReactNode, useEffect, useId, useRef, useState } from 'react'
import { ApiRequestError, isRevision, requestJSON, type Revision } from './api'
import DocumentViewer, { type ViewableDocument } from './DocumentViewer'
import RecordSearchPicker, { type SearchableRecord } from './RecordSearchPicker'
import { ProductHeader, buttonClass, compactInputClass, inputClass, panelClass, secondaryButtonClass, subpanelClass, tableWrapClass } from './ui'

// Requirement: REQ-LEDGER-001. Feature: procurement.finance.

type Vendor = { id: string; name: string; status: string; externalId?: string }
type PurchaseOrderLine = { id?: string; description: string; kind: string; assetId?: string; modelId?: string; licenseId?: string; quantity: number; unitCostMinor: number; amountMinor: number }
type PurchaseOrder = { id: string; number: string; vendorId: string; status: string; currency: string; totalMinor: number; assetIds: string[]; receiptDocumentIds: string[]; lines?: PurchaseOrderLine[]; revision: Revision }
type Contract = { id: string; name: string; vendorId: string; operationalStatus: string; financialStatus: string; currency: string; ceilingMinor: number; startsOn: string; endsOn: string; renewsOn?: string; documentIds?: string[]; revision: Revision }
type Commitment = { id: string; contractId: string; kind: string; description: string; currency: string; amountMinor: number; fiscalPeriod: string; scenario: string }
type Budget = { id: string; name: string; fiscalPeriod: string; scenario: string; currency: string; allocatedMinor: number }
type CostRecord = { id: string; description: string; kind: string; currency: string; amountMinor: number; fiscalPeriod: string; scenario: string; externalReference?: string; revision: Revision }
type LedgerSnapshot = { vendors: Vendor[]; purchaseOrders: PurchaseOrder[]; contracts: Contract[]; commitments: Commitment[]; budgets: Budget[]; costs: CostRecord[] }
type BudgetVariance = { fiscalPeriod: string; scenario: string; currency: string; allocatedMinor: number; recognizedMinor: number; varianceMinor: number; overBudget: boolean; amountsByKindMinor: Record<string, number> }
type LedgerManagerProps = { csrfToken: string; permissions: readonly string[]; onOpenHelp?: () => void }

const purchaseStatuses = ['draft', 'approved', 'ordered', 'partially_received', 'received', 'cancelled']
const operationalStatuses = ['planned', 'active', 'suspended', 'expired', 'terminated', 'cancelled']
const financialStatuses = ['planned', 'committed', 'billed', 'paid', 'closed', 'cancelled']

function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null }
function validBase(value: unknown): value is Record<string, unknown> { return isObject(value) && typeof value.id === 'string' && value.id.length > 0 }
function validMoney(value: unknown) { return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0 }
function validArray(value: unknown, validator: (item: unknown) => boolean) { return Array.isArray(value) && value.every(validator) }

function parseSnapshot(value: unknown): LedgerSnapshot {
  if (!isObject(value)) throw new Error('invalid Ledger response')
  const vendors = value.vendors
  const purchaseOrders = value.purchaseOrders
  const contracts = value.contracts
  const commitments = value.commitments
  const budgets = value.budgets
  const costs = value.costs
  if (!validArray(vendors, (item) => validBase(item) && typeof item.name === 'string' && typeof item.status === 'string')
    || !validArray(purchaseOrders, (item) => validBase(item) && typeof item.number === 'string' && typeof item.status === 'string' && validMoney(item.totalMinor) && isRevision(item.revision))
    || !validArray(contracts, (item) => validBase(item) && typeof item.name === 'string' && typeof item.operationalStatus === 'string' && typeof item.financialStatus === 'string' && validMoney(item.ceilingMinor) && isRevision(item.revision))
    || !validArray(commitments, (item) => validBase(item) && typeof item.description === 'string' && typeof item.kind === 'string' && validMoney(item.amountMinor))
    || !validArray(budgets, (item) => validBase(item) && typeof item.name === 'string' && validMoney(item.allocatedMinor))
    || !validArray(costs, (item) => validBase(item) && typeof item.description === 'string' && typeof item.kind === 'string' && validMoney(item.amountMinor) && isRevision(item.revision))) {
    throw new Error('invalid Ledger response')
  }
  return { vendors: vendors as Vendor[], purchaseOrders: purchaseOrders as PurchaseOrder[], contracts: contracts as Contract[], commitments: commitments as Commitment[], budgets: budgets as Budget[], costs: costs as CostRecord[] }
}

function parseVariance(value: unknown): BudgetVariance {
  if (!isObject(value) || typeof value.fiscalPeriod !== 'string' || typeof value.scenario !== 'string' || typeof value.currency !== 'string'
    || !validMoney(value.allocatedMinor) || !validMoney(value.recognizedMinor) || typeof value.varianceMinor !== 'number'
    || !Number.isSafeInteger(value.varianceMinor) || typeof value.overBudget !== 'boolean' || !isObject(value.amountsByKindMinor)) {
    throw new Error('invalid Ledger variance response')
  }
  return value as BudgetVariance
}

function money(minor: number, currency = 'USD') {
  try { return new Intl.NumberFormat(undefined, { style: 'currency', currency }).format(minor / 100) } catch { return `${currency} ${(minor / 100).toFixed(2)}` }
}

function minorUnits(value: FormDataEntryValue | null) {
  const normalized = String(value ?? '').trim()
  if (!/^\d+(?:\.\d{1,2})?$/.test(normalized)) throw new Error('Enter a non-negative amount with at most two decimal places.')
  const [whole, fraction = ''] = normalized.split('.')
  const amount = Number(whole) * 100 + Number(fraction.padEnd(2, '0'))
  if (!Number.isSafeInteger(amount)) throw new Error('The amount is too large.')
  return amount
}

function dateValue(value: FormDataEntryValue | null) { return `${String(value ?? '')}T00:00:00Z` }

export default function LedgerManager({ csrfToken, permissions, onOpenHelp }: LedgerManagerProps) {
  const canRead = permissions.includes('finance.read')
  const canWrite = permissions.includes('finance.write')
  const [snapshot, setSnapshot] = useState<LedgerSnapshot>({ vendors: [], purchaseOrders: [], contracts: [], commitments: [], budgets: [], costs: [] })
  const [variance, setVariance] = useState<BudgetVariance | null>(null)
  const [fiscalPeriod, setFiscalPeriod] = useState('FY2027')
  const [scenario, setScenario] = useState('baseline')
  const [busy, setBusy] = useState('')
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')
  const [poAssets, setPoAssets] = useState<SearchableRecord[]>([])
  const [poDocuments, setPoDocuments] = useState<SearchableRecord[]>([])
  const [contractDocuments, setContractDocuments] = useState<SearchableRecord[]>([])
  const [costAsset, setCostAsset] = useState<SearchableRecord[]>([])
  const [costDocument, setCostDocument] = useState<SearchableRecord[]>([])
  const [costPurchaseOrder, setCostPurchaseOrder] = useState<SearchableRecord[]>([])
  const [budgetDepartment, setBudgetDepartment] = useState<SearchableRecord[]>([])
  const [budgetSite, setBudgetSite] = useState<SearchableRecord[]>([])
  const [poLines, setPoLines] = useState([{ key: 0, description: '', kind: 'asset', quantity: '1', unitCost: '' }])
  const [preview, setPreview] = useState<ViewableDocument | null>(null)
  const errorRef = useRef<HTMLDivElement>(null)

  useEffect(() => { if (error) errorRef.current?.focus() }, [error])

  useEffect(() => {
    if (!canRead) return
    let active = true
    Promise.all([
      requestJSON('/api/v1/ledger'),
      requestJSON(`/api/v1/ledger/budget-variance?fiscalPeriod=${encodeURIComponent(fiscalPeriod)}&scenario=${encodeURIComponent(scenario)}`),
    ]).then(([snapshotValue, varianceValue]) => {
      if (!active) return
      setSnapshot(parseSnapshot(snapshotValue))
      setVariance(parseVariance(varianceValue))
    }).catch(() => { if (active) showError('Ledger financial records could not be loaded.') })
    return () => { active = false }
  }, [canRead, fiscalPeriod, scenario])

  function showError(value: string) { setError(value); queueMicrotask(() => errorRef.current?.focus()) }

  async function openDocument(id: string) {
    setError('')
    try {
      const value = await requestJSON(`/api/v1/blobs/${encodeURIComponent(id)}`)
      if (typeof value !== 'object' || value === null) throw new Error('invalid Vault blob')
      const blob = value as Record<string, unknown>
      if (typeof blob.id !== 'string' || typeof blob.name !== 'string' || typeof blob.mediaType !== 'string') throw new Error('invalid Vault blob')
      setPreview({ id: blob.id, name: blob.name, mediaType: blob.mediaType })
    } catch (cause) {
      showError(cause instanceof ApiRequestError ? cause.message : 'The document could not be opened.')
    }
  }

  async function reload() {
    const [snapshotValue, varianceValue] = await Promise.all([
      requestJSON('/api/v1/ledger'),
      requestJSON(`/api/v1/ledger/budget-variance?fiscalPeriod=${encodeURIComponent(fiscalPeriod)}&scenario=${encodeURIComponent(scenario)}`),
    ])
    setSnapshot(parseSnapshot(snapshotValue))
    setVariance(parseVariance(varianceValue))
  }

  async function create(event: FormEvent<HTMLFormElement>, kind: 'vendor' | 'purchase' | 'contract' | 'commitment' | 'budget' | 'cost') {
    event.preventDefault()
    const form = event.currentTarget
    const values = new FormData(form)
    const configurations = {
      vendor: { path: '/api/v1/ledger/vendors', body: () => ({ name: text(values, 'name'), externalId: text(values, 'externalId') }), message: 'Vendor created.' },
      purchase: { path: '/api/v1/ledger/purchase-orders', body: () => ({ number: text(values, 'number'), vendorId: text(values, 'vendorId'), status: text(values, 'status'), currency: text(values, 'currency'), totalMinor: minorUnits(values.get('amount') || '0'), orderedOn: text(values, 'orderedOn') ? dateValue(values.get('orderedOn')) : undefined, assetIds: poAssets.map((item) => item.id), receiptDocumentIds: poDocuments.map((item) => item.id), lines: poLines.filter((line) => line.description.trim()).map((line, index) => ({ id: `line-${index + 1}`, description: line.description.trim(), kind: line.kind, quantity: Number(line.quantity || '1'), unitCostMinor: minorUnits(line.unitCost || '0') })) }), message: 'Purchase order created.' },
      contract: { path: '/api/v1/ledger/contracts', body: () => ({ name: text(values, 'name'), vendorId: text(values, 'vendorId'), operationalStatus: text(values, 'operationalStatus'), financialStatus: text(values, 'financialStatus'), currency: text(values, 'currency'), ceilingMinor: minorUnits(values.get('amount')), startsOn: dateValue(values.get('startsOn')), endsOn: dateValue(values.get('endsOn')), renewsOn: text(values, 'renewsOn') ? dateValue(values.get('renewsOn')) : undefined, documentIds: contractDocuments.map((item) => item.id) }), message: 'Contract created.' },
      commitment: { path: '/api/v1/ledger/commitments', body: () => ({ contractId: text(values, 'contractId'), kind: text(values, 'kind'), description: text(values, 'description'), currency: text(values, 'currency'), amountMinor: minorUnits(values.get('amount')), startsOn: dateValue(values.get('startsOn')), endsOn: dateValue(values.get('endsOn')), fiscalPeriod: text(values, 'fiscalPeriod'), scenario: text(values, 'scenario') }), message: 'Commitment created.' },
      budget: { path: '/api/v1/ledger/budgets', body: () => ({ name: text(values, 'name'), fiscalPeriod: text(values, 'fiscalPeriod'), scenario: text(values, 'scenario'), currency: text(values, 'currency'), allocatedMinor: minorUnits(values.get('amount')), departmentId: text(values, 'departmentId'), siteId: text(values, 'siteId') }), message: 'Budget created.' },
      cost: { path: '/api/v1/ledger/costs/reconcile', body: () => ({ description: text(values, 'description'), kind: text(values, 'kind'), currency: text(values, 'currency'), amountMinor: minorUnits(values.get('amount')), fiscalPeriod: text(values, 'fiscalPeriod'), scenario: text(values, 'scenario'), purchaseOrderId: text(values, 'purchaseOrderId'), contractId: text(values, 'contractId'), assetId: costAsset[0]?.id ?? '', documentId: costDocument[0]?.id ?? '', externalReference: text(values, 'externalReference'), sourceSystemId: text(values, 'sourceSystemId'), sourceRecordId: text(values, 'sourceRecordId') }), message: 'Cost reconciled.' },
    } as const
    setBusy(kind); setError(''); setMessage('')
    try {
      const configuration = configurations[kind]
      await requestJSON(configuration.path, { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken }, body: JSON.stringify(configuration.body()) })
      await reload()
      form.reset()
      if (kind === 'purchase') { setPoAssets([]); setPoDocuments([]); setPoLines([{ key: 0, description: '', kind: 'asset', quantity: '1', unitCost: '' }]) }
      if (kind === 'contract') setContractDocuments([])
      if (kind === 'cost') { setCostAsset([]); setCostDocument([]); setCostPurchaseOrder([]) }
      if (kind === 'budget') { setBudgetDepartment([]); setBudgetSite([]) }
      setMessage(configuration.message)
    } catch (cause) {
      showError(cause instanceof ApiRequestError || cause instanceof Error ? cause.message : 'The Ledger record could not be saved.')
    } finally { setBusy('') }
  }

  async function updatePurchaseStatus(event: FormEvent<HTMLFormElement>, purchaseOrder: PurchaseOrder) {
    event.preventDefault(); const values = new FormData(event.currentTarget)
    await updateStatus(`/api/v1/ledger/purchase-orders/${encodeURIComponent(purchaseOrder.id)}/status`, { status: text(values, 'status'), revision: purchaseOrder.revision }, 'Purchase order status updated.')
  }

  async function updateContractStatus(event: FormEvent<HTMLFormElement>, contract: Contract) {
    event.preventDefault(); const values = new FormData(event.currentTarget)
    await updateStatus(`/api/v1/ledger/contracts/${encodeURIComponent(contract.id)}/status`, { operationalStatus: text(values, 'operationalStatus'), financialStatus: text(values, 'financialStatus'), revision: contract.revision }, 'Contract statuses updated.')
  }

  async function updateStatus(path: string, body: object, success: string) {
    setBusy(path); setError(''); setMessage('')
    try { await requestJSON(path, { method: 'PUT', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken }, body: JSON.stringify(body) }); await reload(); setMessage(success) }
    catch (cause) { showError(cause instanceof ApiRequestError ? cause.message : 'The status could not be updated.') }
    finally { setBusy('') }
  }

  if (!canRead) return <section aria-labelledby="ledger-heading" className={`${panelClass} p-5 sm:p-6`} data-feature="procurement.finance" data-requirement="REQ-LEDGER-001"><div className="flex flex-wrap items-start justify-between gap-4"><div><h2 id="ledger-heading" className="text-2xl font-semibold">Ledger — Procurement and budgets</h2><p className="mt-2 text-steward-mist-muted">Your role does not include permission to view financial records.</p></div>{onOpenHelp && <button className={secondaryButtonClass} onClick={onOpenHelp} type="button">Ledger help</button>}</div></section>

  return (
    <section aria-labelledby="ledger-heading" className={`${panelClass} p-4 sm:p-6`} data-feature="procurement.finance" data-requirement="REQ-LEDGER-001">
      <ProductHeader
        actions={onOpenHelp ? <button className={secondaryButtonClass} onClick={onOpenHelp} type="button">Ledger help</button> : undefined}
        description="Track obligations in exact minor units, keep operational and financial contract states separate, and reconcile source records without creating duplicates."
        headingId="ledger-heading"
        kicker="Ledger"
        title="Procurement, contracts, budgets, and costs"
      />
      {error && <div ref={errorRef} className="mt-4 rounded-lg border border-steward-danger/50 bg-steward-danger/15 p-3 text-[#ffccd1]" role="alert" tabIndex={-1}>{error}</div>}
      {message && <p className="mt-4 rounded-lg border border-steward-success/50 bg-steward-success/15 p-3 text-[#aaf0c6]" role="status">{message}</p>}

      <div className="mt-6 grid gap-4 md:grid-cols-[1fr_1fr_auto]">
        <LedgerField id="ledger-period" label="Fiscal period"><input className={inputClass} id="ledger-period" onChange={(event) => setFiscalPeriod(event.target.value)} value={fiscalPeriod} /></LedgerField>
        <LedgerField id="ledger-scenario" label="Scenario"><input className={inputClass} id="ledger-scenario" onChange={(event) => setScenario(event.target.value)} value={scenario} /></LedgerField>
        <a className="min-h-11 self-end rounded-lg border border-steward-teal px-4 py-3 text-center font-semibold text-steward-teal hover:bg-steward-teal/10" href={`/api/v1/ledger/export.csv?fiscalPeriod=${encodeURIComponent(fiscalPeriod)}&scenario=${encodeURIComponent(scenario)}`}>Export CSV</a>
      </div>

      {variance && <div className={`mt-5 grid gap-3 rounded-xl border p-4 sm:grid-cols-3 ${variance.overBudget ? 'border-steward-danger/60 bg-steward-danger/10' : 'border-steward-ink-800 bg-steward-ink-950/40'}`} aria-live="polite">
        <Metric label="Allocated" value={money(variance.allocatedMinor, variance.currency || 'USD')} />
        <Metric label="Actual and committed" value={money(variance.recognizedMinor, variance.currency || 'USD')} />
        <Metric label={variance.overBudget ? 'Over budget' : 'Remaining'} value={money(Math.abs(variance.varianceMinor), variance.currency || 'USD')} />
      </div>}

      {canWrite && <div className="mt-6 grid gap-3 lg:grid-cols-2">
        <CreatePanel title="Add vendor"><form className="grid gap-3" onSubmit={(event) => create(event, 'vendor')}><Input name="name" label="Vendor name" required /><Input name="externalId" label="External ID" /><Submit busy={busy === 'vendor'} label="Create vendor" /></form></CreatePanel>
        <CreatePanel title="Add purchase order"><form className="grid gap-3" onSubmit={(event) => create(event, 'purchase')}><Input name="number" label="PO number" required /><Select name="vendorId" label="Vendor" options={snapshot.vendors.map((item) => [item.id, item.name])} required /><Select name="status" label="Status" options={purchaseStatuses.map((item) => [item, label(item)])} /><MoneyFields /><Input name="orderedOn" label="Ordered on" type="date" /><RecordSearchPicker help="Search Atlas assets instead of pasting IDs." kind="asset" label="Assets on this PO" onChange={setPoAssets} selected={poAssets} /><RecordSearchPicker help="Search Vault receipts, packing slips, and photos." kind="document" label="Receipt or contract documents" onChange={setPoDocuments} selected={poDocuments} /><fieldset className="grid gap-3 rounded-xl border border-white/10 p-3"><legend className="px-1 text-sm font-semibold">Line items</legend>{poLines.map((line) => <div className="grid gap-2 sm:grid-cols-4" key={line.key}><Input name={`line-${line.key}-description`} label="Description" onChange={(value) => setPoLines((current) => current.map((item) => item.key === line.key ? { ...item, description: value } : item))} value={line.description} /><Select name={`line-${line.key}-kind`} label="Kind" onChange={(value) => setPoLines((current) => current.map((item) => item.key === line.key ? { ...item, kind: value } : item))} options={['asset', 'model', 'accessory', 'software', 'other'].map((item) => [item, label(item)])} value={line.kind} /><Input name={`line-${line.key}-quantity`} label="Quantity" onChange={(value) => setPoLines((current) => current.map((item) => item.key === line.key ? { ...item, quantity: value } : item))} value={line.quantity} /><Input name={`line-${line.key}-unitCost`} label="Unit cost" onChange={(value) => setPoLines((current) => current.map((item) => item.key === line.key ? { ...item, unitCost: value } : item))} value={line.unitCost} /></div>)}<button className={secondaryButtonClass} onClick={() => setPoLines((current) => [...current, { key: current.length, description: '', kind: 'accessory', quantity: '1', unitCost: '' }])} type="button">Add line</button></fieldset><Submit busy={busy === 'purchase'} label="Create purchase order" /></form></CreatePanel>
        <CreatePanel title="Add contract"><form className="grid gap-3" onSubmit={(event) => create(event, 'contract')}><Input name="name" label="Contract name" required /><Select name="vendorId" label="Vendor" options={snapshot.vendors.map((item) => [item.id, item.name])} required /><Select name="operationalStatus" label="Operational status" options={operationalStatuses.map((item) => [item, label(item)])} /><Select name="financialStatus" label="Financial status" options={financialStatuses.map((item) => [item, label(item)])} /><MoneyFields label="Ceiling" /><Input name="startsOn" label="Starts on" type="date" required /><Input name="endsOn" label="Ends on" type="date" required /><Input name="renewsOn" label="Renews on" type="date" /><RecordSearchPicker kind="document" label="Contract documents" onChange={setContractDocuments} selected={contractDocuments} /><Submit busy={busy === 'contract'} label="Create contract" /></form></CreatePanel>
        <CreatePanel title="Add commitment"><form className="grid gap-3" onSubmit={(event) => create(event, 'commitment')}><Select name="contractId" label="Contract" options={snapshot.contracts.map((item) => [item.id, item.name])} required /><Select name="kind" label="Commitment type" options={['savings_plan', 'subscription', 'reserved_capacity', 'lease', 'maintenance', 'license', 'financing', 'other'].map((item) => [item, label(item)])} required /><Input name="description" label="Description" required /><MoneyFields /><Input name="startsOn" label="Starts on" type="date" required /><Input name="endsOn" label="Ends on" type="date" required /><PeriodFields fiscalPeriod={fiscalPeriod} scenario={scenario} /><Submit busy={busy === 'commitment'} label="Create commitment" /></form></CreatePanel>
        <CreatePanel title="Add budget"><form className="grid gap-3" onSubmit={(event) => create(event, 'budget')}><Input name="name" label="Budget name" required /><PeriodFields fiscalPeriod={fiscalPeriod} scenario={scenario} /><MoneyFields label="Allocated amount" /><RecordSearchPicker browseHref="#workspace-people" browseLabel="Open departments" create={{
          label: 'Add department',
          fields: [{ key: 'name', label: 'Department name', required: true }],
          submit: async (values) => {
            const saved = await requestJSON('/api/v1/departments', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken }, body: JSON.stringify({ name: values.name, status: 'active' }) })
            if (typeof saved !== 'object' || saved === null || typeof (saved as { id?: unknown }).id !== 'string') throw new Error('invalid department')
            const record = { id: (saved as { id: string }).id, label: values.name }
            return record
          },
        }} kind="department" label="Department" multiple={false} name="departmentId" onChange={setBudgetDepartment} selected={budgetDepartment} /><RecordSearchPicker browseHref="#workspace-people" browseLabel="Open sites" create={{
          label: 'Add site',
          fields: [{ key: 'name', label: 'Site name', required: true }],
          submit: async (values) => {
            const saved = await requestJSON('/api/v1/sites', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken }, body: JSON.stringify({ name: values.name, status: 'active' }) })
            if (typeof saved !== 'object' || saved === null || typeof (saved as { id?: unknown }).id !== 'string') throw new Error('invalid site')
            return { id: (saved as { id: string }).id, label: values.name }
          },
        }} kind="site" label="Site" multiple={false} name="siteId" onChange={setBudgetSite} selected={budgetSite} /><Submit busy={busy === 'budget'} label="Create budget" /></form></CreatePanel>
        <CreatePanel title="Reconcile cost"><form className="grid gap-3" onSubmit={(event) => create(event, 'cost')}><Input name="description" label="Description" required /><Select name="kind" label="Cost state" options={['planned', 'estimated', 'actual', 'billed', 'paid', 'committed', 'normalized_real', 'tco'].map((item) => [item, label(item)])} required /><MoneyFields /><PeriodFields fiscalPeriod={fiscalPeriod} scenario={scenario} /><RecordSearchPicker browseHref="#workspace-ledger" browseLabel="Open purchase orders" create={{
          label: 'Add purchase order',
          fields: [
            { key: 'number', label: 'PO number', required: true },
            { key: 'vendorId', label: 'Vendor', required: true, options: snapshot.vendors.map((vendor) => ({ id: vendor.id, label: vendor.name })) },
          ],
          submit: async (values) => {
            const saved = await requestJSON('/api/v1/ledger/purchase-orders', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken }, body: JSON.stringify({ number: values.number, vendorId: values.vendorId, status: 'draft', currency: 'USD', totalMinor: 0 }) })
            if (typeof saved !== 'object' || saved === null || typeof (saved as { id?: unknown }).id !== 'string') throw new Error('invalid purchase order')
            await reload()
            return { id: (saved as { id: string }).id, label: values.number }
          },
        }} kind="purchase-order" label="Purchase order" multiple={false} name="purchaseOrderId" onChange={setCostPurchaseOrder} options={snapshot.purchaseOrders.map((item) => ({ id: item.id, label: item.number, detail: item.status }))} selected={costPurchaseOrder} /><Select name="contractId" label="Contract" options={snapshot.contracts.map((item) => [item.id, item.name])} /><RecordSearchPicker kind="asset" label="Asset" multiple={false} onChange={setCostAsset} selected={costAsset} /><RecordSearchPicker kind="document" label="Vault document" multiple={false} onChange={setCostDocument} selected={costDocument} /><Input name="externalReference" label="Invoice or payment reference" /><Input name="sourceSystemId" label="Source system ID" help="Provide both source values to make reconciliation idempotent." /><Input name="sourceRecordId" label="Source record ID" /><Submit busy={busy === 'cost'} label="Reconcile cost" /></form></CreatePanel>
      </div>}

      <div className="mt-8 grid gap-6">
        <LedgerTable title="Purchase orders" empty="No purchase orders yet." headings={['Number', 'Vendor', 'Amount', 'Assets and evidence', 'Status']} rows={snapshot.purchaseOrders.map((item) => [item.number, vendorName(snapshot, item.vendorId), <span key={`${item.id}-amount`}>{money(item.totalMinor, item.currency)}{item.lines && item.lines.length > 0 && <span className="mt-1 block text-xs text-steward-mist-muted">{item.lines.length} line{item.lines.length === 1 ? '' : 's'}: {item.lines.map((line) => line.description).join(', ')}</span>}</span>, <span key={`${item.id}-evidence`}>{item.assetIds.length} assets · {item.receiptDocumentIds.length} documents{item.receiptDocumentIds.map((documentId) => <button className="mt-1 block text-left text-steward-teal" key={documentId} onClick={() => void openDocument(documentId)} type="button">View document</button>)}</span>, canWrite ? <form className="flex min-w-56 gap-2" key={item.id} onSubmit={(event) => updatePurchaseStatus(event, item)}><select aria-label={`Status for ${item.number}`} className={compactInputClass} defaultValue={item.status} name="status">{purchaseStatuses.map((status) => <option key={status} value={status}>{label(status)}</option>)}</select><button className={smallButtonClass} disabled={busy !== ''} type="submit">Save</button></form> : label(item.status)])} />
        <LedgerTable title="Contracts and commitments" empty="No contracts yet." headings={['Contract', 'Term', 'Ceiling', 'Statuses']} rows={snapshot.contracts.map((item) => [<span key={item.id}><strong className="block">{item.name}</strong><span className="text-xs text-steward-mist-muted">{snapshot.commitments.filter((commitment) => commitment.contractId === item.id).length} commitments</span></span>, <span key={`${item.id}-term`}>{new Date(item.startsOn).toLocaleDateString()} – {new Date(item.endsOn).toLocaleDateString()}{item.renewsOn && <span className="block text-xs text-steward-mist-muted">Renews {new Date(item.renewsOn).toLocaleDateString()}</span>}</span>, money(item.ceilingMinor, item.currency), canWrite ? <form className="grid min-w-64 grid-cols-[1fr_1fr_auto] gap-2" key={item.id} onSubmit={(event) => updateContractStatus(event, item)}><select aria-label={`Operational status for ${item.name}`} className={compactInputClass} defaultValue={item.operationalStatus} name="operationalStatus">{operationalStatuses.map((status) => <option key={status} value={status}>{label(status)}</option>)}</select><select aria-label={`Financial status for ${item.name}`} className={compactInputClass} defaultValue={item.financialStatus} name="financialStatus">{financialStatuses.map((status) => <option key={status} value={status}>{label(status)}</option>)}</select><button className={smallButtonClass} disabled={busy !== ''} type="submit">Save</button></form> : `${label(item.operationalStatus)} · ${label(item.financialStatus)}`])} />
        <LedgerTable title="Costs" empty="No costs match this Ledger yet." headings={['Description', 'State', 'Period', 'Amount']} rows={snapshot.costs.map((item) => [<span key={item.id}><strong className="block">{item.description}</strong>{item.externalReference && <span className="text-xs text-steward-mist-muted">{item.externalReference}</span>}</span>, label(item.kind), `${item.fiscalPeriod} · ${item.scenario}`, money(item.amountMinor, item.currency)])} />
      </div>
      {preview && <div className="mt-6"><DocumentViewer csrfToken={csrfToken} document={preview} onClose={() => setPreview(null)} /></div>}
    </section>
  )
}

const smallButtonClass = secondaryButtonClass
function text(values: FormData, name: string) { return String(values.get(name) ?? '').trim() }
function label(value: string) { return value.replaceAll('_', ' ').replace(/\b\w/g, (letter) => letter.toUpperCase()) }
function vendorName(snapshot: LedgerSnapshot, id: string) { return snapshot.vendors.find((item) => item.id === id)?.name ?? id }

function Metric({ label: metricLabel, value }: { label: string; value: string }) { return <div><p className="text-xs font-medium text-steward-mist-muted">{metricLabel}</p><p className="mt-1 text-2xl font-semibold tabular-nums">{value}</p></div> }
function CreatePanel({ title, children }: { title: string; children: ReactNode }) { return <details className={`${subpanelClass} p-4`}><summary className="cursor-pointer font-semibold text-steward-teal">{title}</summary><div className="mt-4">{children}</div></details> }
function LedgerField({ id, label: fieldLabel, help, children }: { id: string; label: string; help?: string; children: ReactNode }) { return <div><label className="block text-sm font-semibold text-steward-mist-muted" htmlFor={id}>{fieldLabel}</label>{help && <p className="mt-1 text-xs text-steward-mist-muted" id={`${id}-help`}>{help}</p>}{children}</div> }
function Input({ name, label: fieldLabel, help, type = 'text', required = false, value, onChange }: { name: string; label: string; help?: string; type?: string; required?: boolean; value?: string; onChange?: (value: string) => void }) { const id = useId(); return <LedgerField help={help} id={id} label={fieldLabel}><input aria-describedby={help ? `${id}-help` : undefined} className={inputClass} id={id} name={name} onChange={onChange ? (event) => onChange(event.target.value) : undefined} required={required} type={type} value={value} /></LedgerField> }
function Select({ name, label: fieldLabel, options, required = false, value, onChange }: { name: string; label: string; options: string[][]; required?: boolean; value?: string; onChange?: (value: string) => void }) { const id = useId(); return <LedgerField id={id} label={fieldLabel}><select className={inputClass} defaultValue={value === undefined ? '' : undefined} id={id} name={name} onChange={onChange ? (event) => onChange(event.target.value) : undefined} required={required} value={value}>{value === undefined && <option value="">{required ? 'Select one' : 'None'}</option>}{value !== undefined && !required && <option value="">None</option>}{options.map(([optionValue, optionLabel]) => <option key={optionValue} value={optionValue}>{optionLabel}</option>)}</select></LedgerField> }
function MoneyFields({ label: amountLabel = 'Amount' }: { label?: string }) { const currencyID = useId(); return <div className="grid grid-cols-[1fr_7rem] gap-3"><Input name="amount" label={amountLabel} required /><LedgerField id={currencyID} label="Currency"><input className={inputClass} defaultValue="USD" id={currencyID} maxLength={3} name="currency" pattern="[A-Za-z]{3}" required /></LedgerField></div> }
function PeriodFields({ fiscalPeriod, scenario }: { fiscalPeriod: string; scenario: string }) { const fiscalID = useId(); const scenarioID = useId(); return <div className="grid grid-cols-2 gap-3"><LedgerField id={fiscalID} label="Fiscal period"><input className={inputClass} defaultValue={fiscalPeriod} id={fiscalID} name="fiscalPeriod" required /></LedgerField><LedgerField id={scenarioID} label="Scenario"><input className={inputClass} defaultValue={scenario} id={scenarioID} name="scenario" required /></LedgerField></div> }
function Submit({ busy, label: buttonLabel }: { busy: boolean; label: string }) { return <button className={buttonClass} disabled={busy} type="submit">{busy ? 'Saving…' : buttonLabel}</button> }
function LedgerTable({ title, headings, rows, empty }: { title: string; headings: string[]; rows: ReactNode[][]; empty: string }) { return <section className="min-w-0" aria-labelledby={`ledger-${title.replaceAll(' ', '-').toLowerCase()}`}><h3 className="text-lg font-semibold" id={`ledger-${title.replaceAll(' ', '-').toLowerCase()}`}>{title}</h3><div className={`${tableWrapClass} mt-3`}><table className="w-full min-w-[720px] border-collapse text-left text-sm"><thead><tr className="border-b border-steward-ink-800 text-steward-mist-muted">{headings.map((heading) => <th className="px-3 py-3 font-semibold" key={heading} scope="col">{heading}</th>)}</tr></thead><tbody>{rows.map((row, rowIndex) => <tr className="border-b border-steward-ink-800/70 align-top" key={rowIndex}>{row.map((cell, cellIndex) => <td className="px-3 py-4" key={cellIndex}>{cell}</td>)}</tr>)}{rows.length === 0 && <tr><td className="px-3 py-6 text-steward-mist-muted" colSpan={headings.length}>{empty}</td></tr>}</tbody></table></div></section> }
