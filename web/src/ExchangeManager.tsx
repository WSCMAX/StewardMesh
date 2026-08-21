import { type FormEvent, useEffect, useRef, useState } from 'react'
import { ApiRequestError, isRevision, requestArtifact, requestJSON, type Revision } from './api'
import { ProductHeader, StatusBadge, buttonClass, emptyStateClass, panelClass, plainButtonClass, secondaryButtonClass, subpanelClass, tableWrapClass } from './ui'

// Requirements: REQ-EXCHANGE-001, REQ-PATTERNS-001. Features: migration.packages, templates.schemas. GitHub: #8, #9.

export const exchangeMediaType = 'application/vnd.stewardmesh.openinventory+zip'
export const maximumExchangePackageBytes = 32 * 1024 * 1024

type ExchangeReference = { type: string; id: string }
export type ExchangeRecord = ExchangeReference & { revision: Revision; templateId: string; templateVersion: number; dependencies: ExchangeReference[]; hasFile: boolean }
export type ExchangeExcludedRecordType = { type: string; reason: string }
export type ExchangeProviderStatus = { portableRecordTypes: string[]; registeredRecordTypes: string[]; complete: boolean }
type ExchangeRecordOutcome = ExchangeReference & {
  revision: Revision
  checksum: string
  status: 'created' | 'unchanged' | 'holding'
  missingDependencies: ExchangeReference[]
  writeLocked: boolean
}
export type ExchangePackage = {
  packageId: string
  direction: 'export' | 'import'
  schemaVersion: string
  sourceSystemId: string
  archiveSha256: string
  sizeBytes: number
  fileMode: 'metadata' | 'include'
  status: 'processing' | 'completed' | 'holding' | 'failed'
  recordCount: number
  fileCount: number
  createdCount: number
  unchangedCount: number
  holdingCount: number
  records: ExchangeRecordOutcome[]
  errorCode?: string
  createdAt: string
  updatedAt: string
}

type ImportResult = { package: ExchangePackage; replay: boolean }
type ExchangeManagerProps = { csrfToken: string; permissions: readonly string[]; onOpenHelp?: () => void }
type PreparedDownload = { href: string; name: string; packageId: string; sizeBytes: number; sha256: string }

const stableIDPattern = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/
const recordTypePattern = /^[a-z][a-z0-9._-]{0,63}$/
const sha256Pattern = /^[a-f0-9]{64}$/
const errorCodePattern = /^[a-z][a-z0-9_]{0,63}$/
const packageStatuses = new Set(['processing', 'completed', 'holding', 'failed'])
const outcomeStatuses = new Set(['created', 'unchanged', 'holding'])

function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null }
function isCount(value: unknown, maximum = 10_000): value is number { return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0 && value <= maximum }
function isPositiveCount(value: unknown): value is number { return typeof value === 'number' && Number.isSafeInteger(value) && value > 0 }
function isInstant(value: unknown): value is string { return typeof value === 'string' && !Number.isNaN(Date.parse(value)) }
function isReference(value: unknown): value is ExchangeReference {
  return isObject(value) && typeof value.type === 'string' && recordTypePattern.test(value.type)
    && typeof value.id === 'string' && stableIDPattern.test(value.id)
}

function isReferenceList(value: unknown, maximum = 130): value is ExchangeReference[] {
  return Array.isArray(value) && value.length <= maximum && value.every(isReference)
}

function isExchangeRecord(value: unknown): value is ExchangeRecord {
  if (!isReference(value)) return false
  const candidate = value as ExchangeReference & Record<string, unknown>
  return isRevision(candidate.revision) && typeof candidate.templateId === 'string' && stableIDPattern.test(candidate.templateId)
    && isPositiveCount(candidate.templateVersion) && isReferenceList(candidate.dependencies, 128) && typeof candidate.hasFile === 'boolean'
}

function isRecordOutcome(value: unknown): value is ExchangeRecordOutcome {
  if (!isReference(value)) return false
  const candidate = value as ExchangeReference & Record<string, unknown>
  return isRevision(candidate.revision) && typeof candidate.checksum === 'string' && sha256Pattern.test(candidate.checksum)
    && typeof candidate.status === 'string' && outcomeStatuses.has(candidate.status) && isReferenceList(candidate.missingDependencies)
    && typeof candidate.writeLocked === 'boolean'
}

function isExchangePackage(value: unknown): value is ExchangePackage {
  if (!isObject(value)) return false
  const candidate = value as Record<string, unknown>
  if (typeof candidate.packageId !== 'string' || !stableIDPattern.test(candidate.packageId)
    || (candidate.direction !== 'export' && candidate.direction !== 'import') || (candidate.schemaVersion !== '1.0' && candidate.schemaVersion !== '1.1')
    || typeof candidate.sourceSystemId !== 'string' || !stableIDPattern.test(candidate.sourceSystemId)
    || typeof candidate.archiveSha256 !== 'string' || !sha256Pattern.test(candidate.archiveSha256)
    || !isPositiveCount(candidate.sizeBytes) || candidate.sizeBytes > maximumExchangePackageBytes
    || (candidate.fileMode !== 'metadata' && candidate.fileMode !== 'include') || typeof candidate.status !== 'string' || !packageStatuses.has(candidate.status)
    || !isCount(candidate.recordCount) || candidate.recordCount < 1 || !isCount(candidate.fileCount, 1_000)
    || !isCount(candidate.createdCount) || !isCount(candidate.unchangedCount) || !isCount(candidate.holdingCount)
    || !Array.isArray(candidate.records) || candidate.records.length > 10_000 || !candidate.records.every(isRecordOutcome)
    || (candidate.errorCode !== undefined && (typeof candidate.errorCode !== 'string' || !errorCodePattern.test(candidate.errorCode)))
    || !isInstant(candidate.createdAt) || !isInstant(candidate.updatedAt)) return false
  if (candidate.createdCount + candidate.unchangedCount + candidate.holdingCount > candidate.recordCount) return false
  if ((candidate.status === 'holding') !== (candidate.holdingCount > 0)) return false
	if ((candidate.status === 'failed') !== (candidate.errorCode !== undefined)) return false
	if (Date.parse(candidate.updatedAt) < Date.parse(candidate.createdAt)) return false
	if ((candidate.status === 'completed' || candidate.status === 'holding') && candidate.records.length !== candidate.recordCount) return false
	const outcomes = candidate.records.reduce((totals, record) => ({ ...totals, [record.status]: totals[record.status] + 1 }), { created: 0, unchanged: 0, holding: 0 })
	if (outcomes.created !== candidate.createdCount || outcomes.unchanged !== candidate.unchangedCount || outcomes.holding !== candidate.holdingCount) return false
	return true
}

export function parseExchangeRecords(value: unknown): ExchangeRecord[] {
  if (!isObject(value) || !Array.isArray(value.items) || value.items.length > 10_000 || !value.items.every(isExchangeRecord)) {
    throw new Error('invalid Exchange record response')
  }
  return value.items
}

export function parseExchangeExcludedRecordTypes(value: unknown): ExchangeExcludedRecordType[] {
  if (!isObject(value) || !Array.isArray(value.excludedRecordTypes) || value.excludedRecordTypes.length > 51) {
    throw new Error('invalid Exchange exclusion response')
  }
  const seen = new Set<string>()
  return value.excludedRecordTypes.map((item) => {
    if (!isObject(item) || typeof item.type !== 'string' || !recordTypePattern.test(item.type)
      || typeof item.reason !== 'string' || item.reason.trim() !== item.reason || item.reason.length < 1 || item.reason.length > 500
      || seen.has(item.type)) throw new Error('invalid Exchange exclusion response')
    seen.add(item.type)
    return { type: item.type, reason: item.reason }
  })
}

export function parseExchangeProviderStatus(value: unknown): ExchangeProviderStatus {
  if (!isObject(value) || !Array.isArray(value.portableRecordTypes) || value.portableRecordTypes.length < 1 || value.portableRecordTypes.length > 51
    || !Array.isArray(value.registeredRecordTypes) || value.registeredRecordTypes.length > 51 || typeof value.providerRegistryComplete !== 'boolean') {
    throw new Error('invalid Exchange provider status response')
  }
  const parseTypes = (items: unknown[], label: string) => {
    const result: string[] = []
    const seen = new Set<string>()
    for (const item of items) {
      if (typeof item !== 'string' || !recordTypePattern.test(item) || seen.has(item)) throw new Error(`invalid Exchange ${label} response`)
      seen.add(item)
      result.push(item)
    }
    return result
  }
  const portableRecordTypes = parseTypes(value.portableRecordTypes, 'portable provider status')
  const registeredRecordTypes = parseTypes(value.registeredRecordTypes, 'registered provider status')
  const portable = new Set(portableRecordTypes)
  const registered = new Set(registeredRecordTypes)
  const complete = portableRecordTypes.length === registeredRecordTypes.length
    && portableRecordTypes.every((recordType) => registered.has(recordType))
  if (registeredRecordTypes.some((recordType) => !portable.has(recordType))
    || value.providerRegistryComplete !== complete) {
    throw new Error('invalid Exchange provider status response')
  }
  return { portableRecordTypes, registeredRecordTypes, complete: value.providerRegistryComplete }
}

export function parseExchangePackages(value: unknown): ExchangePackage[] {
  if (!isObject(value) || !Array.isArray(value.items) || value.items.length > 100 || !value.items.every(isExchangePackage)) {
    throw new Error('invalid Exchange package response')
  }
  return value.items
}

export function parseExchangeImport(value: unknown): ImportResult {
  if (!isObject(value) || !isExchangePackage(value.package) || typeof value.replay !== 'boolean' || value.package.direction !== 'import') {
    throw new Error('invalid Exchange import response')
  }
  return { package: value.package, replay: value.replay }
}

function referenceKey(reference: ExchangeReference) { return `${reference.type}:${reference.id}` }
function referenceLabel(reference: ExchangeReference) { return `${reference.type} · ${reference.id}` }
export function exchangeRecordTypeDescription(recordType: string) {
	if (recordType === 'bridge.oauth-client') return 'Public PKCE client configuration only; OAuth grants, credentials, and authorization transactions are excluded.'
	return 'Portable domain record'
}
function formatBytes(value: number) {
  if (value < 1024) return `${value} B`
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KiB`
  return `${(value / (1024 * 1024)).toFixed(1)} MiB`
}
function formatInstant(value: string) { return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) }
function title(value: string) { return value.replaceAll('_', ' ').replace(/\b\w/g, (letter) => letter.toUpperCase()) }

export default function ExchangeManager({ csrfToken, permissions, onOpenHelp }: ExchangeManagerProps) {
  const canRead = permissions.includes('integrations.read')
  const canWrite = permissions.includes('integrations.write')
  const [records, setRecords] = useState<ExchangeRecord[]>([])
  const [excludedRecordTypes, setExcludedRecordTypes] = useState<ExchangeExcludedRecordType[]>([])
  const [providerStatus, setProviderStatus] = useState<ExchangeProviderStatus>({ portableRecordTypes: [], registeredRecordTypes: [], complete: false })
  const [packages, setPackages] = useState<ExchangePackage[]>([])
  const [selected, setSelected] = useState<ReadonlySet<string>>(() => new Set())
  const [includeDependencies, setIncludeDependencies] = useState(true)
  const [fileMode, setFileMode] = useState<'metadata' | 'include'>('metadata')
  const [busy, setBusy] = useState<'loading' | 'export' | 'import' | ''>('')
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')
  const [download, setDownload] = useState<PreparedDownload | null>(null)
  const errorRef = useRef<HTMLDivElement>(null)
  const packageInputRef = useRef<HTMLInputElement>(null)
  const downloadRef = useRef<PreparedDownload | null>(null)

  useEffect(() => { downloadRef.current = download }, [download])
  useEffect(() => () => { if (downloadRef.current) URL.revokeObjectURL(downloadRef.current.href) }, [])
  useEffect(() => { if (error) errorRef.current?.focus() }, [error])
  useEffect(() => {
    if (!canRead) return
    const controller = new AbortController()
    setBusy('loading')
    Promise.all([
      requestJSON('/api/v1/exchange/records', { signal: controller.signal }),
      requestJSON('/api/v1/exchange/packages?limit=25', { signal: controller.signal }),
    ]).then(([recordValue, packageValue]) => {
      setRecords(parseExchangeRecords(recordValue))
      setExcludedRecordTypes(parseExchangeExcludedRecordTypes(recordValue))
      setProviderStatus(parseExchangeProviderStatus(recordValue))
      setPackages(parseExchangePackages(packageValue))
      setError('')
    }).catch((cause) => {
      if (cause instanceof DOMException && cause.name === 'AbortError') return
      setError('Exchange records and package history could not be loaded.')
    }).finally(() => { if (!controller.signal.aborted) setBusy('') })
    return () => controller.abort()
  }, [canRead])

  function showError(value: string) { setError(value); setMessage(''); queueMicrotask(() => errorRef.current?.focus()) }

  async function refreshPackages() {
    setPackages(parseExchangePackages(await requestJSON('/api/v1/exchange/packages?limit=25')))
  }

  function toggleRecord(key: string) {
    setSelected((current) => {
      const next = new Set(current)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  async function exportPackage(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const selection = records.filter((record) => selected.has(referenceKey(record))).map(({ type, id }) => ({ type, id }))
    if (selection.length === 0) { showError('Select at least one record to export.'); return }
    setBusy('export'); setError(''); setMessage('')
    try {
      const response = await requestArtifact('/api/v1/exchange/export', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({ selection, includeDependencies, fileMode }),
      })
      const responseType = response.headers.get('Content-Type')?.split(';', 1)[0].trim().toLowerCase()
      const packageId = response.headers.get('X-Exchange-Package-ID')?.trim() ?? ''
      const sha256 = response.headers.get('X-Content-SHA256')?.trim() ?? ''
      if (responseType !== exchangeMediaType || !stableIDPattern.test(packageId) || !sha256Pattern.test(sha256)) throw new Error('invalid Exchange export response')
      const artifact = await response.blob()
      if (artifact.size < 1 || artifact.size > maximumExchangePackageBytes) throw new Error('invalid Exchange export size')
      if (downloadRef.current) URL.revokeObjectURL(downloadRef.current.href)
      const prepared = { href: URL.createObjectURL(artifact), name: `${packageId}.openinventory`, packageId, sizeBytes: artifact.size, sha256 }
      downloadRef.current = prepared
      setDownload(prepared)
      setMessage(`Package ${packageId} is ready to download.`)
      try { await refreshPackages() } catch { setMessage(`Package ${packageId} is ready to download. Package history could not be refreshed.`) }
    } catch (cause) {
      showError(cause instanceof ApiRequestError ? cause.message : 'The Exchange package could not be prepared.')
    } finally { setBusy('') }
  }

  async function importPackage(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const file = packageInputRef.current?.files?.[0]
    if (!file || file.size === 0) { showError('Choose a non-empty .openinventory package.'); return }
    if (file.size > maximumExchangePackageBytes) { showError(`Packages cannot exceed ${formatBytes(maximumExchangePackageBytes)}.`); return }
    if (!file.name.toLowerCase().endsWith('.openinventory')) { showError('Choose a file with the .openinventory extension.'); return }
    if (file.type && file.type !== exchangeMediaType && file.type !== 'application/zip') { showError('The selected file is not an Exchange package.'); return }
    setBusy('import'); setError(''); setMessage('')
    try {
      const result = parseExchangeImport(await requestJSON('/api/v1/exchange/import', {
        method: 'POST', headers: { 'Content-Type': exchangeMediaType, 'X-CSRF-Token': csrfToken }, body: file,
      }))
      setPackages((current) => [result.package, ...current.filter((item) => !(item.direction === 'import' && item.packageId === result.package.packageId))].slice(0, 25))
      form.reset()
      const outcome = result.package.status === 'holding'
        ? `${result.package.holdingCount} record${result.package.holdingCount === 1 ? '' : 's'} placed in holding for review.`
        : `${result.package.createdCount} created and ${result.package.unchangedCount} unchanged.`
      setMessage(`${result.replay ? 'Package replay verified' : 'Import complete'}: ${outcome}`)
    } catch (cause) {
      showError(cause instanceof ApiRequestError ? cause.message : 'The Exchange package could not be imported.')
    } finally { setBusy('') }
  }

  if (!canRead) return <section aria-label="Exchange package workflow" className={`${panelClass} p-5 sm:p-6`} data-feature="migration.packages" data-requirement="REQ-EXCHANGE-001"><div className="flex flex-wrap items-start justify-between gap-4"><div><h2 className="text-2xl font-semibold" id="exchange-heading">Exchange — Migration packages</h2><p className="mt-2 text-steward-mist-muted">Your role does not include permission to view migration records or package history.</p></div>{onOpenHelp && <button className={secondaryButtonClass} onClick={onOpenHelp} type="button">Exchange help</button>}</div></section>

  const allSelected = records.length > 0 && records.every((record) => selected.has(referenceKey(record)))
  return <section aria-label="Exchange package workflow" className="min-w-0 space-y-5" data-feature="migration.packages" data-requirement="REQ-EXCHANGE-001">
    <div className={`${panelClass} p-5 sm:p-6`}>
      <ProductHeader
        actions={onOpenHelp ? <button className={secondaryButtonClass} onClick={onOpenHelp} type="button">Exchange help</button> : undefined}
        description="Export selected records with provenance and checksums, or import a bounded .openinventory package. Imports preserve source identity and remain write-locked until an explicit ownership claim."
        headingId="exchange-heading"
        kicker="Portable, dependency-aware archives"
        title="Exchange — Migration packages"
      />
      {error && <div className="mt-4 rounded-xl border border-steward-danger/50 bg-steward-danger/15 p-4 text-sm text-[#ffccd1]" ref={errorRef} role="alert" tabIndex={-1}>{error}</div>}
      {providerStatus.portableRecordTypes.length > 0 && <div className={`mt-4 rounded-xl border p-4 text-sm leading-6 ${providerStatus.complete ? 'border-steward-teal/35 bg-steward-teal/10 text-steward-mist-muted' : 'border-steward-warning/45 bg-steward-warning/10 text-[#ffdca8]'}`} role="status"><strong className="text-steward-mist">Provider availability:</strong> {providerStatus.registeredRecordTypes.length} of {providerStatus.portableRecordTypes.length} portable record families are registered.{!providerStatus.complete && ' This build does not satisfy the complete phase-one Exchange provider gate; only records shown below are selectable.'}</div>}
      <p aria-live="polite" className="mt-4 text-sm font-semibold text-[#aaf0c6]" role="status">{message}</p>
    </div>

    <form className={`${panelClass} min-w-0 p-5 sm:p-6`} onSubmit={exportPackage}>
      <div className="flex flex-wrap items-start justify-between gap-3"><div><h3 className="text-xl font-semibold" id="exchange-export-heading">Build an export package</h3><p className="mt-2 max-w-3xl text-sm leading-6 text-steward-mist-muted">Choose explicit records. Required dependencies can be added automatically; the server still validates the complete dependency graph.</p></div>{records.length > 0 && <button className={plainButtonClass} disabled={!canWrite || busy !== ''} onClick={() => setSelected(allSelected ? new Set() : new Set(records.map(referenceKey)))} type="button">{allSelected ? 'Clear selection' : 'Select all records'}</button>}</div>
      {records.length === 0 ? <p className={`${emptyStateClass} mt-4`}>{busy === 'loading' ? 'Loading portable records…' : 'No portable records are available.'}</p> : <fieldset className="mt-4 min-w-0 max-w-full" disabled={!canWrite || busy !== ''}><legend className="sr-only">Records to export</legend><div aria-label="Portable records" className={tableWrapClass} role="region" tabIndex={0}><table className="min-w-[52rem] w-full text-left text-sm"><thead className="border-b border-white/10 text-xs uppercase tracking-wide text-steward-slate"><tr><th className="px-4 py-3" scope="col">Select</th><th className="px-4 py-3" scope="col">Record</th><th className="px-4 py-3" scope="col">Revision</th><th className="px-4 py-3" scope="col">Patterns schema</th><th className="px-4 py-3" scope="col">Dependencies</th><th className="px-4 py-3" scope="col">File</th></tr></thead><tbody>{records.map((record) => { const key = referenceKey(record); return <tr className="border-b border-white/[0.07] last:border-0" key={key}><td className="px-4 py-3"><input aria-label={`Select ${referenceLabel(record)}`} checked={selected.has(key)} className="size-5 accent-steward-teal" onChange={() => toggleRecord(key)} type="checkbox" /></td><th className="px-4 py-3 font-semibold text-steward-mist" scope="row"><span className="block">{record.type}</span><span className="mt-1 block text-xs font-normal leading-5 text-steward-mist-muted">{exchangeRecordTypeDescription(record.type)}</span><span className="mt-1 block break-all font-mono text-xs font-normal text-steward-mist-muted">{record.id}</span></th><td className="px-4 py-3 text-steward-mist-muted">{record.revision}</td><td className="px-4 py-3 text-steward-mist-muted"><span className="block break-all font-mono text-xs">{record.templateId}</span><span className="mt-1 block">version {record.templateVersion}</span></td><td className="px-4 py-3 text-steward-mist-muted">{record.dependencies.length === 0 ? 'None' : record.dependencies.map(referenceLabel).join(', ')}</td><td className="px-4 py-3 text-steward-mist-muted">{record.hasFile ? 'Available' : 'None'}</td></tr> })}</tbody></table></div></fieldset>}
      <div className="mt-5 grid gap-4 lg:grid-cols-2">
        <fieldset className={`${subpanelClass} p-4`} disabled={!canWrite || busy !== ''}><legend className="font-semibold">Dependency scope</legend><label className="mt-3 flex min-h-11 items-start gap-3 text-sm leading-6"><input aria-label="Include required dependencies" checked={includeDependencies} className="mt-1 size-5 shrink-0 accent-steward-teal" onChange={(event) => setIncludeDependencies(event.target.checked)} type="checkbox" /><span><strong className="block text-steward-mist">Include required dependencies</strong><span className="text-steward-mist-muted">Recommended for a complete round trip. Dependencies are ordered before records that use them.</span></span></label></fieldset>
        <fieldset className={`${subpanelClass} p-4`} disabled={!canWrite || busy !== ''}><legend className="font-semibold">Vault file handling</legend><label className="mt-3 flex min-h-11 items-start gap-3 text-sm leading-6"><input aria-label="Metadata only" checked={fileMode === 'metadata'} className="mt-1 size-5 shrink-0 accent-steward-teal" name="file-mode" onChange={() => setFileMode('metadata')} type="radio" /><span><strong className="block text-steward-mist">Metadata only</strong><span className="text-steward-mist-muted">Move checksums, names, provider metadata, and relationships without file bytes.</span></span></label><label className="mt-3 flex min-h-11 items-start gap-3 text-sm leading-6"><input aria-label="Include file bytes" checked={fileMode === 'include'} className="mt-1 size-5 shrink-0 accent-steward-teal" name="file-mode" onChange={() => setFileMode('include')} type="radio" /><span><strong className="block text-steward-mist">Include file bytes</strong><span className="text-steward-mist-muted">Embed bounded, checksummed Vault content. Credentials and signed URLs are never packaged.</span></span></label></fieldset>
      </div>
      <div className="mt-5 flex flex-wrap items-center gap-3"><button className={buttonClass} disabled={!canWrite || busy !== '' || selected.size === 0} type="submit">{busy === 'export' ? 'Preparing package…' : 'Prepare export'}</button>{!canWrite && <span className="text-sm text-steward-warning">Requires integrations.write</span>}{download && <a className={secondaryButtonClass} download={download.name} href={download.href}>Download {download.name}</a>}</div>
      {download && <p className="mt-3 break-words text-xs leading-5 text-steward-mist-muted">{formatBytes(download.sizeBytes)} · SHA-256 <code className="break-all">{download.sha256}</code></p>}
    </form>

    <section aria-labelledby="exchange-exclusions-heading" className={`${panelClass} min-w-0 p-5 sm:p-6`}>
      <h3 className="text-xl font-semibold" id="exchange-exclusions-heading">Deliberately non-portable records</h3>
      <p className="mt-2 max-w-3xl text-sm leading-6 text-steward-mist-muted">These record families remain destination-owned, derived, operational, or security-sensitive. They are never silently interpreted as ordinary domain data.</p>
      {excludedRecordTypes.length === 0 ? <p className={`${emptyStateClass} mt-4`}>No exclusion policy was returned.</p> : <dl className="mt-4 grid min-w-0 gap-3 lg:grid-cols-2">{excludedRecordTypes.map((item) => <div className={`${subpanelClass} min-w-0 p-4`} key={item.type}><dt className="break-all font-mono text-sm font-semibold text-steward-mist">{item.type}</dt><dd className="mt-2 text-sm leading-6 text-steward-mist-muted">{item.reason}</dd></div>)}</dl>}
    </section>

    <form className={`${panelClass} p-5 sm:p-6`} onSubmit={importPackage}>
      <h3 className="text-xl font-semibold">Import a package</h3><p className="mt-2 max-w-3xl text-sm leading-6 text-steward-mist-muted">Packages are limited to {formatBytes(maximumExchangePackageBytes)}. StewardMesh verifies the archive, manifest, checksums, identity, and dependencies before domain records are written.</p>
      <label className="mt-4 block text-sm font-semibold text-steward-mist" htmlFor="exchange-package">.openinventory package</label><input accept=".openinventory,application/vnd.stewardmesh.openinventory+zip,application/zip" className="mt-2 block min-h-11 w-full min-w-0 rounded-xl border border-white/10 bg-steward-ink-950/75 p-2 text-sm text-steward-mist file:mr-3 file:rounded-lg file:border-0 file:bg-steward-teal file:px-3 file:py-2 file:font-semibold file:text-steward-ink-950" disabled={!canWrite || busy !== ''} id="exchange-package" name="package" ref={packageInputRef} required type="file" />
      <div className="mt-4 flex flex-wrap items-center gap-3"><button className={buttonClass} disabled={!canWrite || busy !== ''} type="submit">{busy === 'import' ? 'Verifying package…' : 'Import package'}</button>{!canWrite && <span className="text-sm text-steward-warning">Requires integrations.write</span>}</div>
    </form>

    <section aria-labelledby="exchange-history-heading" className={`${panelClass} min-w-0 p-5 sm:p-6`}>
      <div className="flex flex-wrap items-start justify-between gap-3"><div><h3 className="text-xl font-semibold" id="exchange-history-heading">Package history</h3><p className="mt-2 text-sm leading-6 text-steward-mist-muted">Receipts make retries and exact replays visible without exposing package payloads or operator identities.</p></div><p className="text-sm text-steward-mist-muted">Latest {packages.length} of 25</p></div>
      {packages.length === 0 ? <p className={`${emptyStateClass} mt-4`}>{busy === 'loading' ? 'Loading package history…' : 'No packages have been processed.'}</p> : <div className="mt-4 min-w-0 max-w-full space-y-3">{packages.map((item) => <PackageHistory key={`${item.direction}:${item.packageId}`} value={item} />)}</div>}
      <div className="mt-5 rounded-xl border border-steward-warning/35 bg-steward-warning/10 p-4 text-sm leading-6 text-steward-mist-muted"><strong className="text-[#ffd08a]">Ownership boundary:</strong> imported records are readable but write-locked until an administrator explicitly claims them in <a className="font-semibold text-steward-teal underline underline-offset-4" href="#workspace-guard">Guard</a>. Holding records are not written and show the dependencies that must be resolved.</div>
    </section>
  </section>
}

function PackageHistory({ value }: { value: ExchangePackage }) {
  const statusTone = value.status === 'completed' ? 'success' : value.status === 'holding' || value.status === 'failed' ? 'warning' : 'info'
  return <details className={`${subpanelClass} min-w-0 max-w-full overflow-hidden`}><summary className="flex min-h-12 min-w-0 cursor-pointer list-none flex-wrap items-center justify-between gap-3 px-4 py-3 marker:hidden"><span className="min-w-0"><span className="font-semibold text-steward-mist">{title(value.direction)} · <span className="break-all font-mono text-sm">{value.packageId}</span></span><span className="mt-1 block text-xs text-steward-mist-muted">{formatInstant(value.updatedAt)} · {formatBytes(value.sizeBytes)} · schema {value.schemaVersion}</span></span><StatusBadge tone={statusTone}>{title(value.status)}</StatusBadge></summary><div className="min-w-0 max-w-full border-t border-white/[0.08] p-4"><dl className="grid min-w-0 gap-3 text-sm sm:grid-cols-2 xl:grid-cols-4"><div><dt className="text-steward-slate">Source system</dt><dd className="mt-1 break-all font-semibold">{value.sourceSystemId}</dd></div><div><dt className="text-steward-slate">Records</dt><dd className="mt-1 font-semibold">{value.recordCount} total · {value.createdCount} created · {value.unchangedCount} unchanged · {value.holdingCount} holding</dd></div><div><dt className="text-steward-slate">Files</dt><dd className="mt-1 font-semibold">{value.fileCount} · {value.fileMode === 'include' ? 'bytes included' : 'metadata only'}</dd></div><div><dt className="text-steward-slate">Archive SHA-256</dt><dd className="mt-1 break-all font-mono text-xs">{value.archiveSha256}</dd></div></dl>{value.errorCode && <p className="mt-4 text-sm text-[#ffccd1]">Failure code: <code>{value.errorCode}</code></p>}{value.records.length > 0 && <div aria-label={`Outcomes for package ${value.packageId}`} className={`${tableWrapClass} mt-4`} role="region" tabIndex={0}><table className="min-w-[46rem] w-full text-left text-sm"><thead className="border-b border-white/10 text-xs uppercase tracking-wide text-steward-slate"><tr><th className="px-4 py-3" scope="col">Record</th><th className="px-4 py-3" scope="col">Outcome</th><th className="px-4 py-3" scope="col">Ownership</th><th className="px-4 py-3" scope="col">Missing dependencies</th></tr></thead><tbody>{value.records.map((record) => <tr className="border-b border-white/[0.07] last:border-0" key={referenceKey(record)}><th className="px-4 py-3" scope="row"><span className="block font-semibold">{record.type}</span><span className="mt-1 block break-all font-mono text-xs font-normal text-steward-mist-muted">{record.id} · revision {record.revision}</span></th><td className="px-4 py-3"><StatusBadge tone={record.status === 'holding' ? 'warning' : record.status === 'created' ? 'success' : 'neutral'}>{title(record.status)}</StatusBadge></td><td className="px-4 py-3 text-steward-mist-muted">{record.writeLocked ? 'Write locked until claimed' : record.status === 'holding' ? 'Not imported' : 'No import lock'}</td><td className="px-4 py-3 text-steward-mist-muted">{record.missingDependencies.length === 0 ? 'None' : <ul className="list-disc space-y-1 pl-4">{record.missingDependencies.map((dependency) => <li className="break-all" key={referenceKey(dependency)}>{referenceLabel(dependency)}</li>)}</ul>}</td></tr>)}</tbody></table></div>}</div></details>
}
