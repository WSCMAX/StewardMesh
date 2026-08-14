// Requirements: SEC-HTTP-001, SEC-GUARD-001, REQ-PEOPLE-001, REQ-THREADS-001, REQ-EXCHANGE-001, REQ-WORKSPACE-001, A11Y-001, DOC-001, DOC-002.

const httpNoContent = 204
const correlationPattern = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/
const maximumSafeRevision = BigInt(Number.MAX_SAFE_INTEGER)
const maximumRevision = 9223372036854775807n
let lastCorrelationId = ''

export type Revision = number | string

function isCanonicalPositiveDecimal(value: string) {
  if (value.length === 0 || value[0] < '1' || value[0] > '9') return false
  for (let index = 1; index < value.length; index += 1) {
    if (value[index] < '0' || value[index] > '9') return false
  }
  return true
}

function revisionInteger(value: string) {
  if (!isCanonicalPositiveDecimal(value) || value.length > 19) return undefined
  const parsed = BigInt(value)
  return parsed <= maximumRevision ? parsed : undefined
}

function isUnsafeRevisionString(value: string) {
  const parsed = revisionInteger(value)
  return parsed !== undefined && parsed > maximumSafeRevision
}

export function isRevision(value: unknown): value is Revision {
  if (typeof value === 'number') return Number.isSafeInteger(value) && value > 0
  return typeof value === 'string' && isUnsafeRevisionString(value)
}

type RevisionTransformMode = 'response' | 'request'

class RevisionJSONTransformer {
  private index = 0

  constructor(private readonly source: string, private readonly mode: RevisionTransformMode) {}

  transform() {
    const transformed = this.value(false) + this.whitespace()
    if (this.index !== this.source.length) this.invalidJSON()
    return transformed
  }

  private value(revisionField: boolean): string {
    let transformed = this.whitespace()
    const token = this.source[this.index]
    if (token === '{') return transformed + this.object()
    if (token === '[') return transformed + this.array()
    if (token === '"') {
      const raw = this.string()
      if (revisionField && this.mode === 'request') {
        const decoded = JSON.parse(raw) as string
        if (raw === JSON.stringify(decoded) && isUnsafeRevisionString(decoded)) return transformed + decoded
      }
      return transformed + raw
    }
    if (token === '-' || this.isDigit(token)) {
      const raw = this.number()
      if (revisionField) {
        if (this.mode === 'request' && raw === '0') return transformed + raw
        if (!isCanonicalPositiveDecimal(raw)) this.invalidRevision()
        const parsed = revisionInteger(raw)
        if (parsed === undefined) this.invalidRevision()
        if (parsed > maximumSafeRevision) {
          if (this.mode === 'request') this.invalidRevision()
          return transformed + JSON.stringify(raw)
        }
      }
      return transformed + raw
    }
    if (this.source.startsWith('true', this.index)) return transformed + this.literal('true')
    if (this.source.startsWith('false', this.index)) return transformed + this.literal('false')
    if (this.source.startsWith('null', this.index)) return transformed + this.literal('null')
    return this.invalidJSON()
  }

  private object() {
    let transformed = this.take('{') + this.whitespace()
    if (this.source[this.index] === '}') return transformed + this.take('}')
    while (true) {
      if (this.source[this.index] !== '"') this.invalidJSON()
      const rawKey = this.string()
      const key = JSON.parse(rawKey) as string
      transformed += rawKey + this.whitespace() + this.take(':')
      transformed += this.value(key === 'revision') + this.whitespace()
      const delimiter = this.source[this.index]
      if (delimiter === '}') return transformed + this.take('}')
      if (delimiter !== ',') this.invalidJSON()
      transformed += this.take(',') + this.whitespace()
    }
  }

  private array() {
    let transformed = this.take('[') + this.whitespace()
    if (this.source[this.index] === ']') return transformed + this.take(']')
    while (true) {
      transformed += this.value(false) + this.whitespace()
      const delimiter = this.source[this.index]
      if (delimiter === ']') return transformed + this.take(']')
      if (delimiter !== ',') this.invalidJSON()
      transformed += this.take(',') + this.whitespace()
    }
  }

  private string() {
    const start = this.index
    this.take('"')
    while (this.index < this.source.length) {
      const token = this.source[this.index]
      if (token === '"') {
        this.index += 1
        return this.source.slice(start, this.index)
      }
      if (token === '\\') {
        this.index += 1
        const escaped = this.source[this.index]
        if (escaped === 'u') {
          this.index += 1
          for (let count = 0; count < 4; count += 1) {
            if (!this.isHexDigit(this.source[this.index])) this.invalidJSON()
            this.index += 1
          }
          continue
        }
        if (escaped === undefined || !'"\\/bfnrt'.includes(escaped)) this.invalidJSON()
        this.index += 1
        continue
      }
      if (token.charCodeAt(0) < 0x20) this.invalidJSON()
      this.index += 1
    }
    return this.invalidJSON()
  }

  private number() {
    const start = this.index
    if (this.source[this.index] === '-') this.index += 1
    if (this.source[this.index] === '0') {
      this.index += 1
    } else {
      if (!this.isNonZeroDigit(this.source[this.index])) this.invalidJSON()
      while (this.isDigit(this.source[this.index])) this.index += 1
    }
    if (this.source[this.index] === '.') {
      this.index += 1
      if (!this.isDigit(this.source[this.index])) this.invalidJSON()
      while (this.isDigit(this.source[this.index])) this.index += 1
    }
    if (this.source[this.index] === 'e' || this.source[this.index] === 'E') {
      this.index += 1
      if (this.source[this.index] === '+' || this.source[this.index] === '-') this.index += 1
      if (!this.isDigit(this.source[this.index])) this.invalidJSON()
      while (this.isDigit(this.source[this.index])) this.index += 1
    }
    return this.source.slice(start, this.index)
  }

  private literal(value: string) {
    this.index += value.length
    return value
  }

  private whitespace() {
    const start = this.index
    while (' \n\r\t'.includes(this.source[this.index] ?? '\0')) this.index += 1
    return this.source.slice(start, this.index)
  }

  private take(expected: string) {
    if (this.source[this.index] !== expected) this.invalidJSON()
    this.index += 1
    return expected
  }

  private isDigit(value: string | undefined): value is string {
    return value !== undefined && value >= '0' && value <= '9'
  }

  private isNonZeroDigit(value: string | undefined): value is string {
    return value !== undefined && value >= '1' && value <= '9'
  }

  private isHexDigit(value: string | undefined): value is string {
    return value !== undefined && ((value >= '0' && value <= '9') || (value >= 'a' && value <= 'f') || (value >= 'A' && value <= 'F'))
  }

  private invalidJSON(): never {
    throw new SyntaxError(`Invalid JSON payload at position ${this.index}`)
  }

  private invalidRevision(): never {
    throw new SyntaxError(`Revision is outside the positive int64 range at position ${this.index}`)
  }
}

function transformRevisions(source: string, mode: RevisionTransformMode) {
  return new RevisionJSONTransformer(source, mode).transform()
}

function mutationRequest(init?: RequestInit) {
  const method = (init?.method ?? 'GET').toUpperCase()
  if (typeof init?.body !== 'string' || method === 'GET' || method === 'HEAD') return init
  return { ...init, body: transformRevisions(init.body, 'request') }
}

async function responseJSON(response: Response) {
  return JSON.parse(transformRevisions(await response.text(), 'response')) as unknown
}

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
  body?: unknown

  constructor(status: number, message: string, body?: unknown) {
    super(message)
    this.name = 'ApiRequestError'
    this.status = status
    this.body = body
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
    let responseBody: unknown
    try {
      responseBody = await responseJSON(response)
      if (typeof responseBody === 'object' && responseBody !== null) {
        const error = (responseBody as Record<string, unknown>).error
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
    throw new ApiRequestError(response.status, message, responseBody)
  }
  return response
}

// requestJSON is same-origin by construction. Session cookies remain HttpOnly,
// and callers must explicitly add the in-memory CSRF value for mutations.
export async function requestJSON(path: string, init?: RequestInit): Promise<unknown> {
  const response = await requestAPI(path, mutationRequest(init), 'application/json')
  if (response.status === httpNoContent) return undefined
  return responseJSON(response)
}

// requestArtifact preserves the same correlation, session-expiry, CSRF, and
// same-origin behavior while leaving vector/PDF/printer-language/package bytes intact.
export async function requestArtifact(path: string, init?: RequestInit): Promise<Response> {
  return requestAPI(path, init, 'image/svg+xml, application/pdf, application/vnd.stewardmesh.openinventory+zip')
}
