import { expect, test, vi } from 'vitest'
import { authenticationRequiredEventName, correlationEventName, getLastCorrelationId, isRevision, requestArtifact, requestJSON } from './api'

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

  await expect(requestJSON('/api/v1/assets')).rejects.toMatchObject({ status: 401, body: { error: { message: 'expired' } } })
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
    headers: expect.objectContaining({ Accept: 'image/svg+xml, application/pdf, application/vnd.stewardmesh.openinventory+zip', 'X-CSRF-Token': 'csrf' }),
  }))
  await expect(requestArtifact('https://printer.example/labels')).rejects.toThrow('same-origin')
})

test('accepts safe numeric and unsafe canonical int64 revisions only', () => {
  expect(isRevision(1)).toBe(true)
  expect(isRevision(Number.MAX_SAFE_INTEGER)).toBe(true)
  expect(isRevision('9007199254740992')).toBe(true)
  expect(isRevision('9223372036854775807')).toBe(true)

  expect(isRevision(0)).toBe(false)
  expect(isRevision(Number.MAX_SAFE_INTEGER + 1)).toBe(false)
  expect(isRevision('1')).toBe(false)
  expect(isRevision('09007199254740992')).toBe(false)
  expect(isRevision('9223372036854775808')).toBe(false)
  expect(isRevision('-9007199254740992')).toBe(false)
})

test('preserves unsafe response revisions through escaped keys, arrays, and nested objects', async () => {
  const source = '{"revision":9007199254740993,"safe":{"revision":17},"items":[{"revis\\u0069on":9223372036854775807}],"note":"embedded {\\"revision\\":9007199254740995}"}'
  vi.stubGlobal('fetch', vi.fn(async () => new Response(source, { headers: { 'Content-Type': 'application/json' } })))

  const value = await requestJSON('/api/v1/revisions') as {
    revision: unknown
    safe: { revision: unknown }
    items: Array<{ revision: unknown }>
    note: string
  }
  expect(value).toEqual({
    revision: '9007199254740993',
    safe: { revision: 17 },
    items: [{ revision: '9223372036854775807' }],
    note: 'embedded {"revision":9007199254740995}',
  })
})

test('writes canonical unsafe revision strings as exact JSON integers without changing other strings or fields', async () => {
  const fetchMock = vi.fn(async () => new Response('{"revision":9007199254740996}', { headers: { 'Content-Type': 'application/json' } }))
  vi.stubGlobal('fetch', fetchMock)
  const body = '{"revision":"9007199254740993","safe":"17","note":"{\\"revision\\":\\"9007199254740994\\"}","nested":{"revis\\u0069on":"9223372036854775807"},"escaped":{"revision":"\\u0039007199254740993"},"otherRevision":"9007199254740995"}'

  await expect(requestJSON('/api/v1/revisions/example', { method: 'PUT', body })).resolves.toEqual({ revision: '9007199254740996' })
  expect(fetchMock).toHaveBeenCalledWith('/api/v1/revisions/example', expect.objectContaining({
    body: '{"revision":9007199254740993,"safe":"17","note":"{\\"revision\\":\\"9007199254740994\\"}","nested":{"revis\\u0069on":9223372036854775807},"escaped":{"revision":"\\u0039007199254740993"},"otherRevision":"9007199254740995"}',
  }))
})

test('passes the numeric revision zero create sentinel only in mutation requests', async () => {
  const fetchMock = vi.fn(async () => new Response('{"revision":1}', { headers: { 'Content-Type': 'application/json' } }))
  vi.stubGlobal('fetch', fetchMock)

  await expect(requestJSON('/api/v1/revisions/example', {
    method: 'PUT',
    body: '{"mode":"include","revision":0}',
  })).resolves.toEqual({ revision: 1 })
  expect(fetchMock).toHaveBeenCalledWith('/api/v1/revisions/example', expect.objectContaining({
    body: '{"mode":"include","revision":0}',
  }))

  for (const revision of ['-1', '1.5', '1e2']) {
    await expect(requestJSON('/api/v1/revisions/example', {
      method: 'PUT', body: `{"revision":${revision}}`,
    })).rejects.toThrow(SyntaxError)
  }
  expect(fetchMock).toHaveBeenCalledTimes(1)
})

test('rejects malformed JSON and out-of-range response revisions without losing precision', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => new Response('{"revision":9223372036854775808}', { headers: { 'Content-Type': 'application/json' } })))
  await expect(requestJSON('/api/v1/revisions')).rejects.toThrow(SyntaxError)

  vi.stubGlobal('fetch', vi.fn(async () => new Response('{"revision":9007199254740993 trailing}', { headers: { 'Content-Type': 'application/json' } })))
  await expect(requestJSON('/api/v1/revisions')).rejects.toThrow(SyntaxError)

  const fetchMock = vi.fn()
  vi.stubGlobal('fetch', fetchMock)
  await expect(requestJSON('/api/v1/revisions/example', { method: 'PATCH', body: '{"revision":"9007199254740993"' })).rejects.toThrow(SyntaxError)
  expect(fetchMock).not.toHaveBeenCalled()
})

test('rejects non-canonical revision numbers and unsafe numeric mutation bodies', async () => {
  for (const revision of ['0', '-1', '1.5', '1e2']) {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(`{"revision":${revision}}`, { headers: { 'Content-Type': 'application/json' } })))
    await expect(requestJSON('/api/v1/revisions')).rejects.toThrow(SyntaxError)
  }

  const fetchMock = vi.fn()
  vi.stubGlobal('fetch', fetchMock)
  await expect(requestJSON('/api/v1/revisions/example', { method: 'PUT', body: '{"revision":9007199254740993}' })).rejects.toThrow(SyntaxError)
  expect(fetchMock).not.toHaveBeenCalled()
})
