import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import axe from 'axe-core'
import { beforeEach, expect, test, vi } from 'vitest'
import StackManager, { parseStackAnalytics, parseStackImportResult, parseStackSnapshot } from './StackManager'

// Requirement: REQ-STACK-001. Feature: software.licenses.

const snapshot = {
  products: [{ id: 'writer', name: 'Steward Writer', publisher: 'Example', category: 'productivity', status: 'active', revision: 1 }],
  versions: [{ id: 'writer-v1', productId: 'writer', name: '1.0', releasedOn: '2026-01-01T00:00:00Z', status: 'active', revision: 1 }],
  installations: [{ id: 'installation-1', versionId: 'writer-v1', assetId: 'asset-1', status: 'installed', usageState: 'used', installedAt: '2026-08-01T00:00:00Z', revision: 1 }],
  licenses: [{ id: 'license-1', productId: 'writer', versionId: 'writer-v1', name: 'Device subscription', entitlementMetric: 'device', quantity: 1, status: 'active', expiresOn: '2026-09-01T00:00:00Z', documentIds: [], revision: 1 }],
  assignments: [{ id: 'assignment-1', licenseId: 'license-1', assigneeKind: 'asset', assigneeId: 'asset-1', seats: 2, usageState: 'unused', assignedAt: '2026-08-01T00:00:00Z', lastUsedAt: '2026-08-12T00:00:00Z', revision: 1 }],
}
const analytics = {
  asOf: '2026-08-13T00:00:00Z', expiringWithinDays: 90, products: 1, activeInstallations: 1, activeLicenses: 1,
  entitledQuantity: 1, assignedQuantity: 2, underusedAssignments: 1,
  complianceConditions: [
    { code: 'expiring', severity: 'warning', productId: 'writer', licenseId: 'license-1', daysUntilExpiry: 19, humanReadableState: 'License expires in 19 days.' },
    { code: 'over_assigned', severity: 'critical', productId: 'writer', licenseId: 'license-1', entitledQuantity: 1, assignedQuantity: 2, humanReadableState: '2 seats are assigned against 1 entitled seat.' },
  ],
}
const importResult = {
  packageId: `stack-import-${'a'.repeat(64)}`, status: 'completed', created: 1, unchanged: 1, holding: 0, replay: false,
  records: [
    { type: 'stack.product', id: 'portable', revision: 1, checksum: 'b'.repeat(64), status: 'created', missingDependencies: [], writeLocked: true },
    { type: 'stack.product', id: 'existing', revision: 1, checksum: 'c'.repeat(64), status: 'unchanged', missingDependencies: [], writeLocked: true },
  ],
  pendingOwnership: [],
}

function response(value: unknown, status = 200) { return new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } }) }
function fetchForRead(input: RequestInfo | URL) { return Promise.resolve(response(String(input).includes('/analytics') ? analytics : snapshot)) }

// Every Stack record type renders through the shared data grid, so tests locate
// records by the grid's accessible names rather than by table markup: each grid
// is a region named for its record type, each cell is a gridcell named by its
// visible text, and each cell editor is named "<column> for <record>".
function grid(name: string) { return screen.getByRole('region', { name }) }
function cell(gridName: string, cellName: string | RegExp) { return within(grid(gridName)).getByRole('gridcell', { name: cellName }) }

function editCell(gridName: string, cellName: string, editorName: string, value: string) {
  fireEvent.doubleClick(cell(gridName, cellName))
  const field = screen.getByLabelText(editorName)
  fireEvent.change(field, { target: { value } })
  fireEvent.keyDown(field, { key: 'Enter' })
}

beforeEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals() })

test('renders authoritative analytics and read-only records without accessibility violations', async () => {
  vi.stubGlobal('fetch', vi.fn(fetchForRead))
  const { container } = render(<StackManager assets={[]} csrfToken="csrf" permissions={['software.read']} />)
  expect(await screen.findByRole('region', { name: 'Software products' })).toBeInTheDocument()
  expect(cell('Software products', 'Steward Writer')).toBeInTheDocument()
  expect(cell('Software versions', '1.0')).toBeInTheDocument()
  expect(cell('License entitlements', 'Device subscription')).toBeInTheDocument()
  expect(cell('Seat assignments', 'asset-1')).toBeInTheDocument()
  expect(screen.getByText('Expiring')).toBeInTheDocument()
  expect(screen.getByText('Over Assigned')).toBeInTheDocument()
  expect(screen.getByText('2 seats are assigned against 1 entitled seat.')).toBeInTheDocument()
  expect(screen.queryByText('Add and connect records')).not.toBeInTheDocument()
  // Without software.write every cell reports itself read-only to assistive technology.
  expect(cell('Software products', 'Active')).toHaveAttribute('aria-readonly', 'true')
  expect(screen.queryByRole('button', { name: 'Save changes' })).not.toBeInTheDocument()
  expect((await axe.run(container)).violations).toEqual([])
})

test('keeps version and license calendar dates stable west of UTC', async () => {
  vi.stubEnv('TZ', 'America/Chicago')
  const calendarSnapshot = {
    ...snapshot,
    versions: [{ ...snapshot.versions[0], releasedOn: '2026-09-12T00:00:00Z' }],
    licenses: [{ ...snapshot.licenses[0], startsOn: '2026-08-13T00:00:00Z', expiresOn: '2026-09-12T00:00:00Z' }],
  }
  vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => Promise.resolve(response(String(input).includes('/analytics') ? analytics : calendarSnapshot))))

  try {
    render(<StackManager assets={[]} csrfToken="csrf" permissions={['software.read', 'software.write']} />)
    await screen.findByRole('region', { name: 'Software versions' })
    const calendarLabel = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeZone: 'UTC' }).format(new Date('2026-09-12T00:00:00Z'))
    const driftedLocalLabel = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium' }).format(new Date('2026-09-12T00:00:00Z'))
    expect(driftedLocalLabel).not.toBe(calendarLabel)
    expect(cell('Software versions', calendarLabel)).toBeInTheDocument()
    expect(cell('License entitlements', '2026-08-13')).toBeInTheDocument()
    expect(cell('License entitlements', '2026-09-12')).toBeInTheDocument()
    // The editor opens on the same calendar day the cell shows, not the local shift.
    fireEvent.doubleClick(cell('License entitlements', '2026-09-12'))
    expect(screen.getByLabelText('Expires on for Device subscription')).toHaveValue('2026-09-12')
  } finally {
    vi.unstubAllEnvs()
  }
})

test('sorting, filtering, and column visibility narrow a grid without touching the data', async () => {
  const wideSnapshot = {
    ...snapshot,
    products: [
      snapshot.products[0],
      { id: 'reader', name: 'Steward Reader', publisher: 'Example', category: 'productivity', status: 'retired', revision: 1 },
    ],
  }
  vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => Promise.resolve(response(String(input).includes('/analytics') ? analytics : wideSnapshot))))
  render(<StackManager assets={[]} csrfToken="csrf" permissions={['software.read']} />)
  await screen.findByRole('region', { name: 'Software products' })
  const products = grid('Software products')

  const productNames = () => within(products).getAllByRole('row').slice(1).map((row) => within(row).getAllByRole('gridcell')[0].textContent)
  expect(productNames()).toEqual(['Steward Writer', 'Steward Reader'])
  fireEvent.click(within(products).getByRole('button', { name: 'Product' }))
  expect(productNames()).toEqual(['Steward Reader', 'Steward Writer'])

  fireEvent.change(within(products).getByLabelText('Filter Status'), { target: { value: 'retired' } })
  expect(productNames()).toEqual(['Steward Reader'])
  expect(within(products.parentElement as HTMLElement).getByText('1 of 2 rows')).toBeInTheDocument()

  fireEvent.click(within(products.parentElement as HTMLElement).getByRole('button', { name: 'Columns' }))
  fireEvent.click(within(products.parentElement as HTMLElement).getByLabelText('Publisher'))
  expect(within(products).queryByRole('columnheader', { name: /Publisher/ })).not.toBeInTheDocument()
})

test('creates a product with CSRF and reloads the connected view', async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path === '/api/v1/stack/products' && init?.method === 'POST') return response(snapshot.products[0], 201)
    return fetchForRead(input)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<StackManager assets={[]} csrfToken="csrf-token" permissions={['software.read', 'software.write']} />)
  await screen.findByRole('region', { name: 'Software products' })
  fireEvent.click(screen.getByRole('button', { name: 'Define product' }))
  const form = screen.getByRole('button', { name: 'Create product' }).closest('form')
  if (!form) throw new Error('product form missing')
  const scoped = within(form)
  fireEvent.change(scoped.getByLabelText('Product name'), { target: { value: 'Browser Writer' } })
  fireEvent.change(scoped.getByLabelText('Publisher'), { target: { value: 'Browser Publisher' } })
  fireEvent.click(scoped.getByRole('button', { name: 'Create product' }))
  expect(await screen.findByText('Software product created.')).toBeInTheDocument()
  const call = fetchMock.mock.calls.find(([path, init]) => path === '/api/v1/stack/products' && init?.method === 'POST')
  expect(call?.[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'csrf-token' })
  expect(JSON.parse(String(call?.[1]?.body))).toMatchObject({ name: 'Browser Writer', publisher: 'Browser Publisher', status: 'active' })
})

test('updates assignment usage from the grid with optimistic revision and CSRF', async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    if (String(input).endsWith('/assignments/assignment-1/usage') && init?.method === 'PUT') return response({ ...snapshot.assignments[0], usageState: 'used', revision: 2 })
    return fetchForRead(input)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<StackManager assets={[]} csrfToken="csrf-token" permissions={['software.read', 'software.write']} />)
  await screen.findByRole('region', { name: 'Seat assignments' })

  editCell('Seat assignments', 'unused', 'Usage for asset-1', 'used')
  expect(screen.getByText('1 cell changed in 1 record')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Save changes' }))

  expect(await screen.findByText('Assignment updated.')).toBeInTheDocument()
  await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/v1/stack/assignments/assignment-1/usage', expect.objectContaining({ method: 'PUT' })))
  const call = fetchMock.mock.calls.find(([path, init]) => String(path).endsWith('/assignments/assignment-1/usage') && init?.method === 'PUT')
  expect(call?.[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'csrf-token' })
  expect(JSON.parse(String(call?.[1]?.body))).toMatchObject({ usageState: 'used', lastUsedAt: '2026-08-12T00:00:00Z', revision: 1 })
})

test('retires products and ends assignments through the grids that own those fields', async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path.endsWith('/products/writer/status') && init?.method === 'PUT') return response({ ...snapshot.products[0], status: 'retired', revision: 2 })
    if (path.endsWith('/assignments/assignment-1/end') && init?.method === 'PUT') return response({ ...snapshot.assignments[0], endedAt: '2026-08-13T12:00:00Z', revision: 2 })
    return fetchForRead(input)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<StackManager assets={[]} csrfToken="csrf-token" permissions={['software.read', 'software.write']} />)
  await screen.findByRole('region', { name: 'Software products' })

  editCell('Software products', 'Active', 'Status for Steward Writer', 'retired')
  fireEvent.click(screen.getByRole('button', { name: 'Save changes' }))
  expect(await screen.findByText('Product status updated.')).toBeInTheDocument()

  fireEvent.doubleClick(within(grid('Seat assignments')).getAllByRole('gridcell')[7])
  const endsAt = screen.getByLabelText('Ended for asset-1')
  fireEvent.change(endsAt, { target: { value: '2026-08-13T12:00' } })
  fireEvent.keyDown(endsAt, { key: 'Enter' })
  fireEvent.click(screen.getByRole('button', { name: 'Save changes' }))
  expect(await screen.findByText('Assignment updated.')).toBeInTheDocument()

  const productCall = fetchMock.mock.calls.find(([path, init]) => String(path).endsWith('/products/writer/status') && init?.method === 'PUT')
  expect(productCall?.[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'csrf-token' })
  expect(JSON.parse(String(productCall?.[1]?.body))).toEqual({ status: 'retired', revision: 1 })
  const assignmentCall = fetchMock.mock.calls.find(([path, init]) => String(path).endsWith('/assignments/assignment-1/end') && init?.method === 'PUT')
  expect(JSON.parse(String(assignmentCall?.[1]?.body))).toEqual({ endedAt: new Date('2026-08-13T12:00').toISOString(), revision: 1 })
})

test('reports the record that lost a revision race and leaves the edit pending for retry', async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    if (String(input).endsWith('/products/writer/status') && init?.method === 'PUT') {
      return response({ error: { code: 'revision_conflict', message: 'The product changed since it was loaded.', correlationId: 'stack-test' } }, 409)
    }
    return fetchForRead(input)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<StackManager assets={[]} csrfToken="csrf-token" permissions={['software.read', 'software.write']} />)
  await screen.findByRole('region', { name: 'Software products' })

  editCell('Software products', 'Active', 'Status for Steward Writer', 'retired')
  fireEvent.click(screen.getByRole('button', { name: 'Save changes' }))

  expect(await screen.findByRole('alert')).toHaveTextContent('0 of 1 records saved. 1 conflicted with a newer change. Reload to see current values.')
  expect(screen.getByText('1 cell changed in 1 record')).toBeInTheDocument()
})

test('locks the cells of records the Stack endpoints refuse to change again', async () => {
  const closedSnapshot = {
    ...snapshot,
    products: [{ ...snapshot.products[0], status: 'retired' }],
    assignments: [{ ...snapshot.assignments[0], endedAt: '2026-08-12T00:00:00Z' }],
  }
  vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => Promise.resolve(response(String(input).includes('/analytics') ? analytics : closedSnapshot))))
  render(<StackManager assets={[]} csrfToken="csrf" permissions={['software.read', 'software.write']} />)
  await screen.findByRole('region', { name: 'Software products' })

  expect(cell('Software products', 'Retired')).toHaveAttribute('aria-readonly', 'true')
  expect(within(grid('Seat assignments')).getAllByRole('gridcell')[4]).toHaveAttribute('aria-readonly', 'true')
  // Immutable columns stay read-only even on records that are still open.
  expect(cell('License entitlements', 'Device')).toHaveAttribute('aria-readonly', 'true')
  expect(cell('License entitlements', 'Active')).not.toHaveAttribute('aria-readonly')
})

test('imports exported records and reports replay totals', async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    if (String(input) === '/api/v1/stack/exchange/import' && init?.method === 'POST') return response(importResult)
    return fetchForRead(input)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<StackManager assets={[]} csrfToken="csrf" permissions={['software.read', 'software.write']} />)
  await screen.findByRole('region', { name: 'Software products' })
  fireEvent.click(screen.getByRole('button', { name: 'Import portable records' }))
  const form = screen.getByRole('button', { name: 'Import records' }).closest('form')
  if (!form) throw new Error('import form missing')
  const scoped = within(form)
  fireEvent.change(scoped.getByLabelText('Source system ID'), { target: { value: 'exchange-upload' } })
  fireEvent.change(scoped.getByLabelText('Exported JSON'), { target: { value: '{"records":[{"type":"stack.product","id":"portable","revision":1,"dependencies":[],"payload":{}}]}' } })
  fireEvent.click(scoped.getByRole('button', { name: 'Import records' }))
  expect(await screen.findByText(new RegExp(`Import complete: 1 created, 1 unchanged, and 0 holding.*${importResult.packageId}`))).toBeInTheDocument()
  expect(screen.getByRole('heading', { name: 'Latest import receipt' })).toBeInTheDocument()
  expect(screen.getAllByText('Write locked until claimed')).toHaveLength(2)
  const call = fetchMock.mock.calls.find(([path]) => path === '/api/v1/stack/exchange/import')
  expect(JSON.parse(String(call?.[1]?.body))).toMatchObject({ sourceSystemId: 'exchange-upload', records: [{ type: 'stack.product' }] })
})

test('shows the durable receipt and committed prefix when a Stack import fails', async () => {
  const failedImport = {
    ...importResult, status: 'failed', created: 1, unchanged: 0, errorCode: 'package_conflict', records: [importResult.records[0]],
    pendingOwnership: [{ type: 'stack.version', id: 'portable-version', writeLocked: true }],
  }
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    if (String(input) === '/api/v1/stack/exchange/import' && init?.method === 'POST') {
      return response({
        error: { code: 'package_conflict', message: 'The durable Stack import did not complete.', correlationId: 'stack-import-test' },
        import: failedImport,
      }, 409)
    }
    return fetchForRead(input)
  })
  vi.stubGlobal('fetch', fetchMock)
  const { container } = render(<StackManager assets={[]} csrfToken="csrf" permissions={['software.read', 'software.write']} />)
  await screen.findByRole('region', { name: 'Software products' })
  fireEvent.click(screen.getByRole('button', { name: 'Import portable records' }))
  const form = screen.getByRole('button', { name: 'Import records' }).closest('form')
  if (!form) throw new Error('import form missing')
  const scoped = within(form)
  fireEvent.change(scoped.getByLabelText('Source system ID'), { target: { value: 'exchange-upload' } })
  fireEvent.change(scoped.getByLabelText('Exported JSON'), { target: { value: '{"records":[{"type":"stack.product","id":"portable","revision":1,"dependencies":[],"payload":{}}]}' } })
  fireEvent.click(scoped.getByRole('button', { name: 'Import records' }))
  expect(await screen.findByRole('alert')).toHaveTextContent(`Receipt ${failedImport.packageId} records 1 created and 0 unchanged. Retry the exact JSON to resume.`)
  expect(screen.getByText('Failure code:')).toHaveTextContent('package_conflict')
  expect(screen.getByRole('region', { name: 'Latest Stack import outcomes' })).toHaveTextContent('stack.product:portable')
  expect(screen.getByText('Pending Guard ownership locks (1)')).toBeInTheDocument()
  expect(screen.getByRole('region', { name: 'Pending Stack import ownership locks' })).toHaveTextContent('stack.version:portable-version')
  expect(screen.getByRole('region', { name: 'Pending Stack import ownership locks' })).toHaveTextContent('Write locked until claimed')
  expect((await axe.run(container)).violations).toEqual([])
})

test('accepts a processing receipt with an exact pending Guard ownership projection', () => {
  const processing = {
    ...importResult,
    status: 'processing', created: 0, unchanged: 0, records: [], errorCode: 'receipt_read_failed',
    pendingOwnership: [{ type: 'stack.product', id: 'portable', writeLocked: false }],
  }
  expect(parseStackImportResult(processing)).toEqual(processing)
})

test('explains missing software permission', () => {
  render(<StackManager assets={[]} csrfToken="csrf" permissions={[]} />)
  expect(screen.getByText(/does not include permission/)).toBeInTheDocument()
})

test('rejects malformed optional fields in Stack responses', () => {
  expect(parseStackSnapshot({ ...snapshot, licenses: [{ ...snapshot.licenses[0], documentIds: null }] }).licenses[0].documentIds).toEqual([])
  expect(parseStackSnapshot({
    ...snapshot,
    assignmentTotal: 535,
    installationTotal: 796,
    assignments: [{ ...snapshot.assignments[0], lastUsedAt: null, endedAt: null }],
    installations: [{ ...snapshot.installations[0], lastUsedAt: null, removedAt: null }],
  }).assignmentTotal).toBe(535)
  expect(() => parseStackSnapshot({ ...snapshot, licenses: [{ ...snapshot.licenses[0], documentIds: [42] }] })).toThrow('invalid Stack response')
  expect(() => parseStackSnapshot({ ...snapshot, assignments: [{ ...snapshot.assignments[0], revision: 0 }] })).toThrow('invalid Stack response')
  expect(() => parseStackAnalytics({ ...analytics, expiringWithinDays: 0 })).toThrow('invalid Stack analytics response')
  expect(() => parseStackImportResult({ ...importResult, records: [{ ...importResult.records[0], checksum: 'not-a-digest' }] })).toThrow('invalid Stack import response')
  expect(() => parseStackImportResult({ ...importResult, created: 2 })).toThrow('invalid Stack import response')
  expect(() => parseStackImportResult({ ...importResult, pendingOwnership: undefined })).toThrow('invalid Stack import response')
  expect(() => parseStackImportResult({ ...importResult, pendingOwnership: [{ type: 'stack.product', id: 'pending' }] })).toThrow('invalid Stack import response')
  expect(() => parseStackImportResult({ ...importResult, pendingOwnership: [{ type: 'stack.product', id: 'pending', writeLocked: true, operationToken: 'private' }] })).toThrow('invalid Stack import response')
  expect(() => parseStackImportResult({ ...importResult, pendingOwnership: [{ type: 'stack.product', id: 'pending', writeLocked: 'yes' }] })).toThrow('invalid Stack import response')
  expect(() => parseStackImportResult({ ...importResult, pendingOwnership: [{ type: 'stack.product', id: 'portable', writeLocked: true }] })).toThrow('invalid Stack import response')
  expect(() => parseStackImportResult({ ...importResult, pendingOwnership: [{ type: 'stack.version', id: 'pending', writeLocked: true }] })).toThrow('invalid Stack import response')
  expect(() => parseStackImportResult({ ...importResult, pendingOwnership: [
    { type: 'stack.product', id: 'pending', writeLocked: true },
    { type: 'stack.product', id: 'pending', writeLocked: true },
  ] })).toThrow('invalid Stack import response')
  expect(() => parseStackImportResult({ ...importResult, status: 'failed', errorCode: 'provider_failed', holding: 1, created: 0, unchanged: 0, records: [{ ...importResult.records[0], status: 'holding', writeLocked: false, missingDependencies: [{ type: 'stack.product', id: 'missing' }] }] })).toThrow('invalid Stack import response')
})
