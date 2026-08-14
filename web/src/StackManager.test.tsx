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

beforeEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals() })

test('renders authoritative analytics and read-only records without accessibility violations', async () => {
  vi.stubGlobal('fetch', vi.fn(fetchForRead))
  const { container } = render(<StackManager assets={[]} csrfToken="csrf" permissions={['software.read']} />)
  expect(await screen.findByText('Steward Writer')).toBeInTheDocument()
  expect(screen.getByText('Expiring')).toBeInTheDocument()
  expect(screen.getByText('Over Assigned')).toBeInTheDocument()
  expect(screen.getByText('2 seats are assigned against 1 entitled seat.')).toBeInTheDocument()
  expect(screen.queryByText('Add and connect records')).not.toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Update' })).not.toBeInTheDocument()
  expect(screen.getByRole('region', { name: 'Stack software records' })).toHaveClass('overflow-x-auto')
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
    const versionRow = (await screen.findByText('1.0')).closest('tr')
    const licenseRow = screen.getByText('Device subscription').closest('tr')
    if (!versionRow || !licenseRow) throw new Error('Stack calendar rows missing')
    const calendarLabel = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeZone: 'UTC' }).format(new Date('2026-09-12T00:00:00Z'))
    const driftedLocalLabel = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium' }).format(new Date('2026-09-12T00:00:00Z'))
    expect(driftedLocalLabel).not.toBe(calendarLabel)
    expect(within(versionRow).getByText(calendarLabel)).toBeInTheDocument()
    expect(within(licenseRow).getByText(`1 Device · expires ${calendarLabel}`)).toBeInTheDocument()
    expect(within(licenseRow).getByLabelText('License starts for Device subscription')).toHaveValue('2026-08-13')
    expect(within(licenseRow).getByLabelText('License expires for Device subscription')).toHaveValue('2026-09-12')
  } finally {
    vi.unstubAllEnvs()
  }
})

test('creates a product with CSRF and reloads the connected view', async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path === '/api/v1/stack/products' && init?.method === 'POST') return response(snapshot.products[0], 201)
    return fetchForRead(input)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<StackManager assets={[]} csrfToken="csrf-token" permissions={['software.read', 'software.write']} />)
  await screen.findByText('Steward Writer')
  fireEvent.click(screen.getByText('Define product'))
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

test('updates assignment usage with optimistic revision and CSRF', async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    if (String(input).endsWith('/assignments/assignment-1/usage') && init?.method === 'PUT') return response({ ...snapshot.assignments[0], usageState: 'used', revision: 2 })
    return fetchForRead(input)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<StackManager assets={[]} csrfToken="csrf-token" permissions={['software.read', 'software.write']} />)
  await screen.findByText('Steward Writer')
  const row = screen.getByText('Assignment').closest('tr')
  if (!row) throw new Error('assignment row missing')
  fireEvent.change(within(row).getByLabelText('Assignment usage for asset-1'), { target: { value: 'used' } })
  fireEvent.click(within(row).getByRole('button', { name: 'Update assignment usage' }))
  expect(await screen.findByText('Assignment usage updated.')).toBeInTheDocument()
  await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/v1/stack/assignments/assignment-1/usage', expect.objectContaining({ method: 'PUT' })))
  const call = fetchMock.mock.calls.find(([path, init]) => String(path).endsWith('/assignments/assignment-1/usage') && init?.method === 'PUT')
  expect(JSON.parse(String(call?.[1]?.body))).toMatchObject({ usageState: 'used', lastUsedAt: '2026-08-12T00:00:00.000Z', revision: 1 })
})

test('retires products and ends assignments through explicit lifecycle controls', async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path.endsWith('/products/writer/status') && init?.method === 'PUT') return response({ ...snapshot.products[0], status: 'retired', revision: 2 })
    if (path.endsWith('/assignments/assignment-1/end') && init?.method === 'PUT') return response({ ...snapshot.assignments[0], endedAt: '2026-08-13T12:00:00Z', revision: 2 })
    return fetchForRead(input)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<StackManager assets={[]} csrfToken="csrf-token" permissions={['software.read', 'software.write']} />)
  await screen.findByText('Steward Writer')

  const productRow = screen.getByLabelText('Product status for Steward Writer').closest('tr')
  if (!productRow) throw new Error('product row missing')
  fireEvent.change(within(productRow).getByLabelText('Product status for Steward Writer'), { target: { value: 'retired' } })
  fireEvent.click(within(productRow).getByRole('button', { name: 'Update product' }))
  expect(await screen.findByText('Product status updated.')).toBeInTheDocument()

  const assignmentRow = screen.getByLabelText('Assignment ends at for asset-1').closest('tr')
  if (!assignmentRow) throw new Error('assignment row missing')
  fireEvent.change(within(assignmentRow).getByLabelText('Assignment ends at for asset-1'), { target: { value: '2026-08-13T12:00' } })
  fireEvent.click(within(assignmentRow).getByRole('button', { name: 'End assignment' }))
  expect(await screen.findByText('License assignment ended.')).toBeInTheDocument()

  const productCall = fetchMock.mock.calls.find(([path, init]) => String(path).endsWith('/products/writer/status') && init?.method === 'PUT')
  expect(productCall?.[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'csrf-token' })
  expect(JSON.parse(String(productCall?.[1]?.body))).toEqual({ status: 'retired', revision: 1 })
  const assignmentCall = fetchMock.mock.calls.find(([path, init]) => String(path).endsWith('/assignments/assignment-1/end') && init?.method === 'PUT')
  expect(JSON.parse(String(assignmentCall?.[1]?.body))).toEqual({ endedAt: new Date('2026-08-13T12:00').toISOString(), revision: 1 })
})

test('imports exported records and reports replay totals', async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    if (String(input) === '/api/v1/stack/exchange/import' && init?.method === 'POST') return response(importResult)
    return fetchForRead(input)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<StackManager assets={[]} csrfToken="csrf" permissions={['software.read', 'software.write']} />)
  await screen.findByText('Steward Writer')
  fireEvent.click(screen.getByText('Import portable records'))
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
  await screen.findByText('Steward Writer')
  fireEvent.click(screen.getByText('Import portable records'))
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
