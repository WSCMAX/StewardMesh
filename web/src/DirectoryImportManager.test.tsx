import axe from 'axe-core'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import DirectoryImportManager from './DirectoryImportManager'

// Requirements: REQ-DIRECTORY-EXPANSION-002, REQ-DIRECTORY-EXPANSION-003, REQ-DIRECTORY-EXPANSION-004, REQ-DIRECTORY-EXPANSION-005, REQ-DIRECTORY-EXPANSION-006, REQ-DIRECTORY-EXPANSION-009, A11Y-001, SEC-GUARD-001.
// Features: integrations.protocols, experience.help.

const batchID = 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
const itemID = 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'
const attemptID = 'cccccccccccccccccccccccccccccccc'
const now = '2026-08-13T12:00:00Z'

const source = { id: 'entra-primary', provider: 'entra', configRevision: 'entra-1234567890abcdef' }
const sailPointSource = { id: 'sailpoint-primary', provider: 'sailpoint', configRevision: 'sailpoint-1234567890abcdef' }
const peopleSoftSource = { id: 'campus-solutions', provider: 'peoplesoft', configRevision: 'peoplesoft-1234567890abcdef' }
const counts = { created: 2, updated: 0, unchanged: 0, deactivated: 0, conflicts: 0, failed: 0 }
const previewedBatch = { id: batchID, sourceSystemId: source.id, provider: 'entra', configRevision: source.configRevision, status: 'previewed', completeSnapshot: true, counts, createdAt: now, updatedAt: now, completedAt: now }
const appliedBatch = { ...previewedBatch, status: 'applied' }
const item = {
  id: itemID, ordinal: 0,
  record: {
    sourceRecordId: 'user:aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', kind: 'identity', identityKind: 'person',
    displayName: 'Ada Example', email: 'ada@example.test', status: 'inactive', department: 'Technology',
    directoryAttributes: { 'job-title': 'Engineer' }, groupSourceIds: ['group:cccccccc-cccc-4ccc-8ccc-cccccccccccc'],
  },
  targetId: 'dddddddddddddddddddddddddddddddd', action: 'create', outcome: 'pending', updatedAt: now,
}
const attempt = { id: attemptID, operation: 'preview', number: 1, status: 'previewed', correlationId: 'correlation-1', startedAt: now, completedAt: now }

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

beforeEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  vi.stubGlobal('crypto', { randomUUID: () => '11111111-1111-4111-8111-111111111111' })
})

test('previews and applies the exact Entra plan with CSRF, idempotency, audit detail, and no client credentials', async () => {
  let currentBatch: typeof previewedBatch | typeof appliedBatch | null = null
  const onApplied = vi.fn()
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path === '/api/v1/directory-import-sources' && !init?.method) return jsonResponse({ items: [source, sailPointSource, peopleSoftSource] })
    if (path === '/api/v1/directory-imports?limit=50' && !init?.method) return jsonResponse({ batches: currentBatch ? [currentBatch] : [] })
    if (path === '/api/v1/directory-imports/preview' && init?.method === 'POST') {
      currentBatch = previewedBatch
      return jsonResponse({ batch: previewedBatch, replay: false }, 201)
    }
    if (path === `/api/v1/directory-imports/${batchID}` && !init?.method) {
      return jsonResponse({ batch: currentBatch, items: [{ ...item, outcome: currentBatch?.status === 'applied' ? 'applied' : 'pending' }], attempts: [attempt] })
    }
    if (path === `/api/v1/directory-imports/${batchID}/apply` && init?.method === 'POST') {
      currentBatch = appliedBatch
      return jsonResponse({ batch: appliedBatch, replay: false })
    }
    throw new Error(`unexpected request ${path} ${init?.method ?? 'GET'}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  const { container } = render(<DirectoryImportManager csrfToken="csrf-token" onApplied={onApplied} permissions={['integrations.read', 'integrations.write']} />)

  expect(await screen.findByRole('option', { name: 'entra-primary · Microsoft Entra ID' })).toBeInTheDocument()
  expect(container.querySelector('[data-feature="integrations.protocols"]')).not.toBeNull()
  expect(screen.getByRole('option', { name: 'sailpoint-primary · SailPoint Identity Security Cloud' })).toBeInTheDocument()
  expect(screen.getByRole('option', { name: 'campus-solutions · PeopleSoft Campus Solutions' })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Preview import' }))
  expect(await screen.findByText('Ada Example')).toBeInTheDocument()
  expect(screen.getByText('Technology')).toBeInTheDocument()
  expect(screen.getByText('1 direct group memberships')).toBeInTheDocument()
  expect(screen.getByText('job-title:')).toBeInTheDocument()
  expect(screen.getByText('Inactive at Microsoft Entra ID')).toBeInTheDocument()

  const previewRequest = fetchMock.mock.calls.find(([path, init]) => path === '/api/v1/directory-imports/preview' && init?.method === 'POST')
  expect(previewRequest?.[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'csrf-token', 'Idempotency-Key': expect.stringContaining('directory-preview-') })
  expect(JSON.parse(String(previewRequest?.[1]?.body))).toEqual({ sourceSystemId: 'entra-primary' })
  expect(String(previewRequest?.[1]?.body).toLowerCase()).not.toContain('secret')
  expect(String(previewRequest?.[1]?.body).toLowerCase()).not.toContain('tenant')

  fireEvent.click(screen.getByRole('button', { name: 'Apply exact plan' }))
  await waitFor(() => expect(onApplied).toHaveBeenCalledTimes(1))
  const applyRequest = fetchMock.mock.calls.find(([path, init]) => path === `/api/v1/directory-imports/${batchID}/apply` && init?.method === 'POST')
  expect(applyRequest?.[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'csrf-token', 'Idempotency-Key': expect.stringContaining('directory-apply-') })

  expect((await axe.run(container)).violations).toEqual([])
})

test('shows source and audit history read-only without mutation controls', async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const path = String(input)
    if (path === '/api/v1/directory-import-sources') return jsonResponse({ items: [source] })
    if (path === '/api/v1/directory-imports?limit=50') return jsonResponse({ batches: [previewedBatch] })
    if (path === `/api/v1/directory-imports/${batchID}`) return jsonResponse({ batch: previewedBatch, items: [item], attempts: [attempt] })
    throw new Error(`unexpected request ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<DirectoryImportManager csrfToken="csrf-token" permissions={['integrations.read']} />)
  expect(await screen.findByText((_, element) => element?.tagName === 'P' && element.textContent === 'Applying changes requires integrations.write.')).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Preview import' })).not.toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Apply exact plan' })).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'View audit' }))
  expect(await screen.findByText('Ada Example')).toBeInTheDocument()
})

test('keeps the import panel permission-aware and rejects malformed API responses', async () => {
  const { rerender } = render(<DirectoryImportManager csrfToken="csrf-token" permissions={[]} />)
  expect(screen.getByText((_, element) => element?.tagName === 'P' && element.textContent?.includes('requires integrations.read') === true)).toBeInTheDocument()

  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    if (String(input) === '/api/v1/directory-import-sources') return jsonResponse({ items: [{ id: 'bad source', provider: 'entra', configRevision: 'revision' }] })
    return jsonResponse({ batches: [] })
  }))
  rerender(<DirectoryImportManager csrfToken="csrf-token" permissions={['integrations.read']} />)
  const alert = await screen.findByRole('alert')
  expect(alert).toHaveTextContent('Directory import sources and audit history could not be loaded.')
  await waitFor(() => expect(alert).toHaveFocus())
})

test('names scrollable import tables and makes them keyboard focusable', async () => {
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const path = String(input)
    if (path === '/api/v1/directory-import-sources') return jsonResponse({ items: [source] })
    if (path === '/api/v1/directory-imports?limit=50') return jsonResponse({ batches: [previewedBatch] })
    if (path === `/api/v1/directory-imports/${batchID}`) return jsonResponse({ batch: previewedBatch, items: [item], attempts: [attempt] })
    throw new Error(`unexpected request ${path}`)
  }))
  render(<DirectoryImportManager csrfToken="csrf-token" permissions={['integrations.read']} />)

  const history = await screen.findByRole('region', { name: 'Recent directory imports' })
  expect(history).toHaveAttribute('tabindex', '0')
  history.focus()
  expect(history).toHaveFocus()

  fireEvent.click(screen.getByRole('button', { name: 'View audit' }))
  const records = await screen.findByRole('region', { name: 'Directory import records' })
  expect(records).toHaveAttribute('tabindex', '0')
  records.focus()
  expect(records).toHaveFocus()
})

test('renders SailPoint governance groups and memberships from bounded audit detail', async () => {
  const sailPointBatch = { ...previewedBatch, sourceSystemId: sailPointSource.id, provider: 'sailpoint', configRevision: sailPointSource.configRevision }
  const groupItem = {
    id: 'eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee', ordinal: 0,
    record: { sourceRecordId: 'role:finance-reader', kind: 'group', groupName: 'Finance Reader', displayName: 'Finance Reader',
      description: 'Read finance records', status: 'active', metadata: { 'directory-object-kind': 'role' } },
    targetId: 'ffffffffffffffffffffffffffffffff', action: 'create', outcome: 'pending', updatedAt: now,
  }
  const membershipItem = {
    id: '11111111111111111111111111111111', ordinal: 1,
    record: { sourceRecordId: 'membership:finance-ada', kind: 'membership', displayName: 'Ada Example', status: 'active',
      groupSourceId: 'role:finance-reader', memberSourceId: 'identity:ada', memberKind: 'subject', metadata: { 'directory-object-kind': 'role-assignment' } },
    targetId: '22222222222222222222222222222222', action: 'create', outcome: 'pending', updatedAt: now,
  }
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const path = String(input)
    if (path === '/api/v1/directory-import-sources') return jsonResponse({ items: [sailPointSource] })
    if (path === '/api/v1/directory-imports?limit=50') return jsonResponse({ batches: [sailPointBatch] })
    if (path === `/api/v1/directory-imports/${batchID}`) return jsonResponse({ batch: sailPointBatch, items: [groupItem, membershipItem], attempts: [attempt] })
    throw new Error(`unexpected request ${path}`)
  }))
  render(<DirectoryImportManager csrfToken="csrf-token" permissions={['integrations.read']} />)
  expect((await screen.findAllByText('SailPoint Identity Security Cloud')).length).toBeGreaterThan(0)
  fireEvent.click(screen.getByRole('button', { name: 'View audit' }))
  expect((await screen.findAllByText('Finance Reader')).length).toBeGreaterThan(0)
  expect(screen.getByText('Managed group')).toBeInTheDocument()
  expect(screen.getByText('subject membership')).toBeInTheDocument()
  expect(screen.getAllByText('directory-object-kind:')).toHaveLength(2)
})
