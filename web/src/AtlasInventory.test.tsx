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

const emptyRelated = { components: [], purchaseOrders: [], costs: [], installations: [], assignments: [], licenses: [], documents: [] }

function openAtlasTab(name: string) {
  fireEvent.click(screen.getByRole('tab', { name }))
}

beforeEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

test('filters assets, loads lifecycle details, and has no automated accessibility violations', async () => {
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    if (String(input).includes('/related')) return jsonResponse(emptyRelated)
    if (String(input) === '/api/v1/assets/asset-1/lifecycle') return jsonResponse({ items: [{
      id: 'event-1', toStatus: 'active', note: 'Asset registered', revision: 1,
      actorId: 'account-1', occurredAt: '2026-08-10T12:00:00Z',
    }] })
    if (String(input) === '/api/v1/assets/asset-1/identifiers') return jsonResponse({ items: [] })
    if (String(input).includes('/related')) return jsonResponse(emptyRelated)
    throw new Error(`unexpected request: ${String(input)}`)
  }))
  const { container } = render(<AtlasInventory assets={[asset]} csrfToken="csrf-token" onAssetsChange={() => undefined} permissions={['assets.read']} />)
  expect(screen.getByRole('heading', { name: 'Atlas — Asset inventory' })).toBeInTheDocument()
  expect(screen.getByRole('region', { name: 'Atlas inventory workflow' })).toBeInTheDocument()
  expect(screen.getByRole('tab', { name: 'Assets' })).toHaveAttribute('aria-selected', 'true')
  expect(screen.queryByRole('tab', { name: 'Labels' })).not.toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Find asset' })).not.toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('Search Asset inventory'), { target: { value: 'missing' } })
  expect(screen.getByText('No assets match these filters.')).toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('Search Asset inventory'), { target: { value: 'LAB-001' } })
  fireEvent.click(screen.getByRole('button', { name: 'Open Lab server' }))
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
    if (path.includes('/related')) return jsonResponse(emptyRelated)
    throw new Error(`unexpected request: ${path}`)
  }))
  const { container } = render(<AtlasInventory assets={[linked]} csrfToken="csrf-token" onAssetsChange={() => undefined} permissions={['assets.read']} />)
  fireEvent.click(screen.getByRole('button', { name: 'Open Lab server' }))
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
    if (path.includes('/related')) return jsonResponse(emptyRelated)
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
    if (path.includes('/related')) return jsonResponse(emptyRelated)
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  const onAssetsChange = vi.fn()
  render(<AtlasInventory assets={[]} csrfToken="csrf-token" onAssetsChange={onAssetsChange} permissions={['assets.read', 'assets.write']} />)
  openAtlasTab('Models')
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

test('searches and inspects complete shared model defaults', async () => {
  const model = {
    id: 'model-detail', organizationId: 'example-org', manufacturer: 'Framework', name: 'Laptop 13',
    modelNumber: 'FW13', kind: 'laptop', vendorIdentifier: 'vendor-fw13',
    specifications: { CPU: 'Ryzen', Memory: '32 GB' }, supportUrl: 'https://support.example.test/fw13',
    warrantyMonths: 36, usefulLifeMonths: 48, status: 'active', sourceSystemId: 'model-import',
    sourceRecordId: 'framework-fw13-v1', instanceCount: 0, revision: 2,
    createdAt: '2026-08-12T12:00:00Z', updatedAt: '2026-08-12T14:00:00Z',
  }
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const path = String(input)
    if (path === '/api/v1/asset-models?limit=100' || path === '/api/v1/asset-models?limit=100&q=Framework&kind=laptop&includeRetired=true') return jsonResponse({ items: [model] })
    if (path === '/api/v1/asset-models/model-detail/inventory?limit=100') return jsonResponse({
      modelId: model.id, totalCount: 0, filteredCount: 0, groups: [], items: [],
    })
    if (path.includes('/related')) return jsonResponse(emptyRelated)
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  const { container } = render(<AtlasInventory assets={[]} csrfToken="csrf-token" onAssetsChange={() => undefined} permissions={['assets.read']} />)

  openAtlasTab('Models')
  const searchForm = within(screen.getByRole('search', { name: 'Search models' }))
  fireEvent.change(searchForm.getByLabelText('Search models'), { target: { value: 'Framework' } })
  fireEvent.change(searchForm.getByLabelText('Model kind'), { target: { value: 'laptop' } })
  fireEvent.click(searchForm.getByRole('button', { name: 'Search' }))
  await waitFor(() => expect(fetchMock.mock.calls.some(([path]) => path === '/api/v1/asset-models?limit=100&q=Framework&kind=laptop&includeRetired=true')).toBe(true))

  fireEvent.click(await screen.findByRole('link', { name: 'View inventory' }))
  const details = (await screen.findByRole('heading', { name: 'Shared model defaults' })).closest('section') as HTMLElement
  expect(within(details).getByText('vendor-fw13')).toBeInTheDocument()
  expect(within(details).getByText('https://support.example.test/fw13')).toBeInTheDocument()
  expect(within(details).getByText('model-import')).toBeInTheDocument()
  expect(within(details).getByText('framework-fw13-v1')).toBeInTheDocument()
  expect(within(details).getByText('Ryzen')).toBeInTheDocument()
  expect(within(details).getByText('32 GB')).toBeInTheDocument()
  expect((await axe.run(container)).violations).toEqual([])
})

test('updates specifications and import provenance without erasing model defaults', async () => {
  const model = {
    id: 'model-edit', organizationId: 'example-org', manufacturer: 'Framework', name: 'Laptop 13',
    modelNumber: 'FW13', kind: 'laptop', vendorIdentifier: 'vendor-fw13',
    specifications: { CPU: 'Ryzen', Memory: '32 GB' }, supportUrl: 'https://support.example.test/fw13',
    warrantyMonths: 36, usefulLifeMonths: 48, status: 'active', sourceSystemId: 'model-import',
    sourceRecordId: 'framework-fw13-v1', instanceCount: 0, revision: 1,
    createdAt: '2026-08-12T12:00:00Z', updatedAt: '2026-08-12T12:00:00Z',
  }
  const updated = { ...model, specifications: { CPU: 'Ryzen AI', Memory: '32 GB' }, revision: 2, updatedAt: '2026-08-12T13:00:00Z' }
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path === '/api/v1/asset-models?limit=100') return jsonResponse({ items: [model] })
    if (path === '/api/v1/asset-models/model-edit' && init?.method === 'PUT') return jsonResponse(updated)
    if (path.includes('/related')) return jsonResponse(emptyRelated)
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  const { container } = render(<AtlasInventory assets={[]} csrfToken="csrf-token" onAssetsChange={() => undefined} permissions={['assets.read', 'assets.write']} />)

  openAtlasTab('Models')
  fireEvent.click(await screen.findByRole('button', { name: 'Edit' }))
  const editForm = within(screen.getByRole('form', { name: 'Edit model' }))
  expect(editForm.getByLabelText(/^Source system ID/)).toHaveValue('model-import')
  expect(editForm.getByLabelText(/^Source record ID/)).toHaveValue('framework-fw13-v1')
  expect(editForm.getByLabelText('Specification 1 name')).toHaveValue('CPU')
  expect(editForm.getByLabelText('Specification 2 name')).toHaveValue('Memory')
  fireEvent.change(editForm.getByLabelText('Specification 1 value'), { target: { value: 'Ryzen AI' } })
  fireEvent.click(editForm.getByRole('button', { name: 'Save model' }))

  expect(await screen.findByText('Model updated.')).toBeInTheDocument()
  const request = fetchMock.mock.calls.find(([path, init]) => path === '/api/v1/asset-models/model-edit' && init?.method === 'PUT')
  expect(request?.[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'csrf-token' })
  expect(JSON.parse(String(request?.[1]?.body))).toMatchObject({
    specifications: { CPU: 'Ryzen AI', Memory: '32 GB' }, sourceSystemId: 'model-import',
    sourceRecordId: 'framework-fw13-v1', revision: 1,
  })
  expect((await axe.run(container)).violations).toEqual([])
})

test('opens model inventory with exact filters, grouped counts, and linked asset details', async () => {
  const model = {
    id: 'model-inventory', organizationId: 'example-org', manufacturer: 'Dell', name: 'PowerEdge R760',
    kind: 'server', status: 'active', instanceCount: 2, revision: 1,
    createdAt: '2026-08-12T12:00:00Z', updatedAt: '2026-08-12T12:00:00Z',
  }
  const linked = { ...asset, id: 'inventory-asset', modelId: model.id, name: 'Chicago rack server', siteId: 'site-one', departmentId: 'department-one', userId: 'user-one', deploymentNotes: 'Rack 42 production' }
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const path = String(input)
    if (path === '/api/v1/asset-models?limit=100') return jsonResponse({ items: [model] })
    if (path === '/api/v1/sites') return jsonResponse({ items: [{ id: 'site-one', name: 'Chicago' }] })
    if (path === '/api/v1/departments') return jsonResponse({ items: [{ id: 'department-one', name: 'Infrastructure' }] })
    if (path === '/api/v1/identities?limit=100') return jsonResponse({ items: [{ id: 'user-one', displayName: 'Alex Admin' }] })
    if (path === '/api/v1/buildings' || path === '/api/v1/rooms') return jsonResponse({ items: [] })
    if (path.startsWith('/api/v1/asset-models/model-inventory/inventory?')) return jsonResponse({
      modelId: model.id, totalCount: 2, filteredCount: 1, groupBy: path.includes('groupBy=site') ? 'site' : '',
      groups: path.includes('groupBy=site') ? [{ key: 'site-one', count: 1 }] : [], items: [linked],
    })
    if (path === '/api/v1/assets/inventory-asset/lifecycle') return jsonResponse({ items: [] })
    if (path === '/api/v1/assets/inventory-asset/identifiers') return jsonResponse({ items: [] })
    if (path.includes('/related')) return jsonResponse(emptyRelated)
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  const { container } = render(<AtlasInventory assets={[]} csrfToken="csrf-token" onAssetsChange={() => undefined} permissions={['assets.read', 'directory.read']} />)
  openAtlasTab('Models')
  const viewInventory = await screen.findByRole('link', { name: 'View inventory' })
  expect(viewInventory).toHaveAttribute('href', '#workspace-atlas')
  fireEvent.click(viewInventory)
  const filterForm = await screen.findByRole('form', { name: 'Filter inventory for Dell PowerEdge R760' })
  const filters = within(filterForm)
  fireEvent.change(filters.getByLabelText('Lifecycle state'), { target: { value: 'active' } })
  fireEvent.change(filters.getByLabelText('Site'), { target: { value: 'site-one' } })
  fireEvent.change(filters.getByLabelText('Asset department'), { target: { value: 'department-one' } })
  fireEvent.change(filters.getByLabelText('Primary user (asset)'), { target: { value: 'user-one' } })
  fireEvent.change(filters.getByLabelText('Deployment context'), { target: { value: 'Rack 42' } })
  fireEvent.change(filters.getByLabelText('Group matching assets'), { target: { value: 'site' } })
  fireEvent.click(filters.getByRole('button', { name: 'Apply filters' }))
  expect(await screen.findByRole('heading', { name: 'Grouped counts' })).toBeInTheDocument()
  expect(screen.getAllByText('Chicago').length).toBeGreaterThanOrEqual(2)
  expect(screen.getByText('1 matching')).toBeInTheDocument()
  const filteredRequest = fetchMock.mock.calls.map(([path]) => String(path)).find((path) => path.includes('groupBy=site')) || ''
  expect(filteredRequest).toContain('status=active')
  expect(filteredRequest).toContain('siteId=site-one')
  expect(filteredRequest).toContain('departmentId=department-one')
  expect(filteredRequest).toContain('userId=user-one')
  expect(filteredRequest).toContain('deploymentContext=Rack+42')
  expect(screen.getByText(/Effective-dated primary, additional-user, and responsible-department history remains in People/)).toBeInTheDocument()
  const assetLink = screen.getByRole('link', { name: 'Chicago rack server' })
  expect(assetLink).toHaveAttribute('href', '#workspace-atlas')
  fireEvent.click(assetLink)
  expect(await screen.findByText('Instance-specific record')).toBeInTheDocument()
  expect((await axe.run(container)).violations).toEqual([])
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
    if (path.includes('/related')) return jsonResponse(emptyRelated)
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  const onAssetsChange = vi.fn()
  const { container } = render(<AtlasInventory assets={[]} csrfToken="csrf-token" onAssetsChange={onAssetsChange} permissions={['assets.read', 'assets.write']} />)
  openAtlasTab('Models')
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
    if (path.includes('/related')) return jsonResponse(emptyRelated)
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  const onAssetsChange = vi.fn()
  render(<AtlasInventory assets={[asset]} csrfToken="csrf-token" onAssetsChange={onAssetsChange} permissions={['assets.read', 'assets.write']} />)
  fireEvent.click(screen.getByRole('button', { name: 'Open Lab server' }))
  fireEvent.click(await screen.findByRole('button', { name: 'Edit in full form' }))
  const editForm = within(screen.getByRole('form', { name: 'Edit asset' }))
  fireEvent.change(editForm.getByLabelText('Status'), { target: { value: 'retired' } })
  fireEvent.change(editForm.getByLabelText(/^Lifecycle note/), { target: { value: 'Replacement completed' } })
  fireEvent.click(editForm.getByRole('button', { name: 'Save changes' }))
  expect(await screen.findByText('Asset updated.')).toBeInTheDocument()
  const request = fetchMock.mock.calls.find(([path, init]) => path === '/api/v1/assets/asset-1' && init?.method === 'PUT')
  expect(JSON.parse(String(request?.[1]?.body))).toMatchObject({ revision: 1, status: 'retired', lifecycleNote: 'Replacement completed' })
  await waitFor(() => expect(onAssetsChange).toHaveBeenCalledWith([updated]))
})

test('edits assets in the grid, asks once for the shared lifecycle note, and writes whole records', async () => {
  const second: Asset = { ...asset, id: 'asset-2', name: 'Lab printer', assetTag: 'LAB-002', serialNumber: 'SERIAL-002' }
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (['/api/v1/sites', '/api/v1/buildings', '/api/v1/rooms', '/api/v1/departments', '/api/v1/identities?limit=100'].includes(path)) return jsonResponse({ items: [] })
    if (path === '/api/v1/assets/asset-1' && init?.method === 'PUT') return jsonResponse({ ...asset, status: 'retired', revision: 2 })
    if (path === '/api/v1/assets/asset-2' && init?.method === 'PUT') return jsonResponse({ ...second, status: 'retired', revision: 2 })
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  const onAssetsChange = vi.fn()
  render(<AtlasInventory assets={[asset, second]} csrfToken="csrf-token" onAssetsChange={onAssetsChange} permissions={['assets.read', 'assets.write']} />)

  // Select both status cells and fill the second from the first.
  fireEvent.doubleClick(screen.getByRole('gridcell', { name: 'Lab printer' }))
  const editing = screen.getByLabelText('Asset name for Lab printer')
  fireEvent.change(editing, { target: { value: 'Lab plotter' } })
  fireEvent.keyDown(editing, { key: 'Escape' })

  fireEvent.doubleClick(screen.getAllByRole('gridcell', { name: 'active' })[0])
  const status = screen.getByLabelText('Status for Lab server')
  fireEvent.change(status, { target: { value: 'retired' } })
  fireEvent.keyDown(status, { key: 'Enter' })
  fireEvent.doubleClick(screen.getByRole('gridcell', { name: 'active' }))
  const secondStatus = screen.getByLabelText('Status for Lab printer')
  fireEvent.change(secondStatus, { target: { value: 'retired' } })
  fireEvent.keyDown(secondStatus, { key: 'Enter' })

  fireEvent.click(screen.getByRole('button', { name: 'Save changes' }))
  const note = within(await screen.findByRole('form', { name: 'Lifecycle note' }))
  expect(screen.getByText('2 assets change status. Atlas stores the note with each lifecycle event.')).toBeInTheDocument()
  fireEvent.change(note.getByLabelText('Lifecycle note'), { target: { value: 'Replaced by the refresh cycle' } })
  fireEvent.click(note.getByRole('button', { name: 'Save status changes' }))

  expect(await screen.findByText('2 of 2 records saved.')).toBeInTheDocument()
  const request = fetchMock.mock.calls.find(([path, init]) => path === '/api/v1/assets/asset-1' && init?.method === 'PUT')
  // The API replaces the whole record, so untouched fields must be resent verbatim.
  expect(JSON.parse(String(request?.[1]?.body))).toMatchObject({
    name: 'Lab server', assetTag: 'LAB-001', serialNumber: 'SERIAL-001', hostname: 'lab.example.test',
    kind: 'server', status: 'retired', revision: 1, lifecycleNote: 'Replaced by the refresh cycle',
  })
  await waitFor(() => expect(onAssetsChange).toHaveBeenCalled())
})

test('rejects a pasted asset tag that another asset already uses before any request goes out', async () => {
  const second: Asset = { ...asset, id: 'asset-2', name: 'Lab printer', assetTag: 'LAB-002', serialNumber: 'SERIAL-002' }
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (init?.method && init.method !== 'GET') throw new Error(`unexpected mutation: ${path}`)
    if (['/api/v1/sites', '/api/v1/buildings', '/api/v1/rooms', '/api/v1/departments', '/api/v1/identities?limit=100'].includes(path)) return jsonResponse({ items: [] })
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<AtlasInventory assets={[asset, second]} csrfToken="csrf-token" onAssetsChange={() => undefined} permissions={['assets.read', 'assets.write']} />)

  fireEvent.doubleClick(screen.getByRole('gridcell', { name: 'LAB-002' }))
  const tag = screen.getByLabelText('Asset tag for Lab printer')
  fireEvent.change(tag, { target: { value: 'LAB-001' } })
  fireEvent.keyDown(tag, { key: 'Enter' })
  fireEvent.click(screen.getByRole('button', { name: 'Save changes' }))

  expect(await screen.findByRole('alert')).toHaveTextContent('Asset tags and serial numbers must stay unique for the organization. Already in use: LAB-001.')
  expect(fetchMock.mock.calls.every(([, init]) => (init?.method ?? 'GET') === 'GET')).toBe(true)
  expect(screen.getByText('1 cell changed in 1 record')).toBeInTheDocument()
})

test('creates rows staged past the end of the grid through the atomic bulk endpoint when they share a model', async () => {
  const model = {
    id: 'model-1', organizationId: 'example-org', manufacturer: 'Framework', name: 'Laptop 13',
    kind: 'laptop', status: 'active', instanceCount: 0, revision: 1,
    createdAt: '2026-08-12T12:00:00Z', updatedAt: '2026-08-12T12:00:00Z',
  }
  const created = [
    { ...asset, id: 'asset-9', name: 'Cart laptop 1', kind: 'laptop', modelId: 'model-1', assetTag: 'CART-001' },
    { ...asset, id: 'asset-10', name: 'Cart laptop 2', kind: 'laptop', modelId: 'model-1', assetTag: 'CART-002' },
  ]
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (['/api/v1/sites', '/api/v1/buildings', '/api/v1/rooms', '/api/v1/departments', '/api/v1/identities?limit=100'].includes(path)) return jsonResponse({ items: [] })
    if (path === '/api/v1/asset-models/model-1/assets/bulk' && init?.method === 'POST') return jsonResponse({ items: created }, 201)
    if (path.startsWith('/api/v1/asset-models')) return jsonResponse({ items: [model] })
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  const onAssetsChange = vi.fn()
  render(<AtlasInventory assets={[asset]} csrfToken="csrf-token" onAssetsChange={onAssetsChange} permissions={['assets.read', 'assets.write']} />)

  fireEvent.click(screen.getByRole('button', { name: 'Add row' }))
  fireEvent.click(screen.getByRole('button', { name: 'Add row' }))
  const block = 'Cart laptop 1\tCART-001\t\tlaptop\nCart laptop 2\tCART-002\t\tlaptop'
  fireEvent.keyDown(screen.getByRole('grid'), { key: 'ArrowDown' })
  fireEvent.paste(screen.getByRole('grid'), { clipboardData: { getData: () => block, setData: () => undefined, types: ['text/plain'] } })
  expect(screen.getByText('2 new rows')).toBeInTheDocument()
  expect(screen.getByRole('gridcell', { name: 'Lab server' })).toBeInTheDocument()

  await waitFor(() => expect(fetchMock.mock.calls.some(([path]) => String(path) === '/api/v1/asset-models?limit=100')).toBe(true))
  const gridRows = within(screen.getByRole('grid')).getAllByRole('row')
  for (const row of [-2, -1] as const) {
    fireEvent.doubleClick(within(gridRows.at(row) as HTMLElement).getAllByRole('gridcell')[18])
    fireEvent.click(await screen.findByRole('option', { name: /Framework Laptop 13/ }))
  }

  fireEvent.click(screen.getByRole('button', { name: 'Save changes' }))
  expect(await screen.findByText('2 assets created.')).toBeInTheDocument()
  const request = fetchMock.mock.calls.find(([path]) => String(path) === '/api/v1/asset-models/model-1/assets/bulk')
  expect(request?.[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'csrf-token' })
  expect(JSON.parse(String(request?.[1]?.body))).toMatchObject({
    items: [
      { name: 'Cart laptop 1', assetTag: 'CART-001', kind: 'laptop', modelId: 'model-1' },
      { name: 'Cart laptop 2', assetTag: 'CART-002', kind: 'laptop', modelId: 'model-1' },
    ],
  })
  await waitFor(() => expect(onAssetsChange).toHaveBeenCalled())
})

test('opens the authorized asset detail from the explicit Atlas Codes scan-to-find workflow', async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path === '/api/v1/asset-models?limit=100') return jsonResponse({ items: [] })
    if (path === '/api/v1/asset-identifiers/resolve' && init?.method === 'POST') return jsonResponse({ assetId: asset.id })
    if (path === '/api/v1/assets/asset-1/lifecycle') return jsonResponse({ items: [] })
    if (path === '/api/v1/assets/asset-1/identifiers') return jsonResponse({ items: [] })
    if (path.includes('/related')) return jsonResponse(emptyRelated)
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<AtlasInventory assets={[asset]} csrfToken="csrf-token" onAssetsChange={() => undefined} permissions={['assets.read']} />)

  openAtlasTab('Scan')
  fireEvent.change(await screen.findByLabelText('Symbology'), { target: { value: 'qr' } })
  fireEvent.change(screen.getByLabelText('Scanned or entered value'), { target: { value: 'opaque-asset-route' } })
  fireEvent.click(screen.getByRole('button', { name: 'Find asset' }))

  expect(await screen.findByText(/Identifier matched/)).toBeInTheDocument()
  const detail = screen.getByRole('heading', { name: 'Asset details' }).closest('aside') as HTMLElement
  expect(within(detail).getByText('Lab server')).toBeInTheDocument()
  expect(within(detail).getByText('Instance-specific record')).toBeInTheDocument()
  expect(JSON.parse(String(fetchMock.mock.calls.find(([path]) => path === '/api/v1/asset-identifiers/resolve')?.[1]?.body))).toEqual({ symbology: 'qr', value: 'opaque-asset-route' })
})

test('keeps Labels available for writers and restores Assets after using a model', async () => {
  const model = {
    id: 'model-1', organizationId: 'example-org', manufacturer: 'Framework', name: 'Laptop 13',
    kind: 'laptop', status: 'active', instanceCount: 0, revision: 1,
    createdAt: '2026-08-12T12:00:00Z', updatedAt: '2026-08-12T12:00:00Z',
  }
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const path = String(input)
    if (path === '/api/v1/asset-models?limit=100') return jsonResponse({ items: [model] })
    if (['/api/v1/sites', '/api/v1/buildings', '/api/v1/rooms', '/api/v1/departments', '/api/v1/identities?limit=100'].includes(path)) return jsonResponse({ items: [] })
    if (path.includes('/related')) return jsonResponse(emptyRelated)
    throw new Error(`unexpected request: ${path}`)
  }))
  const { container } = render(<AtlasInventory assets={[asset]} csrfToken="csrf-token" onAssetsChange={() => undefined} permissions={['assets.read', 'assets.write']} />)
  expect(screen.getByRole('tab', { name: 'Labels' })).toBeInTheDocument()
  openAtlasTab('Labels')
  expect(screen.getByRole('heading', { name: 'Atlas Codes — Label printing' })).toBeInTheDocument()
  openAtlasTab('Models')
  fireEvent.click(await screen.findByRole('button', { name: 'Use' }))
  expect(screen.getByRole('tab', { name: 'Assets' })).toHaveAttribute('aria-selected', 'true')
  expect(screen.getByRole('form', { name: 'Add asset' })).toBeInTheDocument()
  expect((await axe.run(container)).violations).toEqual([])
})

test('connects configured tags on a selected asset and model from Atlas', async () => {
  const model = {
    id: 'model-1', organizationId: 'example-org', manufacturer: 'Framework', name: 'Laptop 13',
    kind: 'laptop', status: 'active', instanceCount: 0, revision: 1,
    createdAt: '2026-08-12T12:00:00Z', updatedAt: '2026-08-12T12:00:00Z',
  }
  const tag = {
    id: 'loaner', organizationId: 'example-org', name: 'Loaner pool', valueKind: 'flag',
    applicableRecordTypes: ['atlas.asset', 'atlas.model'], status: 'active', revision: 1,
  }
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path === '/api/v1/asset-models?limit=100') return jsonResponse({ items: [model] })
    if (path === '/api/v1/asset-models/model-1/inventory?limit=100') return jsonResponse({
      modelId: model.id, totalCount: 0, filteredCount: 0, groups: [], items: [],
    })
    if (path === '/api/v1/labels/definitions') return jsonResponse({ items: [tag] })
    if (path.endsWith('/assignments') && !init?.method) return jsonResponse({ items: [] })
    if (path.endsWith('/assignments/loaner') && init?.method === 'PUT') {
      const body = JSON.parse(String(init.body)) as { recordType: string; recordId: string }
      return jsonResponse({ definitionId: tag.id, recordType: body.recordType, recordId: body.recordId, revision: 1 })
    }
    if (path === '/api/v1/assets/asset-1/lifecycle') return jsonResponse({ items: [] })
    if (path === '/api/v1/assets/asset-1/identifiers') return jsonResponse({ items: [] })
    if (path.includes('/related')) return jsonResponse(emptyRelated)
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<AtlasInventory assets={[asset]} csrfToken="csrf-token" onAssetsChange={() => undefined} permissions={['assets.read', 'labels.read', 'labels.write']} />)

  fireEvent.click(screen.getByRole('button', { name: 'Open Lab server' }))
  fireEvent.click(await screen.findByRole('button', { name: 'Connect tag' }))
  expect(await screen.findByText('Connected “Loaner pool” to Lab server.')).toBeInTheDocument()

  openAtlasTab('Models')
  fireEvent.click(await screen.findByRole('link', { name: 'View inventory' }))
  fireEvent.click(await screen.findByRole('button', { name: 'Connect tag' }))
  expect(await screen.findByText('Connected “Loaner pool” to Framework Laptop 13.')).toBeInTheDocument()
  const bodies = fetchMock.mock.calls.filter(([, init]) => init?.method === 'PUT').map(([, init]) => JSON.parse(String(init?.body)))
  expect(bodies).toEqual(expect.arrayContaining([
    expect.objectContaining({ recordType: 'atlas.asset', recordId: 'asset-1' }),
    expect.objectContaining({ recordType: 'atlas.model', recordId: 'model-1' }),
  ]))
})

test('opens the matching asset editor when Mesh focuses an Atlas record', async () => {
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const path = String(input)
    if (path === '/api/v1/asset-models?limit=100') return jsonResponse({ items: [] })
    if (['/api/v1/sites', '/api/v1/buildings', '/api/v1/rooms', '/api/v1/departments', '/api/v1/identities?limit=100'].includes(path)) return jsonResponse({ items: [] })
    throw new Error(`unexpected request: ${path}`)
  }))
  render(<AtlasInventory assets={[asset]} csrfToken="csrf-token" focusRecord={{ area: 'atlas', kind: 'asset', nonce: 1, recordId: 'asset-1' }} onAssetsChange={() => undefined} permissions={['assets.read', 'assets.write']} />)
  const form = await screen.findByRole('form', { name: 'Edit asset' })
  expect(within(form).getByLabelText('Asset name')).toHaveValue('Lab server')
})

test('shows configured tag columns in the asset grid and saves multiselect assignments', async () => {
  const deploymentTag = {
    id: 'deployment-group',
    organizationId: 'example-org',
    name: 'Deployment group',
    valueKind: 'multiselect',
    applicableRecordTypes: ['atlas.asset'],
    options: ['Lab A', 'Office refresh'],
    status: 'active',
    revision: 1,
  }
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path === '/api/v1/asset-models?limit=100') return jsonResponse({ items: [] })
    if (['/api/v1/sites', '/api/v1/buildings', '/api/v1/rooms', '/api/v1/departments', '/api/v1/identities?limit=100'].includes(path)) return jsonResponse({ items: [] })
    if (path === '/api/v1/labels/definitions') return jsonResponse({ items: [deploymentTag] })
    if (path === '/api/v1/labels/definitions/deployment-group/assignments?recordType=atlas.asset') return jsonResponse({ items: [] })
    if (path.endsWith('/assignments/deployment-group') && init?.method === 'PUT') {
      const body = JSON.parse(String(init.body)) as { recordId: string; values?: string[] }
      return jsonResponse({
        definitionId: deploymentTag.id,
        recordType: 'atlas.asset',
        recordId: body.recordId,
        values: body.values,
        revision: 1,
      })
    }
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<AtlasInventory assets={[asset]} csrfToken="csrf-token" onAssetsChange={() => undefined} permissions={['assets.read', 'labels.read', 'labels.write']} />)
  expect(await screen.findByRole('columnheader', { name: 'Deployment group' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Add tag column' })).toBeInTheDocument()
})
