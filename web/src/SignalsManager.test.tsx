import axe from 'axe-core'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import SignalsManager from './SignalsManager'

// Requirement: REQ-SIGNALS-001. Feature: alerts.rules. GitHub: #11.

const rule = {
  id: 'rule-renewal', name: 'Contract renewals', condition: 'renewal', severity: 'warning', enabled: true,
  thresholdDays: [180, 90, 60, 30], revision: 1,
}
const alert = {
  id: 'alert-one', ruleId: rule.id, condition: 'renewal', severity: 'warning', status: 'active',
  title: 'Contract renewal decision is approaching', summary: 'Support contract renews in 30 days.', targetType: 'contract', targetId: 'contract-one',
  dueAt: '2026-09-12T12:00:00Z', thresholdDays: 30, firstDetectedAt: '2026-08-13T12:00:00Z', lastObservedAt: '2026-08-13T12:00:00Z', revision: 1,
}
const subscription = { id: 'subscription-one', targetKind: 'group', targetId: 'finance-owners', enabled: true, createdAt: '2026-08-13T12:00:00Z' }
const subscriptionTargets = [
  { targetKind: 'group', targetId: 'finance-owners', label: 'Finance owners' },
  { targetKind: 'webhook', targetId: 'operations-hook', label: 'Operations webhook' },
]

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

function reads(input: RequestInfo | URL) {
  const path = String(input)
  if (path === '/api/v1/signals/rules') return jsonResponse({ items: [rule] })
  if (path.startsWith('/api/v1/signals/alerts?')) return jsonResponse({ items: [alert] })
  if (path === '/api/v1/signals/subscriptions') return jsonResponse({ items: [subscription] })
  if (path === '/api/v1/signals/subscription-targets') return jsonResponse({ items: subscriptionTargets })
  throw new Error(`unexpected request: ${path}`)
}

beforeEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

test('renders a labeled read-only alert queue without accessibility violations', async () => {
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => reads(input)))
  const { container } = render(<SignalsManager csrfToken="csrf" permissions={['signals.read']} />)

  expect(await screen.findByText('Contract renewal decision is approaching')).toBeInTheDocument()
  expect(screen.getAllByText('Active')).not.toHaveLength(0)
  expect(screen.getByText('Warning severity')).toBeInTheDocument()
  expect(screen.getByText(/read-only Signals access/)).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Acknowledge' })).not.toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'Export CSV' })).toHaveAttribute('href', '/api/v1/signals/report.csv?limit=500')
  expect((await axe.run(container)).violations).toEqual([])
})

test('creates a bounded rule and evaluates with CSRF', async () => {
  let currentRules = [rule]
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path === '/api/v1/signals/rules' && init?.method === 'POST') {
      const created = { ...rule, id: 'rule-expiration', name: 'License expiration', condition: 'expiration', severity: 'critical', thresholdDays: [90, 30] }
      currentRules = [...currentRules, created]
      return jsonResponse(created, 201)
    }
    if (path === '/api/v1/signals/evaluate' && init?.method === 'POST') return jsonResponse({ asOf: '2026-08-13T12:00:00Z', rules: 2, created: 1, refreshed: 1, resolved: 0 })
    if (path === '/api/v1/signals/rules/rule-renewal' && init?.method === 'PUT') {
      const updated = { ...currentRules[0], enabled: false, revision: 2 }
      currentRules = [updated, ...currentRules.slice(1)]
      return jsonResponse(updated)
    }
    if (path === '/api/v1/signals/rules') return jsonResponse({ items: currentRules })
    if (path.startsWith('/api/v1/signals/alerts?')) return jsonResponse({ items: [alert] })
    if (path === '/api/v1/signals/subscriptions') return jsonResponse({ items: [subscription] })
    if (path === '/api/v1/signals/subscription-targets') return jsonResponse({ items: subscriptionTargets })
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<SignalsManager csrfToken="csrf-token" permissions={['signals.read', 'signals.write']} />)
  await screen.findByText('Contract renewal decision is approaching')

  fireEvent.click(screen.getByRole('button', { name: 'Disable Contract renewals' }))
  expect(await screen.findByText('Alert rule disabled.')).toBeInTheDocument()
  const updateCall = fetchMock.mock.calls.find(([path, init]) => path === '/api/v1/signals/rules/rule-renewal' && init?.method === 'PUT')
  expect(JSON.parse(String(updateCall?.[1]?.body))).toMatchObject({ enabled: false, revision: 1, thresholdDays: [180, 90, 60, 30] })

  fireEvent.click(screen.getByText('Create an alert rule'))
  fireEvent.change(screen.getByLabelText('Rule name'), { target: { value: 'License expiration' } })
  fireEvent.change(screen.getByLabelText('Condition'), { target: { value: 'expiration' } })
  fireEvent.change(screen.getByLabelText('Severity'), { target: { value: 'critical' } })
  fireEvent.change(screen.getByLabelText(/Threshold days/), { target: { value: '90, 30' } })
  fireEvent.click(screen.getByRole('button', { name: 'Create rule' }))
  expect(await screen.findByText('Alert rule created.')).toBeInTheDocument()
  const createCall = fetchMock.mock.calls.find(([path, init]) => path === '/api/v1/signals/rules' && init?.method === 'POST')
  expect(createCall?.[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'csrf-token' })
  expect(JSON.parse(String(createCall?.[1]?.body))).toMatchObject({ name: 'License expiration', condition: 'expiration', severity: 'critical', thresholdDays: [90, 30] })

  fireEvent.click(screen.getByRole('button', { name: 'Evaluate now' }))
  expect(await screen.findByText('Evaluation complete: 1 created, 1 refreshed, and 0 resolved.')).toBeInTheDocument()
  const evaluateCall = fetchMock.mock.calls.find(([path]) => path === '/api/v1/signals/evaluate')
  expect(evaluateCall?.[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'csrf-token' })
})

test('acknowledges and assigns an alert with optimistic revisions', async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path.endsWith('/acknowledge') && init?.method === 'POST') return jsonResponse({ ...alert, status: 'acknowledged', acknowledgedBy: 'identity-one', acknowledgedAt: '2026-08-13T13:00:00Z', revision: 2 })
    if (path.endsWith('/assignment') && init?.method === 'PUT') return jsonResponse({ ...alert, assignedKind: 'group', assignedId: 'finance-owners', revision: 2 })
    return reads(input)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<SignalsManager csrfToken="csrf-token" permissions={['signals.read', 'signals.write']} />)
  await screen.findByText('Contract renewal decision is approaching')

  fireEvent.click(screen.getByRole('button', { name: 'Acknowledge' }))
  expect(await screen.findByText('Alert acknowledged.')).toBeInTheDocument()
  const acknowledgeCall = fetchMock.mock.calls.find(([path]) => String(path).endsWith('/acknowledge'))
  expect(JSON.parse(String(acknowledgeCall?.[1]?.body))).toEqual({ revision: 1 })

  const form = screen.getByRole('button', { name: 'Assign alert' }).closest('form')
  if (!form) throw new Error('assignment form missing')
  const configuredTarget = within(form).getByLabelText('Configured target ID') as HTMLInputElement
  expect(() => new RegExp(configuredTarget.pattern, 'v')).not.toThrow()
  fireEvent.change(within(form).getByLabelText('Assign to'), { target: { value: 'group' } })
  fireEvent.change(configuredTarget, { target: { value: 'finance-owners' } })
  fireEvent.click(within(form).getByRole('button', { name: 'Assign alert' }))
  expect(await screen.findByText('Alert assignment updated.')).toBeInTheDocument()
  const assignCall = fetchMock.mock.calls.find(([path]) => String(path).endsWith('/assignment'))
  expect(JSON.parse(String(assignCall?.[1]?.body))).toEqual({ kind: 'group', targetId: 'finance-owners', revision: 1 })
})

test('creates and removes configured subscriber references without accepting URLs', async () => {
  let currentSubscriptions = [subscription]
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path === '/api/v1/signals/subscriptions' && init?.method === 'POST') {
      const created = { ...subscription, id: 'subscription-two', targetKind: 'webhook', targetId: 'operations-hook' }
      currentSubscriptions = [...currentSubscriptions, created]
      return jsonResponse(created, 201)
    }
    if (path.endsWith('/subscription-one') && init?.method === 'DELETE') {
      currentSubscriptions = currentSubscriptions.filter((item) => item.id !== 'subscription-one')
      return new Response(null, { status: 204 })
    }
    if (path === '/api/v1/signals/rules') return jsonResponse({ items: [rule] })
    if (path.startsWith('/api/v1/signals/alerts?')) return jsonResponse({ items: [alert] })
    if (path === '/api/v1/signals/subscriptions') return jsonResponse({ items: currentSubscriptions })
    if (path === '/api/v1/signals/subscription-targets') return jsonResponse({ items: subscriptionTargets })
    throw new Error(`unexpected request: ${path}`)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<SignalsManager csrfToken="csrf-token" permissions={['signals.read', 'signals.write']} />)
  await screen.findByText('Finance owners')

  fireEvent.change(screen.getByLabelText('Delivery target'), { target: { value: 'webhook|operations-hook' } })
  fireEvent.click(screen.getByRole('button', { name: 'Create subscription' }))
  expect(await screen.findByText('Subscription created.')).toBeInTheDocument()
  const createCall = fetchMock.mock.calls.find(([path, init]) => path === '/api/v1/signals/subscriptions' && init?.method === 'POST')
  expect(JSON.parse(String(createCall?.[1]?.body))).toEqual({ ruleId: '', targetKind: 'webhook', targetId: 'operations-hook' })

  fireEvent.click(screen.getAllByRole('button', { name: 'Remove' })[0])
  expect(await screen.findByText('Subscription removed.')).toBeInTheDocument()
  await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/v1/signals/subscriptions/subscription-one', expect.objectContaining({ method: 'DELETE' })))
})

test('fails closed on malformed server records', async () => {
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    if (String(input) === '/api/v1/signals/rules') return jsonResponse({ items: [{ ...rule, thresholdDays: [-1] }] })
    if (String(input).startsWith('/api/v1/signals/alerts?')) return jsonResponse({ items: [] })
    return jsonResponse({ items: [] })
  }))
  render(<SignalsManager csrfToken="csrf" permissions={['signals.read']} />)
  expect(await screen.findByRole('alert')).toHaveTextContent('Signals could not be loaded.')
})

test('marks a disabled Reach target unavailable and prevents new subscriptions', async () => {
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const path = String(input)
    if (path === '/api/v1/signals/subscription-targets') return jsonResponse({ items: [] })
    return reads(input)
  }))
  render(<SignalsManager csrfToken="csrf" permissions={['signals.read', 'signals.write']} />)
  expect(await screen.findByText('Group target unavailable')).toBeInTheDocument()
  expect(screen.getByText(/New deliveries fail closed/)).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Create subscription' })).toBeDisabled()
  expect(screen.getByText(/Configure and enable a valid Reach group or webhook/)).toBeInTheDocument()
})
