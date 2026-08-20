import { useCallback, useEffect, useMemo, useRef, useState, type ClipboardEvent, type KeyboardEvent, type PointerEvent, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { buttonClass, compactInputClass, cx, gridActionButtonClass, gridActionCellClass, gridCellClass, gridEditorClass, gridFilterClass, gridHeaderCellClass, gridTableClass, gridToolbarClass, gridWrapClass, labelClass, menuSurfaceClass, plainButtonClass, secondaryButtonClass, subpanelClass } from '../ui'
import { lockScroll } from '../scrollLock'
import type { ColumnRules, GridColumn, ValidationRule } from './columns'
import QueryBuilder from './QueryBuilder'
import { describeQuery, emptyQuery, encodeQuery, isQueryEmpty, matchQuery, maximumEncodedQueryLength, maximumSavedQueries, newQueryId, parseQuery, sanitizeQueryName, type QueryModel } from './queryLanguage'
import { isEmptyRule, lookupExportText, lookupLabel, parseLookupText, withValidation } from './columns'
import { useGridSelection, type CellPosition } from './useGridSelection'
import { groupEditsByRow, useCellEditing, type CellEdit } from './useCellEditing'
import { copyRange, describeOutcome, fillDown, pasteText, type ClipboardSurface } from './useGridClipboard'
import ContextMenu, { separator, type MenuAnchor, type MenuEntry } from './ContextMenu'
import CellCamera from './CellCamera'
import CellLookup from './CellLookup'
import Drawer from './Drawer'
import { csvFromSheet, downloadBlob, filenameFor, jsonFromSheet, sheetFromGrid, type ExportFormat, type ExportScope } from './export'
import { xlsxFromSheet } from './xlsx'
import {
  clampWidth, countViewAdjustments, highlightColors, highlightLabels, highlightRowClasses, highlightSwatchClasses,
  rowDensities, rowDensityClasses, rowDensityLabels, useGridView, type GridIdentity, type GridView, type HighlightColor,
  type RecordColumnMode, type SavedQuery, type SortDirection, type SortState,
} from './viewState'

// Requirements: REQ-WORKSPACE-001, REQ-ATLAS-001, REQ-STACK-001, A11Y-001. Feature: experience.grid.

// A dense, sortable, filterable, editable grid over semantic table markup. Rows
// keep a fixed height so a screenful scans at a glance, and every cell is
// addressable so selection, editing, and the clipboard share stable coordinates.

export type { SortDirection, SortState }

export type RowSaveState = 'saving' | 'saved' | 'failed'

/** A row pasted or added below the data, keyed by column, awaiting creation. */
export type StagedDraft = { id: string; values: Record<string, string> }

export type DataGridProps<T> = {
  /** Accessible name for the grid. Tests and screen readers rely on this. */
  label: string
  rows: readonly T[]
  columns: readonly GridColumn<T>[]
  rowId: (row: T) => string
  /** Human-readable row name used in cell and control accessible names. */
  rowLabel?: (row: T) => string
  emptyMessage?: string
  /** Enables in-cell editing for columns that declare themselves editable. */
  editable?: boolean
  /**
   * Locks every cell in a row. Stack uses this for records the API refuses to
   * change further, such as retired products and ended assignments.
   */
  isRowEditable?: (row: T) => boolean
  /** Persists pending edits. Rejecting leaves the edits in place for retry. */
  onSaveEdits?: (edits: readonly CellEdit[]) => Promise<void> | void
  /** Creates rows staged past the end of the data. Enables paste-to-create. */
  onCreateRows?: (drafts: readonly StagedDraft[]) => Promise<void> | void
  rowState?: (row: T) => RowSaveState | undefined
  rowMessage?: (row: T) => string | undefined
  /** Renders a leading checkbox column and enables bulk actions. */
  selectable?: boolean
  /** Rendered in the toolbar whenever at least one row is checked. */
  bulkActions?: (selected: readonly T[]) => ReactNode
  /** Renders a trailing action column that opens the full record. */
  onOpenRow?: (row: T) => void
  /** Opens the full edit form for a row, surfaced in the row menu when set. */
  onEditRow?: (row: T) => void
  /** Fetches every inventory record before an all-records export when more pages remain. */
  exportAllRows?: () => Promise<readonly T[]>
  /** Extra controls rendered in the toolbar, before the column chooser. */
  toolbar?: ReactNode
  /** Caps the visible body height so the sticky header stays useful. */
  maximumBodyHeight?: string
  /** Persists column layout, filters, grouping, and highlights per principal. */
  viewId?: string
  identity?: GridIdentity | null
  /** Initial arrangement merged with hidden-column defaults and restored on reset. */
  viewDefaults?: Partial<GridView>
  /** Soft-deletes saved records. Staged rows are removed locally without this. */
  onDeleteRows?: (rows: readonly T[]) => Promise<void> | void
  /** Reports the filtered, grouped listing so another surface can follow the grid. */
  onListingChange?: (listing: GridListing) => void
  /** Controlled grouping. When set, the toolbar Group by select writes through. */
  groupBy?: string | null
  onGroupByChange?: (groupBy: string | null) => void
}

export type GridListing = {
  rowIds: readonly string[]
  total: number
  groupBy: string | null
  groups: readonly { value: string; rowIds: readonly string[] }[]
}

function compareText(left: string, right: string, column: ColumnRules) {
  if (column.kind === 'number' || column.kind === 'money') return Number(left) - Number(right)
  return left.localeCompare(right, undefined, { numeric: true, sensitivity: 'base' })
}

export function sortRows<T>(rows: readonly T[], columns: readonly GridColumn<T>[], sort: SortState | null) {
  if (!sort) return [...rows]
  const column = columns.find((candidate) => candidate.key === sort.key)
  if (!column) return [...rows]
  const direction = sort.direction === 'ascending' ? 1 : -1
  return [...rows].sort((left, right) => {
    const leftText = column.text(left)
    const rightText = column.text(right)
    // Blanks sort last in both directions, matching spreadsheet behavior.
    if (!leftText && !rightText) return 0
    if (!leftText) return 1
    if (!rightText) return -1
    return compareText(leftText, rightText, column) * direction
  })
}

export function filterRows<T>(rows: readonly T[], columns: readonly GridColumn<T>[], filters: Readonly<Record<string, string>>, search: string, query?: QueryModel) {
  const term = search.trim().toLowerCase()
  const active = Object.entries(filters).filter(([, value]) => value.trim().length > 0)
  const queryActive = query && !isQueryEmpty(query)
  if (!term && active.length === 0 && !queryActive) return [...rows]
  const fields = columns.map((column) => ({ key: column.key, header: column.header, kind: column.kind, options: column.options }))
  return rows.filter((row) => {
    for (const [key, value] of active) {
      const column = columns.find((candidate) => candidate.key === key)
      if (!column) continue
      if (!column.text(row).toLowerCase().includes(value.trim().toLowerCase())) return false
    }
    if (queryActive && !matchQuery(row, fields, query, (item, field) => columns.find((column) => column.key === field)?.text(item) ?? '')) return false
    if (!term) return true
    return columns.some((column) => column.text(row).toLowerCase().includes(term))
  })
}

function SortIcon({ direction }: { direction: SortDirection | null }) {
  if (!direction) return <span aria-hidden="true" className="text-steward-slate opacity-0 transition group-hover:opacity-60">↕</span>
  return <span aria-hidden="true" className="text-steward-teal">{direction === 'ascending' ? '↑' : '↓'}</span>
}

function LookupChips({ text, column }: { text: string; column: ColumnRules }) {
  const selected = parseLookupText(text)
  if (selected.length === 0) return null
  return <span className="flex flex-wrap items-center gap-1">
    {selected.map((item) => (
      <span className="inline-flex items-center gap-1 rounded-full border border-white/10 bg-white/[0.04] px-1.5 text-[11px]" key={item.id}>
        <span className="max-w-28 truncate">{lookupLabel(item.id, column.lookup?.options)}</span>
        {item.primary && <span className="text-[10px] font-semibold text-steward-teal">Primary</span>}
      </span>
    ))}
  </span>
}

const pageStride = 10
const historyLimit = 100
const stateLabels: Record<RowSaveState, string> = { saving: 'Saving', saved: 'Saved', failed: 'Failed' }

function isPrintableKey(event: KeyboardEvent<HTMLTableElement>) {
  return event.key.length === 1 && !event.ctrlKey && !event.metaKey && !event.altKey
}

/** A staged row and where it sits: below `after`, or above `before`. */
type StagedRow = { id: string; after: string | null; before?: string }

/** One addressable grid row: either a loaded record or a staged new row. */
type RowSlot<T> = { id: string; row?: T }

/** Everything undo and redo have to put back. */
type GridSnapshot = { edits: ReadonlyMap<string, CellEdit>; staged: readonly StagedRow[] }

export default function DataGrid<T>({
  label, rows, columns: sourceColumns, rowId, rowLabel, emptyMessage = 'No records yet.',
  editable = false, isRowEditable, onSaveEdits, onCreateRows, rowState, rowMessage,
  selectable = false, bulkActions, onOpenRow, onEditRow, exportAllRows, toolbar, maximumBodyHeight = '60vh',
  viewId, identity, viewDefaults: viewDefaultsProp, onDeleteRows, onListingChange, groupBy: groupByProp, onGroupByChange,
}: DataGridProps<T>) {
  const viewDefaults = useMemo(() => ({
    hiddenColumns: sourceColumns.filter((column) => column.hiddenByDefault).map((column) => column.key),
    ...viewDefaultsProp,
  }), [sourceColumns, viewDefaultsProp])
  const { view, update, reset } = useGridView(viewId, identity, viewDefaults)
  const sort = view.sort
  const filters = view.filters
  const hidden = useMemo(() => new Set(view.hiddenColumns), [view.hiddenColumns])
  const hiddenRows = useMemo(() => new Set(view.hiddenRows), [view.hiddenRows])
  const collapsedGroups = useMemo(() => new Set(view.collapsedGroups), [view.collapsedGroups])
  const [search, setSearch] = useState('')
  const [staged, setStaged] = useState<readonly StagedRow[]>([])
  const [past, setPast] = useState<readonly GridSnapshot[]>([])
  const [future, setFuture] = useState<readonly GridSnapshot[]>([])
  const [saving, setSaving] = useState(false)
  const [notice, setNotice] = useState('')
  const [menu, setMenu] = useState<{ anchor: MenuAnchor; row: number; column: number } | null>(null)
  const [validationOpen, setValidationOpen] = useState(false)
  const [exporting, setExporting] = useState(false)
  const [exportOpen, setExportOpen] = useState(false)
  const [columnsOpen, setColumnsOpen] = useState(false)
  const [cameraOpen, setCameraOpen] = useState(false)
  const [queryOpen, setQueryOpen] = useState(() => Boolean(view.query.trim()))
  const [queryName, setQueryName] = useState('')
  const [fullscreen, setFullscreen] = useState(false)
  const [cellEditorExpanded, setCellEditorExpanded] = useState(false)
  const [queryModel, setQueryModel] = useState<QueryModel>(() => {
    const parsed = parseQuery(view.query)
    return parsed.ok ? parsed.model : emptyQuery()
  })
  const cellRefs = useRef(new Map<string, HTMLTableCellElement>())
  const exportRef = useRef<HTMLDivElement | null>(null)
  const columnsRef = useRef<HTMLDivElement | null>(null)
  const rootRef = useRef<HTMLDivElement | null>(null)
  const fullscreenButtonRef = useRef<HTMLButtonElement | null>(null)
  const exitFullscreenRef = useRef<HTMLButtonElement | null>(null)
  const stagedCounter = useRef(0)
  const nativeClipboard = useRef(false)
  const editor = useCellEditing()
  const openerRef = useRef<HTMLElement | null>(null)

  const columns = useMemo(
    () => sourceColumns.map((column) => withValidation(column, view.validation[column.key])),
    [sourceColumns, view.validation],
  )
  const activeGroupBy = groupByProp !== undefined ? groupByProp : view.groupBy
  const queryFields = useMemo(
    () => columns.map((column) => ({ key: column.key, header: column.header, kind: column.kind, options: column.options })),
    [columns],
  )
  const parsedQuery = parseQuery(view.query, queryFields)
  const queryError = view.query.trim() && !parsedQuery.ok ? parsedQuery.error : ''

  const visibleColumns = useMemo(() => columns.filter((column) => !hidden.has(column.key)), [columns, hidden])
  const listedRows = useMemo(
    () => {
      const parsed = parseQuery(view.query, queryFields)
      const query = parsed.ok && !isQueryEmpty(parsed.model) ? parsed.model : undefined
      return sortRows(filterRows(rows.filter((row) => !hiddenRows.has(rowId(row))), columns, filters, search, query), columns, sort)
    },
    [rows, columns, filters, search, sort, hiddenRows, rowId, view.query, queryFields],
  )
  const groupColumn = useMemo(
    () => (activeGroupBy ? visibleColumns.find((column) => column.key === activeGroupBy) ?? columns.find((column) => column.key === activeGroupBy) : undefined),
    [activeGroupBy, visibleColumns, columns],
  )
  const grouped = useMemo(() => {
    if (!groupColumn) return null
    const buckets = new Map<string, T[]>()
    const order: string[] = []
    for (const row of listedRows) {
      const value = groupColumn.text(row) || '(blank)'
      const existing = buckets.get(value)
      if (existing) existing.push(row)
      else {
        buckets.set(value, [row])
        order.push(value)
      }
    }
    return order.map((value) => ({ value, rows: buckets.get(value) ?? [] }))
  }, [groupColumn, listedRows])
  const onListingChangeRef = useRef(onListingChange)
  onListingChangeRef.current = onListingChange
  useEffect(() => {
    onListingChangeRef.current?.({
      rowIds: listedRows.map(rowId),
      total: rows.length,
      groupBy: activeGroupBy,
      groups: grouped?.map((group) => ({ value: group.value, rowIds: group.rows.map(rowId) })) ?? [],
    })
  }, [activeGroupBy, grouped, listedRows, rows.length])
  const visibleRows = useMemo(() => {
    if (!grouped) return listedRows
    return grouped.flatMap((group) => collapsedGroups.has(group.value) ? [] : group.rows)
  }, [grouped, listedRows, collapsedGroups])

  // Navigable columns are the checkbox column, the data columns, then the open
  // action column. Data columns keep their own contiguous index space so the
  // clipboard can map a pasted rectangle onto fields without offset math.
  const canStage = editable && Boolean(onCreateRows)
  const actionsAvailable = Boolean(onOpenRow) || Boolean(onEditRow) || canStage
  const recordExpanded = view.recordColumn === 'expanded'
  const showRecordColumn = actionsAvailable && view.recordColumn !== 'hidden'
  const actionColumnWidth = !showRecordColumn
    ? 0
    : recordExpanded
      ? (3 + (canStage ? 1.75 : 0) + (onOpenRow ? 3.75 : 0) + (onEditRow ? 3.25 : 0))
      : 2.75
  const selectOffset = selectable ? 1 : 0
  const navColumnCount = selectOffset + visibleColumns.length + (showRecordColumn ? 1 : 0)

  // Staged rows sit wherever they were inserted rather than always at the end,
  // so a row added between two records reads in place. Rows whose anchor is
  // filtered out fall to the end instead of disappearing.
  const slots = useMemo<RowSlot<T>[]>(() => {
    const afterMap = new Map<string, string[]>()
    const beforeMap = new Map<string, string[]>()
    const trailing: string[] = []
    const anchors = new Set(visibleRows.map(rowId))
    for (const item of staged) {
      if (item.before && anchors.has(item.before)) beforeMap.set(item.before, [...beforeMap.get(item.before) ?? [], item.id])
      else if (item.after && anchors.has(item.after)) afterMap.set(item.after, [...afterMap.get(item.after) ?? [], item.id])
      else trailing.push(item.id)
    }
    const ordered: RowSlot<T>[] = []
    for (const row of visibleRows) {
      const id = rowId(row)
      for (const stagedId of beforeMap.get(id) ?? []) ordered.push({ id: stagedId })
      ordered.push({ id, row })
      for (const stagedId of afterMap.get(id) ?? []) ordered.push({ id: stagedId })
    }
    for (const stagedId of trailing) ordered.push({ id: stagedId })
    return ordered
  }, [visibleRows, staged, rowId])

  const stagedNumbers = useMemo(() => {
    const numbers = new Map<string, number>()
    for (const slot of slots) if (!slot.row) numbers.set(slot.id, numbers.size + 1)
    return numbers
  }, [slots])

  const totalRowCount = slots.length
  const selection = useGridSelection({ rowCount: totalRowCount, columnCount: navColumnCount })
  const { active, dragging, focusVersion, selectedRowIds } = selection

  const columnWidth = (column: GridColumn<T>) => view.columnWidths[column.key] ?? column.width ?? 12
  const totalWidth = useMemo(
    () => visibleColumns.reduce((total, column) => total + columnWidth(column), 0) + (selectable ? 2.5 : 0) + (showRecordColumn ? actionColumnWidth : 0),
    [visibleColumns, selectable, showRecordColumn, actionColumnWidth, view.columnWidths],
  )

  const selectedRows = useMemo(() => rows.filter((row) => selectedRowIds.has(rowId(row))), [rows, selectedRowIds, rowId])
  const allVisibleChecked = visibleRows.length > 0 && visibleRows.every((row) => selectedRowIds.has(rowId(row)))
  const nameOf = (row: T) => rowLabel ? rowLabel(row) : rowId(row)
  const stagedSet = useMemo(() => new Set(staged.map((item) => item.id)), [staged])

  const activeSlot: RowSlot<T> | undefined = slots[active.row]
  const activeRow = activeSlot?.row
  const activeColumn: GridColumn<T> | undefined = visibleColumns[active.column - selectOffset]
  const activeRowId = activeSlot?.id
  const activeStored = activeRow && activeColumn ? activeColumn.text(activeRow) : ''
  const rowEditable = (row: T | undefined) => row === undefined || !isRowEditable || isRowEditable(row)
  const canEditActive = editable && Boolean(activeColumn?.editable) && activeRowId !== undefined && rowEditable(activeRow)

  // Move real DOM focus with the active cell so screen readers and the browser
  // agree on where the user is. The initial render must not steal focus.
  useEffect(() => {
    if (focusVersion === 0 || editor.editing || menu) return
    cellRefs.current.get(`${active.row}:${active.column}`)?.focus({ preventScroll: false })
  }, [focusVersion, active.row, active.column, editor.editing, menu])

  useEffect(() => {
    if (!exportOpen && !columnsOpen) return undefined
    function handlePointerDown(event: globalThis.PointerEvent) {
      if (exportOpen && !exportRef.current?.contains(event.target as Node)) setExportOpen(false)
      if (columnsOpen && !columnsRef.current?.contains(event.target as Node)) setColumnsOpen(false)
    }
    document.addEventListener('pointerdown', handlePointerDown, true)
    return () => document.removeEventListener('pointerdown', handlePointerDown, true)
  }, [columnsOpen, exportOpen])

  const closeFullscreen = useCallback(() => {
    setFullscreen(false)
    queueMicrotask(() => fullscreenButtonRef.current?.focus())
  }, [])

  useEffect(() => {
    if (!fullscreen) return undefined
    const releaseScroll = lockScroll()
    if (!editor.editing) queueMicrotask(() => exitFullscreenRef.current?.focus())
    function handleKeyDown(event: globalThis.KeyboardEvent) {
      if (event.key !== 'Escape') return
      if (editor.editing || cellEditorExpanded || menu || cameraOpen || validationOpen) return
      event.preventDefault()
      closeFullscreen()
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => {
      releaseScroll()
      window.removeEventListener('keydown', handleKeyDown)
    }
  }, [cameraOpen, cellEditorExpanded, closeFullscreen, editor.editing, fullscreen, menu, validationOpen])

  const surface: ClipboardSurface = {
    rowCount: totalRowCount,
    columns: visibleColumns,
    rowIdAt: (index) => slots[index]?.id,
    storedText: (rowIndex, columnIndex) => {
      const slot = slots[rowIndex]
      const column = visibleColumns[columnIndex]
      return slot?.row && column ? column.text(slot.row) : ''
    },
    currentText: (rowIndex, columnIndex) => {
      const slot = slots[rowIndex]
      const column = visibleColumns[columnIndex]
      if (!slot || !column) return ''
      return editor.effectiveText(slot.id, column, slot.row ? column.text(slot.row) : '')
    },
    writeCell: (id, column, stored, value) => {
      const target = slots.find((slot) => slot.id === id)?.row
      if (!rowEditable(target)) return
      editor.setCell(id, column, stored, value)
    },
    stageRow: canStage ? () => {
      stagedCounter.current += 1
      const id = `staged-${stagedCounter.current}`
      setStaged((current) => [...current, { id, after: null }])
      return id
    } : undefined,
  }

  // Undo and redo replay whole snapshots of the pending state. Every mutating
  // action records a checkpoint first, so a pasted block undoes as one step
  // rather than one step per cell.
  function checkpoint() {
    setPast((current) => [...current, { edits: editor.editMap, staged }].slice(-historyLimit))
    setFuture([])
  }

  function restoreSnapshot(snapshot: GridSnapshot) {
    editor.restore(snapshot.edits)
    setStaged(snapshot.staged)
  }

  function undo() {
    const previous = past.at(-1)
    if (!previous) {
      setNotice('Nothing to undo.')
      return
    }
    setFuture((current) => [...current, { edits: editor.editMap, staged }])
    setPast((current) => current.slice(0, -1))
    restoreSnapshot(previous)
    setNotice('Undid the last change.')
  }

  function redo() {
    const next = future.at(-1)
    if (!next) {
      setNotice('Nothing to redo.')
      return
    }
    setPast((current) => [...current, { edits: editor.editMap, staged }])
    setFuture((current) => current.slice(0, -1))
    restoreSnapshot(next)
    setNotice('Redid the last change.')
  }

  function toggleSort(key: string) {
    update((current) => {
      const existing = current.sort
      const sort: SortState | null = existing?.key !== key
        ? { key, direction: 'ascending' }
        : existing.direction === 'ascending' ? { key, direction: 'descending' } : null
      return { ...current, sort }
    })
  }

  function toggleColumn(key: string) {
    update((current) => {
      const next = current.hiddenColumns.includes(key)
        ? current.hiddenColumns.filter((item) => item !== key)
        : [...current.hiddenColumns, key]
      return { ...current, hiddenColumns: next }
    })
  }

  function setRecordColumn(recordColumn: RecordColumnMode) {
    update((current) => ({ ...current, recordColumn }))
  }

  function setFilter(key: string, value: string) {
    update((current) => ({ ...current, filters: { ...current.filters, [key]: value } }))
  }

  function applyQueryModel(model: QueryModel) {
    setQueryModel(model)
    update((current) => ({ ...current, query: encodeQuery(model) }))
  }

  function applyEncodedQuery(value: string) {
    const next = value.slice(0, maximumEncodedQueryLength)
    update((current) => ({ ...current, query: next }))
    const parsed = parseQuery(next, queryFields)
    if (parsed.ok) setQueryModel(isQueryEmpty(parsed.model) ? emptyQuery() : parsed.model)
  }

  function saveCurrentQuery() {
    const name = sanitizeQueryName(queryName)
    if (!name) {
      setNotice('Name the query before saving.')
      return
    }
    const encoded = encodeQuery(queryModel) || view.query.trim()
    const parsed = parseQuery(encoded, queryFields)
    if (!parsed.ok) {
      setNotice(parsed.error)
      return
    }
    if (isQueryEmpty(parsed.model)) {
      setNotice('Add a condition before saving the query.')
      return
    }
    const canonical = encodeQuery(parsed.model)
    const existing = view.savedQueries.find((item) => item.name.toLowerCase() === name.toLowerCase())
    if (!existing && view.savedQueries.length >= maximumSavedQueries) {
      setNotice(`Save at most ${maximumSavedQueries} queries.`)
      return
    }
    const next: SavedQuery = { id: existing?.id ?? newQueryId('sq'), name, query: canonical }
    update((current) => ({
      ...current,
      query: canonical,
      savedQueries: existing
        ? current.savedQueries.map((item) => item.id === existing.id ? next : item)
        : [...current.savedQueries, next],
    }))
    setQueryModel(parsed.model)
    setNotice(`Saved query ${name}.`)
  }

  function loadSavedQuery(id: string) {
    const saved = view.savedQueries.find((item) => item.id === id)
    if (!saved) return
    applyEncodedQuery(saved.query)
    setQueryName(saved.name)
    setQueryOpen(true)
    setNotice(`Loaded query ${saved.name}.`)
  }

  function deleteSavedQuery(id: string) {
    const saved = view.savedQueries.find((item) => item.id === id)
    update((current) => ({ ...current, savedQueries: current.savedQueries.filter((item) => item.id !== id) }))
    if (saved && queryName === saved.name) setQueryName('')
    setNotice('Removed the saved query.')
  }

  function resetView() {
    reset()
    setQueryModel(emptyQuery())
    setQueryOpen(false)
  }

  const existingEdits = useMemo(() => editor.edits.filter((edit) => !stagedSet.has(edit.rowId)), [editor.edits, stagedSet])
  const stagedDrafts = useMemo(() => slots.filter((slot) => !slot.row).map((slot) => ({
    id: slot.id,
    values: Object.fromEntries(editor.edits.filter((edit) => edit.rowId === slot.id).map((edit) => [edit.columnKey, edit.text])),
  })), [slots, editor.edits])

  async function saveEdits() {
    if (editor.invalidCount > 0) return
    setSaving(true)
    try {
      if (existingEdits.length > 0 && onSaveEdits) await onSaveEdits(existingEdits)
      if (stagedDrafts.length > 0 && onCreateRows) await onCreateRows(stagedDrafts)
      editor.discard()
      setStaged([])
      setPast([])
      setFuture([])
      setNotice('Changes saved.')
    } catch {
      // The screen reports why the write was rejected. Keeping the pending edits
      // means the operator can correct and retry instead of retyping the block.
      setNotice('Changes could not be saved and are still pending.')
    } finally {
      setSaving(false)
    }
  }

  function discardAll() {
    checkpoint()
    editor.discard()
    setStaged([])
    setNotice('Pending changes discarded.')
  }

  function removeStaged(id: string) {
    checkpoint()
    editor.discard([id])
    setStaged((current) => current.filter((candidate) => candidate.id !== id))
  }

  /** Adds a staged row directly below a record, or at the end when unanchored. */
  function addStagedRow(after: string | null = null, before?: string) {
    checkpoint()
    stagedCounter.current += 1
    const id = `staged-${stagedCounter.current}`
    setStaged((current) => [...current, { id, after: before ? null : after, before }])
    setNotice(before ? 'Added a new row above.' : after ? 'Added a new row below.' : 'Added a new row at the end.')
  }

  function hideRow(id: string) {
    if (stagedSet.has(id)) {
      removeStaged(id)
      return
    }
    update((current) => current.hiddenRows.includes(id) ? current : { ...current, hiddenRows: [...current.hiddenRows, id] })
    setNotice('Hid the row. Restore it from the toolbar.')
  }

  function restoreHiddenRows() {
    update((current) => ({ ...current, hiddenRows: [] }))
    setNotice('Restored hidden rows.')
  }

  function highlightRow(id: string, color: HighlightColor | null) {
    update((current) => {
      const highlights = { ...current.highlights }
      if (color) highlights[id] = color
      else delete highlights[id]
      return { ...current, highlights }
    })
  }

  function clearAllHighlights() {
    update((current) => ({ ...current, highlights: {} }))
    setNotice('Cleared row highlights.')
  }

  async function deleteActiveRow() {
    const slot = activeSlot
    if (!slot) return
    if (!slot.row) {
      removeStaged(slot.id)
      setNotice('Removed the new row.')
      return
    }
    if (!onDeleteRows) {
      setNotice('Saved records move to the recycle bin once that is available. Hide the row to take it out of this view.')
      return
    }
    await onDeleteRows([slot.row])
    setNotice('Moved the record to the recycle bin.')
  }

  function copyRow() {
    const row = active.row
    void navigator.clipboard?.writeText(copyRange(surface, { top: row, bottom: row, left: 0, right: Math.max(visibleColumns.length - 1, 0) }))
    setNotice('Copied the row.')
  }

  function pasteRow() {
    pasteFromClipboard()
  }

  function filterFromCell() {
    if (!activeColumn || !activeRowId) return
    const text = editor.effectiveText(activeRowId, activeColumn, activeStored)
    setFilter(activeColumn.key, text)
    setNotice(`Filtered ${activeColumn.header} to the selected value.`)
  }

  function groupByActiveColumn() {
    if (!activeColumn) return
    setGroupBy(activeGroupBy === activeColumn.key ? null : activeColumn.key)
  }

  function setGroupBy(next: string | null) {
    onGroupByChange?.(next)
    update((current) => ({ ...current, groupBy: next, collapsedGroups: [] }))
  }

  function beginResize(key: string, event: PointerEvent<HTMLDivElement>) {
    event.preventDefault()
    event.stopPropagation()
    const origin = event.clientX
    const start = columnWidth(visibleColumns.find((column) => column.key === key)!)
    const pointer = event.currentTarget
    pointer.setPointerCapture(event.pointerId)
    function move(next: globalThis.PointerEvent) {
      const rem = start + (next.clientX - origin) / 16
      update((current) => ({ ...current, columnWidths: { ...current.columnWidths, [key]: clampWidth(rem) } }))
    }
    function up() {
      pointer.removeEventListener('pointermove', move)
      pointer.removeEventListener('pointerup', up)
    }
    pointer.addEventListener('pointermove', move)
    pointer.addEventListener('pointerup', up)
  }

  function cellText(row: T, column: GridColumn<T>) {
    return editor.effectiveText(rowId(row), column, column.text(row))
  }

  function exportCellText(row: T, column: GridColumn<T>) {
    if (column.exportText) return column.exportText(row)
    const raw = cellText(row, column)
    if (column.lookup) return lookupExportText(raw, column.lookup.options, Boolean(column.lookup.multiple))
    return raw
  }

  async function exportSheet(format: ExportFormat, scope: ExportScope) {
    const range = dataRange()
    const highlighted = Object.keys(view.highlights)
    const scopedRows = scope === 'highlighted'
      ? visibleRows.filter((row) => highlighted.includes(rowId(row)))
      : scope === 'selected'
        ? slots.slice(range.top, range.bottom + 1).flatMap((slot) => slot.row ? [slot.row] : [])
        : visibleRows
    const scopedColumns = scope === 'selected' || scope === 'highlighted'
      ? visibleColumns.slice(range.left, range.right + 1)
      : visibleColumns
    if (scopedRows.length === 0 || scopedColumns.length === 0) {
      setNotice('Nothing to export in that selection.')
      return
    }
    await downloadSheet(format, scopedColumns, scopedRows, `${scopedRows.length} ${scopedRows.length === 1 ? 'row' : 'rows'}`)
  }

  async function exportAllRecords(format: ExportFormat) {
    if (!exportAllRows) return
    setExporting(true)
    try {
      const allRows = await exportAllRows()
      if (allRows.length === 0) {
        setNotice('Nothing to export in that selection.')
        return
      }
      await downloadSheet(format, visibleColumns, allRows, `${allRows.length} inventory ${allRows.length === 1 ? 'record' : 'records'}`)
    } catch {
      setNotice('The spreadsheet could not be exported.')
    } finally {
      setExporting(false)
    }
  }

  async function downloadSheet(format: ExportFormat, scopedColumns: readonly GridColumn<T>[], scopedRows: readonly T[], summary: string) {
    const sheet = sheetFromGrid({
      name: label,
      columns: scopedColumns,
      rows: scopedRows,
      text: (row, column) => exportCellText(row, column),
    })
    setExporting(true)
    try {
      if (format === 'csv') downloadBlob(filenameFor(label, 'csv'), new Blob([csvFromSheet(sheet)], { type: 'text/csv;charset=utf-8' }))
      else if (format === 'json') downloadBlob(filenameFor(label, 'json'), new Blob([jsonFromSheet(sheet)], { type: 'application/json;charset=utf-8' }))
      else {
        const bytes = await xlsxFromSheet(sheet)
        downloadBlob(filenameFor(label, 'xlsx'), new Blob([bytes.buffer as ArrayBuffer], { type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet' }))
      }
      setNotice(`Exported ${summary} as ${format.toUpperCase()}.`)
    } catch {
      setNotice('The spreadsheet could not be exported.')
    } finally {
      setExporting(false)
    }
  }

  function openMenu(row: number, column: number, event: { clientX: number; clientY: number; preventDefault: () => void; currentTarget?: EventTarget | null }) {
    event.preventDefault()
    if (event.currentTarget instanceof HTMLElement) openerRef.current = event.currentTarget
    selection.focusCell({ row, column }, false)
    setMenu({ anchor: { x: event.clientX, y: event.clientY }, row, column })
  }

  function closeMenu() {
    setMenu(null)
    queueMicrotask(() => openerRef.current?.focus())
  }

  function menuEntries(): MenuEntry[] {
    const slot = slots[menu?.row ?? active.row]
    const column = visibleColumns[(menu?.column ?? active.column) - selectOffset]
    const rowName = slot?.row ? nameOf(slot.row) : slot ? `new row ${stagedNumbers.get(slot.id)}` : 'this row'
    const highlight = slot ? view.highlights[slot.id] : undefined
    const selectedCount = Math.max(1, selection.range.bottom - selection.range.top + 1)
    const highlightedCount = Object.keys(view.highlights).length
    return [
      ...(onOpenRow && slot?.row ? [{ kind: 'action' as const, id: 'open-row', label: 'Open record', run: () => onOpenRow(slot.row!) }] : []),
      ...(onEditRow && slot?.row ? [{ kind: 'action' as const, id: 'edit-row', label: 'Edit in full form', run: () => onEditRow(slot.row!) }] : []),
      ...(onOpenRow || onEditRow ? [separator('sep-open')] : []),
      { kind: 'action', id: 'copy-cells', label: 'Copy', hint: 'Ctrl+C', run: () => { copyToClipboard() } },
      { kind: 'action', id: 'copy-row', label: `Copy ${rowName}`, run: copyRow },
      { kind: 'action', id: 'paste', label: 'Paste', hint: 'Ctrl+V', disabled: !editable, run: pasteRow },
      separator('sep-insert'),
      { kind: 'action', id: 'insert-above', label: 'Insert 1 row above', disabled: !canStage, run: () => addStagedRow(null, slot?.id) },
      { kind: 'action', id: 'insert-below', label: 'Insert 1 row below', disabled: !canStage, run: () => addStagedRow(slot?.id ?? null) },
      { kind: 'action', id: 'delete', label: slot && !slot.row ? 'Delete new row' : 'Delete row', disabled: !slot || (Boolean(slot.row) && !onDeleteRows), hint: slot?.row && !onDeleteRows ? 'Moves to recycle bin when available' : undefined, run: () => { void deleteActiveRow() } },
      { kind: 'action', id: 'hide', label: 'Hide row', disabled: !slot, run: () => slot && hideRow(slot.id) },
      separator('sep-view'),
      { kind: 'group', id: 'density', label: 'Row height', entries: rowDensities.map((density) => ({
        kind: 'choice' as const, id: `density-${density}`, label: rowDensityLabels[density], checked: view.density === density,
        run: () => update((current) => ({ ...current, density })),
      })) },
      { kind: 'action', id: 'filter', label: column ? `Filter ${column.header} to this value` : 'Filter by this cell', disabled: !column, run: filterFromCell },
      { kind: 'action', id: 'group', label: column ? (activeGroupBy === column.key ? `Ungroup ${column.header}` : `Group by ${column.header}`) : 'Group by column', disabled: !column, run: groupByActiveColumn },
      { kind: 'action', id: 'validate', label: column ? `Data validation for ${column.header}` : 'Data validation', disabled: !column, run: () => setValidationOpen(true) },
      separator('sep-color'),
      { kind: 'group', id: 'highlight', label: 'Highlight', entries: highlightColors.map((color) => ({
        kind: 'choice' as const, id: `highlight-${color}`, label: highlightLabels[color], checked: highlight === color, swatch: highlightSwatchClasses[color],
        run: () => slot && highlightRow(slot.id, color),
      })) },
      { kind: 'action', id: 'clear-highlight', label: 'Clear highlight on row', disabled: !highlight, run: () => slot && highlightRow(slot.id, null) },
      { kind: 'action', id: 'clear-highlights', label: 'Clear all highlights', disabled: highlightedCount === 0, run: clearAllHighlights },
      separator('sep-export'),
      { kind: 'group', id: 'export-view', label: 'Export this view', entries: [
        { kind: 'action' as const, id: 'csv-filtered', label: 'CSV', run: () => { void exportSheet('csv', 'filtered') } },
        { kind: 'action' as const, id: 'json-filtered', label: 'JSON', run: () => { void exportSheet('json', 'filtered') } },
        { kind: 'action' as const, id: 'xlsx-filtered', label: 'Excel (.xlsx)', run: () => { void exportSheet('xlsx', 'filtered') } },
      ] },
      { kind: 'group', id: 'export-sel', label: `Export ${selectedCount} selected`, entries: [
        { kind: 'action' as const, id: 'csv-sel', label: 'CSV', run: () => { void exportSheet('csv', 'selected') } },
        { kind: 'action' as const, id: 'json-sel', label: 'JSON', run: () => { void exportSheet('json', 'selected') } },
        { kind: 'action' as const, id: 'xlsx-sel', label: 'Excel (.xlsx)', run: () => { void exportSheet('xlsx', 'selected') } },
      ] },
      ...(highlightedCount > 0 ? [{
        kind: 'group' as const, id: 'export-hi', label: `Export ${highlightedCount} highlighted`, entries: [
          { kind: 'action' as const, id: 'csv-hi', label: 'CSV', run: () => { void exportSheet('csv', 'highlighted') } },
          { kind: 'action' as const, id: 'json-hi', label: 'JSON', run: () => { void exportSheet('json', 'highlighted') } },
          { kind: 'action' as const, id: 'xlsx-hi', label: 'Excel (.xlsx)', run: () => { void exportSheet('xlsx', 'highlighted') } },
        ],
      }] : []),
    ]
  }

  // The checkbox and action columns are navigable but hold no field, so
  // clipboard coordinates clamp into the data columns instead of sliding the
  // copied or pasted block sideways when the active cell sits on one of them.
  function dataColumn(index: number) {
    return Math.min(Math.max(index - selectOffset, 0), Math.max(visibleColumns.length - 1, 0))
  }

  function dataRange() {
    return { ...selection.range, left: dataColumn(selection.range.left), right: dataColumn(selection.range.right) }
  }

  function handleCopy(event: ClipboardEvent<HTMLTableElement>) {
    if (editor.editing) return
    event.clipboardData.setData('text/plain', copyRange(surface, dataRange()))
    event.preventDefault()
  }

  function applyPaste(text: string) {
    checkpoint()
    setNotice(describeOutcome(pasteText(surface, { row: active.row, column: dataColumn(active.column) }, dataRange(), text)))
  }

  function handlePaste(event: ClipboardEvent<HTMLTableElement>) {
    nativeClipboard.current = true
    if (!editable || editor.editing) return
    const text = event.clipboardData.getData('text/plain')
    if (!text) return
    applyPaste(text)
    event.preventDefault()
  }

  /** Blanks every editable cell in the selection, as Delete does in a spreadsheet. */
  function clearRange() {
    checkpoint()
    const range = dataRange()
    for (let rowIndex = range.top; rowIndex <= range.bottom; rowIndex += 1) {
      const slot = slots[rowIndex]
      if (!slot || !rowEditable(slot.row)) continue
      for (let columnIndex = range.left; columnIndex <= range.right; columnIndex += 1) {
        const column = visibleColumns[columnIndex]
        if (!column?.editable) continue
        editor.setCell(slot.id, column, slot.row ? column.text(slot.row) : '', '')
      }
    }
  }

  // Ctrl+C and Ctrl+V normally arrive as clipboard events on the focused cell.
  // Some browsers withhold them from a table cell that holds no text selection,
  // so the keyboard path reads and writes the clipboard directly and steps
  // aside whenever the native event does arrive first.
  function copyToClipboard() {
    if (!navigator.clipboard?.writeText) return false
    void navigator.clipboard.writeText(copyRange(surface, dataRange()))
      .then(() => setNotice('Copied the selection.'))
      .catch(() => setNotice('Copying was blocked. Use the browser Edit menu.'))
    return true
  }

  function pasteFromClipboard() {
    nativeClipboard.current = false
    window.setTimeout(() => {
      if (nativeClipboard.current || !navigator.clipboard?.readText) return
      void navigator.clipboard.readText()
        .then((text) => { if (text) applyPaste(text) })
        .catch(() => setNotice('Pasting was blocked. Allow clipboard access, or use the browser Edit menu.'))
    }, 0)
  }

  function handleKeyDown(event: KeyboardEvent<HTMLTableElement>) {
    if (editor.editing) return
    const target = event.target
    if (target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement || target instanceof HTMLSelectElement) return
    const extend = event.shiftKey
    const stride = event.ctrlKey || event.metaKey
    if (event.key === 'ArrowUp') { selection.moveBy(stride ? -totalRowCount : -1, 0, extend); event.preventDefault(); return }
    if (event.key === 'ArrowDown') { selection.moveBy(stride ? totalRowCount : 1, 0, extend); event.preventDefault(); return }
    if (event.key === 'ArrowLeft') { selection.moveBy(0, stride ? -navColumnCount : -1, extend); event.preventDefault(); return }
    if (event.key === 'ArrowRight') { selection.moveBy(0, stride ? navColumnCount : 1, extend); event.preventDefault(); return }
    if (event.key === 'PageUp') { selection.moveBy(-pageStride, 0, extend); event.preventDefault(); return }
    if (event.key === 'PageDown') { selection.moveBy(pageStride, 0, extend); event.preventDefault(); return }
    if (event.key === 'Home') { selection.moveTo({ row: stride ? 0 : active.row, column: 0 }, extend); event.preventDefault(); return }
    if (event.key === 'End') { selection.moveTo({ row: stride ? totalRowCount - 1 : active.row, column: navColumnCount - 1 }, extend); event.preventDefault(); return }
    if (stride && (event.key === 'a' || event.key === 'A')) { selection.selectAll(); event.preventDefault(); return }
    if (stride && (event.key === 'c' || event.key === 'C')) {
      if (copyToClipboard()) event.preventDefault()
      return
    }
    if (stride && (event.key === 'x' || event.key === 'X')) {
      // Without the clipboard API the browser copy event fires after this
      // handler, so clearing first would put emptied cells on the clipboard.
      if (!editable || !copyToClipboard()) return
      clearRange()
      event.preventDefault()
      return
    }
    if (stride && (event.key === 'v' || event.key === 'V')) {
      if (!editable) return
      pasteFromClipboard()
      return
    }
    if (stride && (event.key === 'z' || event.key === 'Z')) {
      if (!editable) return
      if (extend) redo()
      else undo()
      event.preventDefault()
      return
    }
    if (stride && (event.key === 'y' || event.key === 'Y')) {
      if (!editable) return
      redo()
      event.preventDefault()
      return
    }
    if (stride && (event.key === 'd' || event.key === 'D')) {
      if (!editable) return
      checkpoint()
      setNotice(describeOutcome(fillDown(surface, dataRange())))
      event.preventDefault()
      return
    }
    if (event.key === 'ContextMenu' || (event.shiftKey && event.key === 'F10')) {
      const cell = cellRefs.current.get(`${active.row}:${active.column}`)
      const rect = cell?.getBoundingClientRect()
      openMenu(active.row, active.column, { clientX: rect?.left ?? 8, clientY: rect?.bottom ?? 8, preventDefault: () => event.preventDefault(), currentTarget: cell })
      return
    }
    if (event.key === ' ' && selectable && active.column === 0) {
      if (activeRow) selection.toggleRowId(rowId(activeRow))
      event.preventDefault()
      return
    }
    if (editable && (event.key === 'Delete' || event.key === 'Backspace')) {
      clearRange()
      event.preventDefault()
      return
    }
    if (!canEditActive || !activeRowId || !activeColumn) return
    if (event.key === 'Enter' || event.key === 'F2') {
      editor.beginEdit(activeRowId, activeColumn, activeStored)
      event.preventDefault()
      return
    }
    // Typing over a cell replaces it, as in a spreadsheet.
    if (isPrintableKey(event)) {
      editor.beginEdit(activeRowId, activeColumn, activeStored, event.key)
      event.preventDefault()
    }
  }

  function finishEdit(column: GridColumn<T>, stored: string, move: 'down' | 'right' | 'none', draftOverride?: string) {
    const editing = editor.editing
    const draft = draftOverride ?? editing?.draft
    if (editing && draft !== undefined && draft !== editor.effectiveText(editing.rowId, column, stored)) checkpoint()
    if (editing && draft !== undefined && draft !== editing.draft) {
      editor.setCell(editing.rowId, column, stored, draft)
      editor.cancelDraft()
    } else {
      editor.commitDraft(column, stored)
    }
    setCameraOpen(false)
    setCellEditorExpanded(false)
    if (move === 'down') selection.moveBy(1, 0, false)
    else if (move === 'right') selection.moveBy(0, 1, false)
    else selection.focusCell(active, false)
  }

  function registerCell(row: number, column: number, element: HTMLTableCellElement | null) {
    const key = `${row}:${column}`
    if (element) cellRefs.current.set(key, element)
    else cellRefs.current.delete(key)
  }

  function cellPointerDown(position: CellPosition, event: PointerEvent<HTMLTableCellElement>) {
    if (event.button !== 0 || editor.editing) return
    if (event.shiftKey) selection.focusCell(position, true)
    else selection.beginDrag(position)
  }

  function effectiveRowValues(id: string, row: T | undefined): Record<string, string> {
    const values: Record<string, string> = {}
    for (const column of visibleColumns) {
      values[column.key] = editor.effectiveText(id, column, row ? column.text(row) : '')
    }
    return values
  }

  function renderEditor(column: GridColumn<T>, stored: string, name: string, context: { id: string; row: T | undefined }) {
    const commitKeys = (event: KeyboardEvent<HTMLElement>) => {
      if (event.key === 'Enter') { finishEdit(column, stored, 'down'); event.preventDefault() }
      else if (event.key === 'Tab') { finishEdit(column, stored, 'right'); event.preventDefault() }
      else if (event.key === 'Escape') {
        setCellEditorExpanded(false)
        editor.cancelDraft()
        selection.focusCell(active, false)
        event.preventDefault()
      }
    }
    const draft = editor.editing?.draft ?? ''
    const cell = cellRefs.current.get(`${active.row}:${active.column}`)
    const anchor = cell?.getBoundingClientRect() ?? new DOMRect(8, 8, 0, 0)
    const lookup = column.resolveLookup?.({ row: context.row, values: effectiveRowValues(context.id, context.row) }) ?? column.lookup
    if (lookup) {
      return <>
        <span className="sr-only">{name}</span>
        <CellLookup
          anchor={anchor}
          label={column.header}
          lookup={lookup}
          onChange={(text) => {
            if (!lookup.multiple) finishEdit(column, stored, 'none', text)
            else editor.updateDraft(text)
          }}
          onClose={() => finishEdit(column, stored, 'none')}
          value={draft}
        />
      </>
    }
    if (column.kind === 'enum' && column.options) {
      return <span className="flex h-full min-h-8 items-stretch">
        <select
          aria-label={name}
          autoFocus
          className={gridEditorClass}
          onBlur={() => finishEdit(column, stored, 'none')}
          onChange={(event) => editor.updateDraft(event.target.value)}
          onKeyDown={commitKeys}
          value={draft}
        >
          <option value="">Not set</option>
          {column.options.map((option) => <option key={option} value={option}>{option}</option>)}
        </select>
      </span>
    }
    return <span className="flex h-full min-h-8 items-stretch">
      <input
        aria-label={name}
        autoFocus={!cellEditorExpanded}
        className={gridEditorClass}
        inputMode={column.kind === 'money' || column.kind === 'number' ? 'decimal' : undefined}
        onBlur={(event) => {
          if (column.scannable || cellEditorExpanded) return
          const next = event.relatedTarget
          if (next instanceof Node && event.currentTarget.parentElement?.contains(next)) return
          finishEdit(column, stored, 'none')
        }}
        onChange={(event) => editor.updateDraft(event.target.value)}
        onKeyDown={commitKeys}
        type={column.kind === 'date' ? 'date' : column.kind === 'instant' ? 'datetime-local' : 'text'}
        value={draft}
      />
      {column.kind === 'text' && <button
        aria-expanded={cellEditorExpanded}
        aria-label={`Edit ${column.header} in full screen`}
        className={cx(plainButtonClass, 'min-h-8 shrink-0 px-1 py-0 text-xs')}
        onMouseDown={(event) => event.preventDefault()}
        onClick={() => setCellEditorExpanded(true)}
        type="button"
      >Expand</button>}
      {column.scannable && <>
        <button
          aria-label={`Scan ${column.header} with camera`}
          className={cx(plainButtonClass, 'min-h-8 shrink-0 px-1 py-0 text-xs')}
          onMouseDown={(event) => event.preventDefault()}
          onClick={() => setCameraOpen(true)}
          type="button"
        >Scan</button>
        {cameraOpen && <CellCamera
          anchor={anchor}
            onCapture={(value) => {
            editor.updateDraft(value)
            setCameraOpen(false)
            finishEdit(column, stored, 'none', value)
          }}
          onClose={() => setCameraOpen(false)}
        />}
      </>}
      {cellEditorExpanded && createPortal(
        <div className="fixed inset-0 z-50 flex items-stretch justify-center bg-steward-ink-950/80 p-4 sm:p-6">
          <div
            aria-labelledby="grid-cell-editor-title"
            aria-modal="true"
            className={cx(subpanelClass, 'flex h-full w-full max-w-5xl flex-col bg-steward-ink-900 p-4 shadow-2xl')}
            role="dialog"
          >
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0">
                <p className="text-xs font-medium text-steward-slate">Cell editor</p>
                <h2 className="mt-1 text-lg font-semibold text-steward-mist" id="grid-cell-editor-title">{name}</h2>
              </div>
              <div className="flex flex-wrap gap-2">
                <button className={buttonClass} onMouseDown={(event) => event.preventDefault()} onClick={() => finishEdit(column, stored, 'none')} type="button">Done</button>
                <button className={secondaryButtonClass} onMouseDown={(event) => event.preventDefault()} onClick={() => { setCellEditorExpanded(false); editor.cancelDraft(); selection.focusCell(active, false) }} type="button">Cancel</button>
              </div>
            </div>
            <label className="mt-4 flex min-h-0 flex-1 flex-col">
              <span className="sr-only">{name}</span>
              <textarea
                autoFocus
                className={cx(compactInputClass, 'min-h-0 w-full flex-1 resize-none p-3 font-mono text-sm')}
                onChange={(event) => editor.updateDraft(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === 'Escape') {
                    setCellEditorExpanded(false)
                    editor.cancelDraft()
                    selection.focusCell(active, false)
                    event.preventDefault()
                  }
                }}
                spellCheck={false}
                value={draft}
              />
            </label>
          </div>
        </div>,
        document.body,
      )}
    </span>
  }

  function renderCell(options: { id: string; column: GridColumn<T>; row: T | undefined; rowIndex: number; navIndex: number; name: string }) {
    const { id, column, row, rowIndex, navIndex, name } = options
    const stored = row ? column.text(row) : ''
    const edit = editor.editFor(id, column.key)
    const isEditing = editor.editing?.rowId === id && editor.editing.columnKey === column.key
    const cellEditable = editable && Boolean(column.editable) && rowEditable(row)
    return <td
      aria-invalid={edit?.error ? true : undefined}
      aria-readonly={cellEditable ? undefined : true}
      aria-selected={selection.isSelected(rowIndex, navIndex)}
      className={cx(
        gridCellClass,
        rowDensityClasses[view.density],
        column.align === 'right' && 'text-right tabular-nums',
        selection.isSelected(rowIndex, navIndex) && !isEditing && 'bg-steward-teal/15',
        selection.isActive(rowIndex, navIndex) && !isEditing && 'ring-2 ring-inset ring-steward-teal',
        edit && !edit.error && 'bg-steward-blue/20',
        edit?.error && 'bg-steward-danger/25',
        isEditing && 'p-0',
      )}
      key={column.key}
      onContextMenu={(event) => openMenu(rowIndex, navIndex, event)}
      onDoubleClick={() => cellEditable && editor.beginEdit(id, column, stored)}
      onPointerDown={(event) => cellPointerDown({ row: rowIndex, column: navIndex }, event)}
      onPointerEnter={() => dragging && selection.extendDrag({ row: rowIndex, column: navIndex })}
      ref={(element) => registerCell(rowIndex, navIndex, element)}
      role="gridcell"
      tabIndex={selection.isActive(rowIndex, navIndex) ? 0 : -1}
      title={edit?.error ?? editor.effectiveText(id, column, stored)}
    >
      {isEditing
        ? renderEditor(column, stored, `${column.header} for ${name}`, { id, row })
        : <>
          {edit
            ? (column.lookup ? <LookupChips text={edit.text} column={column} /> : edit.text)
            : row && column.display ? column.display(row) : row && column.lookup ? <LookupChips text={column.text(row)} column={column} /> : row ? column.text(row) : ''}
          {edit?.error && <span className="sr-only">Invalid: {edit.error}</span>}
        </>}
    </td>
  }

  function renderSlotRow(slot: RowSlot<T>, rowIndex: number) {
    const { id, row } = slot
    const stagedNumber = stagedNumbers.get(id)
    const name = row ? nameOf(row) : `new row ${stagedNumber}`
    const state = row ? rowState?.(row) : undefined
    const highlight = view.highlights[id]
    return <tr
      aria-rowindex={rowIndex + 2}
      aria-selected={selectable && row ? selectedRowIds.has(id) : undefined}
      className={cx('group/row bg-steward-ink-950', !row && 'bg-steward-ink-900', highlight && highlightRowClasses[highlight])}
      key={id}
      role="row"
    >
      {selectable && (row
        ? <td
          aria-selected={selection.isSelected(rowIndex, 0)}
          className={cx(gridCellClass, rowDensityClasses[view.density], 'text-center', selection.isSelected(rowIndex, 0) && 'bg-steward-teal/15', selection.isActive(rowIndex, 0) && 'ring-2 ring-inset ring-steward-teal')}
          onContextMenu={(event) => openMenu(rowIndex, 0, event)}
          onPointerDown={(event) => cellPointerDown({ row: rowIndex, column: 0 }, event)}
          onPointerEnter={() => dragging && selection.extendDrag({ row: rowIndex, column: 0 })}
          ref={(element) => registerCell(rowIndex, 0, element)}
          role="gridcell"
          tabIndex={selection.isActive(rowIndex, 0) ? 0 : -1}
        >
          <input
            aria-label={`Select ${name}`}
            checked={selectedRowIds.has(id)}
            onChange={() => selection.toggleRowId(id)}
            tabIndex={-1}
            type="checkbox"
          />
        </td>
        : <td className={cx(gridCellClass, rowDensityClasses[view.density], 'text-center text-xs text-steward-slate')} role="gridcell" tabIndex={-1}>New</td>)}
      {visibleColumns.map((column, columnIndex) => renderCell({ id, column, row, rowIndex, navIndex: columnIndex + selectOffset, name }))}
      {showRecordColumn && <td
        aria-selected={row ? selection.isSelected(rowIndex, navColumnCount - 1) : undefined}
        className={cx(
          gridActionCellClass,
          rowDensityClasses[view.density],
          highlight && highlightRowClasses[highlight],
          !row && 'bg-steward-ink-900',
          selection.isActive(rowIndex, navColumnCount - 1) && 'ring-2 ring-inset ring-steward-teal',
        )}
        onContextMenu={(event) => openMenu(rowIndex, navColumnCount - 1, event)}
        onPointerDown={(event) => cellPointerDown({ row: rowIndex, column: navColumnCount - 1 }, event)}
        ref={(element) => registerCell(rowIndex, navColumnCount - 1, element)}
        role="gridcell"
        tabIndex={selection.isActive(rowIndex, navColumnCount - 1) ? 0 : -1}
      >
        <span className="flex items-center gap-1 whitespace-nowrap">
          {recordExpanded && canStage && row && <button
            aria-label={`Insert a new row below ${name}`}
            className={cx(gridActionButtonClass, 'text-sm leading-none text-steward-slate group-hover/row:text-steward-teal')}
            onClick={() => addStagedRow(id)}
            onPointerDown={(event) => event.stopPropagation()}
            tabIndex={-1}
            title={`Insert a new row below ${name}`}
            type="button"
          >+</button>}
          <button
            aria-label={`Row actions for ${name}`}
            className={gridActionButtonClass}
            onClick={(event) => openMenu(rowIndex, navColumnCount - 1, { clientX: event.clientX, clientY: event.clientY, preventDefault: () => {}, currentTarget: event.currentTarget })}
            onPointerDown={(event) => event.stopPropagation()}
            tabIndex={-1}
            type="button"
          >⋯</button>
          {recordExpanded && onOpenRow && row && <button aria-label={`Open ${name}`} className={gridActionButtonClass} onClick={() => onOpenRow(row)} onPointerDown={(event) => event.stopPropagation()} tabIndex={-1} type="button">Open</button>}
          {recordExpanded && onEditRow && row && <button aria-label={`Edit ${name} in full form`} className={gridActionButtonClass} onClick={() => onEditRow(row)} onPointerDown={(event) => event.stopPropagation()} tabIndex={-1} type="button">Edit</button>}
          {recordExpanded && !row && <button aria-label={`Remove ${name}`} className={gridActionButtonClass} onClick={() => removeStaged(id)} tabIndex={-1} type="button">Remove</button>}
          {state && <span className={cx('text-xs', state === 'failed' ? 'text-[#ffccd1]' : state === 'saving' ? 'text-steward-mist-muted' : 'text-[#98eab9]')} title={row ? rowMessage?.(row) : undefined}>
            {stateLabels[state]}
          </span>}
        </span>
      </td>}
    </tr>
  }

  function renderGroupHeader(value: string, count: number) {
    const collapsed = collapsedGroups.has(value)
    return <tr className="bg-steward-ink-900" key={`group-${value}`}>
      <td className="border-b border-white/10 px-2.5 py-1" colSpan={Math.max(navColumnCount, 1)}>
        <button
          aria-expanded={!collapsed}
          className={cx(plainButtonClass, 'min-h-8 justify-start px-1 py-0 text-xs font-semibold')}
          onClick={() => update((current) => ({
            ...current,
            collapsedGroups: collapsed ? current.collapsedGroups.filter((item) => item !== value) : [...current.collapsedGroups, value],
          }))}
          type="button"
        >
          <span aria-hidden="true">{collapsed ? '▸' : '▾'}</span>
          {groupColumn?.header ?? 'Group'}: {value || '(blank)'} · {count}
        </button>
      </td>
    </tr>
  }

  function renderBody() {
    if (totalRowCount === 0 && !grouped) {
      return <tr role="row"><td className="px-3 py-8 text-center text-steward-mist-muted" colSpan={Math.max(navColumnCount, 1)} role="gridcell">{emptyMessage}</td></tr>
    }
    if (!grouped) return slots.map((slot, rowIndex) => renderSlotRow(slot, rowIndex))
    const nodes: ReactNode[] = []
    let slotIndex = 0
    for (const group of grouped) {
      nodes.push(renderGroupHeader(group.value, group.rows.length))
      if (collapsedGroups.has(group.value)) continue
      while (slotIndex < slots.length) {
        const slot = slots[slotIndex]
        if (slot.row && (groupColumn?.text(slot.row) || '(blank)') !== group.value) break
        nodes.push(renderSlotRow(slot, slotIndex))
        slotIndex += 1
      }
    }
    while (slotIndex < slots.length) {
      nodes.push(renderSlotRow(slots[slotIndex], slotIndex))
      slotIndex += 1
    }
    if (nodes.length === 0) {
      return <tr role="row"><td className="px-3 py-8 text-center text-steward-mist-muted" colSpan={Math.max(navColumnCount, 1)} role="gridcell">{emptyMessage}</td></tr>
    }
    return nodes
  }

  const filtered = visibleRows.length !== rows.length
  const changedRecords = useMemo(() => groupEditsByRow(existingEdits).size, [existingEdits])
  const pendingTotal = existingEdits.length + staged.length
  const selectedSavedQuery = view.savedQueries.find((item) => item.name === queryName)

  const grid = <div
    aria-label={fullscreen ? `${label} full screen editor` : undefined}
    aria-modal={fullscreen ? true : undefined}
    className={cx(
      'min-w-0',
      fullscreen
        ? 'fixed inset-0 z-30 flex flex-col bg-steward-ink-950'
        : subpanelClass,
    )}
    ref={rootRef}
    role={fullscreen ? 'dialog' : undefined}
  >
    {fullscreen && <div className="flex shrink-0 items-center justify-between gap-3 border-b border-white/[0.08] bg-steward-ink-900 px-3 py-2">
      <p className="text-sm font-semibold text-steward-mist">{label}</p>
      <button className={cx(secondaryButtonClass, 'min-h-8 px-3 py-1 text-xs')} onClick={closeFullscreen} ref={exitFullscreenRef} type="button">
        <CollapseIcon /> Exit full screen
      </button>
    </div>}
    <div className={gridToolbarClass}>
      <label className="flex min-w-0 items-center gap-2">
        <span className="sr-only">Search {label}</span>
        <input
          className={cx(gridFilterClass, 'w-56')}
          onChange={(event) => setSearch(event.target.value)}
          placeholder={`Search ${label.toLowerCase()}`}
          type="search"
          value={search}
        />
      </label>
      <p className="text-xs text-steward-mist-muted">{filtered ? `${visibleRows.length} of ${rows.length} rows` : `${rows.length} rows`}</p>
      <button
        aria-expanded={queryOpen}
        className={cx(plainButtonClass, 'min-h-8 px-2 py-1 text-xs', (view.query.trim() || queryOpen) && 'text-steward-mist')}
        onClick={() => setQueryOpen((open) => !open)}
        type="button"
      >{view.query.trim() ? 'Edit filter' : 'Filter'}</button>
      <div className="flex min-w-0 items-center gap-1.5 text-xs text-steward-mist-muted">
        <span className="whitespace-nowrap">Group by</span>
        <select
          aria-label={`Group ${label}`}
          className={cx(compactInputClass, 'min-h-7 w-36 px-2 py-0 text-xs')}
          onChange={(event) => setGroupBy(event.target.value || null)}
          value={activeGroupBy ?? ''}
        >
          <option value="">No grouping</option>
          {columns.map((column) => <option key={column.key} value={column.key}>{column.header}</option>)}
        </select>
      </div>
      {view.hiddenRows.length > 0 && <button className={cx(plainButtonClass, 'min-h-8 px-2 py-1 text-xs')} onClick={restoreHiddenRows} type="button">Show {view.hiddenRows.length} hidden</button>}
      {activeGroupBy && <button className={cx(plainButtonClass, 'min-h-8 px-2 py-1 text-xs')} onClick={() => setGroupBy(null)} type="button">Ungroup</button>}
      {countViewAdjustments(view) > 0 && <button className={cx(plainButtonClass, 'min-h-8 px-2 py-1 text-xs')} onClick={resetView} type="button">Reset view</button>}
      {selectedRows.length > 0 && <p className="text-xs font-semibold text-steward-teal">{selectedRows.length} selected</p>}
      {selectedRows.length > 0 && bulkActions?.(selectedRows)}
      {canStage && <button className={cx(plainButtonClass, 'min-h-8 px-2 py-1 text-xs')} onClick={() => addStagedRow()} type="button"><span aria-hidden="true">+</span> Add row</button>}
      {editable && <span className="flex items-center gap-1">
        <button className={cx(plainButtonClass, 'min-h-8 px-2 py-1 text-xs')} disabled={past.length === 0} onClick={undo} title="Undo (Ctrl+Z)" type="button">Undo</button>
        <button className={cx(plainButtonClass, 'min-h-8 px-2 py-1 text-xs')} disabled={future.length === 0} onClick={redo} title="Redo (Ctrl+Shift+Z)" type="button">Redo</button>
      </span>}
      {toolbar}
      {!fullscreen && <button
        aria-label={`Open ${label} in full screen`}
        className={cx(plainButtonClass, 'min-h-8 px-2 py-1 text-xs')}
        onClick={() => setFullscreen(true)}
        ref={fullscreenButtonRef}
        type="button"
      ><ExpandIcon /> Full screen</button>}
      <div className="relative" ref={exportRef}>
        <button
          aria-expanded={exportOpen}
          aria-haspopup="menu"
          aria-label="Export"
          className={cx(plainButtonClass, 'min-h-8 px-2 py-1 text-xs')}
          onClick={() => setExportOpen((open) => !open)}
          type="button"
        >{exporting ? 'Exporting…' : 'Export'}</button>
        {exportOpen && <div className={cx(menuSurfaceClass, 'absolute right-0 z-20 mt-1 w-56 p-1')} role="menu">
          <p className="px-2 pt-1 text-[11px] font-medium text-steward-slate">This view</p>
          <button className={cx(plainButtonClass, 'min-h-8 w-full justify-start px-2 py-1 text-xs')} onClick={() => { setExportOpen(false); void exportSheet('xlsx', 'filtered') }} role="menuitem" type="button">Excel (.xlsx)</button>
          <button className={cx(plainButtonClass, 'min-h-8 w-full justify-start px-2 py-1 text-xs')} onClick={() => { setExportOpen(false); void exportSheet('csv', 'filtered') }} role="menuitem" type="button">CSV</button>
          <button className={cx(plainButtonClass, 'min-h-8 w-full justify-start px-2 py-1 text-xs')} onClick={() => { setExportOpen(false); void exportSheet('json', 'filtered') }} role="menuitem" type="button">JSON</button>
          {exportAllRows && <>
            <p className="px-2 pt-2 text-[11px] font-medium text-steward-slate">All records</p>
            <button className={cx(plainButtonClass, 'min-h-8 w-full justify-start px-2 py-1 text-xs')} onClick={() => { setExportOpen(false); void exportAllRecords('xlsx') }} role="menuitem" type="button">Excel (.xlsx)</button>
            <button className={cx(plainButtonClass, 'min-h-8 w-full justify-start px-2 py-1 text-xs')} onClick={() => { setExportOpen(false); void exportAllRecords('csv') }} role="menuitem" type="button">CSV</button>
            <button className={cx(plainButtonClass, 'min-h-8 w-full justify-start px-2 py-1 text-xs')} onClick={() => { setExportOpen(false); void exportAllRecords('json') }} role="menuitem" type="button">JSON</button>
          </>}
        </div>}
      </div>
      {actionsAvailable && !showRecordColumn && <button className={cx(plainButtonClass, 'min-h-8 px-2 py-1 text-xs')} onClick={() => setRecordColumn('expanded')} type="button">Show Record</button>}
      <div className="relative ml-auto" ref={columnsRef}>
        <button
          aria-expanded={columnsOpen}
          aria-haspopup="menu"
          className={cx(plainButtonClass, 'min-h-8 px-2 py-1 text-xs')}
          onClick={() => setColumnsOpen((open) => !open)}
          type="button"
        >Columns</button>
        {columnsOpen && <div className={cx(menuSurfaceClass, 'absolute right-0 z-20 mt-1 max-h-72 w-56 overflow-y-auto p-2 steward-scrollbar')} onWheel={(event) => event.stopPropagation()} role="menu">
          <fieldset>
            <legend className="sr-only">Visible columns for {label}</legend>
            {columns.map((column) => <label className="flex min-h-8 items-center gap-2 px-1 text-xs" key={column.key}>
              <input checked={!hidden.has(column.key)} onChange={() => toggleColumn(column.key)} type="checkbox" />
              <span>{column.header}</span>
            </label>)}
            {actionsAvailable && <label className="flex min-h-8 items-center gap-2 px-1 text-xs">
              <input checked={showRecordColumn} onChange={() => setRecordColumn(showRecordColumn ? 'hidden' : 'expanded')} type="checkbox" />
              <span>Record</span>
            </label>}
          </fieldset>
        </div>}
      </div>
    </div>
    {queryOpen && <div className="border-b border-white/[0.08] bg-steward-ink-900 px-3 py-3">
      <QueryBuilder
        encoded={view.query}
        error={queryError}
        fields={queryFields}
        model={queryModel}
        onEncodedChange={applyEncodedQuery}
        onModelChange={applyQueryModel}
      />
      <div className="mt-3 flex flex-wrap items-center gap-2">
        <button className={cx(secondaryButtonClass, 'min-h-8 px-3 py-1 text-xs')} onClick={() => { applyQueryModel(emptyQuery()); setQueryOpen(false) }} type="button">Clear filter</button>
        <label className="flex min-w-0 items-center gap-2">
          <span className="sr-only">Query name</span>
          <input
            className={cx(compactInputClass, 'min-h-8 w-44 px-2 py-1 text-xs')}
            maxLength={80}
            onChange={(event) => setQueryName(event.target.value)}
            placeholder="Name this query"
            value={queryName}
          />
        </label>
        <button className={cx(buttonClass, 'min-h-8 px-3 py-1 text-xs')} onClick={saveCurrentQuery} type="button">Save query</button>
        {view.savedQueries.length > 0 && <label className="flex min-w-0 items-center gap-2">
          <span className="sr-only">Saved queries</span>
          <select
            aria-label="Saved queries"
            className={cx(compactInputClass, 'min-h-8 w-48 px-2 py-1 text-xs')}
            onChange={(event) => { if (event.target.value) loadSavedQuery(event.target.value) }}
            value={selectedSavedQuery && view.query === selectedSavedQuery.query ? selectedSavedQuery.id : ''}
          >
            <option value="">Saved queries</option>
            {view.savedQueries.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
          </select>
        </label>}
        {selectedSavedQuery && <button className={cx(plainButtonClass, 'min-h-8 px-2 py-1 text-xs')} onClick={() => deleteSavedQuery(selectedSavedQuery.id)} type="button">Delete saved query</button>}
        {parsedQuery.ok && !isQueryEmpty(parsedQuery.model) && <p className="text-xs text-steward-mist-muted">{describeQuery(parsedQuery.model, queryFields)}</p>}
      </div>
    </div>}
    {grouped && grouped.length > 0 && <div aria-label={`${label} grouped counts`} className="flex flex-wrap gap-1.5 border-b border-white/[0.08] bg-steward-ink-900 px-3 py-2" role="status">
      {grouped.map((group) => (
        <span className="inline-flex items-center gap-1.5 rounded-sm border border-white/10 px-2 py-0.5 text-[11px] text-steward-mist-muted" key={group.value}>
          <span className="max-w-40 truncate text-steward-mist">{group.value}</span>
          <span className="font-medium text-steward-mist">{group.rows.length}</span>
        </span>
      ))}
    </div>}
    <p className="sr-only" role="status">{notice}</p>
    {editable && pendingTotal > 0 && <div className="flex flex-wrap items-center gap-3 border-b border-steward-teal/30 bg-steward-teal/10 px-3 py-2">
      <p className="text-xs font-semibold text-steward-mist">
        {existingEdits.length > 0 && `${existingEdits.length} ${existingEdits.length === 1 ? 'cell' : 'cells'} changed in ${changedRecords} ${changedRecords === 1 ? 'record' : 'records'}`}
        {existingEdits.length > 0 && staged.length > 0 && ' · '}
        {staged.length > 0 && `${staged.length} new ${staged.length === 1 ? 'row' : 'rows'}`}
      </p>
      {editor.invalidCount > 0 && <p className="text-xs font-semibold text-[#ffccd1]" role="alert">{editor.invalidCount} invalid, fix before saving</p>}
      <button className={cx(buttonClass, 'min-h-8 px-3 py-1 text-xs')} disabled={saving || editor.invalidCount > 0} onClick={() => void saveEdits()} type="button">
        {saving ? 'Saving…' : 'Save changes'}
      </button>
      <button className={cx(secondaryButtonClass, 'min-h-8 px-3 py-1 text-xs')} disabled={saving} onClick={discardAll} type="button">Discard</button>
    </div>}
    <div aria-label={label} className={cx(gridWrapClass, fullscreen && 'min-h-0 flex-1')} role="region" style={fullscreen ? undefined : { maxHeight: maximumBodyHeight }} tabIndex={0}>
      <table
        aria-colcount={navColumnCount}
        aria-multiselectable={selectable || undefined}
        aria-rowcount={totalRowCount + 1}
        className={cx(gridTableClass, dragging && 'select-none')}
        onCopy={handleCopy}
        onKeyDown={handleKeyDown}
        onPaste={handlePaste}
        role="grid"
        style={{ minWidth: `${totalWidth}rem` }}
      >
        <thead>
          <tr aria-rowindex={1} role="row">
            {selectable && <th className={gridHeaderCellClass} role="columnheader" scope="col" style={{ width: '2.5rem' }}>
              <input
                aria-label={`Select all visible ${label.toLowerCase()}`}
                checked={allVisibleChecked}
                onChange={() => allVisibleChecked ? selection.clearRowIds() : selection.setAllRowIds(visibleRows.map(rowId))}
                type="checkbox"
              />
            </th>}
            {visibleColumns.map((column) => {
              const direction = sort?.key === column.key ? sort.direction : null
              return <th
                aria-sort={direction ?? 'none'}
                className={gridHeaderCellClass}
                key={column.key}
                role="columnheader"
                scope="col"
                style={{ width: `${columnWidth(column)}rem` }}
              >
                <button className="group flex w-full items-center gap-1 text-left text-xs font-semibold text-steward-mist-muted hover:text-steward-mist focus-visible:-outline-offset-2" onClick={() => toggleSort(column.key)} type="button">
                  <span className="truncate">{column.header}</span>
                  <SortIcon direction={direction} />
                </button>
                <input
                  aria-label={`Filter ${column.header}`}
                  className={cx(gridFilterClass, 'mt-1')}
                  onChange={(event) => setFilter(column.key, event.target.value)}
                  onKeyDown={(event) => event.stopPropagation()}
                  onPointerDown={(event) => event.stopPropagation()}
                  placeholder="Filter"
                  value={filters[column.key] ?? ''}
                />
                <div
                  aria-hidden="true"
                  className="absolute right-0 top-0 h-full w-1.5 cursor-col-resize hover:bg-steward-teal/40"
                  onPointerDown={(event) => beginResize(column.key, event)}
                />
              </th>
            })}
            {showRecordColumn && <th aria-label="Record" className={gridHeaderCellClass} role="columnheader" scope="col" style={{ width: `${actionColumnWidth}rem` }}>
              <div className="flex items-center gap-1">
                <span className="min-w-0 truncate text-xs font-semibold">{recordExpanded ? 'Record' : ''}</span>
                <span className="ml-auto flex shrink-0 items-center">
                  <button
                    aria-expanded={recordExpanded}
                    aria-label={recordExpanded ? 'Collapse record column' : 'Expand record column'}
                    className={cx(gridActionButtonClass, 'text-steward-mist-muted')}
                    onClick={() => setRecordColumn(recordExpanded ? 'collapsed' : 'expanded')}
                    type="button"
                  >{recordExpanded ? '«' : '»'}</button>
                  <button
                    aria-label="Hide record column"
                    className={cx(gridActionButtonClass, 'text-steward-mist-muted')}
                    onClick={() => setRecordColumn('hidden')}
                    type="button"
                  >×</button>
                </span>
              </div>
            </th>}
          </tr>
        </thead>
        <tbody>{renderBody()}</tbody>
      </table>
    </div>
    {menu && <ContextMenu anchor={menu.anchor} entries={menuEntries()} label={`Actions for ${label}`} onClose={closeMenu} />}
    <Drawer
      description="These checks run after the column's built-in rules, so they can tighten a field but never accept a value the API would reject."
      kicker="Grid"
      onClose={() => setValidationOpen(false)}
      open={validationOpen}
      title={activeColumn ? `Validation for ${activeColumn.header}` : 'Data validation'}
    >
      {activeColumn && <ValidationForm
        column={activeColumn}
        onSave={(rule) => {
          update((current) => {
            const validation = { ...current.validation }
            if (!rule) delete validation[activeColumn.key]
            else validation[activeColumn.key] = rule
            return { ...current, validation }
          })
          setValidationOpen(false)
        }}
        rule={view.validation[activeColumn.key]}
      />}
    </Drawer>
  </div>

  return fullscreen ? createPortal(grid, document.body) : grid
}

function ExpandIcon() {
  return (
    <svg aria-hidden="true" className="size-3.5" fill="none" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="1.8" viewBox="0 0 24 24">
      <path d="M8 4H4v4M16 4h4v4M8 20H4v-4M16 20h4v-4" />
    </svg>
  )
}

function CollapseIcon() {
  return (
    <svg aria-hidden="true" className="size-3.5" fill="none" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="1.8" viewBox="0 0 24 24">
      <path d="M9 4v5H4M15 4v5h5M9 20v-5H4M15 20v-5h5" />
    </svg>
  )
}

function ValidationForm({ column, rule, onSave }: { column: ColumnRules; rule?: ValidationRule; onSave: (rule?: ValidationRule) => void }) {
  const [required, setRequired] = useState(Boolean(rule?.required || column.required))
  const [allowed, setAllowed] = useState((rule?.allowed ?? []).join(', '))
  const [minimum, setMinimum] = useState(rule?.minimum?.toString() ?? '')
  const [maximum, setMaximum] = useState(rule?.maximum?.toString() ?? '')
  const [pattern, setPattern] = useState(rule?.pattern ?? '')
  const [message, setMessage] = useState(rule?.message ?? '')
  const numeric = column.kind === 'number' || column.kind === 'money'
  return <form
    className="grid gap-3"
    onSubmit={(event) => {
      event.preventDefault()
      const next: ValidationRule = {
        required: required && !column.required ? true : undefined,
        allowed: allowed.split(',').map((item) => item.trim()).filter(Boolean),
        minimum: numeric && minimum.trim() ? Number(minimum) : undefined,
        maximum: numeric && maximum.trim() ? Number(maximum) : undefined,
        pattern: pattern.trim() || undefined,
        message: message.trim() || undefined,
      }
      onSave(isEmptyRule(next) ? undefined : next)
    }}
  >
    <label className="flex min-h-8 items-center gap-2 text-sm">
      <input checked={required} disabled={column.required} onChange={(event) => setRequired(event.target.checked)} type="checkbox" />
      Required
    </label>
    <label className={labelClass}>
      Allowed values
      <input className={compactInputClass + ' mt-1 w-full'} onChange={(event) => setAllowed(event.target.value)} placeholder="comma-separated" value={allowed} />
    </label>
    {numeric && <div className="grid gap-3 sm:grid-cols-2">
      <label className={labelClass}>Minimum<input className={compactInputClass + ' mt-1 w-full'} onChange={(event) => setMinimum(event.target.value)} type="number" value={minimum} /></label>
      <label className={labelClass}>Maximum<input className={compactInputClass + ' mt-1 w-full'} onChange={(event) => setMaximum(event.target.value)} type="number" value={maximum} /></label>
    </div>}
    <label className={labelClass}>
      Pattern
      <input className={compactInputClass + ' mt-1 w-full'} onChange={(event) => setPattern(event.target.value)} placeholder="regular expression" value={pattern} />
    </label>
    <label className={labelClass}>
      Custom message
      <input className={compactInputClass + ' mt-1 w-full'} onChange={(event) => setMessage(event.target.value)} value={message} />
    </label>
    <div className="flex flex-wrap gap-2">
      <button className={buttonClass} type="submit">Save rule</button>
      <button className={secondaryButtonClass} onClick={() => onSave(undefined)} type="button">Clear rule</button>
    </div>
  </form>
}
