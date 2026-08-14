import axe from 'axe-core'
import { fireEvent, render, screen, within } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import ThreadsManager from './ThreadsManager'

// Requirements: REQ-THREADS-001, A11Y-001.

const timestamp = '2026-08-11T12:00:00Z'
const asset = { id: 'asset-1', organizationId: 'example-org', name: 'Primary server', kind: 'server', status: 'active', revision: 1, createdAt: timestamp, updatedAt: timestamp }
const governance = { id: 'governance', organizationId: 'example-org', name: 'Governance', inheritByDefault: true, revision: 1 }
const security = { id: 'security', organizationId: 'example-org', name: 'Security', parentId: 'governance', inheritByDefault: false, revision: 1 }
const goal = { id: 'reduce-risk', organizationId: 'example-org', name: 'Reduce operational risk', description: 'Lower material service exposure.', revision: 1 }
const permissions = ['goals.read', 'goals.write']

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

function installThreadsFetch() {
  let linked = false
  let suppressed = false
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path === '/api/v1/tags' && init?.method === 'POST') return jsonResponse({ ...security, id: 'created-tag' }, 201)
    if (path === '/api/v1/tags') return jsonResponse({ items: [governance, security] })
    if (path === '/api/v1/goals' && init?.method === 'POST') return jsonResponse({ ...goal, id: 'created-goal' }, 201)
    if (path === '/api/v1/goals') return jsonResponse({ items: [goal] })
    if (path === '/api/v1/threads/asset/asset-1/tags' && !init?.method) {
      return jsonResponse({ items: suppressed ? [
        { tag: governance, state: 'inherited', sourceTagId: security.id },
        { tag: security, state: 'suppressed', rule: { tagId: security.id, mode: 'suppress', revision: 2 } },
      ] : [
        { tag: governance, state: 'inherited', sourceTagId: security.id },
        { tag: security, state: 'explicit', rule: { tagId: security.id, mode: 'include', revision: 1 } },
      ] })
    }
    if (path === '/api/v1/threads/asset/asset-1/tags/security' && init?.method === 'PUT') {
      suppressed = true
      return jsonResponse({ tagId: security.id, mode: 'suppress', revision: 2 })
    }
    if (path === '/api/v1/threads/asset/asset-1/goals' && !init?.method) return jsonResponse({ items: linked ? [{ goalId: goal.id, targetType: 'asset', targetId: asset.id }] : [] })
    if (path === '/api/v1/threads/asset/asset-1/goals/reduce-risk' && init?.method === 'PUT') {
      linked = true
      return jsonResponse({ goalId: goal.id, targetType: 'asset', targetId: asset.id })
    }
    throw new Error(`unexpected request: ${path} ${init?.method ?? 'GET'}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

beforeEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

test('shows the accessible hierarchy and explicit provenance without automated WCAG violations', async () => {
  installThreadsFetch()
  const { container } = render(<ThreadsManager assets={[asset]} csrfToken="csrf-value" permissions={permissions} />)
  expect(await screen.findByRole('heading', { name: 'Threads — Tags and goals' })).toBeInTheDocument()
  const hierarchy = await screen.findByRole('list', { name: 'Organization tag hierarchy' })
  expect(within(hierarchy).getByText('Governance')).toBeInTheDocument()
  expect(within(hierarchy).getByText('Security')).toBeInTheDocument()
  expect(await screen.findByText('Inherited from Security')).toBeInTheDocument()
  expect(screen.getByText('Explicit include rule, revision 1')).toBeInTheDocument()
  const results = await axe.run(container)
  expect(results.violations).toEqual([])
})

test('creates hierarchy records and manages guarded asset relationships with CSRF', async () => {
  const fetchMock = installThreadsFetch()
  render(<ThreadsManager assets={[asset]} csrfToken="csrf-value" permissions={permissions} />)
  await screen.findByRole('list', { name: 'Organization tag hierarchy' })

  fireEvent.change(screen.getByLabelText('Tag name'), { target: { value: 'Compliance' } })
  fireEvent.change(screen.getByLabelText('Parent tag (optional)'), { target: { value: governance.id } })
  fireEvent.click(screen.getByLabelText('Inherit this tag when a child tag is applied'))
  fireEvent.click(screen.getByRole('button', { name: 'Create tag' }))
  expect(await screen.findByText('Tag created in the organization hierarchy.')).toBeInTheDocument()

  fireEvent.change(screen.getByLabelText('Goal name'), { target: { value: 'Improve resilience' } })
  fireEvent.change(screen.getByLabelText('Description (optional)'), { target: { value: 'Protect critical services.' } })
  fireEvent.click(screen.getByRole('button', { name: 'Create goal' }))
  expect(await screen.findByText('Goal created in the strategy hierarchy.')).toBeInTheDocument()

  const hierarchy = screen.getByRole('list', { name: 'Organization tag hierarchy' })
  const securityRow = within(hierarchy).getByText('Security').closest('div.rounded-lg')
  if (!securityRow) throw new Error('security row missing')
  fireEvent.click(within(securityRow as HTMLElement).getByRole('button', { name: 'Suppress' }))
  expect(await screen.findByText('Security explicitly suppressed.')).toBeInTheDocument()

  fireEvent.change(screen.getByLabelText('Goal', { selector: 'select' }), { target: { value: goal.id } })
  fireEvent.click(screen.getByRole('button', { name: 'Link goal' }))
  expect(await screen.findByText('Goal linked to the selected asset.')).toBeInTheDocument()
  expect(screen.getByRole('list', { name: 'Linked goals' })).toHaveTextContent('Reduce operational risk')

  const createTagCall = fetchMock.mock.calls.find(([path, init]) => path === '/api/v1/tags' && init?.method === 'POST')
  expect(createTagCall?.[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'csrf-value' })
  expect(JSON.parse(String(createTagCall?.[1]?.body))).toEqual({ name: 'Compliance', parentId: governance.id, inheritByDefault: true })
  for (const call of fetchMock.mock.calls.filter(([, init]) => ['POST', 'PUT', 'DELETE'].includes(String(init?.method)))) {
    expect(call[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'csrf-value' })
  }
})

test('creates the first direct tag rule with the revision zero sentinel', async () => {
  let created = false
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path === '/api/v1/tags') return jsonResponse({ items: [governance, security] })
    if (path === '/api/v1/goals') return jsonResponse({ items: [goal] })
    if (path === '/api/v1/threads/asset/asset-1/tags' && !init?.method) {
      return jsonResponse({ items: created ? [{ tag: security, state: 'explicit', rule: { tagId: security.id, mode: 'include', revision: 1 } }] : [] })
    }
    if (path === '/api/v1/threads/asset/asset-1/goals' && !init?.method) return jsonResponse({ items: [] })
    if (path === '/api/v1/threads/asset/asset-1/tags/security' && init?.method === 'PUT') {
      created = true
      return jsonResponse({ tagId: security.id, mode: 'include', revision: 1 })
    }
    throw new Error(`unexpected request: ${path} ${init?.method ?? 'GET'}`)
  })
  vi.stubGlobal('fetch', fetchMock)

  render(<ThreadsManager assets={[asset]} csrfToken="csrf-value" permissions={permissions} />)
  const hierarchy = await screen.findByRole('list', { name: 'Organization tag hierarchy' })
  const securityRow = within(hierarchy).getByText('Security').closest('div.rounded-lg')
  if (!securityRow) throw new Error('security row missing')
  fireEvent.click(within(securityRow as HTMLElement).getByRole('button', { name: 'Apply' }))

  expect(await screen.findByText('Security explicitly applied.')).toBeInTheDocument()
  const createRuleCall = fetchMock.mock.calls.find(([path, init]) => path === '/api/v1/threads/asset/asset-1/tags/security' && init?.method === 'PUT')
  expect(createRuleCall?.[1]?.body).toBe('{"mode":"include","revision":0}')
})

test('renders nothing when goals read permission is absent', () => {
  const fetchMock = installThreadsFetch()
  const { container } = render(<ThreadsManager assets={[asset]} csrfToken="csrf-value" permissions={[]} />)
  expect(container).toBeEmptyDOMElement()
  expect(fetchMock).not.toHaveBeenCalled()
})
