import { act, renderHook } from '@testing-library/react'
import { expect, test, vi } from 'vitest'
import { ApiRequestError } from '../api'
import { applyCellPayload, decodeTSV, encodeTSV, parseCell, type ColumnRules } from './columns'
import { useCellEditing, groupEditsByRow, type CellEdit } from './useCellEditing'
import { copyRange, describeOutcome, fillDown, pasteText, type ClipboardSurface } from './useGridClipboard'
import { rangeBetween, rangeContains, useGridSelection } from './useGridSelection'
import { buildPayload, classifyWriteError, runWriteQueue, summarizeReport, tasksFromEdits, type WriteTask } from './writeQueue'

// Requirements: REQ-WORKSPACE-001, REQ-ATLAS-001, REQ-STACK-001, A11Y-001. Feature: experience.grid.

const columns: ColumnRules[] = [
  { key: 'name', header: 'Name', kind: 'text', editable: true, required: true, maxLength: 12 },
  { key: 'status', header: 'Status', kind: 'enum', options: ['active', 'retired'], editable: true },
  { key: 'seats', header: 'Seats', kind: 'number', minimum: 1, editable: true },
  { key: 'expiresOn', header: 'Expires on', kind: 'date', editable: true },
  { key: 'id', header: 'ID', kind: 'text' },
]

type Cell = Record<string, string>

/** An in-memory grid surface so the clipboard math can be exercised directly. */
function fakeSurface(rows: readonly Cell[], options: { stage?: boolean } = {}) {
  const stagedIds: string[] = []
  const written: { rowId: string; key: string; stored: string; value: string }[] = []
  const pending = new Map<string, string>()
  const surface: ClipboardSurface = {
    get rowCount() { return rows.length + stagedIds.length },
    columns,
    rowIdAt: (index) => index < rows.length ? rows[index].id : stagedIds[index - rows.length],
    storedText: (rowIndex, columnIndex) => rows[rowIndex]?.[columns[columnIndex]?.key ?? ''] ?? '',
    currentText: (rowIndex, columnIndex) => {
      const key = columns[columnIndex]?.key
      if (!key) return ''
      const id = surface.rowIdAt(rowIndex)
      return pending.get(`${id}:${key}`) ?? rows[rowIndex]?.[key] ?? ''
    },
    writeCell: (rowId, column, stored, value) => {
      written.push({ rowId, key: column.key, stored, value })
      pending.set(`${rowId}:${column.key}`, value)
    },
    stageRow: options.stage ? () => {
      const id = `staged-${stagedIds.length + 1}`
      stagedIds.push(id)
      return id
    } : undefined,
  }
  return { surface, written, stagedIds }
}

const sampleRows: Cell[] = [
  { id: 'row-1', name: 'Writer', status: 'active', seats: '3', expiresOn: '2026-09-01' },
  { id: 'row-2', name: 'Reader', status: 'retired', seats: '1', expiresOn: '' },
]

test('cell ranges cover the rectangle between the anchor and the active cell in any direction', () => {
  expect(rangeBetween({ row: 3, column: 4 }, { row: 1, column: 2 })).toEqual({ top: 1, left: 2, bottom: 3, right: 4 })
  expect(rangeContains({ top: 1, left: 2, bottom: 3, right: 4 }, 2, 3)).toBe(true)
  expect(rangeContains({ top: 1, left: 2, bottom: 3, right: 4 }, 0, 3)).toBe(false)
})

test('grid selection navigates, extends, and clamps inside the addressable area', () => {
  const { result } = renderHook(() => useGridSelection({ rowCount: 4, columnCount: 3 }))

  act(() => result.current.moveBy(2, 1, false))
  expect(result.current.active).toEqual({ row: 2, column: 1 })
  expect(result.current.range).toEqual({ top: 2, left: 1, bottom: 2, right: 1 })

  act(() => result.current.moveBy(1, 1, true))
  expect(result.current.range).toEqual({ top: 2, left: 1, bottom: 3, right: 2 })
  expect(result.current.isSelected(3, 2)).toBe(true)
  expect(result.current.isActive(3, 2)).toBe(true)

  act(() => result.current.moveBy(50, 50, false))
  expect(result.current.active).toEqual({ row: 3, column: 2 })

  act(() => result.current.selectAll())
  expect(result.current.range).toEqual({ top: 0, left: 0, bottom: 3, right: 2 })
})

test('grid selection pulls the active cell back inside a shrinking row count', () => {
  const { result, rerender } = renderHook((props: { rowCount: number }) => useGridSelection({ rowCount: props.rowCount, columnCount: 3 }), {
    initialProps: { rowCount: 6 },
  })
  act(() => result.current.moveBy(5, 0, false))
  expect(result.current.active.row).toBe(5)

  rerender({ rowCount: 2 })
  expect(result.current.active.row).toBe(1)
  expect(result.current.anchor.row).toBe(1)
})

test('row selection is tracked by record identity so it survives sorting', () => {
  const { result } = renderHook(() => useGridSelection({ rowCount: 3, columnCount: 2 }))
  act(() => result.current.toggleRowId('row-2'))
  expect(result.current.selectedRowIds.has('row-2')).toBe(true)
  act(() => result.current.toggleRowId('row-2'))
  expect(result.current.selectedRowIds.size).toBe(0)
  act(() => result.current.setAllRowIds(['row-1', 'row-3']))
  expect([...result.current.selectedRowIds]).toEqual(['row-1', 'row-3'])
  act(() => result.current.clearRowIds())
  expect(result.current.selectedRowIds.size).toBe(0)
})

test('tab-separated text round-trips through quoting so ranges paste cleanly into Excel', () => {
  const matrix = [['plain', 'has\ttab'], ['has "quote"', 'has\nnewline']]
  expect(decodeTSV(encodeTSV(matrix))).toEqual(matrix)
  expect(decodeTSV('a\tb\r\nc\td')).toEqual([['a', 'b'], ['c', 'd']])
  expect(decodeTSV('')).toEqual([])
  expect(decodeTSV('only')).toEqual([['only']])
})

test('copying a range emits the visible text of every cell inside it', () => {
  const { surface } = fakeSurface(sampleRows)
  expect(copyRange(surface, { top: 0, left: 0, bottom: 1, right: 2 })).toBe('Writer\tactive\t3\nReader\tretired\t1')
})

test('pasting a block maps positionally from the origin and skips read-only columns', () => {
  const { surface, written } = fakeSurface(sampleRows)
  const outcome = pasteText(surface, { row: 0, column: 2 }, { top: 0, left: 2, bottom: 0, right: 2 }, '9\t2026-12-31\tignored')
  expect(outcome).toEqual({ applied: 2, skipped: 1, staged: 0 })
  expect(written).toEqual([
    { rowId: 'row-1', key: 'seats', stored: '3', value: '9' },
    { rowId: 'row-1', key: 'expiresOn', stored: '2026-09-01', value: '2026-12-31' },
  ])
})

test('pasting one cell over a selected range fills the range, as in a spreadsheet', () => {
  const { surface, written } = fakeSurface(sampleRows)
  const outcome = pasteText(surface, { row: 0, column: 1 }, { top: 0, left: 1, bottom: 1, right: 1 }, 'retired')
  expect(outcome).toEqual({ applied: 2, skipped: 0, staged: 0 })
  expect(written.map((entry) => entry.rowId)).toEqual(['row-1', 'row-2'])
  expect(written.every((entry) => entry.value === 'retired')).toBe(true)
})

test('pasting past the last row stages new records when the screen supports creation', () => {
  const { surface, written, stagedIds } = fakeSurface(sampleRows, { stage: true })
  const outcome = pasteText(surface, { row: 1, column: 0 }, { top: 1, left: 0, bottom: 1, right: 0 }, 'Reader\nEditor\nAuthor')
  expect(outcome).toEqual({ applied: 3, skipped: 0, staged: 2 })
  expect(stagedIds).toEqual(['staged-1', 'staged-2'])
  expect(written.map((entry) => `${entry.rowId}=${entry.value}`)).toEqual(['row-2=Reader', 'staged-1=Editor', 'staged-2=Author'])
  expect(written[1].stored).toBe('')
})

test('pasting past the last row is skipped when the screen cannot create records', () => {
  const { surface, written } = fakeSurface(sampleRows)
  expect(pasteText(surface, { row: 1, column: 0 }, { top: 1, left: 0, bottom: 1, right: 0 }, 'Reader\nEditor')).toEqual({ applied: 1, skipped: 1, staged: 0 })
  expect(written).toHaveLength(1)
})

test('fill-down copies the top row of the range over the rows below it', () => {
  const { surface, written } = fakeSurface(sampleRows)
  expect(fillDown(surface, { top: 0, left: 1, bottom: 1, right: 2 })).toEqual({ applied: 2, skipped: 0, staged: 0 })
  expect(written).toEqual([
    { rowId: 'row-2', key: 'status', stored: 'retired', value: 'active' },
    { rowId: 'row-2', key: 'seats', stored: '1', value: '3' },
  ])
  expect(fillDown(surface, { top: 0, left: 0, bottom: 0, right: 2 })).toEqual({ applied: 0, skipped: 0, staged: 0 })
})

test('clipboard outcomes are described in the terms the operator sees', () => {
  expect(describeOutcome({ applied: 1, skipped: 0, staged: 0 })).toBe('1 cell pasted.')
  expect(describeOutcome({ applied: 4, skipped: 2, staged: 1 })).toBe('4 cells pasted, 1 new row staged, 2 skipped as read-only.')
})

test('cell parsing canonicalizes spreadsheet text and rejects values the API would refuse', () => {
  expect(parseCell(columns[1], 'Retired')).toEqual({ ok: true, text: 'retired' })
  expect(parseCell(columns[1], 'archived')).toEqual({ ok: false, error: 'Use one of active, retired.' })
  expect(parseCell(columns[2], '1,200')).toEqual({ ok: true, text: '1200' })
  expect(parseCell(columns[2], '0')).toEqual({ ok: false, error: 'Use 1 or more.' })
  expect(parseCell(columns[3], '9/12/2026')).toEqual({ ok: true, text: '2026-09-12' })
  expect(parseCell(columns[3], '2026-02-30')).toEqual({ ok: false, error: 'Not a real calendar date.' })
  expect(parseCell(columns[0], '')).toEqual({ ok: false, error: 'Name is required.' })
  expect(parseCell(columns[0], 'a'.repeat(13))).toEqual({ ok: false, error: 'Use 12 characters or fewer.' })
})

test('cell payloads convert canonical text back into the shapes the API expects', () => {
  const draft: Record<string, unknown> = {}
  applyCellPayload(columns[2], draft, '4')
  applyCellPayload(columns[3], draft, '2026-09-12')
  applyCellPayload({ key: 'cost', header: 'Cost', kind: 'money' }, draft, '1250.50')
  applyCellPayload({ key: 'note', header: 'Note', kind: 'text', toPayload: (target, text) => { target.lifecycleNote = text } }, draft, 'Retired')
  expect(draft).toEqual({ seats: 4, expiresOn: '2026-09-12T00:00:00Z', cost: 125050, lifecycleNote: 'Retired' })
})

test('committing a cell edit records canonical text and clears when it returns to the stored value', () => {
  const { result } = renderHook(() => useCellEditing())

  act(() => result.current.beginEdit('row-1', columns[1], 'active'))
  expect(result.current.editing).toEqual({ rowId: 'row-1', columnKey: 'status', draft: 'active' })
  act(() => result.current.updateDraft('Retired'))
  act(() => result.current.commitDraft(columns[1], 'active'))
  expect(result.current.editing).toBeNull()
  expect(result.current.edits).toEqual([{ rowId: 'row-1', columnKey: 'status', text: 'retired', previous: 'active' }])

  act(() => result.current.setCell('row-1', columns[1], 'active', 'active'))
  expect(result.current.edits).toEqual([])
  expect(result.current.hasEdits).toBe(false)
})

test('an invalid cell keeps the rejected text visible and blocks saving', () => {
  const { result } = renderHook(() => useCellEditing())
  act(() => result.current.setCell('row-1', columns[2], '3', 'many'))
  expect(result.current.editFor('row-1', 'seats')).toEqual({ rowId: 'row-1', columnKey: 'seats', text: 'many', previous: '3', error: 'Use a whole number.' })
  expect(result.current.invalidCount).toBe(1)
  expect(result.current.effectiveText('row-1', columns[2], '3')).toBe('many')

  act(() => result.current.setCell('row-1', columns[2], '3', '4'))
  expect(result.current.invalidCount).toBe(0)
})

test('escaping an edit reverts the draft and discarding clears the chosen rows', () => {
  const { result } = renderHook(() => useCellEditing())
  act(() => result.current.beginEdit('row-1', columns[1], 'active'))
  act(() => result.current.updateDraft('retired'))
  act(() => result.current.cancelDraft())
  expect(result.current.editing).toBeNull()
  expect(result.current.edits).toEqual([])

  act(() => result.current.setCell('row-1', columns[1], 'active', 'retired'))
  act(() => result.current.setCell('row-2', columns[1], 'active', 'retired'))
  act(() => result.current.discard(['row-1']))
  expect(result.current.edits.map((edit) => edit.rowId)).toEqual(['row-2'])
  act(() => result.current.discard())
  expect(result.current.edits).toEqual([])
})

test('read-only columns never accept an edit', () => {
  const { result } = renderHook(() => useCellEditing())
  act(() => result.current.setCell('row-1', columns[4], 'row-1', 'row-9'))
  act(() => result.current.beginEdit('row-1', columns[4], 'row-1'))
  expect(result.current.edits).toEqual([])
  expect(result.current.editing).toBeNull()
})

test('pending edits group into one write task per record', () => {
  const edits: CellEdit[] = [
    { rowId: 'row-1', columnKey: 'status', text: 'retired', previous: 'active' },
    { rowId: 'row-2', columnKey: 'seats', text: '4', previous: '1' },
    { rowId: 'row-1', columnKey: 'seats', text: '9', previous: '3' },
  ]
  expect([...groupEditsByRow(edits).keys()]).toEqual(['row-1', 'row-2'])
  expect(tasksFromEdits(edits).map((task) => ({ rowId: task.rowId, count: task.edits.length }))).toEqual([
    { rowId: 'row-1', count: 2 },
    { rowId: 'row-2', count: 1 },
  ])
})

test('a record payload starts from the current record and applies only the changed fields', () => {
  const payload = buildPayload(
    [{ rowId: 'row-1', columnKey: 'seats', text: '9', previous: '3' }],
    columns,
    { name: 'Writer', status: 'active', seats: 3, revision: 4 },
  )
  expect(payload).toEqual({ name: 'Writer', status: 'active', seats: 9, revision: 4 })
})

test('the write queue reports per-record outcomes and distinguishes conflicts from ownership locks', async () => {
  const attempted: string[] = []
  const progress: string[] = []
  const tasks: WriteTask[] = ['row-1', 'row-2', 'row-3', 'row-4'].map((rowId) => ({ rowId, edits: [{ rowId, columnKey: 'status', text: 'retired', previous: 'active' }] }))
  const report = await runWriteQueue(tasks, {
    concurrency: 2,
    writeRecord: async (task) => {
      attempted.push(task.rowId)
      if (task.rowId === 'row-2') throw new ApiRequestError(409, 'Another change landed first.')
      if (task.rowId === 'row-3') throw new ApiRequestError(423, 'Write locked.')
      if (task.rowId === 'row-4') throw new ApiRequestError(400, 'Seats must be at least one.')
    },
  }, (rowId, state) => progress.push(`${rowId}:${state}`))

  expect(attempted.sort()).toEqual(['row-1', 'row-2', 'row-3', 'row-4'])
  expect(report).toMatchObject({ saved: 1, failed: 3, conflicts: 1, locked: 1 })
  expect(report.outcomes.find((outcome) => outcome.rowId === 'row-2')).toMatchObject({ code: 'conflict' })
  expect(report.outcomes.find((outcome) => outcome.rowId === 'row-4')).toMatchObject({ code: 'validation', message: 'Seats must be at least one.' })
  expect(progress).toContain('row-1:saving')
  expect(progress).toContain('row-1:saved')
  expect(progress).toContain('row-3:failed')
  expect(summarizeReport(report)).toBe('1 of 4 records saved. 1 conflicted with a newer change, 1 write locked pending ownership, 1 rejected. Reload to see current values.')
})

test('the write queue holds concurrency to the transport limit', async () => {
  let inFlight = 0
  let peak = 0
  const tasks: WriteTask[] = ['a', 'b', 'c', 'd', 'e'].map((rowId) => ({ rowId, edits: [] }))
  const report = await runWriteQueue(tasks, {
    concurrency: 2,
    writeRecord: async () => {
      inFlight += 1
      peak = Math.max(peak, inFlight)
      await new Promise<void>((resolve) => setTimeout(resolve, 1))
      inFlight -= 1
    },
  })
  expect(peak).toBe(2)
  expect(report.saved).toBe(5)
})

test('a batch transport is preferred over fanning out one request per record', async () => {
  const writeRecord = vi.fn()
  const report = await runWriteQueue([{ rowId: 'row-1', edits: [] }, { rowId: 'row-2', edits: [] }], {
    writeRecord,
    writeBatch: async (tasks) => tasks.map((task) => task.rowId === 'row-2'
      ? { rowId: task.rowId, state: 'failed', code: 'conflict', message: 'Another change landed first.' }
      : { rowId: task.rowId, state: 'saved' }),
  })
  expect(writeRecord).not.toHaveBeenCalled()
  expect(report).toMatchObject({ saved: 1, failed: 1, conflicts: 1 })
})

test('a batch transport that fails outright marks every record in the batch failed', async () => {
  const report = await runWriteQueue([{ rowId: 'row-1', edits: [] }, { rowId: 'row-2', edits: [] }], {
    writeRecord: async () => {},
    writeBatch: async () => { throw new ApiRequestError(409, 'Another change landed first.') },
  })
  expect(report).toMatchObject({ saved: 0, failed: 2, conflicts: 2 })
})

test('an empty queue does no work and a non-API failure still reports a usable message', async () => {
  expect(await runWriteQueue([], { writeRecord: async () => {} })).toEqual({ outcomes: [], saved: 0, failed: 0, conflicts: 0, locked: 0 })
  expect(classifyWriteError(new Error('socket closed'))).toEqual({ code: 'error', message: 'The change could not be saved.' })
  expect(classifyWriteError(new ApiRequestError(422, 'Referenced model is missing.'))).toEqual({ code: 'reference_missing', message: 'Referenced model is missing.' })
  expect(summarizeReport({ outcomes: [], saved: 1, failed: 0, conflicts: 0, locked: 0 })).toBe('1 of 1 record saved.')
})

test('lookup text round-trips ids with a single primary flag', async () => {
  const { encodeLookupText, filterLookupOptions, lookupExportText, parseLookupText, parseCell } = await import('./columns')
  expect(parseLookupText('user-1*|user-2')).toEqual([{ id: 'user-1', primary: true }, { id: 'user-2', primary: false }])
  expect(encodeLookupText([{ id: 'user-1', primary: true }, { id: 'user-2', primary: false }])).toBe('user-1*|user-2')
  expect(lookupExportText('user-1*|user-2', [{ id: 'user-1', label: 'Ada' }, { id: 'user-2', label: 'Grace' }], true)).toBe('Ada (primary), Grace')
  expect(filterLookupOptions([{ id: 'd-1', label: 'Technology' }, { id: 'd-2', label: 'Studio Arts' }], 'studio')).toEqual([{ id: 'd-2', label: 'Studio Arts' }])
  const column: ColumnRules = {
    key: 'users', header: 'Users', kind: 'lookup', lookup: {
      multiple: true, allowPrimary: true, search: async () => [],
      options: [{ id: 'user-1', label: 'Ada' }, { id: 'user-2', label: 'Grace' }],
    },
  }
  expect(parseCell(column, 'Ada')).toEqual({ ok: true, text: 'user-1*' })
})

test('stored views ignore unknown fields and keep highlights on record ids', async () => {
  const { parseView, viewStorageKey } = await import('./viewState')
  const view = parseView({
    hiddenColumns: ['id'], sort: { key: 'name', direction: 'descending' },
    highlights: { 'row-1': 'amber', 'row-2': 'nope' }, density: 'tall', extra: true,
    savedQueries: [{ id: 'sq-1', name: ' Active ', query: 'status=active' }, { id: '../etc', name: 'bad', query: 'x' }],
    recordColumn: 'collapsed',
  })
  expect(view.hiddenColumns).toEqual(['id'])
  expect(view.sort).toEqual({ key: 'name', direction: 'descending' })
  expect(view.highlights).toEqual({ 'row-1': 'amber' })
  expect(view.density).toBe('tall')
  expect(view.savedQueries).toEqual([{ id: 'sq-1', name: 'Active', query: 'status=active' }])
  expect(view.recordColumn).toBe('collapsed')
  expect(viewStorageKey('atlas-assets', { subject: 'account-1', organizationId: 'example-org' }))
    .toBe('stewardmesh.grid.v1.example-org.account-1.atlas-assets')
})

test('CSV export includes the header row and the visible cells', async () => {
  const { csvFromSheet, jsonFromSheet, formulaSafeText } = await import('./export')
  const sheet = { name: 'Licenses', columns: [{ key: 'name', header: 'Name', kind: 'text' as const, width: 12 }], rows: [['Writer'], ['Reader']] }
  expect(csvFromSheet(sheet)).toContain('Name')
  expect(csvFromSheet(sheet)).toContain('Writer')
  expect(jsonFromSheet(sheet)).toContain('"name": "Writer"')
  expect(formulaSafeText('=HYPERLINK("https://example.test")')).toBe(`'=HYPERLINK("https://example.test")`)
  expect(csvFromSheet({ ...sheet, rows: [['=HYPERLINK("https://example.test")']] })).toContain(`'=HYPERLINK`)
  expect(csvFromSheet({ ...sheet, rows: [['Doe, Jane']] })).toBe('Name\n"Doe, Jane"')
  expect(jsonFromSheet({ ...sheet, rows: [['=HYPERLINK("https://example.test")']] })).toContain('=HYPERLINK')
})
