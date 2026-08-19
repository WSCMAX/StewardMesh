import { decodeTSV, encodeTSV, type ColumnRules } from './columns'
import type { CellRange } from './useGridSelection'

// Requirements: REQ-WORKSPACE-001, REQ-ATLAS-001, REQ-STACK-001, A11Y-001. Feature: experience.grid.

// Clipboard and fill operations over a rectangular cell range. These are pure
// functions over an addressable surface so they can be exercised directly and
// reused for copy, paste, and fill without duplicating the coordinate math.

/** The grid surface a clipboard operation reads from and writes to. */
export type ClipboardSurface = {
  /** Addressable rows, including any already-staged rows. */
  rowCount: number
  columns: readonly ColumnRules[]
  /** Stable identifier for an addressable row, or undefined past the end. */
  rowIdAt: (rowIndex: number) => string | undefined
  /** Stored text for a cell, before any pending edit. Empty for staged rows. */
  storedText: (rowIndex: number, columnIndex: number) => string
  /** Text currently shown, including any pending edit. */
  currentText: (rowIndex: number, columnIndex: number) => string
  /** Records an edit against a row. */
  writeCell: (rowId: string, column: ColumnRules, stored: string, value: string) => void
  /** Creates a staged row past the end of the data and returns its identifier. */
  stageRow?: () => string
}

export type ClipboardOutcome = {
  applied: number
  /** Cells that fell on read-only columns or outside the column range. */
  skipped: number
  /** Rows created past the end of the existing data. */
  staged: number
}

export function describeOutcome(outcome: ClipboardOutcome) {
  const parts = [`${outcome.applied} ${outcome.applied === 1 ? 'cell' : 'cells'} pasted`]
  if (outcome.staged > 0) parts.push(`${outcome.staged} new ${outcome.staged === 1 ? 'row' : 'rows'} staged`)
  if (outcome.skipped > 0) parts.push(`${outcome.skipped} skipped as read-only`)
  return `${parts.join(', ')}.`
}

/** Builds the tab-separated text for a range, ready for the clipboard. */
export function copyRange(surface: ClipboardSurface, range: CellRange) {
  const matrix: string[][] = []
  for (let row = range.top; row <= range.bottom; row += 1) {
    const line: string[] = []
    for (let column = range.left; column <= range.right; column += 1) {
      const withinColumns = column >= 0 && column < surface.columns.length
      line.push(withinColumns && row < surface.rowCount ? surface.currentText(row, column) : '')
    }
    matrix.push(line)
  }
  return encodeTSV(matrix)
}

function applyMatrix(surface: ClipboardSurface, origin: { row: number; column: number }, matrix: readonly (readonly string[])[]): ClipboardOutcome {
  const outcome: ClipboardOutcome = { applied: 0, skipped: 0, staged: 0 }
  const stagedIds = new Map<number, string>()
  for (let offsetRow = 0; offsetRow < matrix.length; offsetRow += 1) {
    const targetRow = origin.row + offsetRow
    const line = matrix[offsetRow]
    const existingId = surface.rowIdAt(targetRow) ?? stagedIds.get(targetRow)
    let rowId = existingId
    if (!rowId) {
      if (!surface.stageRow) {
        outcome.skipped += line.length
        continue
      }
      rowId = surface.stageRow()
      stagedIds.set(targetRow, rowId)
      outcome.staged += 1
    }
    for (let offsetColumn = 0; offsetColumn < line.length; offsetColumn += 1) {
      const targetColumn = origin.column + offsetColumn
      const column = surface.columns[targetColumn]
      if (!column || !column.editable) {
        outcome.skipped += 1
        continue
      }
      const stored = existingId ? surface.storedText(targetRow, targetColumn) : ''
      surface.writeCell(rowId, column, stored, line[offsetColumn])
      outcome.applied += 1
    }
  }
  return outcome
}

/**
 * Pastes spreadsheet text starting at the origin cell. A single copied cell
 * fills the whole selection, matching Excel; otherwise the block is applied
 * positionally and rows past the end are staged as new records.
 */
export function pasteText(surface: ClipboardSurface, origin: { row: number; column: number }, range: CellRange, text: string): ClipboardOutcome {
  const matrix = decodeTSV(text)
  if (matrix.length === 0) return { applied: 0, skipped: 0, staged: 0 }
  const single = matrix.length === 1 && matrix[0].length === 1
  const spansRange = range.bottom > range.top || range.right > range.left
  if (single && spansRange) {
    const value = matrix[0][0]
    const filled = Array.from({ length: range.bottom - range.top + 1 }, () => Array.from({ length: range.right - range.left + 1 }, () => value))
    return applyMatrix(surface, { row: range.top, column: range.left }, filled)
  }
  return applyMatrix(surface, origin, matrix)
}

/** Copies the top row of a range down over the rest of the range. */
export function fillDown(surface: ClipboardSurface, range: CellRange): ClipboardOutcome {
  const outcome: ClipboardOutcome = { applied: 0, skipped: 0, staged: 0 }
  if (range.bottom <= range.top) return outcome
  for (let column = range.left; column <= range.right; column += 1) {
    const rules = surface.columns[column]
    if (!rules || !rules.editable) {
      outcome.skipped += range.bottom - range.top
      continue
    }
    const value = surface.currentText(range.top, column)
    for (let row = range.top + 1; row <= range.bottom; row += 1) {
      const rowId = surface.rowIdAt(row)
      if (!rowId) {
        outcome.skipped += 1
        continue
      }
      surface.writeCell(rowId, rules, surface.storedText(row, column), value)
      outcome.applied += 1
    }
  }
  return outcome
}
