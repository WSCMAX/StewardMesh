import { useId, useMemo, useState } from 'react'
import { requestJSON } from './api'
import { filterLookupOptions, mergeLookupOptions, type LookupCreateConfig } from './grid/columns'
import { compactInputClass, cx, inputClass, labelClass, menuSurfaceClass, plainButtonClass, secondaryButtonClass } from './ui'

export type SearchableRecord = { id: string; label: string; detail?: string }

export type RecordSearchKind = 'asset' | 'document' | 'room' | 'identity' | 'site' | 'building' | 'department' | 'model' | 'purchase-order' | 'vendor'

type RecordSearchPickerProps = {
  label: string
  help?: string
  kind: RecordSearchKind
  selected: SearchableRecord[]
  onChange: (records: SearchableRecord[]) => void
  multiple?: boolean
  compact?: boolean
  /** Writes the selected id (or pipe-delimited ids) into a surrounding form. */
  name?: string
  options?: readonly SearchableRecord[]
  create?: LookupCreateConfig
  browseHref?: string
  browseLabel?: string
  onBrowse?: () => void
}

function items(value: unknown): unknown[] {
  if (typeof value !== 'object' || value === null) return []
  const candidate = (value as Record<string, unknown>).items
  return Array.isArray(candidate) ? candidate : []
}

function recordFromUnknown(value: unknown, kind: RecordSearchKind): SearchableRecord | null {
  if (typeof value !== 'object' || value === null) return null
  const item = value as Record<string, unknown>
  if (typeof item.id !== 'string' || item.id.length === 0) return null
  if (kind === 'asset') {
    const name = typeof item.name === 'string' ? item.name : item.id
    const tag = typeof item.assetTag === 'string' ? item.assetTag : ''
    return { id: item.id, label: name, detail: [tag, typeof item.kind === 'string' ? item.kind : ''].filter(Boolean).join(' · ') }
  }
  if (kind === 'document') {
    const name = typeof item.name === 'string' ? item.name : item.id
    const mediaType = typeof item.mediaType === 'string' ? item.mediaType : ''
    return { id: item.id, label: name, detail: mediaType }
  }
  if (kind === 'identity') {
    const name = typeof item.displayName === 'string' && item.displayName ? item.displayName : typeof item.name === 'string' ? item.name : item.id
    const email = typeof item.email === 'string' ? item.email : ''
    return { id: item.id, label: name, detail: email || undefined }
  }
  if (kind === 'model') {
    const manufacturer = typeof item.manufacturer === 'string' ? item.manufacturer : ''
    const name = typeof item.name === 'string' ? item.name : item.id
    const modelNumber = typeof item.modelNumber === 'string' ? item.modelNumber : ''
    const status = typeof item.status === 'string' ? item.status : ''
    const kindLabel = typeof item.kind === 'string' ? item.kind : item.id
    const detailParts = [kindLabel]
    if (status === 'retired') detailParts.push('retired')
    return { id: item.id, label: `${manufacturer} ${name}${modelNumber ? ` ${modelNumber}` : ''}`.trim(), detail: detailParts.join(' · ') }
  }
  if (kind === 'purchase-order') {
    const number = typeof item.number === 'string' ? item.number : item.id
    const status = typeof item.status === 'string' ? item.status : ''
    return { id: item.id, label: number, detail: status || undefined }
  }
  if (kind === 'vendor') {
    const name = typeof item.name === 'string' ? item.name : item.id
    return { id: item.id, label: name, detail: typeof item.status === 'string' ? item.status : undefined }
  }
  const name = typeof item.name === 'string' && item.name ? item.name : typeof item.number === 'string' ? item.number : item.id
  const number = typeof item.number === 'string' ? item.number : ''
  const extra = typeof item.displayName === 'string' ? item.displayName : ''
  return { id: item.id, label: extra || name, detail: number && number !== name ? number : undefined }
}

function filterRecords(records: readonly SearchableRecord[], query: string) {
  return filterLookupOptions(records, query)
}

async function searchRecords(kind: RecordSearchKind, query: string): Promise<SearchableRecord[]> {
  const encoded = encodeURIComponent(query.trim())
  const needle = query.trim().toLowerCase()
  if (kind === 'asset') {
    const response = await requestJSON(`/api/v1/assets?q=${encoded}&limit=20`)
    return items(response).map((item) => recordFromUnknown(item, kind)).filter((item): item is SearchableRecord => item !== null)
  }
  if (kind === 'document') {
    const response = await requestJSON('/api/v1/blobs')
    return items(response)
      .map((item) => recordFromUnknown(item, kind))
      .filter((item): item is SearchableRecord => item !== null)
      .filter((item) => !needle || item.label.toLowerCase().includes(needle) || item.id.toLowerCase().includes(needle) || (item.detail ?? '').toLowerCase().includes(needle))
      .slice(0, 20)
  }
  if (kind === 'identity') {
    const response = await requestJSON(`/api/v1/identities?q=${encoded}&kind=person&limit=20`)
    return items(response).map((item) => recordFromUnknown(item, kind)).filter((item): item is SearchableRecord => item !== null)
  }
  if (kind === 'model') {
    const params = new URLSearchParams({ limit: '20', includeRetired: 'true' })
    if (needle) params.set('q', query.trim())
    const response = await requestJSON(`/api/v1/asset-models?${params.toString()}`)
    return items(response).map((item) => recordFromUnknown(item, kind)).filter((item): item is SearchableRecord => item !== null)
  }
  if (kind === 'purchase-order' || kind === 'vendor') {
    const response = await requestJSON('/api/v1/ledger')
    if (typeof response !== 'object' || response === null) return []
    const body = response as Record<string, unknown>
    const source = kind === 'vendor' ? body.vendors : body.purchaseOrders
    const list = Array.isArray(source) ? source : []
    return list
      .map((item) => recordFromUnknown(item, kind))
      .filter((item): item is SearchableRecord => item !== null)
      .filter((item) => !needle || item.label.toLowerCase().includes(needle) || item.id.toLowerCase().includes(needle) || (item.detail ?? '').toLowerCase().includes(needle))
      .slice(0, 20)
  }
  const path = kind === 'site' ? '/api/v1/sites' : kind === 'building' ? '/api/v1/buildings' : kind === 'department' ? '/api/v1/departments' : '/api/v1/rooms'
  const response = await requestJSON(path)
  return items(response)
    .map((item) => recordFromUnknown(item, kind))
    .filter((item): item is SearchableRecord => item !== null)
    .filter((item) => !needle || item.label.toLowerCase().includes(needle) || item.id.toLowerCase().includes(needle) || (item.detail ?? '').toLowerCase().includes(needle))
    .slice(0, 20)
}

export default function RecordSearchPicker({
  browseHref, browseLabel, compact, create, help, kind, label, multiple = true, name, onBrowse, onChange, options, selected,
}: RecordSearchPickerProps) {
  const generated = useId()
  const inputId = `search-${kind}-${generated}`
  const helpId = help ? `${inputId}-help` : undefined
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<SearchableRecord[]>(() => options ? [...options] : [])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [creating, setCreating] = useState(false)
  const [createValues, setCreateValues] = useState<Record<string, string>>({})
  const [createBusy, setCreateBusy] = useState(false)
  const selectedIDs = useMemo(() => new Set(selected.map((item) => item.id)), [selected])
  const visible = useMemo(
    () => mergeLookupOptions(filterRecords(options ?? [], query), filterRecords(results, query)),
    [options, query, results],
  )

  async function runSearch(value = query) {
    setBusy(true)
    setError('')
    try {
      setResults(await searchRecords(kind, value))
    } catch {
      setResults(filterRecords(options ?? [], value) as SearchableRecord[])
      setError('Matching records could not be searched.')
    } finally {
      setBusy(false)
    }
  }

  function choose(record: SearchableRecord) {
    if (multiple) {
      if (selectedIDs.has(record.id)) return
      onChange([...selected, record])
    } else {
      onChange([record])
      setQuery('')
    }
  }

  function remove(id: string) {
    onChange(selected.filter((item) => item.id !== id))
  }

  async function submitCreate() {
    if (!create) return
    const missing = create.fields.find((field) => field.required && !String(createValues[field.key] ?? '').trim())
    if (missing) {
      setError(`${missing.label} is required.`)
      return
    }
    setCreateBusy(true)
    setError('')
    try {
      const created = await create.submit(createValues)
      setCreating(false)
      setCreateValues({})
      setResults((current) => mergeLookupOptions([created], current))
      choose(created)
    } catch {
      setError(`${create.label} could not be saved.`)
    } finally {
      setCreateBusy(false)
    }
  }

  const hiddenValue = multiple ? selected.map((item) => item.id).join('|') : (selected[0]?.id ?? '')

  return (
    <div className="min-w-0">
      <label className={labelClass} htmlFor={inputId}>{label}</label>
      {help && <p className="mt-1 text-xs font-normal leading-5 text-steward-mist-muted" id={helpId}>{help}</p>}
      {name && <input name={name} type="hidden" value={hiddenValue} />}
      <div className="mt-2 flex flex-wrap gap-2">
        <input
          aria-describedby={helpId}
          className={compact ? compactInputClass : inputClass}
          id={inputId}
          onChange={(event) => setQuery(event.target.value)}
          onFocus={() => { if (results.length === 0 && !busy) void runSearch(query) }}
          onKeyDown={(event) => { if (event.key === 'Enter') { event.preventDefault(); void runSearch() } }}
          placeholder={`Search ${label.toLowerCase()}`}
          type="search"
          value={query}
        />
        <button className={secondaryButtonClass} onClick={() => void runSearch()} type="button">{busy ? 'Searching…' : 'Search'}</button>
        {create && <button className={secondaryButtonClass} onClick={() => setCreating((current) => !current)} type="button">{creating ? 'Cancel' : `+ ${create.label}`}</button>}
        {browseHref && <a className={plainButtonClass} href={browseHref} onClick={onBrowse}>{browseLabel ?? `Open ${label}`}</a>}
        {!browseHref && onBrowse && <button className={plainButtonClass} onClick={onBrowse} type="button">{browseLabel ?? `Open ${label}`}</button>}
      </div>
      {error && <p className="mt-2 text-sm text-[#ffccd1]" role="alert">{error}</p>}
      {creating && create && <div className={cx(menuSurfaceClass, 'mt-2 grid gap-3 p-3')}>
        {create.fields.map((field) => (
          <label className={labelClass} key={field.key}>
            {field.label}
            {field.options
              ? <select
                className={compact ? compactInputClass : inputClass}
                onChange={(event) => setCreateValues((current) => ({ ...current, [field.key]: event.target.value }))}
                required={field.required}
                value={createValues[field.key] ?? ''}
              >
                <option value="">{field.placeholder ?? `Select ${field.label.toLowerCase()}`}</option>
                {field.options.map((option) => <option key={option.id} value={option.id}>{option.label}</option>)}
              </select>
              : <input
                className={compact ? compactInputClass : inputClass}
                onChange={(event) => setCreateValues((current) => ({ ...current, [field.key]: event.target.value }))}
                placeholder={field.placeholder}
                required={field.required}
                value={createValues[field.key] ?? ''}
              />}
          </label>
        ))}
        <button className={secondaryButtonClass} disabled={createBusy} onClick={() => void submitCreate()} type="button">{createBusy ? 'Saving…' : create.label}</button>
      </div>}
      {visible.length > 0 && (
        <div className={cx(menuSurfaceClass, 'mt-2 max-h-48 overflow-y-auto p-1 steward-scrollbar')} role="listbox" aria-label={`${label} matches`}>
          {visible.map((record) => (
            <button
              aria-selected={selectedIDs.has(record.id)}
              className="flex min-h-11 w-full flex-col items-start rounded-lg px-3 py-2 text-left text-sm hover:bg-steward-teal/10 disabled:opacity-50"
              disabled={selectedIDs.has(record.id)}
              key={record.id}
              onClick={() => choose(record)}
              role="option"
              type="button"
            >
              <span className="font-semibold">{record.label}</span>
              <span className="break-all text-xs text-steward-mist-muted">{record.detail || record.id}</span>
            </button>
          ))}
        </div>
      )}
      {selected.length > 0 && (
        <ul className="mt-3 flex flex-wrap gap-2">
          {selected.map((record) => (
            <li className="flex items-center gap-2 rounded-full border border-white/10 bg-steward-ink-950 px-3 py-1 text-sm" key={record.id}>
              <span className="max-w-56 truncate">{record.label}</span>
              <button className={plainButtonClass} onClick={() => remove(record.id)} type="button">Remove</button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
