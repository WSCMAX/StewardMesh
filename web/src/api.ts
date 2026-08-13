// Requirements: SEC-HTTP-001, SEC-GUARD-001, REQ-PEOPLE-001, REQ-THREADS-001, REQ-WORKSPACE-001, A11Y-001, DOC-001, DOC-002.

const httpNoContent = 204
const correlationPattern = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/
let lastCorrelationId = ''

export const correlationEventName = 'stewardmesh:correlation'
export const authenticationRequiredEventName = 'stewardmesh:authentication-required'

export function getLastCorrelationId() { return lastCorrelationId }

function captureCorrelation(response: Response) {
  const candidate = response.headers.get('X-Correlation-ID')?.trim() ?? ''
  if (!correlationPattern.test(candidate)) return
  lastCorrelationId = candidate
  if (typeof window !== 'undefined') window.dispatchEvent(new CustomEvent(correlationEventName, { detail: candidate }))
}

export class ApiRequestError extends Error {
  status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiRequestError'
    this.status = status
  }
}

async function requestAPI(path: string, init: RequestInit | undefined, accept: string) {
  if (!path.startsWith('/') || path.startsWith('//')) throw new Error('API path must be same-origin')
  const response = await fetch(path, {
    ...init,
    credentials: 'same-origin',
    headers: {
      Accept: accept,
      ...init?.headers,
    },
  })
  captureCorrelation(response)
  if (!response.ok) {
    let message = 'The request could not be completed.'
    try {
      const body = await response.json() as unknown
      if (typeof body === 'object' && body !== null) {
        const error = (body as Record<string, unknown>).error
        if (typeof error === 'object' && error !== null) {
          const candidate = (error as Record<string, unknown>).message
          if (typeof candidate === 'string' && candidate.length > 0 && candidate.length <= 300) message = candidate
        }
      }
    } catch {
      // The status code remains authoritative when an intermediary returns non-JSON.
    }
    if (response.status === 401 && path !== '/api/v1/auth/session' && path !== '/api/v1/auth/login' && path !== '/api/v1/auth/bootstrap') {
      window.dispatchEvent(new CustomEvent(authenticationRequiredEventName))
    }
    throw new ApiRequestError(response.status, message)
  }
  return response
}

// requestJSON is same-origin by construction. Session cookies remain HttpOnly,
// and callers must explicitly add the in-memory CSRF value for mutations.
export async function requestJSON(path: string, init?: RequestInit): Promise<unknown> {
  const response = await requestAPI(path, init, 'application/json')
  if (response.status === httpNoContent) return undefined
  return response.json() as Promise<unknown>
}

// requestArtifact preserves the same correlation, session-expiry, CSRF, and
// same-origin behavior while leaving vector/PDF/printer-language bytes intact.
export async function requestArtifact(path: string, init?: RequestInit): Promise<Response> {
  return requestAPI(path, init, 'image/svg+xml, application/pdf, application/vnd.zebra-zpl')
}
