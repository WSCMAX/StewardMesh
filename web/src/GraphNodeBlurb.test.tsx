import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import GraphNodeBlurb from './GraphNodeBlurb'
import type { GraphEdge, GraphNode } from './InteractiveRelationshipGraph'

// Requirement: REQ-DIRECTORY-EXPANSION-008. Feature: threads.relationships.

const assetNode: GraphNode = {
  id: 'asset:asset-1',
  kind: 'asset',
  label: 'Lab server',
  attributes: { source: 'atlas', status: 'active' },
}
const edges: GraphEdge[] = [
  { id: 'edge-1', from: 'organization:org', to: 'asset:asset-1', kind: 'contains' },
]
const nodesByID = new Map<string, GraphNode>([
  [assetNode.id, assetNode],
  ['organization:org', { id: 'organization:org', kind: 'organization', label: 'Example Org' }],
])

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

beforeEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

test('loads editable asset fields and a link into Atlas', async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path === '/api/v1/assets/asset-1' && (!init?.method || init.method === 'GET')) {
      return jsonResponse({
        id: 'asset-1', organizationId: 'example-org', name: 'Lab server', kind: 'server',
        assetTag: 'LAB-001', serialNumber: 'SERIAL-001', hostname: 'lab.example.test',
        status: 'active', revision: 1, createdAt: '2026-08-10T12:00:00Z', updatedAt: '2026-08-10T12:00:00Z',
      })
    }
    if (path === '/api/v1/assets/asset-1' && init?.method === 'PUT') {
      const body = JSON.parse(String(init.body)) as { name: string; revision: number }
      return jsonResponse({
        id: 'asset-1', organizationId: 'example-org', name: body.name, kind: 'server',
        assetTag: 'LAB-001', serialNumber: 'SERIAL-001', hostname: 'lab.example.test',
        status: 'active', revision: body.revision + 1, createdAt: '2026-08-10T12:00:00Z', updatedAt: '2026-08-16T12:00:00Z',
      })
    }
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  const onOpenRecord = vi.fn()
  const onNodeUpdated = vi.fn()
  render(
    <GraphNodeBlurb
      csrfToken="csrf-token-with-at-least-thirty-two-characters"
      edges={edges}
      focusNodeID=""
      node={assetNode}
      nodesByID={nodesByID}
      onClearFocus={() => undefined}
      onClose={() => undefined}
      onFocusNode={() => undefined}
      onNodeUpdated={onNodeUpdated}
      onOpenRecord={onOpenRecord}
      onSelectNode={() => undefined}
      permissions={['assets.read', 'assets.write']}
    />,
  )

  expect(await screen.findByLabelText('Name')).toHaveValue('Lab server')
  expect(screen.getByLabelText('Asset tag')).toHaveValue('LAB-001')
  fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Lab server refreshed' } })
  fireEvent.click(screen.getByRole('button', { name: 'Save asset' }))
  await waitFor(() => expect(onNodeUpdated).toHaveBeenCalledWith(expect.objectContaining({ label: 'Lab server refreshed' })))
  expect(screen.getByRole('status')).toHaveTextContent('Asset updated.')

  fireEvent.click(screen.getByRole('link', { name: 'Edit this asset in Atlas' }))
  expect(onOpenRecord).toHaveBeenCalledWith(assetNode)
})
