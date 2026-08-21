import { render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import DocumentViewer, { documentDownloadHref, documentKind, extractDocxText } from './DocumentViewer'

beforeEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

test('classifies common previewable document types', () => {
  expect(documentKind('application/pdf', 'quote.pdf')).toBe('pdf')
  expect(documentKind('image/png', 'photo.png')).toBe('image')
  expect(documentKind('text/html', 'contract.html')).toBe('text')
  expect(documentKind('application/vnd.openxmlformats-officedocument.wordprocessingml.document', 'agreement.docx')).toBe('word')
})

test('keeps presigned S3 download URLs unmodified', () => {
  const presigned = 'https://bucket.s3.us-east-1.amazonaws.com/object?X-Amz-Signature=abc'
  expect(documentDownloadHref(presigned)).toBe(presigned)
  expect(documentDownloadHref('/api/v1/blobs/id/content?token=abc')).toBe('/api/v1/blobs/id/content?token=abc&download=1')
})

test('extracts readable text from a stored Word document', async () => {
  const xml = '<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:t>Volume license</w:t></w:document>'
  const encoded = new TextEncoder().encode(xml)
  const name = 'word/document.xml'
  const nameBytes = new TextEncoder().encode(name)
  const header = new Uint8Array(30 + nameBytes.length + encoded.length)
  header.set([0x50, 0x4b, 0x03, 0x04], 0)
  header[26] = nameBytes.length
  header[18] = encoded.length
  header.set(nameBytes, 30)
  header.set(encoded, 30 + nameBytes.length)
  await expect(extractDocxText(header.buffer)).resolves.toBe('Volume license')
})

test('extracts Word text when the local header uses a data descriptor', async () => {
  const xml = '<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:t>Volume license</w:t></w:document>'
  const payload = new TextEncoder().encode(xml)
  const name = 'word/document.xml'
  const nameBytes = new TextEncoder().encode(name)
  const localSize = 30 + nameBytes.length
  const centralSize = 46 + nameBytes.length
  const bytes = new Uint8Array(localSize + payload.length + centralSize + 22)
  bytes.set([0x50, 0x4b, 0x03, 0x04], 0)
  bytes[6] = 0x08
  bytes[26] = nameBytes.length
  bytes.set(nameBytes, 30)
  bytes.set(payload, localSize)
  const central = localSize + payload.length
  bytes.set([0x50, 0x4b, 0x01, 0x02], central)
  bytes[central + 20] = payload.length
  bytes[central + 28] = nameBytes.length
  bytes.set(nameBytes, central + 46)
  const eocd = central + centralSize
  bytes.set([0x50, 0x4b, 0x05, 0x06], eocd)
  bytes[eocd + 10] = 1
  bytes[eocd + 12] = centralSize
  bytes[eocd + 16] = central & 0xff
  bytes[eocd + 17] = (central >> 8) & 0xff
  await expect(extractDocxText(bytes.buffer)).resolves.toBe('Volume license')
})

test('authorizes and previews an image in the browser', async () => {
  const objectUrl = 'blob:http://localhost/preview'
  vi.stubGlobal('URL', { ...URL, createObjectURL: () => objectUrl, revokeObjectURL: () => undefined })
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    if (path.includes('download-authorization') && init?.method === 'POST') {
      return new Response(JSON.stringify({ url: '/api/v1/blobs/0123456789abcdef0123456789abcdef/content?token=abc', expiresAt: '2026-08-14T12:05:00Z' }), { status: 201 })
    }
    if (path.startsWith('/api/v1/blobs/')) {
      return new Response(new Uint8Array([137, 80, 78, 71]), { status: 200, headers: { 'Content-Type': 'image/png' } })
    }
    throw new Error(`unexpected request ${path}`)
  }))
  render(<DocumentViewer csrfToken="csrf" document={{ id: '0123456789abcdef0123456789abcdef', name: 'Lab-station-photo.png', mediaType: 'image/png' }} />)
  await waitFor(() => expect(screen.getByRole('img', { name: 'Lab-station-photo.png' })).toHaveAttribute('src', objectUrl))
  expect(screen.getByRole('link', { name: 'Download' })).toHaveAttribute('href', expect.stringContaining('download=1'))
})

test('fetches a validated presigned HTTPS URL without rewriting the signature', async () => {
  const objectUrl = 'blob:http://localhost/preview'
  const presigned = 'https://bucket.s3.us-east-1.amazonaws.com/photo.png?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Signature=abc'
  vi.spyOn(URL, 'createObjectURL').mockReturnValue(objectUrl)
  vi.spyOn(URL, 'revokeObjectURL').mockReturnValue(undefined)
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = input instanceof Request ? input.url : String(input)
    if (path.includes('download-authorization') && init?.method === 'POST') {
      return new Response(JSON.stringify({ url: presigned, expiresAt: '2026-08-14T12:05:00Z' }), { status: 201 })
    }
    if (path === presigned || path.includes('X-Amz-Signature=abc')) {
      return new Response(new Uint8Array([137, 80, 78, 71]), { status: 200, headers: { 'Content-Type': 'image/png' } })
    }
    throw new Error(`unexpected request ${path}`)
  }))
  render(<DocumentViewer csrfToken="csrf" document={{ id: '0123456789abcdef0123456789abcdef', name: 'Lab-station-photo.png', mediaType: 'image/png' }} />)
  await waitFor(() => expect(screen.getByRole('img', { name: 'Lab-station-photo.png' })).toHaveAttribute('src', objectUrl))
  expect(screen.getByRole('link', { name: 'Download' })).toHaveAttribute('href', presigned)
})
