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
    if (path === '/api/v1/templates?includeVersions=true' && !init?.method) return jsonResponse({ items })
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
  expect(screen.getByLabelText('Record type')).toHaveAttribute('pattern', '[a-z][a-z0-9.\\-]{1,79}')
  expect(screen.getByLabelText('Field key')).toHaveAttribute('pattern', '[A-Za-z][A-Za-z0-9_.\\-]{0,63}')
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

test('generates all seven controls, validates the exact version, and round trips a bounded CSV row', async () => {
  const typed = {
    id: 'typed-workbench', recordType: 'example.row', name: 'Typed workbench', version: 4, builtIn: false, status: 'active',
    fields: [
      { key: 'title', label: 'Internal title', help: 'A portable title.', type: 'text', required: true, maximumLength: 80, accessibleLabel: 'Record title', csvHeader: 'Title' },
      { key: 'quantity', label: 'Quantity', type: 'number', required: true, minimum: 1, maximum: 500, accessibleLabel: 'Quantity', csvHeader: 'Quantity' },
      { key: 'dueOn', label: 'Due on', type: 'date', required: true, accessibleLabel: 'Due date', csvHeader: 'Due on' },
      { key: 'budgetMinor', label: 'Budget', help: 'Minor units.', type: 'money', required: true, minimum: 0, maximum: 100000, currencyField: 'currency', accessibleLabel: 'Budget amount', csvHeader: 'Budget minor' },
      { key: 'currency', label: 'Currency', type: 'enum', required: true, options: ['USD', 'EUR'], accessibleLabel: 'Currency', csvHeader: 'Currency' },
      { key: 'evidence', label: 'Evidence', type: 'attachment', required: true, allowHolding: true, referenceType: 'vault.blob', accessibleLabel: 'Evidence file', csvHeader: 'Evidence' },
      { key: 'owner', label: 'Owner', type: 'reference', required: true, allowHolding: true, referenceType: 'people.identity', accessibleLabel: 'Record owner', csvHeader: 'Owner' },
    ],
  }
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path === '/api/v1/templates?includeVersions=true' && !init?.method) return jsonResponse({ items: [typed] })
    if (path === '/api/v1/templates/typed-workbench/validate?version=4' && init?.method === 'POST') {
      const request = JSON.parse(String(init.body))
      return jsonResponse({
        status: request.missingReferences.length > 0 ? 'holding' : 'valid',
        normalizedValues: request.values,
        errors: [],
        holdingReferences: request.missingReferences.map((field: string) => ({ field, referenceType: 'people.identity', value: request.values[field] })),
      })
    }
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  const { container } = render(<PatternsManager csrfToken="exact-csrf" />)
  await screen.findByRole('heading', { name: 'Generated record workbench' })

  expect(screen.getByLabelText('Record title (required)')).toHaveAttribute('maxlength', '80')
  expect(screen.getByLabelText('Record title (required)')).toHaveAccessibleDescription('A portable title.')
  expect(screen.getByLabelText('Quantity (required)')).toHaveAttribute('min', '1')
  expect(screen.getByLabelText('Quantity (required)')).toHaveAttribute('max', '500')
  expect(screen.getByLabelText('Budget amount (required)')).toHaveAttribute('step', '1')
  expect(screen.getByLabelText('Budget amount (required)')).toHaveAttribute('min', '0')
  expect(screen.getByLabelText('Budget amount (required)')).toHaveAttribute('max', '100000')
  expect(screen.getByLabelText('Currency (required)')).toHaveDisplayValue('Choose…')

  fireEvent.change(screen.getByLabelText('Record title (required)'), { target: { value: 'Portable row' } })
  fireEvent.change(screen.getByLabelText('Quantity (required)'), { target: { value: '2.5' } })
  fireEvent.change(screen.getByLabelText('Due date (required)'), { target: { value: '2026-08-13' } })
  fireEvent.change(screen.getByLabelText('Budget amount (required)'), { target: { value: '1250' } })
  fireEvent.change(screen.getByLabelText('Currency (required)'), { target: { value: 'USD' } })
  fireEvent.change(screen.getByLabelText('Evidence file (required)'), { target: { value: 'blob-1' } })
  fireEvent.change(screen.getByLabelText('Record owner (required)'), { target: { value: 'person-1' } })
  fireEvent.click(screen.getByRole('button', { name: 'Validate exact version' }))

  expect(await screen.findByText('Typed workbench version 4 is valid.')).toBeInTheDocument()
  const validateCall = fetchMock.mock.calls.find(([path]) => String(path).includes('/validate?version=4'))
  expect(validateCall?.[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'exact-csrf' })
  expect(JSON.parse(String(validateCall?.[1]?.body))).toMatchObject({
    values: { title: 'Portable row', quantity: 2.5, dueOn: '2026-08-13', budgetMinor: 1250, currency: 'USD', evidence: 'blob-1', owner: 'person-1' },
  })

  fireEvent.click(screen.getByLabelText('Mark record owner as unresolved'))
  fireEvent.click(screen.getByRole('button', { name: 'Validate exact version' }))
  expect(await screen.findByText('The row is valid as a visible holding record.')).toBeInTheDocument()
  const validateCalls = fetchMock.mock.calls.filter(([path]) => String(path).includes('/validate?version=4'))
  expect(JSON.parse(String(validateCalls.at(-1)?.[1]?.body))).toMatchObject({ missingReferences: ['owner'] })

  fireEvent.click(screen.getByRole('button', { name: 'Prepare current row CSV' }))
  const download = await screen.findByRole('link', { name: 'Download current CSV row' })
  expect(download).toHaveAttribute('download', 'example.row-v4.csv')
  expect(download.getAttribute('href')).toContain('Title%2CQuantity%2CDue%20on%2CBudget%20minor')

  fireEvent.change(screen.getByLabelText('CSV header and row'), {
    target: { value: 'Title,Quantity,Due on,Budget minor,Currency,Evidence,Owner\nImported row,3,2026-08-14,2500,EUR,blob-2,person-2\n' },
  })
  fireEvent.click(screen.getByRole('button', { name: 'Import CSV row' }))
  expect(await screen.findByText(/One CSV row was loaded against typed-workbench version 4/)).toBeInTheDocument()
  expect(screen.getByLabelText('Record title (required)')).toHaveValue('Imported row')
  expect(screen.getByLabelText('Budget amount (required)')).toHaveValue(2500)
  expect((await axe.run(container)).violations).toEqual([])
}, 15_000)

test('rejects malformed runtime field bounds', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => jsonResponse({
    items: [{ ...builtIn, fields: [{ ...builtIn.fields[0], minimum: Number.NaN }] }],
  })))
  render(<PatternsManager csrfToken="csrf-value" />)
  const alert = await screen.findByRole('alert')
  expect(alert).toHaveTextContent('Templates could not be loaded.')
})

test('connects field errors, marks the control invalid, focuses it, and remains accessible', async () => {
  const template = {
    ...builtIn,
    fields: [
      { ...builtIn.fields[0], key: 'name', accessibleLabel: 'Record name' },
      { ...builtIn.fields[0], key: 'code', label: 'Code', accessibleLabel: 'Record code', csvHeader: 'code' },
    ],
  }
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    if (String(input) === '/api/v1/templates?includeVersions=true' && !init?.method) return jsonResponse({ items: [template] })
    return jsonResponse({
      status: 'invalid', normalizedValues: {},
      errors: [{ field: 'code', code: 'format', message: 'Enter a valid record code.' }],
      holdingReferences: [],
    })
  }))
  const { container } = render(<PatternsManager csrfToken="csrf-value" />)
  await screen.findByRole('heading', { name: 'Generated record workbench' })
  const code = screen.getByLabelText('Record code (required)')
  fireEvent.change(screen.getByLabelText('Record name (required)'), { target: { value: 'Name' } })
  fireEvent.change(code, { target: { value: 'bad' } })
  fireEvent.click(screen.getByRole('button', { name: 'Validate exact version' }))

  expect(await screen.findAllByText('Enter a valid record code.')).toHaveLength(2)
  await waitFor(() => expect(code).toHaveFocus())
  expect(code).toHaveAttribute('aria-invalid', 'true')
  expect(code).toHaveAccessibleDescription(/Enter a valid record code/)
  expect((await axe.run(container)).violations).toEqual([])
})

test('focuses a visible error when a CSV cell can trigger a spreadsheet formula', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => jsonResponse({ items: [builtIn] })))
  render(<PatternsManager csrfToken="csrf-value" />)
  await screen.findByRole('heading', { name: 'Generated record workbench' })
  fireEvent.change(screen.getByLabelText('CSV header and row'), { target: { value: 'name\n=HYPERLINK(example)\n' } })
  fireEvent.click(screen.getByRole('button', { name: 'Import CSV row' }))
  const alert = await screen.findByRole('alert')
  expect(alert).toHaveTextContent(/spreadsheet formula/)
  await waitFor(() => expect(alert).toHaveFocus())
})

test('selects immutable history by exact id and version and appends a custom version', async () => {
  const v1 = { ...builtIn, id: 'custom-history', name: 'Custom history', builtIn: false, version: 1 }
  const v2 = { ...v1, version: 2, description: 'Second version' }
  const v3 = { ...v2, version: 3, description: 'Third version' }
  let items = [v2, v1]
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path === '/api/v1/templates?includeVersions=true' && !init?.method) return jsonResponse({ items })
    if (path === '/api/v1/templates/custom-history/validate?version=1' && init?.method === 'POST') {
      return jsonResponse({ status: 'valid', normalizedValues: JSON.parse(String(init.body)).values, errors: [], holdingReferences: [] })
    }
    if (path === '/api/v1/templates/custom-history/versions' && init?.method === 'POST') {
      items = [v3, v2, v1]
      return jsonResponse(v3, 201)
    }
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<PatternsManager csrfToken="history-csrf" />)
  const chooser = await screen.findByLabelText('Template and version')
  expect(screen.getAllByRole('option', { name: /Custom history/ })).toHaveLength(2)
  fireEvent.change(chooser, { target: { value: 'custom-history\u00001' } })
  expect(await screen.findAllByText(/custom-history · v1/)).toHaveLength(2)
  expect(screen.getByText(/Select the latest version/)).toBeInTheDocument()
  expect(screen.queryByText('Append a custom version')).not.toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('Asset name (required)'), { target: { value: 'Historical row' } })
  fireEvent.click(screen.getByRole('button', { name: 'Validate exact version' }))
  expect(await screen.findByText('Custom history version 1 is valid.')).toBeInTheDocument()

  fireEvent.change(chooser, { target: { value: 'custom-history\u00002' } })
  fireEvent.click(await screen.findByText('Append a custom version'))
  fireEvent.change(screen.getByLabelText(/Version Accessible label/), { target: { value: 'Historical asset name' } })
  fireEvent.click(screen.getByRole('button', { name: 'Append next immutable version' }))
  expect(await screen.findByText('Custom template version 3 appended.')).toBeInTheDocument()
  expect(screen.getAllByText(/custom-history · v3/)).toHaveLength(2)
  const versionCall = fetchMock.mock.calls.find(([path, init]) => String(path).endsWith('/versions') && init?.method === 'POST')
  expect(versionCall?.[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'history-csrf' })
  expect(JSON.parse(String(versionCall?.[1]?.body))).toMatchObject({ fields: [{ accessibleLabel: 'Historical asset name' }] })
})

test('authors accessible, CSV, length, and numeric bounds', async () => {
  let created: typeof builtIn | undefined
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path === '/api/v1/templates?includeVersions=true' && !init?.method) return jsonResponse({ items: created ? [builtIn, created] : [builtIn] })
    if (path === '/api/v1/templates' && init?.method === 'POST') {
      const request = JSON.parse(String(init.body))
      created = { ...builtIn, ...request, id: 'metadata-template', builtIn: false, version: 1, status: 'active' }
      return jsonResponse(created, 201)
    }
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<PatternsManager csrfToken="metadata-csrf" />)
  await screen.findByText('Asset name')
  fireEvent.click(screen.getByText('Create a custom template'))
  fireEvent.change(screen.getByLabelText('Template name'), { target: { value: 'Metadata template' } })
  fireEvent.change(screen.getByLabelText('Record type'), { target: { value: 'example.metadata' } })
  fireEvent.change(screen.getByLabelText('Field key'), { target: { value: 'title' } })
  fireEvent.change(screen.getByLabelText('Field label'), { target: { value: 'Title' } })
  fireEvent.change(screen.getByLabelText(/^Accessible label/), { target: { value: 'Portable title' } })
  fireEvent.change(screen.getByLabelText('CSV header (defaults to key)'), { target: { value: 'Portable Title' } })
  fireEvent.change(screen.getByLabelText(/^Maximum length/), { target: { value: '512' } })
  fireEvent.click(screen.getByRole('button', { name: 'Add another field' }))
  const keys = screen.getAllByLabelText('Field key')
  const labels = screen.getAllByLabelText('Field label')
  const types = screen.getAllByLabelText('Field type')
  fireEvent.change(keys[1], { target: { value: 'quantity' } })
  fireEvent.change(labels[1], { target: { value: 'Quantity' } })
  fireEvent.change(types[1], { target: { value: 'number' } })
  fireEvent.change(screen.getByLabelText(/Minimum/), { target: { value: '1' } })
  fireEvent.change(screen.getByLabelText(/Maximum \(/), { target: { value: '1000' } })
  fireEvent.click(screen.getByRole('button', { name: 'Create custom template' }))
  expect(await screen.findByText('Custom template version 1 created.')).toBeInTheDocument()
  const createCall = fetchMock.mock.calls.find(([path, init]) => path === '/api/v1/templates' && init?.method === 'POST')
  expect(JSON.parse(String(createCall?.[1]?.body))).toMatchObject({ fields: [
    { key: 'title', accessibleLabel: 'Portable title', csvHeader: 'Portable Title', maximumLength: 512 },
    { key: 'quantity', minimum: 1, maximum: 1000 },
  ] })
})
