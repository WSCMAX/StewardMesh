import { type FormEvent, useCallback, useEffect, useRef, useState } from 'react'
import { ApiRequestError, requestJSON } from './api'
import { buttonClass, inputClass, labelClass, secondaryButtonClass, subpanelClass } from './ui'

// Requirements: REQ-DIRECTORY-EXPANSION-002, REQ-DIRECTORY-EXPANSION-003, REQ-DIRECTORY-EXPANSION-004, REQ-DIRECTORY-EXPANSION-005, REQ-DIRECTORY-EXPANSION-006, A11Y-001.
// Feature: identity.directory.

type Source = { id: string; provider: string; configRevision: string }
type Counts = { created: number; updated: number; unchanged: number; deactivated: number; conflicts: number; failed: number }
type BatchStatus = 'previewed' | 'applying' | 'applied' | 'partially_applied' | 'failed'
type Batch = {
  id: string
  sourceSystemId: string
  provider: string
  configRevision: string
  status: BatchStatus
  completeSnapshot: boolean
  counts: Counts
  createdAt: string
  updatedAt: string
  completedAt?: string
}
type ImportRecord = {
  sourceRecordId: string
  kind: 'identity' | 'group' | 'membership'
  identityKind?: 'person' | 'shared' | 'public' | 'lab'
  displayName: string
  email?: string
  status: 'active' | 'inactive'
  department?: string
  directoryAttributes?: Record<string, string>
  groupSourceIds?: string[]
  groupName?: string
  description?: string
  groupSourceId?: string
  memberSourceId?: string
  memberKind?: 'subject' | 'group'
  metadata?: Record<string, string>
}
type ImportItem = {
  id: string
  ordinal: number
  record: ImportRecord
  targetId?: string
  expectedRevision?: number
  action: 'create' | 'update' | 'deactivate' | 'unchanged' | 'conflict'
  outcome: 'pending' | 'applied' | 'unchanged' | 'conflict' | 'failed'
  failureClass?: 'transient' | 'permanent' | 'conflict'
  retryable?: boolean
  error?: string
  updatedAt: string
}
type ImportAttempt = {
  id: string
  operation: 'preview' | 'apply' | 'retry'
  number: number
  status: BatchStatus
  failureClass?: 'transient' | 'permanent' | 'conflict'
  retryable?: boolean
  error?: string
  correlationId: string
  startedAt: string
  completedAt?: string
}
type BatchDetail = { batch: Batch; items: ImportItem[]; attempts: ImportAttempt[] }

type Props = {
  csrfToken: string
  permissions: readonly string[]
  onApplied?: () => void | Promise<void>
}

const recordIDPattern = /^[a-f0-9]{32}$/
const sourceIDPattern = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/
const providerPattern = /^[a-z0-9][a-z0-9._-]{0,63}$/
const statuses: readonly BatchStatus[] = ['previewed', 'applying', 'applied', 'partially_applied', 'failed']

export default function DirectoryImportManager({ csrfToken, permissions, onApplied }: Props) {
  const canRead = permissions.includes('integrations.read')
  const canWrite = permissions.includes('integrations.write')
  const [sources, setSources] = useState<Source[]>([])
  const [sourceID, setSourceID] = useState('')
  const [batches, setBatches] = useState<Batch[]>([])
  const [detail, setDetail] = useState<BatchDetail | null>(null)
  const [loading, setLoading] = useState(canRead)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [status, setStatus] = useState('')
  const errorRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (error) errorRef.current?.focus()
  }, [error])

  const loadSummary = useCallback(async (signal?: AbortSignal) => {
    const [sourceValue, batchValue] = await Promise.all([
      requestJSON('/api/v1/directory-import-sources', { signal }),
      requestJSON('/api/v1/directory-imports?limit=50', { signal }),
    ])
    const nextSources = readSources(sourceValue)
    const nextBatches = readBatchPage(batchValue)
    setSources(nextSources)
    setSourceID((current) => nextSources.some((source) => source.id === current) ? current : nextSources[0]?.id ?? '')
    setBatches(nextBatches)
    return nextBatches
  }, [])

  useEffect(() => {
    if (!canRead) return
    const controller = new AbortController()
    let active = true
    setLoading(true)
    loadSummary(controller.signal)
      .catch((loadError: unknown) => {
        if (active && !(loadError instanceof DOMException && loadError.name === 'AbortError')) {
          setError(messageFor(loadError, 'Directory import sources and audit history could not be loaded.'))
        }
      })
      .finally(() => { if (active) setLoading(false) })
    return () => { active = false; controller.abort() }
  }, [canRead, loadSummary])

  if (!canRead) {
    return <section aria-labelledby="directory-import-heading" className="border-t border-steward-ink-800 pt-6" data-requirement="REQ-DIRECTORY-EXPANSION-002">
      <h3 className="text-lg font-semibold" id="directory-import-heading">Directory import</h3>
      <p className="mt-2 text-sm text-steward-mist-muted">Import history requires <code>integrations.read</code>. Provider credentials remain on the server.</p>
    </section>
  }

  async function showDetail(batchID: string) {
    setBusy(`detail-${batchID}`)
    setError('')
    try {
      setDetail(readDetail(await requestJSON(`/api/v1/directory-imports/${encodeURIComponent(batchID)}`)))
    } catch (operationError) {
      setError(messageFor(operationError, 'The directory import audit detail could not be loaded.'))
    } finally {
      setBusy('')
    }
  }

  async function preview(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!canWrite || !sourceID) return
    setBusy('preview')
    setError('')
    setStatus('')
    try {
      const result = readOperation(await requestJSON('/api/v1/directory-imports/preview', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken, 'Idempotency-Key': operationKey('preview') },
        body: JSON.stringify({ sourceSystemId: sourceID }),
      }))
      await loadSummary()
      setDetail(readDetail(await requestJSON(`/api/v1/directory-imports/${encodeURIComponent(result.batch.id)}`)))
      setStatus(`Preview completed with ${result.batch.counts.created} creates, ${result.batch.counts.updated} updates, ${result.batch.counts.deactivated} deactivations, and ${result.batch.counts.conflicts} conflicts.`)
    } catch (operationError) {
      setError(messageFor(operationError, 'The directory preview could not be completed.'))
    } finally {
      setBusy('')
    }
  }

  async function run(batch: Batch, operation: 'apply' | 'retry') {
    if (!canWrite) return
    setBusy(`${operation}-${batch.id}`)
    setError('')
    setStatus('')
    try {
      const result = readOperation(await requestJSON(`/api/v1/directory-imports/${encodeURIComponent(batch.id)}/${operation}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken, 'Idempotency-Key': operationKey(operation) },
      }))
      await loadSummary()
      setDetail(readDetail(await requestJSON(`/api/v1/directory-imports/${encodeURIComponent(batch.id)}`)))
      if (result.batch.status === 'applied') await onApplied?.()
      setStatus(`${operation === 'apply' ? 'Apply' : 'Retry'} finished with status ${statusLabel(result.batch.status)}.`)
    } catch (operationError) {
      setError(messageFor(operationError, `The directory import ${operation} could not be completed.`))
    } finally {
      setBusy('')
    }
  }

  return (
    <section aria-busy={loading || busy !== ''} aria-labelledby="directory-import-heading" className="border-t border-steward-ink-800 pt-6" data-feature="identity.directory" data-requirement="REQ-DIRECTORY-EXPANSION-002">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h3 className="text-lg font-semibold" id="directory-import-heading">Directory import</h3>
          <p className="mt-1 max-w-3xl text-sm leading-6 text-steward-mist-muted">Preview a server-configured read-only source before applying its exact plan. Provider endpoints and credentials never enter the browser.</p>
        </div>
        <p className="text-sm text-steward-mist-muted">{batches.length} recent {batches.length === 1 ? 'batch' : 'batches'}</p>
      </div>

      {error && <div className="mt-4 rounded-xl border border-steward-danger/50 bg-steward-danger/15 p-4 text-[#ffccd1]" ref={errorRef} role="alert" tabIndex={-1}>{error}</div>}
	  {status && <p aria-live="polite" className="mt-4 rounded-xl border border-steward-primary/40 bg-steward-primary/10 p-4 text-sm text-steward-mist" role="status">{status}</p>}

      {sources.length === 0 && !loading ? (
        <p className="mt-4 rounded-xl border border-dashed border-steward-ink-800 p-5 text-sm leading-6 text-steward-mist-muted">No read-only directory source is configured. Add an Entra, SailPoint, Grouper, or PeopleSoft source through deployment configuration, then restart the service.</p>
      ) : (
        <form aria-label="Preview a directory import" className={`${subpanelClass} mt-4 grid gap-4 p-4 md:grid-cols-[minmax(0,1fr)_auto] md:items-end`} onSubmit={preview}>
          <div>
            <label className={labelClass} htmlFor="directory-import-source">Configured source</label>
            <p className="mt-1 text-sm text-steward-mist-muted" id="directory-import-source-help">Only stable source identities and non-secret configuration revisions are shown.</p>
            <select aria-describedby="directory-import-source-help" className={inputClass} disabled={loading || sources.length === 0} id="directory-import-source" onChange={(event) => setSourceID(event.target.value)} required value={sourceID}>
              {sources.map((source) => <option key={source.id} value={source.id}>{source.id} · {providerLabel(source.provider)}</option>)}
            </select>
          </div>
          {canWrite ? <button className={buttonClass} disabled={busy !== '' || !sourceID} type="submit">{busy === 'preview' ? 'Previewing…' : 'Preview import'}</button>
            : <p className="max-w-sm text-sm text-steward-mist-muted">Applying changes requires <code>integrations.write</code>.</p>}
        </form>
      )}

      <div aria-label="Recent directory imports" className="mt-5 overflow-x-auto rounded-xl border border-steward-ink-800" role="region" tabIndex={0}>
        <table className="min-w-[52rem] w-full text-left text-sm">
          <caption className="sr-only">Recent directory import batches and available actions</caption>
          <thead className="bg-steward-ink-950/70 text-steward-mist-muted"><tr><th className="px-3 py-3" scope="col">Source</th><th className="px-3 py-3" scope="col">Status</th><th className="px-3 py-3" scope="col">Plan</th><th className="px-3 py-3" scope="col">Updated</th><th className="px-3 py-3" scope="col">Actions</th></tr></thead>
          <tbody className="divide-y divide-steward-ink-800">
            {batches.map((batch) => <tr key={batch.id}>
              <th className="px-3 py-3 font-semibold text-steward-mist" scope="row">{batch.sourceSystemId}<span className="mt-1 block text-xs font-normal text-steward-mist-muted">{providerLabel(batch.provider)}</span></th>
              <td className="px-3 py-3 text-steward-mist-muted">{statusLabel(batch.status)}<span className="mt-1 block text-xs">{batch.completeSnapshot ? 'Complete snapshot' : 'Partial snapshot'}</span></td>
              <td className="px-3 py-3 text-steward-mist-muted">{batch.counts.created} create · {batch.counts.updated} update · {batch.counts.deactivated} deactivate · {batch.counts.conflicts} conflict</td>
              <td className="px-3 py-3 text-steward-mist-muted">{formatDate(batch.updatedAt)}</td>
              <td className="px-3 py-3"><div className="flex flex-wrap gap-2">
                <button className={secondaryButtonClass} disabled={busy !== ''} onClick={() => showDetail(batch.id)} type="button">{busy === `detail-${batch.id}` ? 'Loading…' : 'View audit'}</button>
                {canWrite && batch.status === 'previewed' && <button className={buttonClass} disabled={busy !== ''} onClick={() => run(batch, 'apply')} type="button">{busy === `apply-${batch.id}` ? 'Applying…' : 'Apply exact plan'}</button>}
                {canWrite && (batch.status === 'failed' || batch.status === 'partially_applied') && <button className={secondaryButtonClass} disabled={busy !== ''} onClick={() => run(batch, 'retry')} type="button">{busy === `retry-${batch.id}` ? 'Retrying…' : 'Retry failures'}</button>}
              </div></td>
            </tr>)}
            {batches.length === 0 && <tr><td className="px-3 py-5 text-steward-mist-muted" colSpan={5}>{loading ? 'Loading import history…' : 'No import batches have been previewed.'}</td></tr>}
          </tbody>
        </table>
      </div>

      {detail && <ImportAuditDetail detail={detail} />}
    </section>
  )
}

function ImportAuditDetail({ detail }: { detail: BatchDetail }) {
  return <section aria-labelledby="directory-import-audit-heading" className={`${subpanelClass} mt-5 min-w-0 p-4`}>
    <h4 className="font-semibold" id="directory-import-audit-heading">Import audit results</h4>
    <p className="mt-1 break-words text-sm text-steward-mist-muted">Batch {detail.batch.id} · {statusLabel(detail.batch.status)} · configuration {detail.batch.configRevision}</p>
    <div aria-label="Directory import records" className="mt-4 overflow-x-auto" role="region" tabIndex={0}>
      <table className="min-w-[50rem] w-full text-left text-sm">
        <caption className="sr-only">Normalized records and reconciliation outcomes</caption>
        <thead className="text-steward-mist-muted"><tr><th className="px-2 py-2" scope="col">Directory record</th><th className="px-2 py-2" scope="col">Context</th><th className="px-2 py-2" scope="col">Action</th><th className="px-2 py-2" scope="col">Outcome</th></tr></thead>
        <tbody className="divide-y divide-steward-ink-800">{detail.items.map((item) => <tr key={item.id}>
          <th className="px-2 py-3 align-top font-semibold text-steward-mist" scope="row">{item.record.displayName}<span className="mt-1 block break-all text-xs font-normal text-steward-mist-muted">{recordReference(item.record)}</span>{item.record.status === 'inactive' && <span className="mt-1 block text-xs font-normal text-steward-warning">Inactive at {providerLabel(detail.batch.provider)}</span>}<AttributeList attributes={item.record.directoryAttributes} /><AttributeList attributes={item.record.metadata} /></th>
          <td className="px-2 py-3 align-top text-steward-mist-muted">{recordContext(item.record)}</td>
          <td className="px-2 py-3 align-top text-steward-mist-muted">{item.action}</td>
          <td className="px-2 py-3 align-top text-steward-mist-muted">{item.outcome}{item.error && <span className="mt-1 block max-w-sm text-xs text-steward-warning">{item.error}</span>}</td>
        </tr>)}</tbody>
      </table>
    </div>
    <h5 className="mt-5 font-semibold">Attempts</h5>
    <ol className="mt-3 space-y-2">{detail.attempts.map((attempt) => <li className="rounded-lg border border-steward-ink-800 p-3 text-sm text-steward-mist-muted" key={attempt.id}><strong className="text-steward-mist">Attempt {attempt.number}: {attempt.operation}</strong> · {statusLabel(attempt.status)} · {formatDate(attempt.startedAt)}{attempt.error && <span className="mt-1 block text-steward-warning">{attempt.error}</span>}</li>)}</ol>
  </section>
}

function AttributeList({ attributes }: { attributes?: Record<string, string> }) {
  const entries = Object.entries(attributes ?? {}).sort(([left], [right]) => left.localeCompare(right))
  if (entries.length === 0) return null
  return <dl className="mt-2 space-y-1 text-xs font-normal text-steward-mist-muted">{entries.map(([key, value]) => <div className="flex flex-wrap gap-1" key={key}><dt>{key}:</dt><dd className="break-words">{value}</dd></div>)}</dl>
}

function readSources(value: unknown): Source[] {
  const items = collection(value)
  if (items.length > 100 || !items.every(isSource)) throw new Error('invalid directory source response')
  return items
}

function readBatchPage(value: unknown): Batch[] {
  if (!object(value) || !Array.isArray(value.batches) || value.batches.length > 50 || !value.batches.every(isBatch)) throw new Error('invalid directory import response')
  return value.batches
}

function readDetail(value: unknown): BatchDetail {
  if (!object(value) || !isBatch(value.batch) || !Array.isArray(value.items) || !value.items.every(isItem) ||
    value.items.length > 5000 || !Array.isArray(value.attempts) || value.attempts.length > 100 ||
    !value.attempts.every(isAttempt)) throw new Error('invalid directory import detail')
  return { batch: value.batch, items: value.items, attempts: value.attempts }
}

function readOperation(value: unknown): { batch: Batch; replay: boolean } {
  if (!object(value) || !isBatch(value.batch) || typeof value.replay !== 'boolean') throw new Error('invalid directory import operation response')
  return { batch: value.batch, replay: value.replay }
}

function isSource(value: unknown): value is Source {
  return object(value) && string(value.id, 1, 128) && sourceIDPattern.test(value.id) && string(value.provider, 1, 64) && providerPattern.test(value.provider) && string(value.configRevision, 1, 128)
}

function isBatch(value: unknown): value is Batch {
  return object(value) && recordID(value.id) && string(value.sourceSystemId, 1, 128) && sourceIDPattern.test(value.sourceSystemId) &&
    string(value.provider, 1, 64) && providerPattern.test(value.provider) &&
    string(value.configRevision, 1, 128) && statuses.includes(value.status as BatchStatus) && typeof value.completeSnapshot === 'boolean' &&
    isCounts(value.counts) && instant(value.createdAt) && instant(value.updatedAt) && optionalInstant(value.completedAt)
}

function isCounts(value: unknown): value is Counts {
  return object(value) && ['created', 'updated', 'unchanged', 'deactivated', 'conflicts', 'failed'].every((key) => count(value[key]))
}

function isItem(value: unknown): value is ImportItem {
  return object(value) && recordID(value.id) && count(value.ordinal) && isImportRecord(value.record) &&
    ['create', 'update', 'deactivate', 'unchanged', 'conflict'].includes(String(value.action)) &&
    ['pending', 'applied', 'unchanged', 'conflict', 'failed'].includes(String(value.outcome)) && instant(value.updatedAt) &&
    (value.targetId === undefined || recordID(value.targetId)) &&
    (value.expectedRevision === undefined || typeof value.expectedRevision === 'number' && Number.isInteger(value.expectedRevision) && value.expectedRevision >= 1) &&
    (value.failureClass === undefined || ['transient', 'permanent', 'conflict'].includes(String(value.failureClass))) &&
    (value.retryable === undefined || typeof value.retryable === 'boolean') && (value.error === undefined || string(value.error, 1, 240))
}

function isImportRecord(value: unknown): value is ImportRecord {
  if (!object(value) || !string(value.sourceRecordId, 1, 255) || !['identity', 'group', 'membership'].includes(String(value.kind)) ||
    !string(value.displayName, 1, 200) || !['active', 'inactive'].includes(String(value.status)) || !validMetadata(value.metadata)) return false
  if (value.kind === 'identity') {
    if ((value.email !== undefined && !string(value.email, 1, 320)) ||
      (value.identityKind !== undefined && !['person', 'shared', 'public', 'lab'].includes(String(value.identityKind))) ||
      (value.department !== undefined && !string(value.department, 1, 200)) ||
      value.groupName !== undefined || value.description !== undefined || value.groupSourceId !== undefined ||
      value.memberSourceId !== undefined || value.memberKind !== undefined || value.metadata !== undefined) return false
    if (value.directoryAttributes !== undefined && (!object(value.directoryAttributes) || Object.keys(value.directoryAttributes).length > 16 ||
      !Object.entries(value.directoryAttributes).every(([key, item]) => providerPattern.test(key) && string(item, 1, 500)))) return false
    return value.groupSourceIds === undefined || Array.isArray(value.groupSourceIds) && value.groupSourceIds.length <= 256 &&
      new Set(value.groupSourceIds).size === value.groupSourceIds.length && value.groupSourceIds.every((item) => string(item, 1, 255))
  }
  if (value.identityKind !== undefined || value.email !== undefined || value.department !== undefined ||
    value.directoryAttributes !== undefined || value.groupSourceIds !== undefined) return false
  if (value.kind === 'group') {
    return string(value.groupName, 1, 512) && (value.description === undefined || string(value.description, 0, 2000)) &&
      value.groupSourceId === undefined && value.memberSourceId === undefined && value.memberKind === undefined
  }
  return value.groupName === undefined && value.description === undefined && string(value.groupSourceId, 1, 255) &&
    string(value.memberSourceId, 1, 255) && ['subject', 'group'].includes(String(value.memberKind))
}

function validMetadata(value: unknown) {
  return value === undefined || object(value) && Object.keys(value).length <= 16 &&
    Object.entries(value).every(([key, item]) => providerPattern.test(key) && string(item, 0, 500))
}

function isAttempt(value: unknown): value is ImportAttempt {
  return object(value) && recordID(value.id) && ['preview', 'apply', 'retry'].includes(String(value.operation)) &&
    typeof value.number === 'number' && Number.isInteger(value.number) && value.number >= 1 && value.number <= 100 &&
    statuses.includes(value.status as BatchStatus) && string(value.correlationId, 1, 128) && instant(value.startedAt) && optionalInstant(value.completedAt) &&
    (value.failureClass === undefined || ['transient', 'permanent', 'conflict'].includes(String(value.failureClass))) &&
    (value.retryable === undefined || typeof value.retryable === 'boolean') && (value.error === undefined || string(value.error, 1, 240))
}

function collection(value: unknown): unknown[] {
  if (!object(value) || !Array.isArray(value.items)) throw new Error('invalid collection response')
  return value.items
}

function object(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value) }
function string(value: unknown, minimum: number, maximum: number): value is string { return typeof value === 'string' && value.length >= minimum && value.length <= maximum }
function recordID(value: unknown): value is string { return typeof value === 'string' && recordIDPattern.test(value) }
function count(value: unknown): value is number { return typeof value === 'number' && Number.isInteger(value) && value >= 0 && value <= 5000 }
function instant(value: unknown): value is string { return typeof value === 'string' && value.length <= 64 && !Number.isNaN(Date.parse(value)) }
function optionalInstant(value: unknown): value is string | undefined { return value === undefined || instant(value) }
function messageFor(error: unknown, fallback: string) { return error instanceof ApiRequestError ? error.message : fallback }
function statusLabel(value: BatchStatus) { return value.replaceAll('_', ' ') }
function recordReference(record: ImportRecord) {
  if (record.kind === 'identity') return record.email || record.sourceRecordId
  if (record.kind === 'group') return record.groupName || record.sourceRecordId
  return `${record.memberKind} ${record.memberSourceId}`
}
function recordContext(record: ImportRecord) {
  if (record.kind === 'identity') return <>{record.department || 'No department'}<span className="mt-1 block text-xs">{record.groupSourceIds?.length ?? 0} direct group memberships</span></>
  if (record.kind === 'group') return <>Managed group<span className="mt-1 block break-all text-xs">{record.sourceRecordId}</span></>
  return <>Member of <span className="break-all">{record.groupSourceId}</span><span className="mt-1 block text-xs">{record.memberKind} membership</span></>
}
function providerLabel(value: string) {
  if (value === 'entra') return 'Microsoft Entra ID'
  if (value === 'sailpoint') return 'SailPoint Identity Security Cloud'
  if (value === 'grouper') return 'Internet2 Grouper'
  if (value === 'peoplesoft') return 'PeopleSoft Campus Solutions'
  return value
}
function formatDate(value: string) { return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) }
function operationKey(operation: string) { return `directory-${operation}-${globalThis.crypto.randomUUID()}` }
