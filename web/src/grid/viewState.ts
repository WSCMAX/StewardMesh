import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { ValidationRule } from './columns'

// Requirements: REQ-WORKSPACE-001, REQ-ATLAS-001, REQ-STACK-001, A11Y-001. Feature: experience.grid.

// The arrangement an operator has made of one grid: which columns show and how
// wide, how rows sort, filter, and group, and which records they have hidden or
// highlighted. Row-level state is keyed by record id rather than row position,
// so a highlight follows its record through a re-sort, a filter, or a reload.
//
// This is held in local storage rather than a cookie. A cookie rides along on
// every request to the API and caps out near four kilobytes, which a column
// layout plus a highlight map would exhaust; none of this state is any use to
// the server. The key carries the principal and organization so two people
// sharing a workstation never inherit each other's arrangement.

export type SortDirection = 'ascending' | 'descending'

export type SortState = { key: string; direction: SortDirection }

export const highlightColors = ['amber', 'rose', 'violet', 'sky', 'emerald'] as const

export type HighlightColor = (typeof highlightColors)[number]

export const highlightLabels: Record<HighlightColor, string> = {
  amber: 'Amber', rose: 'Rose', violet: 'Violet', sky: 'Sky', emerald: 'Emerald',
}

// Written out rather than composed so the Tailwind build keeps every variant.
export const highlightRowClasses: Record<HighlightColor, string> = {
  amber: 'bg-amber-900', rose: 'bg-rose-900', violet: 'bg-violet-900',
  sky: 'bg-sky-900', emerald: 'bg-emerald-900',
}

export const highlightSwatchClasses: Record<HighlightColor, string> = {
  amber: 'bg-amber-400', rose: 'bg-rose-400', violet: 'bg-violet-400',
  sky: 'bg-sky-400', emerald: 'bg-emerald-400',
}

export const rowDensities = ['compact', 'normal', 'tall'] as const

export type RowDensity = (typeof rowDensities)[number]

export const rowDensityLabels: Record<RowDensity, string> = { compact: 'Compact', normal: 'Normal', tall: 'Tall' }

export const rowDensityClasses: Record<RowDensity, string> = { compact: 'h-6', normal: 'h-8', tall: 'h-11' }

export type SavedQuery = {
  id: string
  name: string
  query: string
}

export const recordColumnModes = ['expanded', 'collapsed', 'hidden'] as const

export type RecordColumnMode = (typeof recordColumnModes)[number]

export type GridView = {
  hiddenColumns: readonly string[]
  /** Operator-dragged widths in rem, overriding the column definition. */
  columnWidths: Readonly<Record<string, number>>
  sort: SortState | null
  filters: Readonly<Record<string, string>>
  /** Encoded ServiceNow-style query, evaluated with column filters and search. */
  query: string
  /** Named encoded queries the operator can reload on this grid. */
  savedQueries: readonly SavedQuery[]
  groupBy: string | null
  /** Group values the operator has folded shut, by their displayed text. */
  collapsedGroups: readonly string[]
  density: RowDensity
  hiddenRows: readonly string[]
  highlights: Readonly<Record<string, HighlightColor>>
  validation: Readonly<Record<string, ValidationRule>>
  /** Last-column Open/Edit controls: full width, a compact ⋯ strip, or off. */
  recordColumn: RecordColumnMode
}

export const emptyView: GridView = {
  hiddenColumns: [], columnWidths: {}, sort: null, filters: {}, query: '', savedQueries: [], groupBy: null,
  collapsedGroups: [], density: 'normal', hiddenRows: [], highlights: {}, validation: {}, recordColumn: 'expanded',
}

/**
 * The part of an arrangement worth naming and handing to someone else. Collapsed
 * groups and hidden rows are left out: they describe where one person is in the
 * middle of working, not how a team wants to look at the data.
 */
export type SharedViewState = Pick<GridView, 'hiddenColumns' | 'columnWidths' | 'sort' | 'filters' | 'query' | 'savedQueries' | 'groupBy' | 'density' | 'highlights' | 'validation' | 'recordColumn'>

export function shareableState(view: GridView): SharedViewState {
  const { collapsedGroups: _collapsed, hiddenRows: _hidden, ...shareable } = view
  return shareable
}

export function isDefaultView(view: GridView) {
  return view.hiddenColumns.length === 0
    && Object.keys(view.columnWidths).length === 0
    && view.sort === null
    && Object.values(view.filters).every((value) => !value.trim())
    && !view.query.trim()
    && view.savedQueries.length === 0
    && view.groupBy === null
    && view.collapsedGroups.length === 0
    && view.density === 'normal'
    && view.hiddenRows.length === 0
    && Object.keys(view.highlights).length === 0
    && Object.keys(view.validation).length === 0
    && view.recordColumn === 'expanded'
}

/** Counts the arrangement choices a "Reset view" control would undo. */
export function countViewAdjustments(view: GridView) {
  return view.hiddenColumns.length
    + Object.keys(view.columnWidths).length
    + (view.sort ? 1 : 0)
    + Object.values(view.filters).filter((value) => value.trim()).length
    + (view.query.trim() ? 1 : 0)
    + (view.groupBy ? 1 : 0)
    + (view.density === 'normal' ? 0 : 1)
    + view.hiddenRows.length
    + Object.keys(view.highlights).length
    + Object.keys(view.validation).length
    + (view.recordColumn === 'expanded' ? 0 : 1)
}

function textList(value: unknown) {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string') : []
}

function isSafeStoredKey(key: string) {
  return key !== '__proto__' && key !== 'prototype' && key !== 'constructor'
}

function textMap(value: unknown) {
  if (!value || typeof value !== 'object') return {}
  const result: Record<string, string> = {}
  for (const [key, entry] of Object.entries(value)) {
    if (!isSafeStoredKey(key) || typeof entry !== 'string') continue
    result[key] = entry
  }
  return result
}

function widthMap(value: unknown) {
  if (!value || typeof value !== 'object') return {}
  const result: Record<string, number> = {}
  for (const [key, entry] of Object.entries(value)) {
    if (!isSafeStoredKey(key) || typeof entry !== 'number' || !Number.isFinite(entry)) continue
    result[key] = clampWidth(entry)
  }
  return result
}

export const minimumColumnWidth = 4
export const maximumColumnWidth = 60

export function clampWidth(width: number) {
  return Math.min(Math.max(Math.round(width * 100) / 100, minimumColumnWidth), maximumColumnWidth)
}

function highlightMap(value: unknown) {
  if (!value || typeof value !== 'object') return {}
  const result: Record<string, HighlightColor> = {}
  for (const [key, entry] of Object.entries(value)) {
    if (!isSafeStoredKey(key) || typeof entry !== 'string' || !(highlightColors as readonly string[]).includes(entry)) continue
    result[key] = entry as HighlightColor
  }
  return result
}

function optionalNumber(value: unknown) {
  return typeof value === 'number' && Number.isFinite(value) ? value : undefined
}

function ruleMap(value: unknown) {
  if (!value || typeof value !== 'object') return {}
  const result: Record<string, ValidationRule> = {}
  for (const [key, entry] of Object.entries(value)) {
    if (!isSafeStoredKey(key) || !entry || typeof entry !== 'object') continue
    const source = entry as Record<string, unknown>
    result[key] = {
      required: source.required === true,
      allowed: textList(source.allowed),
      minimum: optionalNumber(source.minimum),
      maximum: optionalNumber(source.maximum),
      pattern: typeof source.pattern === 'string' ? source.pattern : undefined,
      message: typeof source.message === 'string' ? source.message : undefined,
    }
  }
  return result
}

const savedQueryIdPattern = /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/
const maximumSavedQueryLength = 4000
const maximumQueryNameLength = 80
const maximumSavedQueries = 25

function savedQueryList(value: unknown): SavedQuery[] {
  if (!Array.isArray(value)) return []
  const result: SavedQuery[] = []
  const seen = new Set<string>()
  for (const item of value) {
    if (!item || typeof item !== 'object') continue
    const source = item as Record<string, unknown>
    if (typeof source.id !== 'string' || !savedQueryIdPattern.test(source.id) || seen.has(source.id)) continue
    if (typeof source.name !== 'string' || typeof source.query !== 'string') continue
    const name = source.name.replaceAll(/\s+/g, ' ').trim().slice(0, maximumQueryNameLength)
    const query = source.query.slice(0, maximumSavedQueryLength)
    if (!name) continue
    seen.add(source.id)
    result.push({ id: source.id, name, query })
    if (result.length >= maximumSavedQueries) break
  }
  return result
}

function sortState(value: unknown): SortState | null {
  if (!value || typeof value !== 'object') return null
  const source = value as Record<string, unknown>
  if (typeof source.key !== 'string') return null
  return { key: source.key, direction: source.direction === 'descending' ? 'descending' : 'ascending' }
}

/**
 * Rebuilds a view from whatever the browser handed back. Stored state outlives
 * the code that wrote it, so every field is re-derived and anything unrecognized
 * falls back to the default rather than reaching the grid.
 */
export function parseView(raw: unknown): GridView {
  if (!raw || typeof raw !== 'object') return emptyView
  const source = raw as Record<string, unknown>
  const density = source.density
  return {
    hiddenColumns: textList(source.hiddenColumns),
    columnWidths: widthMap(source.columnWidths),
    sort: sortState(source.sort),
    filters: textMap(source.filters),
    query: typeof source.query === 'string' ? source.query.slice(0, maximumSavedQueryLength) : '',
    savedQueries: savedQueryList(source.savedQueries),
    groupBy: typeof source.groupBy === 'string' && source.groupBy ? source.groupBy : null,
    collapsedGroups: textList(source.collapsedGroups),
    density: typeof density === 'string' && (rowDensities as readonly string[]).includes(density) ? density as RowDensity : 'normal',
    hiddenRows: textList(source.hiddenRows),
    highlights: highlightMap(source.highlights),
    validation: ruleMap(source.validation),
    recordColumn: typeof source.recordColumn === 'string' && (recordColumnModes as readonly string[]).includes(source.recordColumn)
      ? source.recordColumn as RecordColumnMode
      : 'expanded',
  }
}

const storagePrefix = 'stewardmesh.grid.v1'

/** Who the arrangement belongs to. Absent while the session is still loading. */
export type GridIdentity = { subject: string; organizationId: string }

export function viewStorageKey(viewId: string, identity?: GridIdentity | null) {
  const organization = identity?.organizationId?.trim() || 'unknown-organization'
  const subject = identity?.subject?.trim() || 'anonymous'
  return `${storagePrefix}.${organization}.${subject}.${viewId}`
}

export function readStoredView(key: string | null, defaults?: Partial<GridView>): GridView {
  const baseline = defaults ? { ...emptyView, ...defaults } : emptyView
  if (!key) return baseline
  try {
    const stored = localStorage.getItem(key)
    if (!stored) return baseline
    const parsed = parseView(JSON.parse(stored))
    if (!defaults?.filters) return parsed
    const filters = { ...parsed.filters }
    for (const [filterKey, defaultValue] of Object.entries(defaults.filters)) {
      if (!(filterKey in parsed.filters)) filters[filterKey] = defaultValue
    }
    return { ...parsed, filters }
  } catch {
    return baseline
  }
}

export function writeStoredView(key: string | null, view: GridView) {
  if (!key) return
  try {
    // An untouched grid leaves nothing behind, which also makes "Reset view"
    // clean up after itself instead of persisting an empty record forever.
    if (isDefaultView(view)) localStorage.removeItem(key)
    else localStorage.setItem(key, JSON.stringify(view))
  } catch {
    // Private browsing and full quotas must not take the grid down with them.
  }
}

export type ViewUpdate = (current: GridView) => GridView

/**
 * Holds one grid's arrangement and mirrors it into local storage. Passing no
 * view id keeps everything in memory, which is what a grid embedded in a test
 * or a one-off screen wants.
 */
export function useGridView(viewId?: string, identity?: GridIdentity | null, defaults?: Partial<GridView>) {
  const key = useMemo(
    () => (viewId && identity?.subject && identity.organizationId ? viewStorageKey(viewId, identity) : null),
    [viewId, identity],
  )
  const [view, setView] = useState<GridView>(() => readStoredView(key, defaults))
  const loadedKey = useRef(key)

  // Signing in, or switching organizations, swaps in that principal's own
  // arrangement instead of leaving the previous one on screen.
  useEffect(() => {
    if (loadedKey.current === key) return
    loadedKey.current = key
    setView(readStoredView(key, defaults))
  }, [key, defaults])

  useEffect(() => {
    if (loadedKey.current !== key) return
    writeStoredView(key, view)
  }, [key, view])

  const update = useCallback((change: ViewUpdate) => setView(change), [])
  const reset = useCallback(() => setView((current) => ({
    ...(defaults ? { ...emptyView, ...defaults } : emptyView),
    savedQueries: current.savedQueries,
  })), [defaults])
  const replace = useCallback((next: GridView) => setView(next), [])

  return { view, update, reset, replace, storageKey: key }
}

export type GridViewController = ReturnType<typeof useGridView>
