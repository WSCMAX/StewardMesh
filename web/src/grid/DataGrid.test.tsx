import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import axe from 'axe-core'
import { beforeEach, expect, test, vi } from 'vitest'
import DataGrid, { filterRows, sortRows, type DataGridProps } from './DataGrid'
import type { GridColumn } from './columns'

// Requirements: REQ-WORKSPACE-001, REQ-ATLAS-001, REQ-STACK-001, A11Y-001. Feature: experience.grid.

type Row = { id: string; name: string; status: string; seats: number }

const rows: Row[] = [
  { id: 'row-1', name: 'Writer', status: 'active', seats: 3 },
  { id: 'row-2', name: 'Reader', status: 'retired', seats: 1 },
]

const columns: GridColumn<Row>[] = [
  { key: 'name', header: 'Name', kind: 'text', editable: true, required: true, text: (row) => row.name },
  { key: 'status', header: 'Status', kind: 'enum', options: ['active', 'retired'], editable: true, text: (row) => row.status },
  { key: 'seats', header: 'Seats', kind: 'number', minimum: 1, editable: true, align: 'right', text: (row) => String(row.seats) },
  { key: 'id', header: 'ID', kind: 'text', text: (row) => row.id },
]

function renderGrid(overrides: Partial<DataGridProps<Row>> = {}) {
  const result = render(<DataGrid columns={columns} editable label="Licenses" rowId={(row) => row.id} rowLabel={(row) => row.name} rows={rows} {...overrides} />)
  return { ...result, table: screen.getByRole('grid') }
}

function activeCell() {
  return screen.getAllByRole('gridcell').find((cell) => cell.getAttribute('tabindex') === '0')
}

function clipboard(text: string) {
  return { clipboardData: { getData: () => text, setData: vi.fn(), types: ['text/plain'] } }
}

beforeEach(() => { vi.unstubAllGlobals() })

test('the grid exposes its shape, a single tab stop, and no accessibility violations', async () => {
  const { container, table } = renderGrid()
  expect(table).toHaveAttribute('aria-rowcount', '3')
  expect(table).toHaveAttribute('aria-colcount', '4')
  expect(screen.getAllByRole('gridcell').filter((cell) => cell.getAttribute('tabindex') === '0')).toHaveLength(1)
  expect(within(table).getByRole('columnheader', { name: /Name/ })).toHaveAttribute('aria-sort', 'none')
  expect(screen.getByRole('gridcell', { name: 'row-1' })).toHaveAttribute('aria-readonly', 'true')
  expect(screen.getByRole('gridcell', { name: 'Writer' })).not.toHaveAttribute('aria-readonly')
  expect((await axe.run(container)).violations).toEqual([])
})

test('arrow keys move the tab stop and shift extends a rectangular selection', () => {
  const { table } = renderGrid()
  fireEvent.keyDown(table, { key: 'ArrowRight' })
  expect(activeCell()).toHaveTextContent('active')

  fireEvent.keyDown(table, { key: 'ArrowDown', shiftKey: true })
  const selected = screen.getAllByRole('gridcell').filter((cell) => cell.getAttribute('aria-selected') === 'true')
  expect(selected.map((cell) => cell.textContent)).toEqual(['active', 'retired'])

  fireEvent.keyDown(table, { key: 'End', ctrlKey: true })
  expect(activeCell()).toHaveTextContent('row-2')
})

test('typing replaces a cell, Escape reverts it, and Enter commits the canonical value', () => {
  const { table } = renderGrid()
  fireEvent.keyDown(table, { key: 'a' })
  const first = screen.getByLabelText('Name for Writer')
  expect(first).toHaveValue('a')
  fireEvent.keyDown(first, { key: 'Escape' })
  expect(screen.queryByLabelText('Name for Writer')).not.toBeInTheDocument()
  expect(screen.queryByText(/cell changed/)).not.toBeInTheDocument()

  fireEvent.keyDown(table, { key: 'Enter' })
  const editing = screen.getByLabelText('Name for Writer')
  expect(editing).toHaveValue('Writer')
  fireEvent.change(editing, { target: { value: 'Composer' } })
  fireEvent.keyDown(editing, { key: 'Enter' })
  expect(screen.getByText('1 cell changed in 1 record')).toBeInTheDocument()
  expect(screen.getByRole('gridcell', { name: 'Composer' })).toBeInTheDocument()
})

test('a cell the column rejects stays visible, is marked invalid, and blocks saving', () => {
  const { table } = renderGrid({ onSaveEdits: vi.fn() })
  fireEvent.keyDown(table, { key: 'ArrowRight' })
  fireEvent.keyDown(table, { key: 'ArrowRight' })
  fireEvent.keyDown(table, { key: 'Enter' })
  const editing = screen.getByLabelText('Seats for Writer')
  fireEvent.change(editing, { target: { value: '0' } })
  fireEvent.keyDown(editing, { key: 'Enter' })

  const cell = screen.getByRole('gridcell', { name: /^0/ })
  expect(cell).toHaveAttribute('aria-invalid', 'true')
  expect(cell).toHaveTextContent('Invalid: Use 1 or more.')
  expect(screen.getByRole('alert')).toHaveTextContent('1 invalid, fix before saving')
  expect(screen.getByRole('button', { name: 'Save changes' })).toBeDisabled()
})

test('fill-down copies the top of a range over the rows beneath it', () => {
  const { table } = renderGrid()
  fireEvent.keyDown(table, { key: 'ArrowDown', shiftKey: true })
  fireEvent.keyDown(table, { key: 'd', ctrlKey: true })
  expect(screen.getByText('1 cell changed in 1 record')).toBeInTheDocument()
  expect(screen.getAllByRole('gridcell', { name: 'Writer' })).toHaveLength(2)
  expect(screen.queryByRole('gridcell', { name: 'Reader' })).not.toBeInTheDocument()
})

test('copying a range yields tab-separated text a spreadsheet can consume', () => {
  const { table } = renderGrid()
  fireEvent.keyDown(table, { key: 'ArrowDown', shiftKey: true })
  fireEvent.keyDown(table, { key: 'ArrowRight', shiftKey: true })
  const event = clipboard('')
  fireEvent.copy(table, event)
  expect(event.clipboardData.setData).toHaveBeenCalledWith('text/plain', 'Writer\tactive\nReader\tretired')
})

test('pasting a block from a spreadsheet maps positionally and skips read-only columns', () => {
  const { table } = renderGrid()
  fireEvent.paste(table, clipboard('Composer\tretired\t8\tignored'))
  expect(screen.getByRole('gridcell', { name: 'Composer' })).toBeInTheDocument()
  expect(screen.getByText('3 cells changed in 1 record')).toBeInTheDocument()
  expect(screen.getByRole('gridcell', { name: 'row-1' })).toHaveTextContent('row-1')
})

test('a paste that starts on the checkbox column lands in the first field, not one column left', () => {
  const { table } = renderGrid({ selectable: true })
  expect(activeCell()).toHaveTextContent('')
  fireEvent.paste(table, clipboard('Composer\tretired'))
  expect(screen.getByRole('gridcell', { name: 'Composer' })).toBeInTheDocument()
  expect(screen.getByText('2 cells changed in 1 record')).toBeInTheDocument()
})

test('copying from a grid with a checkbox column reads the fields, not one column left', () => {
  const { table } = renderGrid({ selectable: true })
  fireEvent.keyDown(table, { key: 'ArrowRight', shiftKey: true })
  const event = clipboard('')
  fireEvent.copy(table, event)
  expect(event.clipboardData.setData).toHaveBeenCalledWith('text/plain', 'Writer')
})

test('Ctrl+C writes the selected range to the system clipboard', () => {
  const writeText = vi.fn().mockResolvedValue(undefined)
  vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
  const { table } = renderGrid()
  fireEvent.keyDown(table, { key: 'ArrowRight', shiftKey: true })
  fireEvent.keyDown(table, { key: 'c', ctrlKey: true })
  expect(writeText).toHaveBeenCalledWith('Writer\tactive')

  fireEvent.keyDown(table, { key: 'c', metaKey: true })
  expect(writeText).toHaveBeenCalledTimes(2)
})

test('Ctrl+X copies the range and then blanks every editable cell in it', () => {
  const writeText = vi.fn().mockResolvedValue(undefined)
  vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
  const { table } = renderGrid()
  fireEvent.keyDown(table, { key: 'ArrowDown', shiftKey: true })
  fireEvent.keyDown(table, { key: 'x', ctrlKey: true })
  expect(writeText).toHaveBeenCalledWith('Writer\nReader')
  expect(screen.getByText('2 cells changed in 2 records')).toBeInTheDocument()
  expect(screen.getByRole('alert')).toHaveTextContent('2 invalid, fix before saving')
})

test('Delete blanks the whole selection rather than only the active cell', () => {
  const { table } = renderGrid()
  fireEvent.keyDown(table, { key: 'ArrowRight' })
  fireEvent.keyDown(table, { key: 'ArrowDown', shiftKey: true })
  fireEvent.keyDown(table, { key: 'Delete' })
  expect(screen.getByText('2 cells changed in 2 records')).toBeInTheDocument()
})

test('Ctrl+Z undoes a whole pasted block as one step and Ctrl+Shift+Z puts it back', () => {
  const { table } = renderGrid()
  fireEvent.paste(table, clipboard('Composer\tretired\t8'))
  expect(screen.getByText('3 cells changed in 1 record')).toBeInTheDocument()

  fireEvent.keyDown(table, { key: 'z', ctrlKey: true })
  expect(screen.queryByText(/cells changed/)).not.toBeInTheDocument()
  expect(screen.getByRole('gridcell', { name: 'Writer' })).toBeInTheDocument()

  fireEvent.keyDown(table, { key: 'z', ctrlKey: true, shiftKey: true })
  expect(screen.getByText('3 cells changed in 1 record')).toBeInTheDocument()
  expect(screen.getByRole('gridcell', { name: 'Composer' })).toBeInTheDocument()
})

test('undo steps back through separate cell edits one at a time', () => {
  const { table } = renderGrid()
  // Committing with Enter drops to the next row, so this edits both records.
  for (const value of ['Composer', 'Editor']) {
    fireEvent.keyDown(table, { key: 'Enter' })
    const editing = screen.getByLabelText(/^Name for/)
    fireEvent.change(editing, { target: { value } })
    fireEvent.keyDown(editing, { key: 'Enter' })
  }
  expect(screen.getByText('2 cells changed in 2 records')).toBeInTheDocument()

  fireEvent.keyDown(table, { key: 'z', ctrlKey: true })
  expect(screen.getByText('1 cell changed in 1 record')).toBeInTheDocument()
  fireEvent.keyDown(table, { key: 'y', ctrlKey: true })
  expect(screen.getByText('2 cells changed in 2 records')).toBeInTheDocument()
})

test('committing a cell without changing it does not add an undo step', () => {
  const { table } = renderGrid()
  fireEvent.keyDown(table, { key: 'Enter' })
  fireEvent.keyDown(screen.getByLabelText('Name for Writer'), { key: 'Enter' })
  expect(screen.getByRole('button', { name: 'Undo' })).toBeDisabled()
})

test('the toolbar exposes undo and redo for operators who do not know the shortcuts', () => {
  const { table } = renderGrid()
  expect(screen.getByRole('button', { name: 'Undo' })).toBeDisabled()
  expect(screen.getByRole('button', { name: 'Redo' })).toBeDisabled()

  fireEvent.paste(table, clipboard('Composer'))
  fireEvent.click(screen.getByRole('button', { name: 'Undo' }))
  expect(screen.getByRole('gridcell', { name: 'Writer' })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Redo' }))
  expect(screen.getByRole('gridcell', { name: 'Composer' })).toBeInTheDocument()
})

test('the plus control inserts a new row directly below the row it belongs to', () => {
  renderGrid({ onCreateRows: vi.fn() })
  fireEvent.click(screen.getByRole('button', { name: 'Insert a new row below Writer' }))

  const names = screen.getAllByRole('row').slice(1).map((row) => within(row).getAllByRole('gridcell')[0].textContent)
  expect(names).toEqual(['Writer', '', 'Reader'])
  expect(screen.getByText('1 new row')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Remove new row 1' })).toBeInTheDocument()
})

test('an inserted row keeps its place when saved and can be undone away', async () => {
  const onCreateRows = vi.fn()
  const { table } = renderGrid({ onCreateRows })
  fireEvent.click(screen.getByRole('button', { name: 'Insert a new row below Writer' }))
  fireEvent.keyDown(table, { key: 'ArrowDown' })
  fireEvent.keyDown(table, { key: 'Enter' })
  const editing = screen.getByLabelText('Name for new row 1')
  fireEvent.change(editing, { target: { value: 'Composer' } })
  fireEvent.keyDown(editing, { key: 'Enter' })

  fireEvent.keyDown(table, { key: 'z', ctrlKey: true })
  expect(screen.getByRole('button', { name: 'Remove new row 1' })).toBeInTheDocument()
  fireEvent.keyDown(table, { key: 'z', ctrlKey: true })
  expect(screen.queryByRole('button', { name: 'Remove new row 1' })).not.toBeInTheDocument()

  fireEvent.keyDown(table, { key: 'y', ctrlKey: true })
  fireEvent.keyDown(table, { key: 'y', ctrlKey: true })
  fireEvent.click(screen.getByRole('button', { name: 'Save changes' }))
  await waitFor(() => expect(onCreateRows).toHaveBeenCalledWith([{ id: 'staged-1', values: { name: 'Composer' } }]))
  expect(screen.getByRole('button', { name: 'Undo' })).toBeDisabled()
})

test('pasting past the last row stages new records and Save hands them to the screen', async () => {
  const onCreateRows = vi.fn()
  const onSaveEdits = vi.fn()
  const { table } = renderGrid({ onCreateRows, onSaveEdits })
  fireEvent.keyDown(table, { key: 'ArrowDown' })
  fireEvent.paste(table, clipboard('Reader\nAuthor'))

  expect(screen.getByText('1 new row')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Remove new row 1' })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Save changes' }))
  await waitFor(() => expect(onCreateRows).toHaveBeenCalledWith([{ id: 'staged-1', values: { name: 'Author' } }]))
  expect(onSaveEdits).not.toHaveBeenCalled()
  expect(screen.queryByRole('button', { name: 'Save changes' })).not.toBeInTheDocument()
})

test('a rejected save keeps the pending edits so the operator can retry', async () => {
  const onSaveEdits = vi.fn().mockRejectedValue(new Error('conflict'))
  const { table } = renderGrid({ onSaveEdits })
  fireEvent.keyDown(table, { key: 'Enter' })
  const editing = screen.getByLabelText('Name for Writer')
  fireEvent.change(editing, { target: { value: 'Composer' } })
  fireEvent.keyDown(editing, { key: 'Enter' })
  fireEvent.click(screen.getByRole('button', { name: 'Save changes' }))

  await waitFor(() => expect(onSaveEdits).toHaveBeenCalledWith([{ rowId: 'row-1', columnKey: 'name', text: 'Composer', previous: 'Writer' }]))
  expect(await screen.findByRole('button', { name: 'Save changes' })).toBeEnabled()
  expect(screen.getByText('1 cell changed in 1 record')).toBeInTheDocument()
})

test('row checkboxes drive bulk actions and the header checkbox covers the visible rows', () => {
  renderGrid({ selectable: true, bulkActions: (selected) => <button type="button">Print {selected.length}</button> })
  fireEvent.click(screen.getByLabelText('Select Reader'))
  expect(screen.getByRole('button', { name: 'Print 1' })).toBeInTheDocument()
  fireEvent.click(screen.getByLabelText('Select all visible licenses'))
  expect(screen.getByRole('button', { name: 'Print 2' })).toBeInTheDocument()
  fireEvent.click(screen.getByLabelText('Select all visible licenses'))
  expect(screen.queryByRole('button', { name: /^Print/ })).not.toBeInTheDocument()
})

test('controlled selection reports ids and renders the parent selection', () => {
  const onSelectedRowIdsChange = vi.fn()
  const { rerender } = render(
    <DataGrid columns={columns} label="Licenses" onSelectedRowIdsChange={onSelectedRowIdsChange} rowId={(row) => row.id} rowLabel={(row) => row.name} rows={rows} selectable selectedRowIds={['row-2']} />,
  )
  expect(screen.getByLabelText('Select Reader')).toBeChecked()
  expect(screen.getByLabelText('Select Writer')).not.toBeChecked()
  fireEvent.click(screen.getByLabelText('Select Writer'))
  expect(onSelectedRowIdsChange).toHaveBeenCalledWith(['row-2', 'row-1'])
  rerender(
    <DataGrid columns={columns} label="Licenses" onSelectedRowIdsChange={onSelectedRowIdsChange} rowId={(row) => row.id} rowLabel={(row) => row.name} rows={rows} selectable selectedRowIds={['row-1', 'row-2']} />,
  )
  expect(screen.getByLabelText('Select Writer')).toBeChecked()
})

test('a controlled header checkbox toggles only the rows in this grid', () => {
  const onSelectedRowIdsChange = vi.fn()
  const props = {
    columns, label: 'Licenses', onSelectedRowIdsChange, rowId: (row: (typeof rows)[number]) => row.id,
    rowLabel: (row: (typeof rows)[number]) => row.name, rows, selectable: true,
  }
  const { rerender } = render(<DataGrid {...props} selectedRowIds={['outside-row', 'row-2']} />)
  fireEvent.click(screen.getByLabelText('Select all visible licenses'))
  expect(onSelectedRowIdsChange).toHaveBeenCalledWith(['outside-row', 'row-2', 'row-1'])
  rerender(<DataGrid {...props} selectedRowIds={['outside-row', 'row-2', 'row-1']} />)
  onSelectedRowIdsChange.mockClear()
  fireEvent.click(screen.getByLabelText('Select all visible licenses'))
  expect(onSelectedRowIdsChange).toHaveBeenCalledWith(['outside-row'])
})

test('search narrows the rows and discarding restores every pending cell', () => {
  const { table } = renderGrid()
  fireEvent.keyDown(table, { key: 'Enter' })
  const editing = screen.getByLabelText('Name for Writer')
  fireEvent.change(editing, { target: { value: 'Composer' } })
  fireEvent.keyDown(editing, { key: 'Enter' })
  fireEvent.click(screen.getByRole('button', { name: 'Discard' }))
  expect(screen.getByRole('gridcell', { name: 'Writer' })).toBeInTheDocument()

  fireEvent.change(screen.getByLabelText('Search Licenses'), { target: { value: 'reader' } })
  expect(screen.getByText('1 of 2 rows')).toBeInTheDocument()
  expect(screen.queryByRole('gridcell', { name: 'Writer' })).not.toBeInTheDocument()
})

test('sorting places blanks last in both directions and filters compose with search', () => {
  const sparse: Row[] = [{ id: 'a', name: '', status: 'active', seats: 1 }, ...rows]
  expect(sortRows(sparse, columns, { key: 'name', direction: 'ascending' }).map((row) => row.id)).toEqual(['row-2', 'row-1', 'a'])
  expect(sortRows(sparse, columns, { key: 'name', direction: 'descending' }).map((row) => row.id)).toEqual(['row-1', 'row-2', 'a'])
  expect(sortRows(sparse, columns, { key: 'seats', direction: 'descending' }).map((row) => row.id)).toEqual(['row-1', 'a', 'row-2'])
  expect(sortRows(sparse, columns, null).map((row) => row.id)).toEqual(['a', 'row-1', 'row-2'])
  expect(filterRows(rows, columns, { status: 'retired' }, '').map((row) => row.id)).toEqual(['row-2'])
  expect(filterRows(rows, columns, { status: 'retired' }, 'writer')).toEqual([])
  expect(filterRows(rows, columns, {}, 'WRITER').map((row) => row.id)).toEqual(['row-1'])
})

test('condition queries support AND, OR, grouping, and encoded query text', () => {
  renderGrid()
  fireEvent.click(screen.getByRole('button', { name: 'Filter' }))
  const query = screen.getByLabelText('Query')
  fireEvent.change(query, { target: { value: 'status=active^ORnameLIKEReader' } })
  expect(screen.getByRole('gridcell', { name: 'Writer' })).toBeInTheDocument()
  expect(screen.getByRole('gridcell', { name: 'Reader' })).toBeInTheDocument()
  fireEvent.change(query, { target: { value: 'status=active^nameLIKEWriter' } })
  expect(screen.getByRole('gridcell', { name: 'Writer' })).toBeInTheDocument()
  expect(screen.queryByRole('gridcell', { name: 'Reader' })).not.toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('Group Licenses'), { target: { value: 'status' } })
  expect(screen.getByRole('button', { name: /Status: active/ })).toBeInTheDocument()
  expect(screen.getByLabelText('Licenses grouped counts')).toHaveTextContent('active')
})

test('an empty grid explains itself instead of rendering a bare header', () => {
  render(<DataGrid columns={columns} emptyMessage="No licenses yet." label="Licenses" rowId={(row) => row.id} rows={[]} />)
  expect(screen.getByText('No licenses yet.')).toBeInTheDocument()
  expect(screen.getByRole('grid')).toHaveAttribute('aria-rowcount', '1')
})

test('scrolling inside the row menu does not dismiss it', async () => {
  renderGrid({ onCreateRows: vi.fn() })
  fireEvent.contextMenu(screen.getByRole('gridcell', { name: 'Writer' }))
  const menu = await screen.findByRole('menu', { name: 'Actions for Licenses' })
  fireEvent.scroll(menu)
  expect(screen.getByRole('menu', { name: 'Actions for Licenses' })).toBeInTheDocument()
})

test('a lookup cell lists known records and can add a new one', async () => {
  const columns: GridColumn<Row>[] = [
    { key: 'name', header: 'Name', kind: 'text', editable: true, text: (row) => row.name },
    {
      key: 'departmentId', header: 'Department', kind: 'lookup', editable: true,
      lookup: {
        options: [{ id: 'dept-1', label: 'Technology' }],
        search: async () => [{ id: 'dept-1', label: 'Technology' }],
        create: {
          label: 'Add department',
          fields: [{ key: 'name', label: 'Department name', required: true }],
          submit: async (values) => ({ id: 'dept-new', label: values.name }),
        },
        browseHref: '#workspace-people',
        browseLabel: 'Open departments',
      },
      text: () => '',
    },
  ]
  render(<DataGrid columns={columns} editable label="Assets" rowId={(row) => row.id} rowLabel={(row) => row.name} rows={rows} />)
  const table = screen.getByRole('grid')
  fireEvent.keyDown(table, { key: 'ArrowRight' })
  fireEvent.keyDown(table, { key: 'Enter' })
  expect(await screen.findByRole('dialog', { name: 'Choose Department' })).toBeInTheDocument()
  expect(screen.getByRole('option', { name: /Technology/ })).toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'Open departments' })).toHaveAttribute('href', '#workspace-people')
  fireEvent.click(screen.getByRole('button', { name: '+ Add department' }))
  expect(screen.getByLabelText('Department name')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('option', { name: /Technology/ }))
  expect(screen.getByText('Technology')).toBeInTheDocument()
})

test('the row menu copies, inserts, filters, groups, and highlights', async () => {
  renderGrid({ onCreateRows: vi.fn() })
  fireEvent.contextMenu(screen.getByRole('gridcell', { name: 'Writer' }))
  const menu = await screen.findByRole('menu', { name: 'Actions for Licenses' })
  expect((await axe.run(menu)).violations).toEqual([])
  fireEvent.click(screen.getByRole('menuitem', { name: 'Filter Name to this value' }))
  expect(screen.getByLabelText('Filter Name')).toHaveValue('Writer')
  expect(screen.getByText('1 of 2 rows')).toBeInTheDocument()

  fireEvent.contextMenu(screen.getByRole('gridcell', { name: 'Writer' }))
  fireEvent.click(screen.getByRole('menuitem', { name: 'Group by Name' }))
  expect(screen.getByRole('button', { name: /Name: Writer/ })).toBeInTheDocument()

  fireEvent.contextMenu(screen.getByRole('gridcell', { name: 'Writer' }))
  fireEvent.click(screen.getByRole('menuitemradio', { name: 'Amber' }))
  expect(screen.getByRole('gridcell', { name: 'Writer' }).closest('tr')?.className).toContain('bg-amber')
})

test('export downloads the current view as CSV', () => {
  const createObjectURL = vi.fn(() => 'blob:grid')
  const revoke = vi.fn()
  vi.stubGlobal('URL', { ...URL, createObjectURL, revokeObjectURL: revoke })
  renderGrid()
  fireEvent.click(screen.getByLabelText('Export'))
  fireEvent.click(screen.getByRole('menuitem', { name: 'CSV' }))
  expect(createObjectURL).toHaveBeenCalled()
})

test('export closes when clicking elsewhere', () => {
  renderGrid()
  fireEvent.click(screen.getByLabelText('Export'))
  expect(screen.getByRole('menuitem', { name: 'CSV' })).toBeInTheDocument()
  fireEvent.pointerDown(document.body)
  expect(screen.queryByRole('menuitem', { name: 'CSV' })).not.toBeInTheDocument()
})

test('typing in a column filter updates the filter instead of the active cell', () => {
  const { table } = renderGrid()
  fireEvent.keyDown(table, { key: 'ArrowRight' })
  expect(activeCell()).toHaveTextContent('active')
  const filter = screen.getByLabelText('Filter Name')
  fireEvent.pointerDown(filter)
  fireEvent.focus(filter)
  fireEvent.keyDown(filter, { key: 'W' })
  fireEvent.change(filter, { target: { value: 'Writ' } })
  expect(filter).toHaveValue('Writ')
  expect(screen.queryByLabelText('Name for Writer')).not.toBeInTheDocument()
  expect(screen.getByText('1 of 2 rows')).toBeInTheDocument()
})

test('leaving a cell editor for a column filter keeps typing in the filter', () => {
  const { table } = renderGrid()
  fireEvent.keyDown(table, { key: 'Enter' })
  const editing = screen.getByLabelText('Name for Writer')
  const filter = screen.getByLabelText('Filter Status')
  fireEvent.pointerDown(filter)
  fireEvent.focus(filter)
  fireEvent.blur(editing)
  fireEvent.keyDown(filter, { key: 'r' })
  fireEvent.change(filter, { target: { value: 'retired' } })
  expect(filter).toHaveValue('retired')
  expect(screen.queryByLabelText('Name for Writer')).not.toBeInTheDocument()
  expect(screen.getByRole('gridcell', { name: 'Reader' })).toBeInTheDocument()
  expect(screen.queryByRole('gridcell', { name: 'Writer' })).not.toBeInTheDocument()
})

test('pasting into a column filter does not paste into the active cell', () => {
  renderGrid()
  const filter = screen.getByLabelText('Filter Name')
  fireEvent.focus(filter)
  fireEvent.paste(filter, clipboard('Writ'))
  fireEvent.change(filter, { target: { value: 'Writ' } })
  expect(filter).toHaveValue('Writ')
  expect(screen.queryByText(/cell changed/)).not.toBeInTheDocument()
  expect(screen.getByRole('gridcell', { name: 'Writer' })).toBeInTheDocument()
})

test('column filters and search match lookup labels, not only record ids', () => {
  const lookupColumns: GridColumn<Row>[] = [
    { key: 'name', header: 'Name', kind: 'text', text: (row) => row.name },
    {
      key: 'departmentId',
      header: 'Department',
      kind: 'lookup',
      lookup: { options: [{ id: 'dept-1', label: 'Studio Arts' }], search: async () => [] },
      text: (row) => row.id === 'row-1' ? 'dept-1' : '',
      exportText: (row) => row.id === 'row-1' ? 'Studio Arts' : '',
    },
  ]
  expect(filterRows(rows, lookupColumns, { departmentId: 'studio' }, '').map((row) => row.id)).toEqual(['row-1'])
  expect(filterRows(rows, lookupColumns, {}, 'studio arts').map((row) => row.id)).toEqual(['row-1'])
  expect(filterRows(rows, lookupColumns, {}, 'dept-1').map((row) => row.id)).toEqual(['row-1'])
})

test('the row actions button keeps its menu open', async () => {
  renderGrid({ onOpenRow: vi.fn() })
  fireEvent.click(screen.getByRole('button', { name: 'Row actions for Writer' }))
  expect(await screen.findByRole('menu', { name: 'Actions for Licenses' })).toBeInTheDocument()
})

test('the row menu can open a record or the full edit form', async () => {
  const onOpenRow = vi.fn()
  const onEditRow = vi.fn()
  renderGrid({ onOpenRow, onEditRow })
  fireEvent.click(screen.getByRole('button', { name: 'Row actions for Writer' }))
  fireEvent.click(await screen.findByRole('menuitem', { name: 'Edit in full form' }))
  expect(onEditRow).toHaveBeenCalledWith(rows[0])
  fireEvent.click(screen.getByRole('button', { name: 'Row actions for Reader' }))
  fireEvent.click(await screen.findByRole('menuitem', { name: 'Open record' }))
  expect(onOpenRow).toHaveBeenCalledWith(rows[1])
})

test('insert 1 row above places the staged row before the current record', async () => {
  renderGrid({ onCreateRows: vi.fn() })
  fireEvent.contextMenu(screen.getByRole('gridcell', { name: 'Reader' }))
  fireEvent.click(await screen.findByRole('menuitem', { name: 'Insert 1 row above' }))
  const names = screen.getAllByRole('row').slice(1).map((row) => within(row).getAllByRole('gridcell')[0].textContent)
  expect(names).toEqual(['Writer', '', 'Reader'])
})

test('reports the filtered listing and writes grouping through to the parent', async () => {
  const onListingChange = vi.fn()
  const onGroupByChange = vi.fn()
  renderGrid({ onListingChange, onGroupByChange })
  await waitFor(() => expect(onListingChange).toHaveBeenCalled())
  expect(onListingChange.mock.lastCall?.[0]).toEqual({
    rowIds: ['row-1', 'row-2'],
    total: 2,
    groupBy: null,
    groups: [],
  })
  fireEvent.change(screen.getByLabelText('Group Licenses'), { target: { value: 'status' } })
  expect(onGroupByChange).toHaveBeenCalledWith('status')
  await waitFor(() => expect(onListingChange.mock.lastCall?.[0].groupBy).toBe('status'))
  expect(onListingChange.mock.lastCall?.[0].groups.map((group: { value: string }) => group.value)).toEqual(['active', 'retired'])
})

test('the grid body keeps a stable scrollbar gutter so cell edges are not clipped', () => {
  renderGrid()
  expect(screen.getByRole('region', { name: 'Licenses' }).className).toContain('steward-grid-scroll')
})

test('full screen opens a dedicated editor and returns to the page grid', () => {
  renderGrid()
  fireEvent.click(screen.getByRole('button', { name: 'Open Licenses in full screen' }))
  const dialog = screen.getByRole('dialog', { name: 'Licenses full screen editor' })
  expect(dialog).toBeInTheDocument()
  expect(dialog.className).toContain('bg-steward-ink-950')
  expect(dialog.className).toContain('z-50')
  expect(dialog.className).not.toContain('/55')
  fireEvent.click(screen.getByRole('button', { name: 'Exit full screen' }))
  expect(screen.queryByRole('dialog', { name: 'Licenses full screen editor' })).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Open Licenses in full screen' })).toBeInTheDocument()
})

test('the record column can be collapsed, expanded, or hidden', () => {
  renderGrid({ onCreateRows: vi.fn(), onEditRow: vi.fn(), onOpenRow: vi.fn() })
  const header = screen.getByRole('columnheader', { name: 'Record' })
  expect(header.getAttribute('style')).toContain('11.75rem')
  expect(header.className).not.toContain('right-0')
  expect(screen.getByRole('button', { name: 'Open Writer' })).toBeInTheDocument()

  fireEvent.click(screen.getByRole('button', { name: 'Collapse record column' }))
  expect(screen.queryByRole('button', { name: 'Open Writer' })).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Row actions for Writer' })).toBeInTheDocument()
  expect(screen.getByRole('columnheader', { name: 'Record' }).getAttribute('style')).toContain('2.75rem')

  fireEvent.click(screen.getByRole('button', { name: 'Expand record column' }))
  expect(screen.getByRole('button', { name: 'Open Writer' })).toBeInTheDocument()

  fireEvent.click(screen.getByRole('button', { name: 'Hide record column' }))
  expect(screen.queryByRole('columnheader', { name: 'Record' })).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Show Record' }))
  expect(screen.getByRole('button', { name: 'Open Writer' })).toBeInTheDocument()
})

test('a named query can be saved and loaded again', () => {
  renderGrid()
  fireEvent.click(screen.getByRole('button', { name: 'Filter' }))
  fireEvent.change(screen.getByLabelText('Query'), { target: { value: 'status=active' } })
  fireEvent.change(screen.getByLabelText('Query name'), { target: { value: 'Active licenses' } })
  fireEvent.click(screen.getByRole('button', { name: 'Save query' }))
  expect(screen.getByLabelText('Saved queries')).toHaveDisplayValue('Active licenses')
  fireEvent.change(screen.getByLabelText('Query'), { target: { value: '' } })
  expect(screen.getByRole('gridcell', { name: 'Reader' })).toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('Saved queries'), { target: { value: screen.getByRole('option', { name: 'Active licenses' }).getAttribute('value') ?? '' } })
  expect(screen.getByRole('gridcell', { name: 'Writer' })).toBeInTheDocument()
  expect(screen.queryByRole('gridcell', { name: 'Reader' })).not.toBeInTheDocument()
})

test('unknown query fields are rejected instead of matching every row', () => {
  renderGrid()
  fireEvent.click(screen.getByRole('button', { name: 'Filter' }))
  fireEvent.change(screen.getByLabelText('Query'), { target: { value: 'owner=root' } })
  expect(screen.getByRole('alert')).toHaveTextContent('Unknown field "owner"')
  expect(screen.getByRole('gridcell', { name: 'Writer' })).toBeInTheDocument()
  expect(screen.getByRole('gridcell', { name: 'Reader' })).toBeInTheDocument()
})

test('value suggestions list unique visible values and narrow to rows the other conditions match', () => {
  renderGrid()
  fireEvent.click(screen.getByRole('button', { name: 'Filter' }))
  fireEvent.change(screen.getByLabelText('Field'), { target: { value: 'status' } })
  const statusBox = screen.getByRole('combobox', { name: 'Value for Status' })
  fireEvent.focus(statusBox)
  expect(screen.getByRole('option', { name: /^active/ })).toBeInTheDocument()
  expect(screen.getByRole('option', { name: /^retired/ })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('option', { name: /^active/ }))
  expect(screen.queryByRole('gridcell', { name: 'Reader' })).not.toBeInTheDocument()

  // The second condition only offers values from rows that are still active.
  fireEvent.click(screen.getByRole('button', { name: 'Add condition' }))
  fireEvent.change(screen.getAllByLabelText('Field')[1], { target: { value: 'name' } })
  fireEvent.focus(screen.getByRole('combobox', { name: 'Value for Name' }))
  expect(screen.getByRole('option', { name: /Writer/ })).toBeInTheDocument()
  expect(screen.queryByRole('option', { name: /Reader/ })).toBeNull()
})

test('a text cell can be edited in a full screen editor', () => {
  const { table } = renderGrid()
  fireEvent.keyDown(table, { key: 'Enter' })
  fireEvent.click(screen.getByRole('button', { name: 'Edit Name in full screen' }))
  const editor = screen.getByRole('dialog', { name: 'Name for Writer' })
  expect(editor.className).toContain('bg-steward-ink-900')
  expect(editor.className).not.toContain('/55')
  expect(editor.parentElement?.className).toContain('bg-steward-ink-950')
  expect(editor.parentElement?.className).not.toContain('/80')
  fireEvent.change(within(editor).getByRole('textbox'), { target: { value: 'Composer' } })
  fireEvent.click(screen.getByRole('button', { name: 'Done' }))
  expect(screen.getByRole('gridcell', { name: 'Composer' })).toBeInTheDocument()
})

test('a scannable cell offers camera capture in the editor and expanded work area', () => {
  const serialColumns: GridColumn<Row>[] = [
    { key: 'name', header: 'Serial number', kind: 'text', editable: true, scannable: true, text: (row) => row.name },
    { key: 'status', header: 'Status', kind: 'enum', options: ['active', 'retired'], editable: true, text: (row) => row.status },
  ]
  render(<DataGrid columns={serialColumns} editable label="Assets" rowId={(row) => row.id} rowLabel={(row) => row.name} rows={rows} />)
  fireEvent.keyDown(screen.getByRole('grid'), { key: 'Enter' })
  expect(screen.getByRole('button', { name: 'Scan Serial number with camera' })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Scan Serial number with camera' }))
  const camera = screen.getByRole('dialog', { name: 'Scan a barcode into this cell' })
  expect(camera).toBeInTheDocument()
  expect(camera.className).toContain('overflow-y-auto')
  fireEvent.click(screen.getByRole('button', { name: 'Edit Serial number in full screen' }))
  const editor = screen.getByRole('dialog', { name: 'Serial number for Writer' })
  expect(editor.className).not.toContain('/55')
  expect(editor.className).toContain('overflow-hidden')
  expect(within(editor).getByRole('button', { name: 'Scan with camera' })).toBeInTheDocument()
})
