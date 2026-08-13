import axe from 'axe-core'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import PatternsManager from './PatternsManager'

// Requirement: REQ-PATTERNS-001. Feature: templates.schemas. GitHub: #8.

const builtIn = {
  id: 'builtin-atlas-asset', recordType: 'atlas.asset', name: 'Atlas asset', description: 'Register an asset.',
  version: 1, builtIn: true, status: 'active',
  fields: [{ key: 'name', label: 'Asset name', help: 'Shown throughout inventory.', type: 'text', required: true, accessibleLabel: 'Asset name', csvHeader: 'name' }],
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

beforeEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

test('shows accessible versioned schema metadata and CSV export', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => jsonResponse({ items: [builtIn] })))
  const { container } = render(<PatternsManager csrfToken="csrf-value" />)

  expect(await screen.findByRole('heading', { name: 'Versioned record templates' })).toBeInTheDocument()
  expect(screen.getByText('Asset name')).toBeInTheDocument()
  expect(screen.getAllByText('text')).not.toHaveLength(0)
  expect(screen.getByRole('link', { name: 'Download CSV template' })).toHaveAttribute('href', '/api/v1/templates/builtin-atlas-asset/template.csv?version=1')
  expect((await axe.run(container)).violations).toEqual([])
})

test('creates a typed custom template and copies a built-in version', async () => {
  const custom = {
    ...builtIn, id: 'custom-record', recordType: 'exchange.row', name: 'Custom record', builtIn: false,
    fields: [{ key: 'state', label: 'State', help: 'Choose an intake state.', type: 'enum', required: true, options: ['new', 'ready'], accessibleLabel: 'State', csvHeader: 'state' }],
  }
  const copied = { ...builtIn, id: 'asset-copy', name: 'Asset copy', builtIn: false }
  let items = [builtIn]
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path === '/api/v1/templates' && !init?.method) return jsonResponse({ items })
    if (path === '/api/v1/templates' && init?.method === 'POST') {
      items = [...items, custom]
      return jsonResponse(custom, 201)
    }
    if (path === '/api/v1/templates/builtin-atlas-asset/copy?version=1' && init?.method === 'POST') {
      items = [...items, copied]
      return jsonResponse(copied, 201)
    }
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<PatternsManager csrfToken="csrf-value" />)
  await screen.findByText('Asset name')

  fireEvent.change(screen.getByLabelText('Name for editable copy'), { target: { value: 'Asset copy' } })
  fireEvent.click(screen.getByRole('button', { name: 'Copy this version' }))
  expect(await screen.findByText('Editable template copy created.')).toBeInTheDocument()
  const copyCall = fetchMock.mock.calls.find(([path]) => String(path).includes('/copy?version=1'))
  expect(copyCall?.[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'csrf-value' })

  fireEvent.click(screen.getByText('Create a custom template'))
  fireEvent.change(screen.getByLabelText('Template name'), { target: { value: 'Custom record' } })
  fireEvent.change(screen.getByLabelText('Record type'), { target: { value: 'exchange.row' } })
  fireEvent.change(screen.getByLabelText('Field key'), { target: { value: 'state' } })
  fireEvent.change(screen.getByLabelText('Field label'), { target: { value: 'State' } })
  fireEvent.change(screen.getByLabelText('Field type'), { target: { value: 'enum' } })
  fireEvent.change(screen.getByLabelText('Options, comma separated'), { target: { value: 'new, ready' } })
  fireEvent.click(screen.getByRole('button', { name: 'Create custom template' }))
  expect(await screen.findByText('Custom template version 1 created.')).toBeInTheDocument()
  const createCall = fetchMock.mock.calls.find(([path, init]) => path === '/api/v1/templates' && init?.method === 'POST')
  expect(JSON.parse(String(createCall?.[1]?.body))).toMatchObject({
    name: 'Custom record', recordType: 'exchange.row', fields: [{ key: 'state', label: 'State', type: 'enum', options: ['new', 'ready'] }],
  })
  await waitFor(() => expect(screen.getByText(/exchange.row/)).toBeInTheDocument())
})
