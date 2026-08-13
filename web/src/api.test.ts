import { expect, test, vi } from 'vitest'
import { authenticationRequiredEventName, correlationEventName, getLastCorrelationId, requestArtifact, requestJSON } from './api'

// Requirements: REQ-WORKSPACE-001, A11Y-001, DOC-002, SEC-HTTP-001. Features: experience.workspace, experience.help.

test('captures only a valid response correlation ID for Guide reports', async () => {
  const listener = vi.fn()
  window.addEventListener(correlationEventName, listener)
  vi.stubGlobal('fetch', vi.fn()
    .mockResolvedValueOnce(new Response(JSON.stringify({ ok: true }), { headers: { 'Content-Type': 'application/json', 'X-Correlation-ID': 'request-abc_123' } }))
    .mockResolvedValueOnce(new Response(JSON.stringify({ ok: true }), { headers: { 'Content-Type': 'application/json', 'X-Correlation-ID': 'unsafe correlation value' } })))

  await requestJSON('/api/v1/example')
  expect(getLastCorrelationId()).toBe('request-abc_123')
  expect(listener).toHaveBeenCalledOnce()
  await requestJSON('/api/v1/example')
  expect(getLastCorrelationId()).toBe('request-abc_123')
  expect(listener).toHaveBeenCalledOnce()
  window.removeEventListener(correlationEventName, listener)
})

test('announces an expired authenticated request without treating initial session discovery as an expiration', async () => {
  const listener = vi.fn()
  window.addEventListener(authenticationRequiredEventName, listener)
  vi.stubGlobal('fetch', vi.fn()
    .mockResolvedValueOnce(new Response(JSON.stringify({ error: { message: 'expired' } }), { status: 401, headers: { 'Content-Type': 'application/json' } }))
    .mockResolvedValueOnce(new Response(JSON.stringify({ error: { message: 'sign in is required' } }), { status: 401, headers: { 'Content-Type': 'application/json' } })))

  await expect(requestJSON('/api/v1/assets')).rejects.toMatchObject({ status: 401 })
  expect(listener).toHaveBeenCalledOnce()
  await expect(requestJSON('/api/v1/auth/session')).rejects.toMatchObject({ status: 401 })
  expect(listener).toHaveBeenCalledOnce()
  window.removeEventListener(authenticationRequiredEventName, listener)
})

test('retrieves print artifacts with the same-origin session and requested media types intact', async () => {
  const fetchMock = vi.fn(async () => new Response('%PDF-1.4', { headers: { 'Content-Type': 'application/pdf', 'X-Correlation-ID': 'label-request-1' } }))
  vi.stubGlobal('fetch', fetchMock)
  const response = await requestArtifact('/api/v1/asset-label-batches', { method: 'POST', headers: { 'X-CSRF-Token': 'csrf' } })
  expect(await response.text()).toBe('%PDF-1.4')
  expect(fetchMock).toHaveBeenCalledWith('/api/v1/asset-label-batches', expect.objectContaining({
    credentials: 'same-origin',
    headers: expect.objectContaining({ Accept: 'image/svg+xml, application/pdf, application/vnd.zebra-zpl', 'X-CSRF-Token': 'csrf' }),
  }))
  await expect(requestArtifact('https://printer.example/labels')).rejects.toThrow('same-origin')
})
