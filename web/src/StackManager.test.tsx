import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import axe from 'axe-core'
import { beforeEach, expect, test, vi } from 'vitest'
import StackManager, { parseStackAnalytics, parseStackSnapshot } from './StackManager'

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
    if (String(input) === '/api/v1/stack/exchange/import' && init?.method === 'POST') return response({ created: 1, unchanged: 1 })
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
  expect(await screen.findByText('Import complete: 1 created and 1 unchanged.')).toBeInTheDocument()
  const call = fetchMock.mock.calls.find(([path]) => path === '/api/v1/stack/exchange/import')
  expect(JSON.parse(String(call?.[1]?.body))).toMatchObject({ sourceSystemId: 'exchange-upload', records: [{ type: 'stack.product' }] })
})

test('explains missing software permission', () => {
  render(<StackManager assets={[]} csrfToken="csrf" permissions={[]} />)
  expect(screen.getByText(/does not include permission/)).toBeInTheDocument()
})

test('rejects malformed optional fields in Stack responses', () => {
  expect(() => parseStackSnapshot({ ...snapshot, licenses: [{ ...snapshot.licenses[0], documentIds: [42] }] })).toThrow('invalid Stack response')
  expect(() => parseStackSnapshot({ ...snapshot, assignments: [{ ...snapshot.assignments[0], revision: 0 }] })).toThrow('invalid Stack response')
  expect(() => parseStackAnalytics({ ...analytics, expiringWithinDays: 0 })).toThrow('invalid Stack analytics response')
})
