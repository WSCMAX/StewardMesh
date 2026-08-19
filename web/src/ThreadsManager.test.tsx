import axe from 'axe-core'
import { fireEvent, render, screen, within } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import TagsManager from './TagsManager'

// Requirements: REQ-LABELS-001, REQ-THREADS-001, A11Y-001.

const timestamp = '2026-08-11T12:00:00Z'
const asset = { id: 'asset-1', organizationId: 'example-org', name: 'Primary server', kind: 'server', status: 'active', revision: 1, createdAt: timestamp, updatedAt: timestamp }
const studioArts = {
  id: 'studio-arts', organizationId: 'example-org', name: 'Student program', valueKind: 'select', applicableRecordTypes: ['atlas.asset', 'people.identity'],
  options: ['Studio Arts', 'Graphic Design'], status: 'active', revision: 1,
}
const goal = { id: 'reduce-risk', organizationId: 'example-org', name: 'Reduce operational risk', description: 'Lower material service exposure.', revision: 1 }
const permissions = ['labels.read', 'labels.write', 'goals.read', 'goals.write']

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

function installTagsFetch() {
  let linked = false
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path === '/api/v1/labels/definitions') return jsonResponse({ items: [studioArts] })
    if (path === '/api/v1/goals' && init?.method === 'POST') return jsonResponse({ ...goal, id: 'created-goal' }, 201)
    if (path === '/api/v1/goals') return jsonResponse({ items: [goal] })
    if (path.startsWith('/api/v1/labels/records/atlas.asset/asset-1/assignments') && !init?.method) return jsonResponse({ items: [] })
    if (path.startsWith('/api/v1/labels/records/atlas.asset/asset-1/assignments/studio-arts') && init?.method === 'PUT') {
      return jsonResponse({ definitionId: studioArts.id, recordType: 'atlas.asset', recordId: asset.id, valueText: 'Studio Arts', revision: 1 })
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
})

test('TagsManager connects a configured tag to an asset', async () => {
  installTagsFetch()
  render(<TagsManager assets={[asset]} csrfToken="csrf-value" permissions={permissions} />)
  expect(await screen.findByRole('heading', { name: 'Tags' })).toBeTruthy()
  fireEvent.change(await screen.findByLabelText('Value'), { target: { value: 'Studio Arts' } })
  fireEvent.click(screen.getByRole('button', { name: 'Connect tag' }))
  expect(await screen.findByText('Student program: Studio Arts')).toBeTruthy()
})

test('TagsManager links a goal to the selected asset', async () => {
  installTagsFetch()
  render(<TagsManager assets={[asset]} csrfToken="csrf-value" permissions={permissions} />)
  expect(await screen.findByRole('heading', { name: 'Tags' })).toBeTruthy()
  fireEvent.change(screen.getByLabelText('Goal'), { target: { value: goal.id } })
  fireEvent.click(screen.getByRole('button', { name: 'Link goal' }))
  const linkedGoals = await screen.findByRole('list', { name: 'Linked goals' })
  expect(within(linkedGoals).getByText('Reduce operational risk')).toBeTruthy()
})

test('TagsManager has no critical accessibility violations', async () => {
  installTagsFetch()
  const { container } = render(<TagsManager assets={[asset]} csrfToken="csrf-value" permissions={permissions} />)
  expect(await screen.findByRole('heading', { name: 'Tags' })).toBeTruthy()
  const results = await axe.run(container)
  expect(results.violations.filter((violation) => violation.impact === 'critical')).toEqual([])
})

test('TagsManager hides tag configuration without labels permission', async () => {
  installTagsFetch()
  render(<TagsManager assets={[asset]} csrfToken="csrf-value" permissions={['goals.read']} />)
  expect(await screen.findByRole('heading', { name: 'Tags' })).toBeTruthy()
  expect(screen.queryByRole('heading', { name: 'Configure tags' })).toBeNull()
  expect(await screen.findByRole('heading', { name: 'Goals on this asset' })).toBeTruthy()
})
