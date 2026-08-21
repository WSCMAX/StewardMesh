import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

// Requirements: REQ-WORKSPACE-001, A11Y-001. Feature: experience.grid.

// Cell and row selection for the data grid. Cell selection is a rectangle
// between an anchor and the active cell, matching spreadsheet behavior. Row
// selection is tracked separately by record id so it survives sorting and
// filtering, and drives bulk actions.

export type CellPosition = { row: number; column: number }

export type CellRange = { top: number; left: number; bottom: number; right: number }

export type GridSelection = {
  active: CellPosition
  anchor: CellPosition
  range: CellRange
  /** Increments on every navigation so the grid knows when to move DOM focus. */
  focusVersion: number
  dragging: boolean
  selectedRowIds: ReadonlySet<string>
  isSelected: (row: number, column: number) => boolean
  isActive: (row: number, column: number) => boolean
  focusCell: (position: CellPosition, extend?: boolean) => void
  moveBy: (rows: number, columns: number, extend: boolean) => void
  moveTo: (position: CellPosition, extend: boolean) => void
  selectAll: () => void
  beginDrag: (position: CellPosition) => void
  extendDrag: (position: CellPosition) => void
  endDrag: () => void
  toggleRowId: (id: string) => void
  setAllRowIds: (ids: readonly string[]) => void
  clearRowIds: () => void
}

export function rangeBetween(anchor: CellPosition, active: CellPosition): CellRange {
  return {
    top: Math.min(anchor.row, active.row),
    bottom: Math.max(anchor.row, active.row),
    left: Math.min(anchor.column, active.column),
    right: Math.max(anchor.column, active.column),
  }
}

export function rangeContains(range: CellRange, row: number, column: number) {
  return row >= range.top && row <= range.bottom && column >= range.left && column <= range.right
}

function clamp(value: number, maximum: number) {
  if (maximum <= 0) return 0
  return Math.min(Math.max(value, 0), maximum - 1)
}

export function useGridSelection({ rowCount, columnCount }: { rowCount: number; columnCount: number }): GridSelection {
  const [active, setActive] = useState<CellPosition>({ row: 0, column: 0 })
  const [anchor, setAnchor] = useState<CellPosition>({ row: 0, column: 0 })
  const [focusVersion, setFocusVersion] = useState(0)
  const [dragging, setDragging] = useState(false)
  const [selectedRowIds, setSelectedRowIds] = useState<ReadonlySet<string>>(new Set())
  const bounds = useRef({ rowCount, columnCount })
  bounds.current = { rowCount, columnCount }

  // Filtering and sorting change the addressable area, so keep the active cell
  // and anchor inside it rather than pointing at a row that no longer renders.
  useEffect(() => {
    setActive((current) => {
      const next = { row: clamp(current.row, rowCount), column: clamp(current.column, columnCount) }
      return next.row === current.row && next.column === current.column ? current : next
    })
    setAnchor((current) => {
      const next = { row: clamp(current.row, rowCount), column: clamp(current.column, columnCount) }
      return next.row === current.row && next.column === current.column ? current : next
    })
  }, [rowCount, columnCount])

  const apply = useCallback((position: CellPosition, extend: boolean) => {
    const next = { row: clamp(position.row, bounds.current.rowCount), column: clamp(position.column, bounds.current.columnCount) }
    setActive(next)
    if (!extend) setAnchor(next)
    setFocusVersion((version) => version + 1)
  }, [])

  const focusCell = useCallback((position: CellPosition, extend = false) => apply(position, extend), [apply])
  const moveTo = useCallback((position: CellPosition, extend: boolean) => apply(position, extend), [apply])
  const moveBy = useCallback((rows: number, columns: number, extend: boolean) => {
    setActive((current) => {
      const next = { row: clamp(current.row + rows, bounds.current.rowCount), column: clamp(current.column + columns, bounds.current.columnCount) }
      if (!extend) setAnchor(next)
      return next
    })
    setFocusVersion((version) => version + 1)
  }, [])

  const selectAll = useCallback(() => {
    setAnchor({ row: 0, column: 0 })
    setActive({ row: Math.max(bounds.current.rowCount - 1, 0), column: Math.max(bounds.current.columnCount - 1, 0) })
    setFocusVersion((version) => version + 1)
  }, [])

  const beginDrag = useCallback((position: CellPosition) => {
    setDragging(true)
    apply(position, false)
  }, [apply])

  const extendDrag = useCallback((position: CellPosition) => {
    setActive({ row: clamp(position.row, bounds.current.rowCount), column: clamp(position.column, bounds.current.columnCount) })
  }, [])

  const endDrag = useCallback(() => setDragging(false), [])

  // A pointer released outside the grid must still end the drag, otherwise the
  // next hover would keep extending a selection the user already finished.
  useEffect(() => {
    if (!dragging) return undefined
    const stop = () => setDragging(false)
    window.addEventListener('pointerup', stop)
    return () => window.removeEventListener('pointerup', stop)
  }, [dragging])

  const toggleRowId = useCallback((id: string) => {
    setSelectedRowIds((current) => {
      const next = new Set(current)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }, [])

  const setAllRowIds = useCallback((ids: readonly string[]) => setSelectedRowIds(new Set(ids)), [])
  const clearRowIds = useCallback(() => setSelectedRowIds(new Set()), [])

  const range = useMemo(() => rangeBetween(anchor, active), [anchor, active])
  const isSelected = useCallback((row: number, column: number) => rangeContains(range, row, column), [range])
  const isActive = useCallback((row: number, column: number) => active.row === row && active.column === column, [active])

  return {
    active, anchor, range, focusVersion, dragging, selectedRowIds,
    isSelected, isActive, focusCell, moveBy, moveTo, selectAll,
    beginDrag, extendDrag, endDrag, toggleRowId, setAllRowIds, clearRowIds,
  }
}
