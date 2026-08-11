import axe from 'axe-core'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import AtlasInventory, { type Asset } from './AtlasInventory'

// Requirements: REQ-ATLAS-001, A11Y-001, SEC-GUARD-001.

const asset: Asset = {
  id: 'asset-1', organizationId: 'example-org', name: 'Lab server', kind: 'server',
  assetTag: 'LAB-001', serialNumber: 'SERIAL-001', hostname: 'lab.example.test',
  status: 'active', revision: 1, createdAt: '2026-08-10T12:00:00Z', updatedAt: '2026-08-10T12:00:00Z',
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

beforeEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

test('filters assets, loads lifecycle details, and has no automated accessibility violations', async () => {
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    if (String(input) === '/api/v1/assets/asset-1/lifecycle') return jsonResponse({ items: [{
      id: 'event-1', toStatus: 'active', note: 'Asset registered', revision: 1,
      actorId: 'account-1', occurredAt: '2026-08-10T12:00:00Z',
    }] })
    throw new Error(`unexpected request: ${String(input)}`)
  }))
  const { container } = render(<AtlasInventory assets={[asset]} csrfToken="csrf-token" onAssetsChange={() => undefined} permissions={['assets.read']} />)
  expect(screen.getByRole('heading', { name: 'Atlas — Asset inventory' })).toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('Search'), { target: { value: 'missing' } })
  expect(screen.getByText('No assets match these filters.')).toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('Search'), { target: { value: 'LAB-001' } })
  fireEvent.click(screen.getByRole('button', { name: /Lab server/ }))
  expect(await screen.findByText('Asset registered', { exact: false })).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Add asset' })).not.toBeInTheDocument()
  const results = await axe.run(container)
  expect(results.violations).toEqual([])
})

test('creates an asset with CSRF protection and server-managed identity fields', async () => {
  const created = { ...asset, id: 'asset-2', name: 'New laptop', kind: 'laptop', assetTag: 'LAP-002' }
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (['/api/v1/sites', '/api/v1/buildings', '/api/v1/rooms', '/api/v1/departments', '/api/v1/identities?limit=100'].includes(path)) return jsonResponse({ items: [] })
    if (path === '/api/v1/assets' && init?.method === 'POST') return jsonResponse(created, 201)
    if (path === '/api/v1/assets/asset-2/lifecycle') return jsonResponse({ items: [] })
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  const onAssetsChange = vi.fn()
  render(<AtlasInventory assets={[]} csrfToken="csrf-token" onAssetsChange={onAssetsChange} permissions={['assets.read', 'assets.write']} />)
  fireEvent.click(screen.getByRole('button', { name: 'Add asset' }))
  const createForm = within(screen.getByRole('form', { name: 'Add asset' }))
  fireEvent.change(createForm.getByLabelText('Asset name'), { target: { value: 'New laptop' } })
  fireEvent.change(createForm.getByLabelText('Kind'), { target: { value: 'laptop' } })
  fireEvent.change(createForm.getByLabelText('Status'), { target: { value: 'active' } })
  fireEvent.change(createForm.getByLabelText('Asset tag'), { target: { value: 'LAP-002' } })
  fireEvent.click(createForm.getByRole('button', { name: 'Create asset' }))
  expect(await screen.findByText('Asset created.')).toBeInTheDocument()
  expect(onAssetsChange).toHaveBeenCalledWith([created])
  const request = fetchMock.mock.calls.find(([path, init]) => path === '/api/v1/assets' && init?.method === 'POST')
  expect(request?.[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'csrf-token' })
  expect(JSON.parse(String(request?.[1]?.body))).toMatchObject({ name: 'New laptop', kind: 'laptop', status: 'active', assetTag: 'LAP-002' })
})

test('updates an asset using its current revision and records a lifecycle note', async () => {
  const updated = { ...asset, status: 'retired', revision: 2, updatedAt: '2026-08-10T13:00:00Z' }
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (['/api/v1/sites', '/api/v1/buildings', '/api/v1/rooms', '/api/v1/departments', '/api/v1/identities?limit=100'].includes(path)) return jsonResponse({ items: [] })
    if (path === '/api/v1/assets/asset-1' && init?.method === 'PUT') return jsonResponse(updated)
    if (path === '/api/v1/assets/asset-1/lifecycle') return jsonResponse({ items: [] })
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  const onAssetsChange = vi.fn()
  render(<AtlasInventory assets={[asset]} csrfToken="csrf-token" onAssetsChange={onAssetsChange} permissions={['assets.read', 'assets.write']} />)
  fireEvent.click(screen.getByRole('button', { name: 'Edit' }))
  const editForm = within(screen.getByRole('form', { name: 'Edit asset' }))
  fireEvent.change(editForm.getByLabelText('Status'), { target: { value: 'retired' } })
  fireEvent.change(editForm.getByLabelText(/^Lifecycle note/), { target: { value: 'Replacement completed' } })
  fireEvent.click(editForm.getByRole('button', { name: 'Save changes' }))
  expect(await screen.findByText('Asset updated.')).toBeInTheDocument()
  const request = fetchMock.mock.calls.find(([path, init]) => path === '/api/v1/assets/asset-1' && init?.method === 'PUT')
  expect(JSON.parse(String(request?.[1]?.body))).toMatchObject({ revision: 1, status: 'retired', lifecycleNote: 'Replacement completed' })
  await waitFor(() => expect(onAssetsChange).toHaveBeenCalledWith([updated]))
})
