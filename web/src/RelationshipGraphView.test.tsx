import axe from 'axe-core'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import RelationshipGraphView, { readRelationshipGraph } from './RelationshipGraphView'

// Requirement: REQ-DIRECTORY-EXPANSION-008. Feature: threads.relationships.

const nodes = [
  { id: 'organization:example-org', kind: 'organization', label: 'Example Org' },
  { id: 'group:alpha', kind: 'group', label: 'Alpha group' },
  { id: 'group:beta', kind: 'group', label: 'Beta group' },
  { id: 'group:isolated', kind: 'group', label: 'Isolated group' },
]
const edges = [
  { id: 'edge-one', from: 'group:alpha', to: 'group:beta', kind: 'member_of' },
  { id: 'edge-one-duplicate', from: 'group:alpha', to: 'group:beta', kind: 'member_of' },
  { id: 'edge-two', from: 'group:beta', to: 'group:alpha', kind: 'member_of' },
]

function response(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

beforeEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

test('renders cycles, deduplicates repeated relationships, and provides an accessible table fallback', async () => {
  const fetchMock = vi.fn(async () => response({ nodes, edges }))
  vi.stubGlobal('fetch', fetchMock)
  const { container } = render(<RelationshipGraphView permissions={['directory.read']} />)

  expect(await screen.findByText(/Relationship graph loaded with 4 records and 2 relationships/)).toBeInTheDocument()
  expect(await screen.findByRole('img', { name: 'Interactive relationship graph' })).toBeInTheDocument()
  const relationships = screen.getByRole('region', { name: 'Visible relationships' })
  expect(within(relationships).getAllByText('Member Of')).toHaveLength(2)
  expect(within(relationships).getAllByText('Alpha group')).toHaveLength(2)
  const disconnected = screen.getByRole('region', { name: 'Disconnected visible records' })
  expect(within(disconnected).getByText('Example Org')).toBeInTheDocument()
  expect(within(disconnected).getByText('Isolated group')).toBeInTheDocument()
  expect(relationships).toHaveAttribute('tabindex', '0')
  expect(disconnected).toHaveClass('overflow-x-auto')
  expect((await axe.run(container)).violations).toEqual([])

  fireEvent.change(screen.getByLabelText('Search record names'), { target: { value: 'alpha' } })
  fireEvent.change(screen.getByLabelText('Record type'), { target: { value: 'group' } })
  fireEvent.change(screen.getByLabelText('Relationship type'), { target: { value: 'member_of' } })
  fireEvent.change(screen.getByLabelText('Maximum records'), { target: { value: '25' } })
  fireEvent.click(screen.getByRole('button', { name: 'Apply graph filters' }))
  await waitFor(() => expect(fetchMock).toHaveBeenLastCalledWith('/api/v1/graph?search=alpha&kind=group&relationship=member_of&limit=25', expect.any(Object)))
  fireEvent.change(screen.getByLabelText('Maximum records'), { target: { value: 'all' } })
  fireEvent.click(screen.getByRole('button', { name: 'Apply graph filters' }))
  await waitFor(() => expect(fetchMock).toHaveBeenLastCalledWith('/api/v1/graph?search=alpha&kind=group&relationship=member_of&limit=all', expect.any(Object)))
})

test('renders a stable empty state and resets filters', async () => {
  const fetchMock = vi.fn(async () => response({ nodes: [], edges: [] }))
  vi.stubGlobal('fetch', fetchMock)
  render(<RelationshipGraphView permissions={['directory.read']} />)
  expect(await screen.findByText('No visible records match these graph filters.')).toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('Search record names'), { target: { value: 'missing' } })
  fireEvent.click(screen.getByRole('button', { name: 'Reset graph filters' }))
  expect(screen.getByLabelText('Search record names')).toHaveValue('')
  await waitFor(() => expect(fetchMock).toHaveBeenLastCalledWith('/api/v1/graph?limit=100', expect.any(Object)))
})

test('rejects malformed graph responses and focuses the safe error', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => response({
    nodes: [{ id: 'person:visible', kind: 'person', label: 'Visible person' }],
    edges: [{ id: 'bad-edge', from: 'person:visible', to: 'person:hidden', kind: 'belongs_to' }],
  })))
  render(<RelationshipGraphView permissions={['directory.read']} />)
  const alert = await screen.findByRole('alert')
  expect(alert).toHaveTextContent('The relationship graph could not be loaded.')
  await waitFor(() => expect(alert).toHaveFocus())
  expect(screen.queryByText('Visible person')).not.toBeInTheDocument()
})

test('accepts bounded Unicode labels by code point rather than UTF-16 code unit', async () => {
  const unicodeLabel = '🧭'.repeat(200)
  vi.stubGlobal('fetch', vi.fn(async () => response({
    nodes: [{ id: 'site:unicode', kind: 'site', label: unicodeLabel }],
    edges: [],
  })))
  render(<RelationshipGraphView permissions={['directory.read']} />)
  expect(await screen.findByText(/Relationship graph loaded with 1 record and 0 relationships/)).toBeInTheDocument()
  expect(screen.getByText(unicodeLabel)).toBeInTheDocument()
})

test('accepts graph responses above the former 500-node client cap', () => {
  const nodes = Array.from({ length: 501 }, (_, index) => ({
    id: `asset:asset-${index}`,
    kind: 'asset',
    label: `Asset ${index}`,
  }))
  expect(readRelationshipGraph({ nodes, edges: [] }).nodes).toHaveLength(501)
})

test('does not request graph data without directory read permission', () => {
  const fetchMock = vi.fn()
  vi.stubGlobal('fetch', fetchMock)
  render(<RelationshipGraphView permissions={[]} />)
  expect(screen.getByText('Directory read permission is required to load the relationship graph.')).toBeInTheDocument()
  expect(fetchMock).not.toHaveBeenCalled()
})
