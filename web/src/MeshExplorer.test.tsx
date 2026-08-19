import axe from 'axe-core'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import MeshExplorer from './MeshExplorer'

// Requirement: REQ-DIRECTORY-EXPANSION-008. Feature: threads.relationships.

const nodes = [
  { id: 'organization:example-org', kind: 'organization', label: 'Example Org', attributes: { status: 'active' } },
  { id: 'asset:a', kind: 'asset', label: 'Lab Mac 01', attributes: { source: 'atlas', status: 'active' } },
  { id: 'asset:b', kind: 'asset', label: 'Lab Mac 02', attributes: { source: 'atlas', status: 'active' } },
  { id: 'model:m1', kind: 'model', label: 'MacBook Pro 14', attributes: { source: 'atlas', status: 'active' } },
  { id: 'purchase_order:po-1', kind: 'purchase_order', label: 'PO-1001', attributes: { source: 'ledger', status: 'active' } },
  { id: 'vendor:vendor-1', kind: 'vendor', label: 'Acme Supply', attributes: { source: 'ledger', status: 'active' } },
  { id: 'label:goal-tag', kind: 'label', label: 'Campus refresh', attributes: { source: 'labels' } },
  { id: 'person:ada', kind: 'person', label: 'Ada Lovelace', attributes: { source: 'people', status: 'active' } },
]
const edges = [
  { id: 'edge-po-vendor', from: 'purchase_order:po-1', to: 'vendor:vendor-1', kind: 'supplied_by' },
  { id: 'edge-po-label', from: 'purchase_order:po-1', to: 'label:goal-tag', kind: 'tagged_with' },
]

function response(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

beforeEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

test('loads the mesh graph, type filters, legend, sliders, and data table', async () => {
  const fetchMock = vi.fn(async () => response({ nodes, edges, sources: ['people', 'ledger', 'labels'] }))
  vi.stubGlobal('fetch', fetchMock)
  const { container } = render(<MeshExplorer permissions={['directory.read', 'finance.read', 'labels.read']} />)

  expect(await screen.findByText(/Mesh graph loaded with 8 records and 2 relationships/)).toBeInTheDocument()
  expect(screen.getByText(/Atlas inventory in this graph: 2 assets, 1 model\./)).toBeInTheDocument()
  expect(screen.getByLabelText('Maximum records')).toContainHTML('<option value="2000">2000</option>')
  expect(screen.getByRole('img', { name: 'Interactive relationship graph' })).toBeInTheDocument()
  expect(screen.getByRole('list', { name: 'Record type legend' })).toBeInTheDocument()
  expect(screen.getByLabelText(/Zoom/)).toBeInTheDocument()
  expect(screen.getByLabelText(/Spacing/)).toBeInTheDocument()
  expect(screen.getByLabelText(/Gravity/)).toBeInTheDocument()
  expect(screen.getByLabelText(/Repel/)).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Fullscreen' })).toBeInTheDocument()
  expect(screen.getByText(/Included products: People, Ledger, Tags/)).toBeInTheDocument()

  fireEvent.click(screen.getByRole('button', { name: 'Show graph options' }))
  fireEvent.click(within(screen.getByRole('group', { name: 'Record types to graph' })).getByRole('checkbox', { name: 'Person' }))
  expect(document.querySelector('[data-node-id="person:ada"]')).toBeNull()
  expect(document.querySelector('[data-node-id="purchase_order:po-1"]')).not.toBeNull()

  fireEvent.click(screen.getByRole('radio', { name: 'Product' }))
  fireEvent.click(within(screen.getByRole('group', { name: 'Record types to graph' })).getByRole('checkbox', { name: 'Purchase order' }))
  fireEvent.click(screen.getByRole('tab', { name: 'Data' }))
  const records = screen.getByRole('region', { name: 'Mesh records' })
  expect(within(records).getByText('Acme Supply')).toBeInTheDocument()
  expect(within(records).queryByText('PO-1001')).not.toBeInTheDocument()
  expect(screen.getByRole('region', { name: 'Mesh relationships' })).toBeInTheDocument()
  expect((await axe.run(container)).violations).toEqual([])

  fireEvent.click(screen.getByRole('tab', { name: 'Graph' }))
  fireEvent.change(screen.getByLabelText('Search record names'), { target: { value: 'acme' } })
  fireEvent.click(screen.getByRole('button', { name: 'Apply graph filters' }))
  await waitFor(() => expect(fetchMock).toHaveBeenLastCalledWith('/api/v1/mesh/graph?search=acme&kinds=organization%2Csite%2Cbuilding%2Croom%2Cdepartment%2Cshared%2Cpublic%2Clab%2Cgroup%2Csubject%2Casset%2Cmodel%2Cvendor%2Ccontract%2Cbudget%2Ccommitment%2Cproduct%2Cversion%2Clicense%2Clabel%2Cgoal%2Cdocument%2Cplan&limit=100', expect.any(Object)))
  fireEvent.change(screen.getByLabelText('Maximum records'), { target: { value: 'all' } })
  fireEvent.click(screen.getByRole('button', { name: 'Apply graph filters' }))
  await waitFor(() => expect(fetchMock).toHaveBeenLastCalledWith('/api/v1/mesh/graph?search=acme&kinds=organization%2Csite%2Cbuilding%2Croom%2Cdepartment%2Cshared%2Cpublic%2Clab%2Cgroup%2Csubject%2Casset%2Cmodel%2Cvendor%2Ccontract%2Cbudget%2Ccommitment%2Cproduct%2Cversion%2Clicense%2Clabel%2Cgoal%2Cdocument%2Cplan&limit=all', expect.any(Object)))
})

test('lets a finance-only operator load purchase orders without directory access', async () => {
  const fetchMock = vi.fn(async () => response({
    nodes: [{ id: 'purchase_order:po-1', kind: 'purchase_order', label: 'PO-1001', attributes: { source: 'ledger' } }],
    edges: [],
    sources: ['ledger'],
  }))
  vi.stubGlobal('fetch', fetchMock)
  render(<MeshExplorer permissions={['finance.read']} />)
  expect(await screen.findByText(/Mesh graph loaded with 1 record and 0 relationships from Ledger/)).toBeInTheDocument()
  expect(document.querySelector('[data-node-id="purchase_order:po-1"]')).not.toBeNull()
})

test('rejects malformed mesh responses and focuses the safe error', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => response({
    nodes: [{ id: 'purchase_order:visible', kind: 'purchase_order', label: 'Visible PO' }],
    edges: [{ id: 'bad-edge', from: 'purchase_order:visible', to: 'vendor:hidden', kind: 'supplied_by' }],
    sources: ['ledger'],
  })))
  render(<MeshExplorer permissions={['finance.read']} />)
  const alert = await screen.findByRole('alert')
  expect(alert).toHaveTextContent('The mesh graph could not be loaded.')
  await waitFor(() => expect(alert).toHaveFocus())
  expect(screen.queryByText('Visible PO')).not.toBeInTheDocument()
})

test('opens a node inspector with a link into the owning product area', async () => {
  const fetchMock = vi.fn(async () => response({ nodes, edges, sources: ['people', 'ledger', 'labels'] }))
  vi.stubGlobal('fetch', fetchMock)
  const onOpenRecord = vi.fn()
  render(<MeshExplorer onOpenRecord={onOpenRecord} permissions={['directory.read', 'finance.read', 'labels.read']} />)
  expect(await screen.findByText(/Mesh graph loaded with 8 records and 2 relationships/)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('tab', { name: 'Data' }))
  fireEvent.click(screen.getByRole('button', { name: 'Open PO-1001' }))
  expect(screen.getByRole('heading', { name: 'PO-1001' })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('link', { name: 'Open in Ledger' }))
  expect(onOpenRecord).toHaveBeenCalledWith(expect.objectContaining({ id: 'purchase_order:po-1', kind: 'purchase_order' }))
})

test('hides a type from the legend and restores it', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => response({ nodes, edges, sources: ['people', 'ledger', 'labels'] })))
  render(<MeshExplorer permissions={['directory.read', 'finance.read', 'labels.read']} />)
  expect(await screen.findByText(/Mesh graph loaded with 8 records and 2 relationships/)).toBeInTheDocument()
  expect(document.querySelector('[data-node-id="person:ada"]')).not.toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Hide Person from the graph' }))
  expect(document.querySelector('[data-node-id="person:ada"]')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Show Person' }))
  expect(document.querySelector('[data-node-id="person:ada"]')).not.toBeNull()
})

test('adds product hubs and grouped records as extra chart nodes', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => response({ nodes, edges, sources: ['people', 'ledger', 'labels'] })))
  render(<MeshExplorer permissions={['directory.read', 'finance.read', 'labels.read']} />)
  expect(await screen.findByText(/Mesh graph loaded with 8 records and 2 relationships/)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Show Atlas' }))
  await waitFor(() => expect(document.querySelector('[data-node-id="source_group:atlas"]')).not.toBeNull())
  fireEvent.change(screen.getByLabelText('Group as nodes'), { target: { value: 'type' } })
  await waitFor(() => expect(document.querySelector('[data-node-id="chart_group:type:asset"]')).not.toBeNull())
})

test('exposes the Atlas query editor and group-by dropdown on the data tab', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => response({ nodes, edges, sources: ['people', 'ledger', 'labels'] })))
  render(<MeshExplorer permissions={['directory.read', 'finance.read', 'labels.read']} />)
  expect(await screen.findByText(/Mesh graph loaded with 8 records and 2 relationships/)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('tab', { name: 'Data' }))
  expect(screen.getAllByRole('button', { name: 'Filter' }).length).toBeGreaterThan(0)
  expect(screen.getByLabelText('Group Mesh records')).toBeInTheDocument()
  expect(screen.getAllByRole('button', { name: 'Export' }).length).toBeGreaterThan(0)
  fireEvent.click(screen.getAllByRole('button', { name: 'Filter' })[0])
  expect(screen.getByLabelText('Query')).toBeInTheDocument()
})
