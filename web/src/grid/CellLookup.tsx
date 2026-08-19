import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { compactInputClass, cx, menuSurfaceClass, plainButtonClass } from '../ui'
import { encodeLookupText, filterLookupOptions, mergeLookupOptions, parseLookupText, type LookupConfig, type LookupOption } from './columns'

// Requirements: REQ-WORKSPACE-001, REQ-ATLAS-001, A11Y-001. Feature: experience.grid.

// An in-cell people (or record) picker. Selected records sit as chips; one can
// be marked primary. The canonical cell text stays a pipe-delimited id list so
// copy, paste, and the write queue never have to know about display names.

export default function CellLookup({
  anchor, label, lookup, value, onChange, onClose,
}: {
  anchor: DOMRect
  label: string
  lookup: LookupConfig
  value: string
  onChange: (text: string) => void
  onClose: () => void
}) {
  const panelRef = useRef<HTMLDivElement | null>(null)
  const inputRef = useRef<HTMLInputElement | null>(null)
  const [position, setPosition] = useState({ x: anchor.left, y: anchor.bottom + 4 })
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<readonly LookupOption[]>(lookup.options ?? [])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [creating, setCreating] = useState(false)
  const [createValues, setCreateValues] = useState<Record<string, string>>({})
  const [createBusy, setCreateBusy] = useState(false)
  const selected = parseLookupText(value)
  const selectedIDs = new Set(selected.map((item) => item.id))
  const known = new Map([...(lookup.options ?? []), ...results].map((option) => [option.id, option]))
  const visible = useMemo(
    () => mergeLookupOptions(filterLookupOptions(lookup.options ?? [], query), filterLookupOptions(results, query)),
    [lookup.options, query, results],
  )

  useLayoutEffect(() => {
    const element = panelRef.current
    if (!element) return
    const { width, height } = element.getBoundingClientRect()
    const margin = 8
    setPosition({
      x: Math.max(margin, Math.min(anchor.left, window.innerWidth - width - margin)),
      y: anchor.bottom + 4 + height > window.innerHeight - margin
        ? Math.max(margin, anchor.top - height - 4)
        : anchor.bottom + 4,
    })
  }, [anchor.left, anchor.top, anchor.bottom, visible.length, selected.length, creating])

  useEffect(() => {
    inputRef.current?.focus()
  }, [])

  useEffect(() => {
    function handlePointerDown(event: PointerEvent) {
      if (!panelRef.current?.contains(event.target as Node)) onClose()
    }
    function handleKey(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        onClose()
        event.preventDefault()
        event.stopPropagation()
      }
    }
    document.addEventListener('pointerdown', handlePointerDown, true)
    window.addEventListener('keydown', handleKey, true)
    return () => {
      document.removeEventListener('pointerdown', handlePointerDown, true)
      window.removeEventListener('keydown', handleKey, true)
    }
  }, [onClose])

  async function runSearch(value = query) {
    setBusy(true)
    setError('')
    try {
      setResults(await lookup.search(value))
    } catch {
      setResults(filterLookupOptions(lookup.options ?? [], value))
      setError(`Matching ${label.toLowerCase()} records could not be searched.`)
    } finally {
      setBusy(false)
    }
  }

  function commit(next: typeof selected) {
    onChange(encodeLookupText(next))
  }

  function choose(option: LookupOption) {
    if (selectedIDs.has(option.id)) return
    if (!lookup.multiple) {
      commit([{ id: option.id, primary: Boolean(lookup.allowPrimary) }])
      onClose()
      return
    }
    commit([...selected, { id: option.id, primary: selected.length === 0 && Boolean(lookup.allowPrimary) }])
    setQuery('')
    setResults(lookup.options ?? [])
  }

  function chooseFromQuery() {
    const trimmed = query.trim()
    if (!trimmed) {
      void runSearch('')
      return
    }
    const matched = visible.find((option) => option.label.toLowerCase() === trimmed.toLowerCase() || option.id.toLowerCase() === trimmed.toLowerCase())
    if (matched) {
      choose(matched)
      return
    }
    void runSearch(trimmed)
  }

  function remove(id: string) {
    const remaining = selected.filter((item) => item.id !== id)
    if (lookup.allowPrimary && remaining.length > 0 && !remaining.some((item) => item.primary)) {
      remaining[0] = { ...remaining[0], primary: true }
    }
    commit(remaining)
  }

  function makePrimary(id: string) {
    commit(selected.map((item) => ({ ...item, primary: item.id === id })))
  }

  async function submitCreate() {
    if (!lookup.create) return
    const missing = lookup.create.fields.find((field) => field.required && !String(createValues[field.key] ?? '').trim())
    if (missing) {
      setError(`${missing.label} is required.`)
      return
    }
    setCreateBusy(true)
    setError('')
    try {
      const created = await lookup.create.submit(createValues)
      setCreating(false)
      setCreateValues({})
      setResults((current) => mergeLookupOptions([created], current))
      choose(created)
    } catch {
      setError(`${lookup.create.label} could not be saved.`)
    } finally {
      setCreateBusy(false)
    }
  }

  function browse() {
    lookup.onBrowse?.()
    if (!lookup.browseHref) onClose()
  }

  return createPortal(
    <div
      aria-label={`Choose ${label}`}
      className={cx(menuSurfaceClass, 'fixed z-50 w-80 p-2')}
      onWheel={(event) => event.stopPropagation()}
      ref={panelRef}
      role="dialog"
      style={{ left: position.x, top: position.y }}
    >
      <div className="flex gap-2">
        <input
          aria-label={`Search ${label}`}
          className={cx(compactInputClass, 'min-h-8 flex-1 px-2 py-1 text-xs')}
          onChange={(event) => setQuery(event.target.value)}
          onFocus={() => { if (results.length === 0 && !busy) void runSearch(query) }}
          onKeyDown={(event) => {
            if (event.key === 'Enter') {
              event.preventDefault()
              event.stopPropagation()
              chooseFromQuery()
            }
          }}
          placeholder={`Search ${label.toLowerCase()}`}
          ref={inputRef}
          type="search"
          value={query}
        />
        <button className={cx(plainButtonClass, 'min-h-8 px-2 py-1 text-xs')} onClick={() => void runSearch()} type="button">
          {busy ? '…' : 'Search'}
        </button>
      </div>
      <div className="mt-2 flex flex-wrap items-center gap-1">
        {lookup.create && <button className={cx(plainButtonClass, 'min-h-8 px-2 py-1 text-xs')} onClick={() => setCreating((current) => !current)} type="button">
          {creating ? 'Cancel' : `+ ${lookup.create.label}`}
        </button>}
        {(lookup.browseHref || lookup.onBrowse) && (
          lookup.browseHref
            ? <a className={cx(plainButtonClass, 'min-h-8 px-2 py-1 text-xs')} href={lookup.browseHref} onClick={browse}>{lookup.browseLabel ?? `Open ${label}`}</a>
            : <button className={cx(plainButtonClass, 'min-h-8 px-2 py-1 text-xs')} onClick={() => { browse(); onClose() }} type="button">{lookup.browseLabel ?? `Open ${label}`}</button>
        )}
        {selected.length > 0 && !lookup.multiple && <button className={cx(plainButtonClass, 'min-h-8 px-2 py-1 text-xs')} onClick={() => { commit([]); onClose() }} type="button">Clear</button>}
      </div>
      {error && <p className="mt-2 text-xs text-[#ffccd1]" role="alert">{error}</p>}
      {creating && lookup.create && <form className="mt-2 grid gap-2 rounded-lg border border-white/10 p-2" onSubmit={(event) => { event.preventDefault(); void submitCreate() }}>
        {lookup.create.fields.map((field) => (
          <label className="block text-[10px] font-semibold uppercase tracking-[0.12em] text-steward-slate" key={field.key}>
            {field.label}
            {field.options
              ? <select
                className={cx(compactInputClass, 'mt-1 min-h-8 w-full px-2 py-1 text-xs')}
                onChange={(event) => setCreateValues((current) => ({ ...current, [field.key]: event.target.value }))}
                required={field.required}
                value={createValues[field.key] ?? ''}
              >
                <option value="">{field.placeholder ?? `Select ${field.label.toLowerCase()}`}</option>
                {field.options.map((option) => <option key={option.id} value={option.id}>{option.label}</option>)}
              </select>
              : <input
                className={cx(compactInputClass, 'mt-1 min-h-8 w-full px-2 py-1 text-xs')}
                onChange={(event) => setCreateValues((current) => ({ ...current, [field.key]: event.target.value }))}
                placeholder={field.placeholder}
                required={field.required}
                value={createValues[field.key] ?? ''}
              />}
          </label>
        ))}
        <button className={cx(plainButtonClass, 'min-h-8 justify-start px-2 py-1 text-xs')} disabled={createBusy} type="submit">{createBusy ? 'Saving…' : lookup.create.label}</button>
      </form>}
      {selected.length > 0 && <ul className="mt-2 flex flex-wrap gap-1">
        {selected.map((item) => {
          const option = known.get(item.id)
          return <li className="flex items-center gap-1 rounded-full border border-white/10 bg-steward-ink-950 px-2 py-0.5 text-xs" key={item.id}>
            <span className="max-w-36 truncate">{option?.label ?? item.id}</span>
            {item.primary && <span className="rounded-full bg-steward-teal/20 px-1.5 text-[10px] font-semibold text-steward-teal">Primary</span>}
            {lookup.allowPrimary && !item.primary && <button className={cx(plainButtonClass, 'min-h-0 px-1 py-0 text-[10px]')} onClick={() => makePrimary(item.id)} type="button">Make primary</button>}
            <button aria-label={`Remove ${option?.label ?? item.id}`} className={cx(plainButtonClass, 'min-h-0 px-1 py-0 text-[10px]')} onClick={() => remove(item.id)} type="button">×</button>
          </li>
        })}
      </ul>}
      {visible.length > 0 && <div className="mt-2 max-h-48 overflow-y-auto overscroll-contain steward-scrollbar" role="listbox" aria-label={`${label} matches`}>
        {visible.map((record) => (
          <button
            aria-selected={selectedIDs.has(record.id)}
            className="flex min-h-8 w-full flex-col items-start rounded-md px-2 py-1 text-left text-xs hover:bg-steward-teal/10 disabled:opacity-50"
            disabled={selectedIDs.has(record.id)}
            key={record.id}
            onClick={() => choose(record)}
            role="option"
            type="button"
          >
            <span className="font-semibold">{record.label}</span>
            <span className="truncate text-[10px] text-steward-mist-muted">{record.detail || record.id}</span>
          </button>
        ))}
      </div>}
      {visible.length === 0 && !busy && !creating && <p className="mt-2 text-xs text-steward-mist-muted">No matching {label.toLowerCase()} yet. Search, add one, or open the {label.toLowerCase()} page.</p>}
    </div>,
    document.body,
  )
}
