import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import axe from 'axe-core'
import { beforeEach, expect, test, vi } from 'vitest'
import LedgerManager from './LedgerManager'

// Requirement: REQ-LEDGER-001. Feature: procurement.finance.

const snapshot = {
  vendors: [{ id: 'vendor-1', name: 'Example Vendor', status: 'active' }],
  purchaseOrders: [{ id: 'po-1', number: 'PO-2026-001', vendorId: 'vendor-1', status: 'ordered', currency: 'USD', totalMinor: 125000, assetIds: ['asset-1'], receiptDocumentIds: ['doc-1'], revision: 1 }],
  contracts: [{ id: 'contract-1', name: 'Managed service', vendorId: 'vendor-1', operationalStatus: 'active', financialStatus: 'committed', currency: 'USD', ceilingMinor: 500000, startsOn: '2026-01-01T00:00:00Z', endsOn: '2028-12-31T00:00:00Z', renewsOn: '2028-10-01T00:00:00Z', revision: 1 }],
  commitments: [{ id: 'commitment-1', contractId: 'contract-1', kind: 'subscription', description: 'Three-year subscription', currency: 'USD', amountMinor: 150000, fiscalPeriod: 'FY2027', scenario: 'baseline' }],
  budgets: [{ id: 'budget-1', name: 'Infrastructure', fiscalPeriod: 'FY2027', scenario: 'baseline', currency: 'USD', allocatedMinor: 200000 }],
  costs: [{ id: 'cost-1', description: 'Vendor invoice', kind: 'billed', currency: 'USD', amountMinor: 125000, fiscalPeriod: 'FY2027', scenario: 'baseline', externalReference: 'INV-42', revision: 1 }],
}
const variance = { fiscalPeriod: 'FY2027', scenario: 'baseline', currency: 'USD', allocatedMinor: 200000, recognizedMinor: 125000, varianceMinor: 75000, overBudget: false, amountsByKindMinor: { billed: 125000 } }

function response(value: unknown, status = 200) { return new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } }) }

beforeEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals() })

test('shows budget variance and linked financial records without accessibility violations', async () => {
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => String(input).includes('budget-variance') ? response(variance) : response(snapshot)))
  const { container } = render(<LedgerManager csrfToken="csrf-token" permissions={['finance.read']} />)
  expect(await screen.findByText('PO-2026-001')).toBeInTheDocument()
  expect(screen.getByText('Managed service')).toBeInTheDocument()
  expect(screen.getByText(/Renews/)).toBeInTheDocument()
  expect(screen.getByText('Vendor invoice')).toBeInTheDocument()
  expect(screen.getByText('$750.00')).toBeInTheDocument()
  expect(screen.queryByText('Add vendor')).not.toBeInTheDocument()
  expect((await axe.run(container)).violations).toEqual([])
})

test('creates an exact-minor-unit budget with CSRF protection', async () => {
  const empty = { vendors: [], purchaseOrders: [], contracts: [], commitments: [], budgets: [], costs: [] }
  const created = { ...snapshot.budgets[0], allocatedMinor: 123456 }
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path === '/api/v1/ledger/budgets' && init?.method === 'POST') return response(created, 201)
    if (path === '/api/v1/ledger') return response(init ? snapshot : empty)
    if (path.includes('budget-variance')) return response(variance)
    throw new Error(`unexpected request ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<LedgerManager csrfToken="csrf-token" permissions={['finance.read', 'finance.write']} />)
  await screen.findByText('No purchase orders yet.')
  fireEvent.click(screen.getByText('Add budget'))
  const form = screen.getByRole('button', { name: 'Create budget' }).closest('form')
  if (!form) throw new Error('budget form missing')
  const scoped = within(form)
  fireEvent.change(scoped.getByLabelText('Budget name'), { target: { value: 'Infrastructure' } })
  fireEvent.change(scoped.getByLabelText('Allocated amount'), { target: { value: '1234.56' } })
  fireEvent.click(scoped.getByRole('button', { name: 'Create budget' }))
  expect(await screen.findByText('Budget created.')).toBeInTheDocument()
  await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/v1/ledger/budgets', expect.objectContaining({ method: 'POST' })))
  const request = fetchMock.mock.calls.find(([path, init]) => path === '/api/v1/ledger/budgets' && init?.method === 'POST')
  expect(request?.[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'csrf-token' })
  expect(JSON.parse(String(request?.[1]?.body))).toMatchObject({ allocatedMinor: 123456, currency: 'USD', fiscalPeriod: 'FY2027', scenario: 'baseline' })
})

test('submits a vendor without evaluating unrelated money forms', async () => {
  const empty = { vendors: [], purchaseOrders: [], contracts: [], commitments: [], budgets: [], costs: [] }
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path === '/api/v1/ledger/vendors' && init?.method === 'POST') return response({ id: 'vendor-2', name: 'Browser Vendor', status: 'active' }, 201)
    if (path === '/api/v1/ledger') return response(empty)
    if (path.includes('budget-variance')) return response(variance)
    throw new Error(`unexpected request ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<LedgerManager csrfToken="csrf-token" permissions={['finance.read', 'finance.write']} />)
  await screen.findByText('No purchase orders yet.')
  fireEvent.click(screen.getByText('Add vendor'))
  const form = screen.getByRole('button', { name: 'Create vendor' }).closest('form')
  if (!form) throw new Error('vendor form missing')
  const scoped = within(form)
  fireEvent.change(scoped.getByLabelText('Vendor name'), { target: { value: 'Browser Vendor' } })
  fireEvent.click(scoped.getByRole('button', { name: 'Create vendor' }))
  expect(await screen.findByText('Vendor created.')).toBeInTheDocument()
  expect(fetchMock).toHaveBeenCalledWith('/api/v1/ledger/vendors', expect.objectContaining({ method: 'POST' }))
})

test('submits separate contract states and an optional renewal date', async () => {
  const empty = { vendors: snapshot.vendors, purchaseOrders: [], contracts: [], commitments: [], budgets: [], costs: [] }
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path === '/api/v1/ledger/contracts' && init?.method === 'POST') return response(snapshot.contracts[0], 201)
    if (path === '/api/v1/ledger') return response(empty)
    if (path.includes('budget-variance')) return response(variance)
    throw new Error(`unexpected request ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<LedgerManager csrfToken="csrf-token" permissions={['finance.read', 'finance.write']} />)
  await screen.findByText('No contracts yet.')
  fireEvent.click(screen.getByText('Add contract'))
  const form = screen.getByRole('button', { name: 'Create contract' }).closest('form')
  if (!form) throw new Error('contract form missing')
  const scoped = within(form)
  fireEvent.change(scoped.getByLabelText('Contract name'), { target: { value: 'Managed service' } })
  fireEvent.change(scoped.getByLabelText('Vendor'), { target: { value: 'vendor-1' } })
  fireEvent.change(scoped.getByLabelText('Operational status'), { target: { value: 'active' } })
  fireEvent.change(scoped.getByLabelText('Financial status'), { target: { value: 'committed' } })
  fireEvent.change(scoped.getByLabelText('Ceiling'), { target: { value: '5000.00' } })
  fireEvent.change(scoped.getByLabelText('Starts on'), { target: { value: '2026-01-01' } })
  fireEvent.change(scoped.getByLabelText('Ends on'), { target: { value: '2028-12-31' } })
  fireEvent.change(scoped.getByLabelText('Renews on'), { target: { value: '2028-10-01' } })
  fireEvent.click(scoped.getByRole('button', { name: 'Create contract' }))
  expect(await screen.findByText('Contract created.')).toBeInTheDocument()
  const request = fetchMock.mock.calls.find(([path, init]) => path === '/api/v1/ledger/contracts' && init?.method === 'POST')
  expect(JSON.parse(String(request?.[1]?.body))).toMatchObject({
    vendorId: 'vendor-1', operationalStatus: 'active', financialStatus: 'committed', ceilingMinor: 500000,
    startsOn: '2026-01-01T00:00:00Z', endsOn: '2028-12-31T00:00:00Z', renewsOn: '2028-10-01T00:00:00Z',
  })
})

test('explains missing finance permission', () => {
  render(<LedgerManager csrfToken="csrf-token" permissions={[]} />)
  expect(screen.getByText(/does not include permission/)).toBeInTheDocument()
})
