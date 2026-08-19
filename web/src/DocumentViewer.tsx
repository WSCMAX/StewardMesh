import { useEffect, useState } from 'react'
import { ApiRequestError, requestArtifact, requestJSON } from './api'
import { secondaryButtonClass, subpanelClass } from './ui'

export type ViewableDocument = {
  id: string
  name: string
  mediaType: string
}

type DocumentViewerProps = {
  csrfToken: string
  document: ViewableDocument
  onClose?: () => void
}

type DownloadAuthorization = { url: string; expiresAt: string }

const wordTypes = new Set([
  'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
  'application/msword',
])

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}

export function parseDownloadAuthorization(value: unknown): DownloadAuthorization {
  if (!isObject(value) || typeof value.url !== 'string' || typeof value.expiresAt !== 'string') {
    throw new Error('invalid download authorization')
  }
  const sameOrigin = value.url.startsWith('/') && !value.url.startsWith('//')
  if (!sameOrigin) {
    const parsed = new URL(value.url)
    const localHTTP = parsed.protocol === 'http:' && ['localhost', '127.0.0.1', '[::1]'].includes(parsed.hostname)
    if (parsed.protocol !== 'https:' && !localHTTP) throw new Error('unsafe download authorization')
  }
  return { url: value.url, expiresAt: value.expiresAt }
}

export function documentKind(mediaType: string, name = '') {
  const type = mediaType.toLowerCase()
  const lowerName = name.toLowerCase()
  if (type.startsWith('image/') || /\.(png|jpe?g|gif|webp|svg)$/.test(lowerName)) return 'image'
  if (type === 'application/pdf' || lowerName.endsWith('.pdf')) return 'pdf'
  if (type.startsWith('text/') || type === 'application/json' || type === 'text/html' || /\.(txt|html|json|csv|md)$/.test(lowerName)) return 'text'
  if (wordTypes.has(type) || /\.(docx|doc)$/.test(lowerName)) return 'word'
  return 'other'
}

function readUint16(bytes: Uint8Array, offset: number) {
  return bytes[offset] | (bytes[offset + 1] << 8)
}

function readUint32(bytes: Uint8Array, offset: number) {
  return (bytes[offset] | (bytes[offset + 1] << 8) | (bytes[offset + 2] << 16) | (bytes[offset + 3] << 24)) >>> 0
}

async function inflateRaw(payload: Uint8Array) {
  if (typeof DecompressionStream !== 'function') throw new Error('deflate is unavailable in this browser')
  const stream = new Blob([payload as BlobPart]).stream().pipeThrough(new DecompressionStream('deflate-raw'))
  return new Uint8Array(await new Response(stream).arrayBuffer())
}

export async function zipEntry(buffer: ArrayBuffer, path: string) {
  const bytes = new Uint8Array(buffer)
  let offset = 0
  while (offset + 30 <= bytes.length) {
    if (bytes[offset] !== 0x50 || bytes[offset + 1] !== 0x4b || bytes[offset + 2] !== 0x03 || bytes[offset + 3] !== 0x04) break
    const compression = readUint16(bytes, offset + 8)
    const compressedSize = readUint32(bytes, offset + 18)
    const nameLength = readUint16(bytes, offset + 26)
    const extraLength = readUint16(bytes, offset + 28)
    const nameStart = offset + 30
    const name = new TextDecoder().decode(bytes.slice(nameStart, nameStart + nameLength))
    const dataStart = nameStart + nameLength + extraLength
    const dataEnd = dataStart + compressedSize
    if (dataEnd > bytes.length) break
    if (name === path) {
      const payload = bytes.slice(dataStart, dataEnd)
      if (compression === 0) return payload
      if (compression === 8) return inflateRaw(payload)
      throw new Error(`unsupported zip compression ${compression}`)
    }
    offset = dataEnd
  }
  throw new Error(`${path} was not found in the document`)
}

export async function extractDocxText(buffer: ArrayBuffer) {
  const xmlBytes = await zipEntry(buffer, 'word/document.xml')
  const xml = new TextDecoder().decode(xmlBytes)
  const parts = [...xml.matchAll(/<w:t[^>]*>([^<]*)<\/w:t>/g)].map((match) => match[1]
    .replaceAll('&amp;', '&')
    .replaceAll('&lt;', '<')
    .replaceAll('&gt;', '>'))
  return parts.join(' ').replace(/\s+/g, ' ').trim()
}

export default function DocumentViewer({ csrfToken, document: item, onClose }: DocumentViewerProps) {
  const kind = documentKind(item.mediaType, item.name)
  const [busy, setBusy] = useState(true)
  const [error, setError] = useState('')
  const [objectUrl, setObjectUrl] = useState('')
  const [text, setText] = useState('')
  const [downloadUrl, setDownloadUrl] = useState('')

  useEffect(() => {
    let active = true
    let createdUrl = ''
    async function load() {
      setBusy(true)
      setError('')
      setText('')
      setObjectUrl('')
      try {
        const authorization = parseDownloadAuthorization(await requestJSON(`/api/v1/blobs/${encodeURIComponent(item.id)}/download-authorization`, {
          method: 'POST', headers: { 'X-CSRF-Token': csrfToken },
        }))
        if (!active) return
        setDownloadUrl(authorization.url)
        const response = await requestArtifact(authorization.url)
        const buffer = await response.arrayBuffer()
        if (!active) return
        if (kind === 'word') {
          setText(await extractDocxText(buffer) || 'This Word document has no readable text.')
        } else if (kind === 'text') {
          setText(new TextDecoder().decode(buffer))
        } else {
          createdUrl = URL.createObjectURL(new Blob([buffer], { type: item.mediaType || 'application/octet-stream' }))
          setObjectUrl(createdUrl)
        }
      } catch (cause) {
        if (active) setError(cause instanceof ApiRequestError ? cause.message : 'This document could not be opened in the browser.')
      } finally {
        if (active) setBusy(false)
      }
    }
    void load()
    return () => {
      active = false
      if (createdUrl) URL.revokeObjectURL(createdUrl)
    }
  }, [csrfToken, item.id, item.mediaType, kind])

  return (
    <section aria-label={`Preview ${item.name}`} className={`${subpanelClass} p-4`}>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="text-sm font-semibold text-steward-teal">Document preview</p>
          <h3 className="mt-1 break-words font-semibold">{item.name}</h3>
          <p className="mt-1 break-all text-xs text-steward-mist-muted">{item.mediaType}</p>
        </div>
        <div className="flex flex-wrap gap-2">
          {downloadUrl && <a className={secondaryButtonClass} href={`${downloadUrl}${downloadUrl.includes('?') ? '&' : '?'}download=1`}>Download</a>}
          {onClose && <button className={secondaryButtonClass} onClick={onClose} type="button">Close preview</button>}
        </div>
      </div>
      {busy && <p className="mt-4 text-sm text-steward-mist-muted" role="status">Opening document…</p>}
      {error && <p className="mt-4 text-sm text-[#ffccd1]" role="alert">{error}</p>}
      {!busy && !error && kind === 'image' && objectUrl && <img alt={item.name} className="mt-4 max-h-[32rem] w-full rounded-lg bg-black/30 object-contain" src={objectUrl} />}
      {!busy && !error && kind === 'pdf' && objectUrl && <iframe className="mt-4 h-[32rem] w-full rounded-lg bg-white" src={objectUrl} title={item.name} />}
      {!busy && !error && kind === 'text' && <iframe className="mt-4 h-[24rem] w-full rounded-lg bg-white text-black" sandbox="allow-same-origin" srcDoc={text} title={item.name} />}
      {!busy && !error && kind === 'word' && <div className="mt-4 max-h-[24rem] overflow-auto rounded-lg bg-white p-4 text-sm leading-6 text-steward-ink-950">{text}</div>}
      {!busy && !error && kind === 'other' && objectUrl && <p className="mt-4 text-sm text-steward-mist-muted">This file type cannot be previewed here. Use download to open it locally.</p>}
    </section>
  )
}
