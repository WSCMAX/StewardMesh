import axe from 'axe-core'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import AtlasInventory, { type Asset } from './AtlasInventory'

// Requirements: REQ-ATLAS-001, REQ-ATLAS-MODELS-001, REQ-ATLAS-CODES-001, A11Y-001, SEC-GUARD-001.

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
    if (String(input) === '/api/v1/assets/asset-1/identifiers') return jsonResponse({ items: [] })
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

test('shows immutable model defaults, provenance, effective dates, and instance overrides', async () => {
  const linked: Asset = {
    ...asset, kind: 'desktop', modelId: 'model-1', modelContext: {
      manufacturer: 'Framework', name: 'Laptop 13', modelNumber: 'FW13', kind: 'laptop',
      specifications: { CPU: 'Ryzen', Memory: '32 GB' }, warrantyMonths: 36, usefulLifeMonths: 48,
      sourceSystemId: 'model-import', sourceRecordId: 'framework-fw13-v1', modelRevision: 1,
      defaultsEffectiveAt: '2026-08-12T12:00:00Z', appliedAt: '2026-08-12T13:00:00Z', overrides: ['kind'],
    },
  }
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const path = String(input)
    if (path === '/api/v1/asset-models?limit=100') return jsonResponse({ items: [{
      id: 'model-1', organizationId: 'example-org', manufacturer: 'Framework', name: 'Laptop 13 updated',
      kind: 'computer', status: 'active', instanceCount: 1, revision: 2,
      createdAt: '2026-08-12T12:00:00Z', updatedAt: '2026-08-12T14:00:00Z',
    }] })
    if (path === '/api/v1/assets/asset-1/lifecycle') return jsonResponse({ items: [] })
    if (path === '/api/v1/assets/asset-1/identifiers') return jsonResponse({ items: [] })
    throw new Error(`unexpected request: ${path}`)
  }))
  const { container } = render(<AtlasInventory assets={[linked]} csrfToken="csrf-token" onAssetsChange={() => undefined} permissions={['assets.read']} />)
  fireEvent.click(screen.getByRole('button', { name: /Lab server/ }))
  const defaults = await screen.findByRole('heading', { name: 'Model defaults when linked' })
  const section = defaults.closest('section') as HTMLElement
  expect(within(section).getByText('Framework Laptop 13 FW13')).toBeInTheDocument()
  expect(within(section).getByText('desktop (overrides laptop)')).toBeInTheDocument()
  expect(within(section).getByText('framework-fw13-v1')).toBeInTheDocument()
  expect(within(section).getByText('Overrides: Kind')).toBeInTheDocument()
  expect(within(section).getByText('Ryzen')).toBeInTheDocument()
  expect(within(section).queryByText(/updated/)).not.toBeInTheDocument()
  expect((await axe.run(container)).violations).toEqual([])
})

test('creates an asset with CSRF protection and server-managed identity fields', async () => {
  const created = { ...asset, id: 'asset-2', name: 'New laptop', kind: 'laptop', assetTag: 'LAP-002' }
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (['/api/v1/sites', '/api/v1/buildings', '/api/v1/rooms', '/api/v1/departments', '/api/v1/identities?limit=100'].includes(path)) return jsonResponse({ items: [] })
    if (path === '/api/v1/assets' && init?.method === 'POST') return jsonResponse(created, 201)
    if (path === '/api/v1/assets/asset-2/lifecycle') return jsonResponse({ items: [] })
    if (path === '/api/v1/assets/asset-2/identifiers') return jsonResponse({ items: [] })
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

test('creates a model and links a new asset to it', async () => {
  const model = {
    id: 'model-1', organizationId: 'example-org', manufacturer: 'Framework', name: 'Laptop 13',
    modelNumber: 'FW13', kind: 'laptop', status: 'active', warrantyMonths: 36, usefulLifeMonths: 48,
    instanceCount: 0, revision: 1, createdAt: '2026-08-12T12:00:00Z', updatedAt: '2026-08-12T12:00:00Z',
  }
  const created = { ...asset, id: 'asset-3', name: 'Framework laptop', kind: 'laptop', modelId: model.id, assetTag: 'FW-003' }
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path === '/api/v1/asset-models?limit=100') return jsonResponse({ items: [] })
    if (path === '/api/v1/asset-models' && init?.method === 'POST') return jsonResponse(model, 201)
    if (['/api/v1/sites', '/api/v1/buildings', '/api/v1/rooms', '/api/v1/departments', '/api/v1/identities?limit=100'].includes(path)) return jsonResponse({ items: [] })
    if (path === '/api/v1/assets' && init?.method === 'POST') return jsonResponse(created, 201)
    if (path === '/api/v1/assets/asset-3/lifecycle') return jsonResponse({ items: [] })
    if (path === '/api/v1/assets/asset-3/identifiers') return jsonResponse({ items: [] })
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  const onAssetsChange = vi.fn()
  render(<AtlasInventory assets={[]} csrfToken="csrf-token" onAssetsChange={onAssetsChange} permissions={['assets.read', 'assets.write']} />)
  fireEvent.click(screen.getByRole('button', { name: 'Add model' }))
  const modelForm = within(screen.getByRole('form', { name: 'Add model' }))
  fireEvent.change(modelForm.getByLabelText('Manufacturer'), { target: { value: 'Framework' } })
  fireEvent.change(modelForm.getByLabelText('Model name'), { target: { value: 'Laptop 13' } })
  fireEvent.change(modelForm.getByLabelText('Model number'), { target: { value: 'FW13' } })
  fireEvent.change(modelForm.getByLabelText('Kind'), { target: { value: 'laptop' } })
  fireEvent.change(modelForm.getByLabelText('Warranty months'), { target: { value: '36' } })
  fireEvent.change(modelForm.getByLabelText('Useful life months'), { target: { value: '48' } })
  fireEvent.click(modelForm.getByRole('button', { name: 'Create model' }))
  expect(await screen.findByText('Model created.')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Use' }))
  const assetForm = within(screen.getByRole('form', { name: 'Add asset' }))
  expect(assetForm.getByLabelText('Kind')).toHaveValue('laptop')
  fireEvent.change(assetForm.getByLabelText('Asset name'), { target: { value: 'Framework laptop' } })
  fireEvent.change(assetForm.getByLabelText('Status'), { target: { value: 'active' } })
  fireEvent.change(assetForm.getByLabelText('Asset tag'), { target: { value: 'FW-003' } })
  fireEvent.click(assetForm.getByRole('button', { name: 'Create asset' }))
  expect(await screen.findByText('Asset created.')).toBeInTheDocument()
  const assetRequest = fetchMock.mock.calls.find(([path, init]) => path === '/api/v1/assets' && init?.method === 'POST')
  expect(JSON.parse(String(assetRequest?.[1]?.body))).toMatchObject({ modelId: 'model-1', name: 'Framework laptop', kind: 'laptop' })
  expect(onAssetsChange).toHaveBeenCalledWith([created])
})

test('bulk creates model instances with per-asset deployment fields and accessible repeatable rows', async () => {
  const model = {
    id: 'model-bulk', organizationId: 'example-org', manufacturer: 'Framework', name: 'Laptop 13',
    modelNumber: 'FW13', kind: 'laptop', status: 'active', instanceCount: 0, revision: 1,
    createdAt: '2026-08-12T12:00:00Z', updatedAt: '2026-08-12T12:00:00Z',
  }
  const created = [
    { ...asset, id: 'bulk-1', modelId: model.id, name: 'Bulk laptop one', kind: 'laptop', assetTag: 'BULK-001', deploymentNotes: 'North lab cart' },
    { ...asset, id: 'bulk-2', modelId: model.id, name: 'Bulk laptop two', kind: 'laptop', assetTag: 'BULK-002', status: 'draft' },
  ]
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path === '/api/v1/asset-models?limit=100') return jsonResponse({ items: [model] })
    if (['/api/v1/sites', '/api/v1/buildings', '/api/v1/rooms', '/api/v1/departments', '/api/v1/identities?limit=100'].includes(path)) return jsonResponse({ items: [] })
    if (path === '/api/v1/asset-models/model-bulk/assets/bulk' && init?.method === 'POST') return jsonResponse({ items: created }, 201)
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  const onAssetsChange = vi.fn()
  const { container } = render(<AtlasInventory assets={[]} csrfToken="csrf-token" onAssetsChange={onAssetsChange} permissions={['assets.read', 'assets.write']} />)
  fireEvent.click(await screen.findByRole('button', { name: 'Bulk add' }))
  const first = within(screen.getByRole('group', { name: 'Asset 1' }))
  fireEvent.change(first.getByLabelText('Asset name'), { target: { value: 'Bulk laptop one' } })
  fireEvent.change(first.getByLabelText('Asset tag'), { target: { value: 'BULK-001' } })
  fireEvent.change(first.getByLabelText('Deployment notes'), { target: { value: 'North lab cart' } })
  fireEvent.click(screen.getByRole('button', { name: 'Add another asset' }))
  const second = within(screen.getByRole('group', { name: 'Asset 2' }))
  fireEvent.change(second.getByLabelText('Asset name'), { target: { value: 'Bulk laptop two' } })
  fireEvent.change(second.getByLabelText('Asset tag'), { target: { value: 'BULK-002' } })
  expect((await axe.run(container)).violations).toEqual([])
  fireEvent.click(screen.getByRole('button', { name: 'Create 2 assets' }))
  expect(await screen.findByText('2 assets created from Framework Laptop 13 FW13.')).toBeInTheDocument()
  expect(onAssetsChange).toHaveBeenCalledWith(created)
  const request = fetchMock.mock.calls.find(([path, init]) => path === '/api/v1/asset-models/model-bulk/assets/bulk' && init?.method === 'POST')
  expect(request?.[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'csrf-token' })
  expect(JSON.parse(String(request?.[1]?.body))).toEqual({ items: [
    expect.objectContaining({ name: 'Bulk laptop one', assetTag: 'BULK-001', deploymentNotes: 'North lab cart', status: 'draft' }),
    expect.objectContaining({ name: 'Bulk laptop two', assetTag: 'BULK-002', status: 'draft' }),
  ] })
})

test('updates an asset using its current revision and records a lifecycle note', async () => {
  const updated = { ...asset, status: 'retired', revision: 2, updatedAt: '2026-08-10T13:00:00Z' }
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (['/api/v1/sites', '/api/v1/buildings', '/api/v1/rooms', '/api/v1/departments', '/api/v1/identities?limit=100'].includes(path)) return jsonResponse({ items: [] })
    if (path === '/api/v1/assets/asset-1' && init?.method === 'PUT') return jsonResponse(updated)
    if (path === '/api/v1/assets/asset-1/lifecycle') return jsonResponse({ items: [] })
    if (path === '/api/v1/assets/asset-1/identifiers') return jsonResponse({ items: [] })
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

test('opens the authorized asset detail from the explicit Atlas Codes scan-to-find workflow', async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path === '/api/v1/asset-models?limit=100') return jsonResponse({ items: [] })
    if (path === '/api/v1/asset-identifiers/resolve' && init?.method === 'POST') return jsonResponse({ assetId: asset.id })
    if (path === '/api/v1/assets/asset-1/lifecycle') return jsonResponse({ items: [] })
    if (path === '/api/v1/assets/asset-1/identifiers') return jsonResponse({ items: [] })
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<AtlasInventory assets={[asset]} csrfToken="csrf-token" onAssetsChange={() => undefined} permissions={['assets.read']} />)

  fireEvent.click(screen.getByRole('button', { name: 'Open scanner' }))
  fireEvent.change(screen.getByLabelText('Symbology'), { target: { value: 'qr' } })
  fireEvent.change(screen.getByLabelText('Scanned or entered value'), { target: { value: 'opaque-asset-route' } })
  fireEvent.click(screen.getByRole('button', { name: 'Find asset' }))

  expect(await screen.findByText(/Identifier matched/)).toBeInTheDocument()
  const detail = screen.getByRole('heading', { name: 'Asset details' }).closest('aside') as HTMLElement
  expect(within(detail).getByText('Lab server')).toBeInTheDocument()
  expect(JSON.parse(String(fetchMock.mock.calls.find(([path]) => path === '/api/v1/asset-identifiers/resolve')?.[1]?.body))).toEqual({ symbology: 'qr', value: 'opaque-asset-route' })
})
