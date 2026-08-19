import { render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import DocumentViewer, { documentKind, extractDocxText } from './DocumentViewer'

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
