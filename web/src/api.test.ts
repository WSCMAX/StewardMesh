import { expect, test, vi } from 'vitest'
import { correlationEventName, getLastCorrelationId, requestJSON } from './api'

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
