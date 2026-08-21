import { type FormEvent, useEffect, useRef, useState } from 'react'
import { ApiRequestError, requestJSON } from './api'
import DocumentViewer, { type ViewableDocument } from './DocumentViewer'
import { ProductHeader, buttonClass, inputClass, labelClass, panelClass, secondaryButtonClass, subpanelClass, tableWrapClass } from './ui'

export type VaultBlob = {
  id: string
  organizationId: string
  name: string
  mediaType: string
  sizeBytes: number
  sha256: string
  provider: 'local' | 's3'
  sourceSystemId?: string
  sourceRecordId?: string
  resourceType?: string
  resourceId?: string
  createdBy: string
  createdAt: string
}

type VaultResponse = { items: VaultBlob[]; maximumUploadBytes: number }
type DownloadAuthorization = { url: string; expiresAt: string }
type VaultManagerProps = { csrfToken: string; permissions: readonly string[]; onOpenHelp?: () => void }

export function isVaultBlob(value: unknown): value is VaultBlob {
  if (typeof value !== 'object' || value === null) return false
  const candidate = value as Record<string, unknown>
  return typeof candidate.id === 'string' && /^[a-f0-9]{32}$/.test(candidate.id)
    && typeof candidate.organizationId === 'string' && candidate.organizationId.length > 0
    && typeof candidate.name === 'string' && candidate.name.length > 0 && candidate.name.length <= 255
    && typeof candidate.mediaType === 'string' && candidate.mediaType.length > 0
    && typeof candidate.sizeBytes === 'number' && Number.isSafeInteger(candidate.sizeBytes) && candidate.sizeBytes >= 0
    && typeof candidate.sha256 === 'string' && /^[a-f0-9]{64}$/.test(candidate.sha256)
    && (candidate.provider === 'local' || candidate.provider === 's3')
    && typeof candidate.createdBy === 'string' && typeof candidate.createdAt === 'string'
}

function parseVaultResponse(value: unknown): VaultResponse {
  if (typeof value !== 'object' || value === null) throw new Error('invalid Vault response')
  const candidate = value as Record<string, unknown>
  if (!Array.isArray(candidate.items) || !candidate.items.every(isVaultBlob)
    || typeof candidate.maximumUploadBytes !== 'number' || !Number.isSafeInteger(candidate.maximumUploadBytes) || candidate.maximumUploadBytes <= 0) {
    throw new Error('invalid Vault response')
  }
  return { items: candidate.items, maximumUploadBytes: candidate.maximumUploadBytes }
}

function parseDownloadAuthorization(value: unknown): DownloadAuthorization {
  if (typeof value !== 'object' || value === null) throw new Error('invalid download authorization')
  const candidate = value as Record<string, unknown>
  if (typeof candidate.url !== 'string' || typeof candidate.expiresAt !== 'string') throw new Error('invalid download authorization')
  const sameOrigin = candidate.url.startsWith('/') && !candidate.url.startsWith('//')
  if (!sameOrigin) {
    const parsed = new URL(candidate.url)
    const localHTTP = parsed.protocol === 'http:' && ['localhost', '127.0.0.1', '[::1]'].includes(parsed.hostname)
    if (parsed.protocol !== 'https:' && !localHTTP) throw new Error('unsafe download authorization')
  }
  return { url: candidate.url, expiresAt: candidate.expiresAt }
}

function formatBytes(value: number) {
  if (value < 1024) return `${value} B`
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KiB`
  return `${(value / (1024 * 1024)).toFixed(1)} MiB`
}

export default function VaultManager({ csrfToken, permissions, onOpenHelp }: VaultManagerProps) {
  const canRead = permissions.includes('storage.read')
  const canWrite = permissions.includes('storage.write')
  const [blobs, setBlobs] = useState<VaultBlob[]>([])
  const [maximumUploadBytes, setMaximumUploadBytes] = useState(0)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [download, setDownload] = useState<{ id: string; authorization: DownloadAuthorization } | null>(null)
  const [preview, setPreview] = useState<ViewableDocument | null>(null)
  const errorRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (error) errorRef.current?.focus()
  }, [error])

  useEffect(() => {
    if (!canRead) return
    let active = true
    requestJSON('/api/v1/blobs')
      .then((value) => {
        if (!active) return
        const response = parseVaultResponse(value)
        setBlobs(response.items)
        setMaximumUploadBytes(response.maximumUploadBytes)
      })
      .catch(() => { if (active) setError('Vault files could not be loaded.') })
    return () => { active = false }
  }, [canRead])

  async function uploadBlob(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const formData = new FormData(form)
    setBusy(true)
    setError('')
    setDownload(null)
    try {
      const value = await requestJSON('/api/v1/blobs', {
        method: 'POST', headers: { 'X-CSRF-Token': csrfToken }, body: formData,
      })
      if (!isVaultBlob(value)) throw new Error('invalid Vault blob')
      setBlobs((current) => [value, ...current])
      form.reset()
    } catch (cause) {
      setError(cause instanceof ApiRequestError ? cause.message : 'The file could not be uploaded to Vault.')
    } finally {
      setBusy(false)
    }
  }

  async function prepareDownload(id: string) {
    setBusy(true)
    setError('')
    setDownload(null)
    try {
      const value = await requestJSON(`/api/v1/blobs/${id}/download-authorization`, {
        method: 'POST', headers: { 'X-CSRF-Token': csrfToken },
      })
      setDownload({ id, authorization: parseDownloadAuthorization(value) })
    } catch (cause) {
      setError(cause instanceof ApiRequestError ? cause.message : 'A temporary download could not be prepared.')
    } finally {
      setBusy(false)
    }
  }

  if (!canRead) {
    return <section aria-labelledby="vault-heading" className={`${panelClass} p-5 sm:p-6`} data-feature="storage.blobs" data-requirement="REQ-STORAGE-001"><div className="flex flex-wrap items-start justify-between gap-4"><div><h2 id="vault-heading" className="text-2xl font-semibold">Vault — File storage</h2><p className="mt-2 text-steward-mist-muted">Your role does not include permission to view stored files.</p></div>{onOpenHelp && <button className={secondaryButtonClass} onClick={onOpenHelp} type="button">Vault help</button>}</div></section>
  }

  return (
    <section aria-labelledby="vault-heading" className={`${panelClass} p-5 sm:p-6`} data-feature="storage.blobs" data-requirement="REQ-STORAGE-001">
      <ProductHeader
        actions={onOpenHelp ? <button className={secondaryButtonClass} onClick={onOpenHelp} type="button">Vault help</button> : undefined}
        description="Keep checksummed evidence and attachments with their ownership and provenance. Downloads are authorized only when requested and expire shortly afterward."
        headingId="vault-heading"
        kicker="Vault"
        title="Private file storage"
      />
      {error && <div ref={errorRef} className="mt-4 rounded-lg border border-steward-danger/50 bg-steward-danger/15 p-3 text-[#ffccd1]" role="alert" tabIndex={-1}>{error}</div>}

      {canWrite && (
        <form className={`${subpanelClass} mt-6 grid gap-4 p-4 md:grid-cols-2`} onSubmit={uploadBlob}>
          <div className="md:col-span-2">
            <label className={labelClass} htmlFor="vault-file">File</label>
            <input className={`${inputClass} file:mr-3 file:rounded-lg file:border-0 file:bg-steward-teal/12 file:px-3 file:py-1.5 file:font-semibold file:text-steward-teal`} id="vault-file" name="file" type="file" required />
            {maximumUploadBytes > 0 && <p className="mt-1 text-xs text-steward-mist-muted">Maximum {formatBytes(maximumUploadBytes)}.</p>}
          </div>
          <VaultField id="sourceSystemId" label="Source system ID" help="Optional. Enter with a source record ID." />
          <VaultField id="sourceRecordId" label="Source record ID" />
          <VaultField id="resourceType" label="Related resource type" help="Optional, for example asset." />
          <VaultField id="resourceId" label="Related resource ID" />
          <button className={`${buttonClass} md:col-span-2`} disabled={busy} type="submit">{busy ? 'Working…' : 'Upload to Vault'}</button>
        </form>
      )}

      <div className={`${tableWrapClass} mt-6`}>
        <table className="w-full min-w-[720px] border-collapse text-left text-sm">
          <caption className="sr-only">Files stored in Vault</caption>
          <thead><tr className="border-b border-steward-ink-800 text-steward-mist-muted"><th className="px-3 py-3 font-semibold" scope="col">File</th><th className="px-3 py-3 font-semibold" scope="col">Size and type</th><th className="px-3 py-3 font-semibold" scope="col">Provenance</th><th className="px-3 py-3 font-semibold" scope="col">Integrity</th><th className="px-3 py-3 font-semibold" scope="col">View or download</th></tr></thead>
          <tbody>
            {blobs.map((blob) => (
              <tr className="border-b border-steward-ink-800/70 align-top" key={blob.id}>
                <td className="px-3 py-4"><strong className="block text-steward-mist">{blob.name}</strong><span className="mt-1 block text-xs text-steward-mist-muted">{new Date(blob.createdAt).toLocaleString()} via {blob.provider}</span></td>
                <td className="px-3 py-4 text-steward-mist-muted">{formatBytes(blob.sizeBytes)}<span className="mt-1 block break-all text-xs">{blob.mediaType}</span></td>
                <td className="px-3 py-4 text-steward-mist-muted">{blob.sourceSystemId ? `${blob.sourceSystemId} / ${blob.sourceRecordId}` : 'Direct upload'}{blob.resourceType && <span className="mt-1 block text-xs">{blob.resourceType}: {blob.resourceId}</span>}</td>
                <td className="px-3 py-4"><code className="block max-w-52 break-all text-xs text-steward-mist-muted">SHA-256 {blob.sha256}</code></td>
                <td className="px-3 py-4">
                  <div className="flex flex-wrap gap-2">
                    <button className={secondaryButtonClass} disabled={busy} onClick={() => setPreview({ id: blob.id, name: blob.name, mediaType: blob.mediaType })} type="button">View in browser</button>
                    {download?.id === blob.id
                      ? <a className={secondaryButtonClass} href={`${download.authorization.url}${download.authorization.url.includes('?') ? '&' : '?'}download=1`}>Download ready</a>
                      : <button className={secondaryButtonClass} disabled={busy} onClick={() => prepareDownload(blob.id)} type="button">Prepare download</button>}
                  </div>
                </td>
              </tr>
            ))}
            {blobs.length === 0 && <tr><td className="px-3 py-6 text-steward-mist-muted" colSpan={5}>No files have been stored yet.</td></tr>}
          </tbody>
        </table>
      </div>
      {preview && <div className="mt-6"><DocumentViewer csrfToken={csrfToken} document={preview} onClose={() => setPreview(null)} /></div>}
    </section>
  )
}

function VaultField({ id, label, help }: { id: string; label: string; help?: string }) {
  return <div><label className={labelClass} htmlFor={id}>{label}</label>{help && <p className="mt-1 text-xs text-steward-mist-muted" id={`${id}-help`}>{help}</p>}<input aria-describedby={help ? `${id}-help` : undefined} className={inputClass} id={id} name={id} /></div>
}
