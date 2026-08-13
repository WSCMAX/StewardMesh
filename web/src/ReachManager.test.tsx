import axe from 'axe-core'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import ReachManager from './ReachManager'

// Requirement: REQ-REACH-001. Feature: messaging.delivery. GitHub: #12.

const endpoint = { id: 'hook-primary', label: 'Operations webhook', kind: 'webhook' }
const teamsEndpoint = { id: 'teams-primary', label: 'Operations Teams', kind: 'teams', destinationKey: 'operations-channel' }
const provider = { id: 'operations-hook', name: 'Operations webhook', kind: 'webhook', endpointId: endpoint.id, secretConfigured: true, enabled: true, revision: 1 }
const teamsProvider = { id: 'operations-teams', name: 'Operations Teams', kind: 'teams', endpointId: teamsEndpoint.id, secretConfigured: true, enabled: true, revision: 1 }
const template = { id: 'signal-template', name: 'Signal alert', subject: '{{severity}}: {{title}}', body: '{{summary}}\nRecord {{record_id}}', revision: 1 }
const group = { id: 'finance-owners', name: 'Finance owners', providerId: provider.id, templateId: template.id, recipients: [{ kind: 'email', address: 'owner@example.test' }], revision: 1 }
const message = { id: 'message-one', groupId: group.id, providerId: provider.id, sourceKind: 'manual', subject: 'Warning: Renewal', body: 'Renewal due', recipients: group.recipients, status: 'retrying', attempts: 1, nextAttemptAt: '2026-08-13T12:05:00Z', lastErrorCode: 'provider_unavailable', createdAt: '2026-08-13T12:00:00Z', updatedAt: '2026-08-13T12:00:00Z' }
const attempt = { id: 'attempt-one', messageId: message.id, attempt: 1, outcome: 'retrying', errorCode: 'provider_unavailable', retryable: true, nextAttemptAt: '2026-08-13T12:05:00Z', occurredAt: '2026-08-13T12:00:00Z' }

function response(body: unknown, status = 200) { return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }) }
function reads(input: RequestInfo | URL) {
  const path = String(input)
  if (path === '/api/v1/reach/endpoints') return response({ items: [endpoint, teamsEndpoint] })
  if (path === '/api/v1/reach/providers') return response({ items: [provider, teamsProvider] })
  if (path === '/api/v1/reach/providers/operations-hook/tests') return response({ items: [] })
  if (path === '/api/v1/reach/providers/operations-teams/tests') return response({ items: [] })
  if (path === '/api/v1/reach/templates') return response({ items: [template] })
  if (path === '/api/v1/reach/groups') return response({ items: [group] })
  if (path === '/api/v1/reach/messages?limit=100') return response({ items: [message] })
  if (path === '/api/v1/reach/messages/message-one/attempts') return response({ items: [attempt] })
  throw new Error(`unexpected request: ${path}`)
}

beforeEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals() })

test('renders a redacted read-only delivery workspace without accessibility violations', async () => {
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => reads(input)))
  const { container } = render(<ReachManager csrfToken="csrf" permissions={['messaging.read']} />)
  expect(await screen.findByText('Operations webhook')).toBeInTheDocument()
  expect(screen.getByText(/read-only Reach access/)).toBeInTheDocument()
  expect(screen.queryByLabelText('External secret reference')).not.toBeInTheDocument()
  expect(screen.getByText('Warning: Renewal')).toBeInTheDocument()
  expect(screen.queryByText(/hooks\.example/)).not.toBeInTheDocument()
  for (const input of screen.queryAllByLabelText(/secret reference/i) as HTMLInputElement[]) expect(() => new RegExp(input.pattern, 'v')).not.toThrow()
  expect((await axe.run(container)).violations).toEqual([])
})

test('confirms provider tests, sends, retries, and loads sanitized attempt history with CSRF', async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path.endsWith('/test') && init?.method === 'POST') return response({ id: 'provider-test-one', providerId: provider.id, outcome: 'succeeded', testedBy: 'admin', testedAt: '2026-08-13T12:01:00Z' })
    if (path === '/api/v1/reach/messages/send' && init?.method === 'POST') return response({ ...message, id: 'message-two', status: 'delivered', attempts: 1, nextAttemptAt: undefined, lastErrorCode: undefined }, 202)
    if (path.endsWith('/retry') && init?.method === 'POST') return response({ ...message, status: 'delivered', attempts: 2, nextAttemptAt: undefined, lastErrorCode: undefined })
    return reads(input)
  })
  vi.stubGlobal('fetch', fetchMock)
  vi.stubGlobal('crypto', { randomUUID: () => 'manual-send-one' })
  render(<ReachManager csrfToken="csrf-token" permissions={['messaging.read', 'messaging.write']} />)
  await screen.findByText('Warning: Renewal')

  fireEvent.click(screen.getByRole('button', { name: 'Confirm and test Operations webhook' }))
  expect(await screen.findByText('Provider test recorded. No message content was sent.')).toBeInTheDocument()
  const testCall = fetchMock.mock.calls.find(([path]) => String(path).endsWith('/test'))
  expect(testCall?.[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'csrf-token' })
  expect(JSON.parse(String(testCall?.[1]?.body))).toEqual({ confirm: true })

  const sendForm = screen.getByRole('button', { name: 'Confirm and send' }).closest('form')
  if (!sendForm) throw new Error('send form missing')
  fireEvent.change(within(sendForm).getByLabelText('Subscriber group'), { target: { value: group.id } })
  fireEvent.change(within(sendForm).getByLabelText('Title'), { target: { value: 'Renewal' } })
  fireEvent.change(within(sendForm).getByLabelText('Summary'), { target: { value: 'Renewal is due.' } })
  fireEvent.change(within(sendForm).getByLabelText('Severity'), { target: { value: 'warning' } })
  fireEvent.change(within(sendForm).getByLabelText('Record ID'), { target: { value: 'contract-one' } })
  fireEvent.click(within(sendForm).getByRole('checkbox'))
  fireEvent.click(within(sendForm).getByRole('button', { name: 'Confirm and send' }))
  expect(await screen.findByText(/Confirmed message attempted/)).toBeInTheDocument()
  const sendCall = fetchMock.mock.calls.find(([path]) => path === '/api/v1/reach/messages/send')
  expect(sendCall?.[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'csrf-token', 'Idempotency-Key': 'manual-send-one' })
  expect(JSON.parse(String(sendCall?.[1]?.body))).toMatchObject({ groupId: group.id, confirm: true, variables: { severity: 'warning', record_id: 'contract-one' } })

  fireEvent.click(screen.getByRole('button', { name: 'View attempts' }))
  expect(await screen.findByText(/Attempt 1:/)).toBeInTheDocument()
  expect(screen.getAllByText(/Provider Unavailable/)).not.toHaveLength(0)
  fireEvent.click(screen.getByRole('button', { name: 'Confirm retry' }))
  expect(await screen.findByText('Confirmed retry attempted.')).toBeInTheDocument()
  await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/v1/reach/messages/message-one/retry', expect.objectContaining({ method: 'POST' })))
})

test('rejects raw credentials before the provider API is invoked and fails closed on malformed endpoint data', async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, _init?: RequestInit) => reads(input))
  vi.stubGlobal('fetch', fetchMock)
  render(<ReachManager csrfToken="csrf" permissions={['messaging.read', 'messaging.write']} />)
  expect(await screen.findAllByText('Operations webhook')).not.toHaveLength(0)
  for (const input of screen.getAllByLabelText(/secret reference/i) as HTMLInputElement[]) expect(() => new RegExp(input.pattern, 'v')).not.toThrow()
  fireEvent.change(screen.getByLabelText('Provider name'), { target: { value: 'New hook' } })
  fireEvent.change(screen.getByLabelText('Approved endpoint'), { target: { value: endpoint.id } })
  fireEvent.change(screen.getByLabelText('External secret reference'), { target: { value: '01234567890123456789012345678901' } })
  fireEvent.submit(screen.getByRole('button', { name: 'Configure provider' }).closest('form') as HTMLFormElement)
  expect(await screen.findByRole('alert')).toHaveTextContent('Use an external secret reference')
  expect(fetchMock.mock.calls.filter(([, init]) => init?.method === 'POST')).toHaveLength(0)

  cleanup(); vi.unstubAllGlobals()
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => String(input) === '/api/v1/reach/endpoints' ? response({ items: [{ ...endpoint, url: 'https://forbidden.example' }] }) : reads(input)))
  render(<ReachManager csrfToken="csrf" permissions={['messaging.read']} />)
  expect((await screen.findAllByRole('alert')).at(-1)).toHaveTextContent('Reach could not be loaded.')
})

test('derives the exact Teams recipient from safe endpoint metadata', async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path === '/api/v1/reach/groups' && init?.method === 'POST') {
      const body = JSON.parse(String(init.body)) as Record<string, unknown>
      return response({ id: 'operations-team-group', name: body.name, providerId: teamsProvider.id, templateId: template.id, recipients: body.recipients, revision: 1 }, 201)
    }
    return reads(input)
  })
  vi.stubGlobal('fetch', fetchMock)
  render(<ReachManager csrfToken="csrf-token" permissions={['messaging.read', 'messaging.write']} />)
  await screen.findByText('Warning: Renewal')

  const groupForm = screen.getByRole('button', { name: 'Create group' }).closest('form')
  if (!groupForm) throw new Error('group form missing')
  fireEvent.change(within(groupForm).getByLabelText('Group name'), { target: { value: 'Operations team' } })
  fireEvent.change(within(groupForm).getByLabelText('Provider'), { target: { value: teamsProvider.id } })
  fireEvent.change(within(groupForm).getByLabelText('Template'), { target: { value: template.id } })
  const destination = within(groupForm).getByLabelText('Configured Teams destination') as HTMLInputElement
  expect(destination).toHaveValue(teamsEndpoint.destinationKey)
  expect(destination).toHaveAttribute('readonly')
  expect(within(groupForm).queryByLabelText('Recipient address')).not.toBeInTheDocument()
  fireEvent.click(within(groupForm).getByRole('button', { name: 'Create group' }))
  expect(await screen.findByText('Subscriber group created.')).toBeInTheDocument()
  const createCall = fetchMock.mock.calls.find(([path, init]) => path === '/api/v1/reach/groups' && init?.method === 'POST')
  expect(JSON.parse(String(createCall?.[1]?.body))).toMatchObject({
    providerId: teamsProvider.id,
    recipients: [{ kind: 'channel', address: teamsEndpoint.destinationKey }],
  })
})
