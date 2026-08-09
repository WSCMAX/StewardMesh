import axe from 'axe-core'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import PeopleDirectory from './PeopleDirectory'

// Requirements: REQ-PEOPLE-001, A11Y-001, DOC-001, DOC-002.

const timestamp = '2026-08-09T12:00:00Z'
const site = {
  id: 'site-1', organizationId: 'example-org', name: 'Main Campus', status: 'active', revision: 1,
  createdAt: timestamp, updatedAt: timestamp,
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
const asset = { id: 'asset-1', name: 'Lab computer', kind: 'computer', status: 'active' }

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

function installPeopleFetch(options: { assignments?: unknown[] } = {}) {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path === '/api/v1/sites' && init?.method === 'POST') return jsonResponse(site, 201)
    if (path === '/api/v1/sites') return jsonResponse({ items: [site] })
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
  installPeopleFetch({ assignments: [assignment] })
  const { container } = render(<PeopleDirectory assets={[asset]} csrfToken="csrf-value" issuesUrl="https://github.com/WSCMAX/StewardMesh/issues" permissions={permissions} />)
  expect(await screen.findByRole('heading', { name: 'Know who uses and stewards each asset' })).toBeInTheDocument()
  expect((await screen.findAllByText('Alex Rivera')).length).toBeGreaterThan(0)
  expect(screen.getByRole('heading', { name: 'Quick guide' })).toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'Report a People issue' })).toHaveAttribute('href', 'https://github.com/WSCMAX/StewardMesh/issues')
  await screen.findByText('Primary assignee · Active')
  const results = await axe.run(container)
  expect(results.violations).toEqual([])
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
