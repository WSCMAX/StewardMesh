import { useCallback, useMemo, useState } from 'react'
import { parseCell, type ColumnRules } from './columns'

// Requirements: REQ-WORKSPACE-001, REQ-ATLAS-001, REQ-STACK-001, A11Y-001. Feature: experience.grid.

// Tracks in-progress cell edits before they are written. Edits are held per
// record and field so a screen can batch them into as few requests as the API
// allows, and invalid text stays visible on the cell rather than being silently
// discarded, which is what a spreadsheet user expects.
//
// Callers pass the stored text of a cell rather than the record itself, so
// staged rows that do not exist yet use the same edit, validation, and save
// machinery as rows loaded from the API.

export type CellEdit = {
  rowId: string
  columnKey: string
  /** Canonical text when valid, or the raw rejected text when `error` is set. */
  text: string
  previous: string
  error?: string
}

export type EditingCell = { rowId: string; columnKey: string; draft: string }

function editKey(rowId: string, columnKey: string) {
  return `${rowId}\u0000${columnKey}`
}

export function useCellEditing() {
  const [editing, setEditing] = useState<EditingCell | null>(null)
  const [editMap, setEditMap] = useState<ReadonlyMap<string, CellEdit>>(new Map())

  const editFor = useCallback((rowId: string, columnKey: string) => editMap.get(editKey(rowId, columnKey)), [editMap])

  const record = useCallback((rowId: string, column: ColumnRules, stored: string, input: string) => {
    const parsed = parseCell(column, input)
    setEditMap((current) => {
      const next = new Map(current)
      const key = editKey(rowId, column.key)
      // Returning a cell to its stored value clears the edit entirely, so the
      // pending count only ever reflects real differences.
      if (parsed.ok && parsed.text === stored) next.delete(key)
      else if (parsed.ok) next.set(key, { rowId, columnKey: column.key, text: parsed.text, previous: stored })
      else next.set(key, { rowId, columnKey: column.key, text: input, previous: stored, error: parsed.error })
      return next
    })
  }, [])

  const beginEdit = useCallback((rowId: string, column: ColumnRules, stored: string, initial?: string) => {
    if (!column.editable) return
    setEditing({ rowId, columnKey: column.key, draft: initial ?? editMap.get(editKey(rowId, column.key))?.text ?? stored })
  }, [editMap])

  const updateDraft = useCallback((draft: string) => {
    setEditing((current) => current ? { ...current, draft } : current)
  }, [])

  const commitDraft = useCallback((column: ColumnRules, stored: string) => {
    setEditing((current) => {
      if (current) record(current.rowId, column, stored, current.draft)
      return null
    })
  }, [record])

  const cancelDraft = useCallback(() => setEditing(null), [])

  const setCell = useCallback((rowId: string, column: ColumnRules, stored: string, input: string) => {
    if (!column.editable) return
    record(rowId, column, stored, input)
  }, [record])

  const effectiveText = useCallback((rowId: string, column: ColumnRules, stored: string) => {
    return editMap.get(editKey(rowId, column.key))?.text ?? stored
  }, [editMap])

  /** Replaces every pending edit at once. Undo and redo restore through this. */
  const restore = useCallback((map: ReadonlyMap<string, CellEdit>) => {
    setEditing(null)
    setEditMap(map)
  }, [])

  const discard = useCallback((rowIds?: readonly string[]) => {
    setEditing(null)
    if (!rowIds) {
      setEditMap(new Map())
      return
    }
    const drop = new Set(rowIds)
    setEditMap((current) => {
      const next = new Map(current)
      for (const [key, edit] of current) if (drop.has(edit.rowId)) next.delete(key)
      return next
    })
  }, [])

  const edits = useMemo(() => [...editMap.values()], [editMap])
  const invalidCount = useMemo(() => edits.filter((edit) => edit.error).length, [edits])

  return {
    editing, edits, editMap, hasEdits: edits.length > 0, invalidCount,
    beginEdit, updateDraft, commitDraft, cancelDraft, setCell, editFor, effectiveText, discard, restore,
  }
}

export type CellEditing = ReturnType<typeof useCellEditing>

/** Groups pending edits by record so a screen can build one payload per row. */
export function groupEditsByRow(edits: readonly CellEdit[]) {
  const grouped = new Map<string, CellEdit[]>()
  for (const edit of edits) {
    const existing = grouped.get(edit.rowId)
    if (existing) existing.push(edit)
    else grouped.set(edit.rowId, [edit])
  }
  return grouped
}
