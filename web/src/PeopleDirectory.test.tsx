import axe from 'axe-core'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import PeopleDirectory from './PeopleDirectory'

// Requirements: REQ-PEOPLE-001, REQ-DIRECTORY-EXPANSION-001, A11Y-001, DOC-001, DOC-002.

const timestamp = '2026-08-09T12:00:00Z'
const site = {
  id: 'site-1', organizationId: 'example-org', name: 'Main Campus', status: 'active', revision: 1,
  address: { line1: '100 Steward Way', city: 'Madison', region: 'WI', postalCode: '53703', country: 'US' },
  createdAt: timestamp, updatedAt: timestamp,
}
const building = {
  id: 'building-1', organizationId: 'example-org', siteId: site.id, name: 'Innovation Hall', status: 'active', revision: 1,
  createdAt: timestamp, updatedAt: timestamp,
}
const room = {
  id: 'room-1', organizationId: 'example-org', siteId: site.id, buildingId: building.id, number: '101', name: 'Conference room',
  status: 'active', revision: 1, createdAt: timestamp, updatedAt: timestamp,
}
const department = {
  id: 'department-1', organizationId: 'example-org', name: 'Technology', siteId: site.id, status: 'active', revision: 1,
  createdAt: timestamp, updatedAt: timestamp,
}
const person = {
  id: 'identity-1', organizationId: 'example-org', kind: 'person', displayName: 'Alex Rivera', email: 'alex@example.test',
  departmentId: department.id, siteId: site.id, status: 'active', revision: 1, createdAt: timestamp, updatedAt: timestamp,
}
const assignment = {
  id: 'assignment-1', organizationId: 'example-org', assetId: 'asset-1', assigneeKind: 'identity', assigneeId: person.id,
  role: 'primary', effectiveFrom: timestamp, createdBy: 'account-1', createdAt: timestamp,
}
const permissions = ['assets.read', 'assets.write', 'directory.read', 'directory.write']
const asset = {
  id: 'asset-1', organizationId: 'example-org', name: 'Lab computer', kind: 'computer', status: 'active',
  revision: 1, createdAt: '2026-08-10T12:00:00Z', updatedAt: '2026-08-10T12:00:00Z',
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

function installPeopleFetch(options: { assignments?: unknown[]; buildings?: unknown[]; rooms?: unknown[]; sites?: unknown[] } = {}) {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path === '/api/v1/sites' && init?.method === 'POST') {
      const body = JSON.parse(String(init.body)) as Record<string, unknown>
      return jsonResponse({ ...site, name: body.name, address: body.address ?? {} }, 201)
    }
    if (path === '/api/v1/sites') return jsonResponse({ items: options.sites ?? [site] })
    if (path === '/api/v1/buildings' && init?.method === 'POST') {
      const body = JSON.parse(String(init.body)) as Record<string, unknown>
      return jsonResponse({ ...building, siteId: body.siteId, name: body.name }, 201)
    }
    if (path === '/api/v1/buildings') return jsonResponse({ items: options.buildings ?? [building] })
    if (path === '/api/v1/rooms' && init?.method === 'POST') {
      const body = JSON.parse(String(init.body)) as Record<string, unknown>
      return jsonResponse({ ...room, siteId: body.siteId, buildingId: body.buildingId, number: body.number, name: body.name }, 201)
    }
    if (path === '/api/v1/rooms') return jsonResponse({ items: options.rooms ?? [room] })
    if (path === '/api/v1/departments' && init?.method === 'POST') return jsonResponse(department, 201)
    if (path === '/api/v1/departments') return jsonResponse({ items: [department] })
    if (path === '/api/v1/identities' && init?.method === 'POST') {
      const body = JSON.parse(String(init.body)) as Record<string, unknown>
      return jsonResponse({
        ...person,
        id: 'identity-created',
        kind: body.kind,
        displayName: body.displayName,
        email: body.email || undefined,
      }, 201)
    }
    if (path.startsWith('/api/v1/identities?')) return jsonResponse({ items: [person] })
    if (path === '/api/v1/assets/asset-1/assignments' && init?.method === 'POST') return jsonResponse(assignment, 201)
    if (path === '/api/v1/assets/asset-1/assignments') return jsonResponse({ items: options.assignments ?? [] })
    if (path === '/api/v1/assets/asset-1/assignments/assignment-1' && init?.method === 'PATCH') {
      return jsonResponse({ ...assignment, effectiveTo: '2026-08-10T12:00:00Z' })
    }
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

beforeEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

test('renders a scoped People directory and guided help without automated WCAG violations', async () => {
  const fetchMock = installPeopleFetch({ assignments: [assignment] })
  const { container } = render(<PeopleDirectory assets={[asset]} csrfToken="csrf-value" issuesUrl="https://github.com/WSCMAX/StewardMesh/issues" permissions={permissions} />)
  expect(await screen.findByRole('heading', { name: 'Know who uses and stewards each asset' })).toBeInTheDocument()
  expect((await screen.findAllByText('Alex Rivera')).length).toBeGreaterThan(0)
  expect(screen.getByRole('heading', { name: 'Quick guide' })).toBeInTheDocument()
  expect(screen.getByRole('heading', { name: 'Locations in your scope' })).toBeInTheDocument()
  expect(screen.getByRole('heading', { name: 'Innovation Hall' })).toBeInTheDocument()
  expect(screen.getByText('Room 101 · Conference room')).toBeInTheDocument()
  expect(screen.getByLabelText('Main Campus address')).toHaveTextContent('100 Steward Way')
  expect(fetchMock).toHaveBeenCalledWith('/api/v1/buildings', expect.any(Object))
  expect(fetchMock).toHaveBeenCalledWith('/api/v1/rooms', expect.any(Object))
  expect(screen.getByRole('link', { name: 'Report a People issue' })).toHaveAttribute('href', 'https://github.com/WSCMAX/StewardMesh/issues')
  await screen.findByText('Primary assignee · Active')
  for (const summary of ['Add a site', 'Add a building', 'Add a room', 'Add a department', 'Add a directory identity']) {
    fireEvent.click(screen.getByText(summary))
  }
  const results = await axe.run(container)
  expect(results.violations).toEqual([])
})

test('creates structured sites, buildings, and rooms through flat CSRF-protected endpoints', async () => {
  const fetchMock = installPeopleFetch()
  render(<PeopleDirectory assets={[]} csrfToken="csrf-value" issuesUrl="https://github.com/WSCMAX/StewardMesh/issues" permissions={permissions} />)
  await screen.findByRole('heading', { name: 'Innovation Hall' })

  fireEvent.click(screen.getByText('Add a site'))
  fireEvent.change(screen.getByLabelText('Site name'), { target: { value: 'East Campus' } })
  fireEvent.change(screen.getByLabelText('Address line 1'), { target: { value: '200 Lake Street' } })
  fireEvent.change(screen.getByLabelText('Address line 2 (optional)'), { target: { value: 'Floor 2' } })
  fireEvent.change(screen.getByLabelText('City'), { target: { value: 'Madison' } })
  fireEvent.change(screen.getByLabelText('State or region (optional)'), { target: { value: 'WI' } })
  fireEvent.change(screen.getByLabelText('Postal code (optional)'), { target: { value: '53703' } })
  fireEvent.change(screen.getByLabelText('Country code'), { target: { value: 'us' } })
  fireEvent.click(screen.getByRole('button', { name: 'Create site' }))
  expect(await screen.findByText('Site created and available to buildings and departments.')).toBeInTheDocument()

  fireEvent.click(screen.getByText('Add a building'))
  fireEvent.change(screen.getByLabelText('Building site'), { target: { value: site.id } })
  fireEvent.change(screen.getByLabelText('Building name'), { target: { value: 'Science Center' } })
  fireEvent.click(screen.getByRole('button', { name: 'Create building' }))
  expect(await screen.findByText('Building created beneath its site.')).toBeInTheDocument()

  fireEvent.click(screen.getByText('Add a room'))
  fireEvent.change(screen.getByLabelText('Room building'), { target: { value: building.id } })
  fireEvent.change(screen.getByLabelText('Room number'), { target: { value: '204' } })
  fireEvent.change(screen.getByLabelText('Room name (optional)'), { target: { value: 'Robotics lab' } })
  fireEvent.click(screen.getByRole('button', { name: 'Create room' }))
  expect(await screen.findByText('Room created beneath its building.')).toBeInTheDocument()

  const siteCall = fetchMock.mock.calls.find(([path, init]) => path === '/api/v1/sites' && init?.method === 'POST')
  const buildingCall = fetchMock.mock.calls.find(([path, init]) => path === '/api/v1/buildings' && init?.method === 'POST')
  const roomCall = fetchMock.mock.calls.find(([path, init]) => path === '/api/v1/rooms' && init?.method === 'POST')
  expect(siteCall?.[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'csrf-value' })
  expect(buildingCall?.[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'csrf-value' })
  expect(roomCall?.[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'csrf-value' })
  expect(JSON.parse(String(siteCall?.[1]?.body))).toEqual({
    name: 'East Campus',
    status: 'active',
    address: { line1: '200 Lake Street', line2: 'Floor 2', city: 'Madison', region: 'WI', postalCode: '53703', country: 'US' },
  })
  expect(JSON.parse(String(buildingCall?.[1]?.body))).toEqual({ siteId: site.id, name: 'Science Center', status: 'active' })
  expect(JSON.parse(String(roomCall?.[1]?.body))).toEqual({ siteId: site.id, buildingId: building.id, number: '204', name: 'Robotics lab', status: 'active' })
})

test('rejects malformed location collection responses at runtime', async () => {
  installPeopleFetch({ buildings: [{ ...building, siteId: 123 }] })
  render(<PeopleDirectory assets={[]} csrfToken="csrf-value" issuesUrl="https://github.com/WSCMAX/StewardMesh/issues" permissions={permissions} />)
  expect(await screen.findByRole('alert')).toHaveTextContent('The People directory could not be loaded.')
  expect(screen.queryByRole('heading', { name: 'Innovation Hall' })).not.toBeInTheDocument()
})

test('requires a complete structured address when any address field is entered', async () => {
  const fetchMock = installPeopleFetch()
  render(<PeopleDirectory assets={[]} csrfToken="csrf-value" issuesUrl="https://github.com/WSCMAX/StewardMesh/issues" permissions={permissions} />)
  await screen.findByRole('heading', { name: 'Innovation Hall' })
  fireEvent.click(screen.getByText('Add a site'))
  fireEvent.change(screen.getByLabelText('Site name'), { target: { value: 'Incomplete Campus' } })
  fireEvent.change(screen.getByLabelText('State or region (optional)'), { target: { value: 'WI' } })
  fireEvent.click(screen.getByRole('button', { name: 'Create site' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('An address needs address line 1, city, and a two-letter country code.')
  expect(fetchMock.mock.calls.some(([path, init]) => path === '/api/v1/sites' && init?.method === 'POST')).toBe(false)
})

test('creates a typed shared identity with the in-memory CSRF token', async () => {
  const fetchMock = installPeopleFetch()
  render(<PeopleDirectory assets={[]} csrfToken="csrf-value" issuesUrl="https://github.com/WSCMAX/StewardMesh/issues" permissions={permissions} />)
  await screen.findAllByText('Alex Rivera')
  fireEvent.click(screen.getByText('Add a directory identity'))
  fireEvent.change(screen.getAllByLabelText('Identity type')[1], { target: { value: 'shared' } })
  fireEvent.change(screen.getByLabelText('Display name'), { target: { value: 'Public workstation users' } })
  fireEvent.change(screen.getByLabelText('Department (optional)'), { target: { value: department.id } })
  fireEvent.click(screen.getByRole('button', { name: 'Create identity' }))
  expect(await screen.findByText('Directory identity created.')).toBeInTheDocument()
  const createCall = fetchMock.mock.calls.find(([path, init]) => path === '/api/v1/identities' && init?.method === 'POST')
  expect(createCall?.[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'csrf-value' })
  const requestBody = JSON.parse(String(createCall?.[1]?.body)) as Record<string, unknown>
  expect(requestBody).toMatchObject({ kind: 'shared', displayName: 'Public workstation users' })
  expect(requestBody).not.toHaveProperty('email')
})

test('creates and ends effective-dated asset assignments through guarded mutations', async () => {
  const fetchMock = installPeopleFetch({ assignments: [assignment] })
  render(<PeopleDirectory assets={[asset]} csrfToken="csrf-value" issuesUrl="https://github.com/WSCMAX/StewardMesh/issues" permissions={permissions} />)
  await screen.findByText('Primary assignee · Active')
  fireEvent.change(screen.getByLabelText('Assignee'), { target: { value: person.id } })
  fireEvent.click(screen.getByRole('button', { name: 'Create assignment' }))
  await waitFor(() => expect(fetchMock.mock.calls.some(([path, init]) => path === '/api/v1/assets/asset-1/assignments' && init?.method === 'POST')).toBe(true))
  const createCall = fetchMock.mock.calls.find(([path, init]) => path === '/api/v1/assets/asset-1/assignments' && init?.method === 'POST')
  expect(createCall?.[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'csrf-value' })
  fireEvent.click(screen.getByRole('button', { name: 'End assignment' }))
  await waitFor(() => expect(fetchMock.mock.calls.some(([path, init]) => String(path).endsWith('/assignment-1') && init?.method === 'PATCH')).toBe(true))
})
