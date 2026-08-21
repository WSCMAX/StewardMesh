import axe from 'axe-core'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import RecordTags from './RecordTags'

// Requirements: REQ-LABELS-001, A11Y-001. Feature: identity.labels.

const studioArts = {
  id: 'studio-arts', organizationId: 'example-org', name: 'Student program', valueKind: 'select',
  applicableRecordTypes: ['atlas.asset', 'atlas.model'], options: ['Studio Arts', 'Graphic Design'],
  status: 'active', revision: 1,
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

beforeEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

test('connects and removes a tag on an Atlas asset', async () => {
  let assigned = false
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path === '/api/v1/labels/definitions') return jsonResponse({ items: [studioArts] })
    if (path.startsWith('/api/v1/labels/records/atlas.asset/asset-1/assignments') && !init?.method) {
      return jsonResponse({ items: assigned ? [{ definitionId: studioArts.id, recordType: 'atlas.asset', recordId: 'asset-1', valueText: 'Studio Arts', revision: 1 }] : [] })
    }
    if (path.startsWith('/api/v1/labels/records/atlas.asset/asset-1/assignments/studio-arts') && init?.method === 'PUT') {
      assigned = true
      return jsonResponse({ definitionId: studioArts.id, recordType: 'atlas.asset', recordId: 'asset-1', valueText: 'Studio Arts', revision: 1 })
    }
    if (path.startsWith('/api/v1/labels/records/atlas.asset/asset-1/assignments/studio-arts') && init?.method === 'DELETE') {
      assigned = false
      return new Response(null, { status: 204 })
    }
    throw new Error(`unexpected request: ${path} ${init?.method ?? 'GET'}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  const { container } = render(<RecordTags csrfToken="csrf-token" permissions={['labels.read', 'labels.write']} recordId="asset-1" recordName="Lab server" recordType="atlas.asset" />)

  fireEvent.change(await screen.findByLabelText('Tag'), { target: { value: 'studio-arts' } })
  fireEvent.change(screen.getByLabelText('Value'), { target: { value: 'Studio Arts' } })
  fireEvent.click(screen.getByRole('button', { name: 'Connect tag' }))
  expect(await screen.findByText('Connected “Student program” to Lab server.')).toBeInTheDocument()
  expect(screen.getByLabelText('Connected tags')).toHaveTextContent('Student program: Studio Arts')
  const save = fetchMock.mock.calls.find(([, init]) => init?.method === 'PUT')
  expect(save?.[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'csrf-token' })
  expect(JSON.parse(String(save?.[1]?.body))).toMatchObject({ recordType: 'atlas.asset', recordId: 'asset-1', valueText: 'Studio Arts' })

  fireEvent.click(screen.getByRole('button', { name: 'Remove' }))
  expect(await screen.findByText('Removed “Student program” from Lab server.')).toBeInTheDocument()
  expect(String(fetchMock.mock.calls.find(([, init]) => init?.method === 'DELETE')?.[0])).toContain('revision=1')
  expect((await axe.run(container)).violations).toEqual([])
})

test('connects a tag to an Atlas model and stays hidden without labels.read', async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path === '/api/v1/labels/definitions') return jsonResponse({ items: [studioArts] })
    if (path.startsWith('/api/v1/labels/records/atlas.model/model-1/assignments') && !init?.method) return jsonResponse({ items: [] })
    if (path.startsWith('/api/v1/labels/records/atlas.model/model-1/assignments/studio-arts') && init?.method === 'PUT') {
      return jsonResponse({ definitionId: studioArts.id, recordType: 'atlas.model', recordId: 'model-1', valueText: 'Graphic Design', revision: 1 })
    }
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  const { rerender } = render(<RecordTags csrfToken="csrf-token" permissions={['assets.read']} recordId="model-1" recordName="Framework Laptop 13" recordType="atlas.model" />)
  expect(screen.queryByRole('heading', { name: 'Tags' })).not.toBeInTheDocument()
  expect(fetchMock).not.toHaveBeenCalled()

  rerender(<RecordTags csrfToken="csrf-token" permissions={['labels.read', 'labels.write']} recordId="model-1" recordName="Framework Laptop 13" recordType="atlas.model" />)
  fireEvent.change(await screen.findByLabelText('Value'), { target: { value: 'Graphic Design' } })
  fireEvent.click(screen.getByRole('button', { name: 'Connect tag' }))
  expect(await screen.findByText('Connected “Student program” to Framework Laptop 13.')).toBeInTheDocument()
  await waitFor(() => expect(JSON.parse(String(fetchMock.mock.calls.find(([, init]) => init?.method === 'PUT')?.[1]?.body))).toMatchObject({ recordType: 'atlas.model', recordId: 'model-1' }))
})
