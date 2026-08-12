import { expect, test, vi } from 'vitest'
import { authenticationRequiredEventName, correlationEventName, getLastCorrelationId, requestJSON } from './api'

// Requirements: A11Y-001, DOC-002, SEC-HTTP-001. Feature: experience.help.

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
