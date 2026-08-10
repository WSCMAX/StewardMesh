import axe from 'axe-core'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import GuardAccessManager from './GuardAccessManager'

// Requirements: SEC-GUARD-001, SEC-HTTP-001, A11Y-001.

const accountId = '11111111111111111111111111111111'
const roleId = '22222222222222222222222222222222'
const localAssignment = {
  id: '33333333333333333333333333333333',
  accountId,
  roleId,
  scope: { kind: 'organization', resourceId: 'example-org' },
  source: 'local',
  managed: false,
  createdAt: '2026-08-09T12:00:00Z',
}
const managedAssignment = {
  id: '44444444444444444444444444444444',
  accountId,
  roleId,
  scope: { kind: 'site', resourceId: 'site-one' },
  source: 'oidc:0123456789abcdef0123456789abcdef',
  managed: true,
  createdAt: '2026-08-09T12:05:00Z',
}
const account = {
  id: accountId,
  username: 'administrator',
  email: 'administrator@example.test',
  displayName: 'Example Administrator',
  status: 'active',
}
const role = {
  id: roleId,
  name: 'Administrator',
  description: 'Organization administrator',
  permissions: ['guard.manage'],
  policyBundleIds: ['55555555555555555555555555555555'],
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

beforeEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

test('creates and removes local scoped assignments while keeping provider assignments read only', async () => {
  let assignments = [localAssignment, managedAssignment]
  const createdAssignment = {
    ...localAssignment,
    id: '66666666666666666666666666666666',
    scope: { kind: 'department', resourceId: 'department-one' },
    createdAt: '2026-08-09T12:10:00Z',
  }
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path === '/api/v1/guard/access') return jsonResponse({ accounts: [account], roles: [role], assignments })
    if (path === '/api/v1/guard/role-assignments' && init?.method === 'POST') {
      assignments = [...assignments, createdAssignment]
      return jsonResponse(createdAssignment, 201)
    }
    if (path.endsWith(createdAssignment.id) && init?.method === 'DELETE') {
      assignments = assignments.filter((assignment) => assignment.id !== createdAssignment.id)
      return new Response(null, { status: 204 })
    }
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  const { container } = render(<GuardAccessManager csrfToken="csrf-value" />)

  expect(await screen.findByRole('heading', { name: 'Assign the right access at the right scope' })).toBeInTheDocument()
  expect(screen.getByText('Identity provider')).toBeInTheDocument()
  expect(screen.getByText('Read only')).toBeInTheDocument()
  const results = await axe.run(container)
  expect(results.violations).toEqual([])

  fireEvent.click(screen.getByText('Add a scoped role assignment'))
  fireEvent.change(screen.getByLabelText('Access scope'), { target: { value: 'department' } })
  fireEvent.change(screen.getByLabelText('Scoped resource ID'), { target: { value: 'department-one' } })
  fireEvent.click(screen.getByRole('button', { name: 'Assign role' }))
  expect(await screen.findByText('Scoped role assignment created.')).toBeInTheDocument()
  const createCall = fetchMock.mock.calls.find(([path, init]) => path === '/api/v1/guard/role-assignments' && init?.method === 'POST')
  expect(createCall?.[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'csrf-value' })
  expect(JSON.parse(String(createCall?.[1]?.body))).toEqual({
    accountId,
    roleId,
    scope: { kind: 'department', resourceId: 'department-one' },
  })

  const removeButtons = screen.getAllByRole('button', { name: 'Remove Administrator assignment for Example Administrator' })
  fireEvent.click(removeButtons[removeButtons.length - 1])
  expect(await screen.findByText('Role assignment removed.')).toBeInTheDocument()
  await waitFor(() => expect(fetchMock.mock.calls.some(([path, init]) => String(path).endsWith(createdAssignment.id) && init?.method === 'DELETE')).toBe(true))
})

test('announces the server protection when removal would lock out the last administrator', async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path === '/api/v1/guard/access') return jsonResponse({ accounts: [account], roles: [role], assignments: [localAssignment] })
    if (path.endsWith(localAssignment.id) && init?.method === 'DELETE') {
      return jsonResponse({ error: { message: 'Assign another organization administrator before removing this assignment.' } }, 409)
    }
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<GuardAccessManager csrfToken="csrf-value" />)

  const remove = await screen.findByRole('button', { name: 'Remove Administrator assignment for Example Administrator' })
  fireEvent.click(remove)
  const alert = await screen.findByRole('alert')
  expect(alert).toHaveTextContent('Assign another organization administrator')
  await waitFor(() => expect(alert).toHaveFocus())
})
