import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import axe from 'axe-core'
import { afterEach, expect, test, vi } from 'vitest'
import VaultManager from './VaultManager'

// Requirement: REQ-STORAGE-001. Feature: storage.blobs.

const blob = {
  id: '0123456789abcdef0123456789abcdef', organizationId: 'example-org', name: 'evidence.txt',
  mediaType: 'text/plain', sizeBytes: 8, sha256: 'c2cc8e71a926b30b67bdb504ee9218d727d0bb9bc3b1d714736e198df3867d21',
  provider: 'local', createdBy: 'account-1', createdAt: '2026-08-10T12:00:00Z',
}

afterEach(() => vi.restoreAllMocks())

test('loads Vault metadata and prepares a temporary download accessibly', async () => {
  const fetchMock = vi.spyOn(globalThis, 'fetch')
    .mockResolvedValueOnce(new Response(JSON.stringify({ items: [blob], maximumUploadBytes: 26214400 }), { status: 200 }))
    .mockResolvedValueOnce(new Response(JSON.stringify({ url: `/api/v1/blobs/${blob.id}/content`, expiresAt: '2026-08-10T12:05:00Z' }), { status: 201 }))
  const { container } = render(<VaultManager csrfToken="csrf-value" permissions={['storage.read', 'storage.write']} />)
  expect(await screen.findByText('evidence.txt')).toBeInTheDocument()
  expect(screen.getByText(/SHA-256/)).toHaveTextContent(blob.sha256)
  fireEvent.click(screen.getByRole('button', { name: 'Prepare download' }))
  expect(await screen.findByRole('link', { name: 'Download ready' })).toHaveAttribute('href', `/api/v1/blobs/${blob.id}/content?download=1`)
  expect(fetchMock.mock.calls[1]?.[1]).toMatchObject({ method: 'POST', headers: { 'X-CSRF-Token': 'csrf-value' } })
  expect((await axe.run(container)).violations).toHaveLength(0)
})

test('uploads multipart content without setting a reusable credential', async () => {
  const fetchMock = vi.spyOn(globalThis, 'fetch')
    .mockResolvedValueOnce(new Response(JSON.stringify({ items: [], maximumUploadBytes: 26214400 }), { status: 200 }))
    .mockResolvedValueOnce(new Response(JSON.stringify(blob), { status: 201 }))
  render(<VaultManager csrfToken="csrf-value" permissions={['storage.read', 'storage.write']} />)
  await screen.findByText('No files have been stored yet.')
  const file = new File(['verified'], 'evidence.txt', { type: 'text/plain' })
  fireEvent.change(screen.getByLabelText('File'), { target: { files: [file] } })
  const form = screen.getByRole('button', { name: 'Upload to Vault' }).closest('form')
  if (!form) throw new Error('Vault upload form not found')
  fireEvent.submit(form)
  await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
  const request = fetchMock.mock.calls[1]?.[1]
  expect(request?.body).toBeInstanceOf(FormData)
  expect(request?.headers).not.toHaveProperty('Content-Type')
  expect(await screen.findByText('evidence.txt')).toBeInTheDocument()
})

test('explains missing Vault permission', () => {
  render(<VaultManager csrfToken="csrf-value" permissions={[]} />)
  expect(screen.getByText(/does not include permission/)).toBeInTheDocument()
})
