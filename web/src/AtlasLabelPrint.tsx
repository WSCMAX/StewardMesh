import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { ApiRequestError, requestArtifact, requestJSON } from './api'
import { buttonClass, inputClass, secondaryButtonClass, subpanelClass } from './ui'

// Requirement: REQ-ATLAS-CODES-001. Features: inventory.identifiers, templates.schemas.

export type PrintableAsset = { id: string; name: string; assetTag?: string }

type PrintableIdentifier = {
  id: string
  assetId: string
  assetName: string
  assetTag?: string
  symbology: 'code128' | 'qr'
  normalizedValue: string
  displayValue: string
  status: 'active' | 'replaced' | 'deactivated'
}

type LabelTemplate = {
  id: string
  patternTemplateId: string
  patternVersion: number
  name: string
  version: number
  widthMm: number
  heightMm: number
  marginMm: number
  quietZoneMm: number
  symbology: 'code128' | 'qr'
  payloadSource: 'identifier_value' | 'organization_route'
  humanReadableField: string
  safeAssetFields?: string[]
  organizationBranding?: string
}

type Artifact = {
  batchId: string
  output: 'svg' | 'pdf'
  itemCount: number
  widthMm: number
  heightMm: number
  previewSrc?: string
  objectUrl?: string
  fileName: string
  replay: boolean
}

const maximumBatchSize = 50
const maximumCandidates = 250
const maximumArtifactBytes = 4 << 20
const artifactMediaTypes = { svg: 'image/svg+xml', pdf: 'application/pdf' } as const
const batchIDPattern = /^label-batch-[a-f0-9]{24}$/

function isLabelTemplate(value: unknown): value is LabelTemplate {
  if (typeof value !== 'object' || value === null) return false
  const item = value as Record<string, unknown>
  return typeof item.id === 'string' && typeof item.patternTemplateId === 'string'
    && typeof item.patternVersion === 'number' && typeof item.name === 'string' && typeof item.version === 'number'
    && typeof item.widthMm === 'number' && typeof item.heightMm === 'number' && typeof item.marginMm === 'number'
    && typeof item.quietZoneMm === 'number' && (item.symbology === 'code128' || item.symbology === 'qr')
    && (item.payloadSource === 'identifier_value' || item.payloadSource === 'organization_route')
    && item.humanReadableField === 'identifier.displayValue'
}

function isIdentifier(value: unknown): value is Omit<PrintableIdentifier, 'assetName' | 'assetTag'> {
  if (typeof value !== 'object' || value === null) return false
  const item = value as Record<string, unknown>
  return typeof item.id === 'string' && typeof item.assetId === 'string'
    && (item.symbology === 'code128' || item.symbology === 'qr')
    && typeof item.normalizedValue === 'string' && typeof item.displayValue === 'string'
    && (item.status === 'active' || item.status === 'replaced' || item.status === 'deactivated')
}

function readItems(value: unknown): unknown[] {
  if (typeof value !== 'object' || value === null) return []
  const items = (value as Record<string, unknown>).items
  return Array.isArray(items) ? items : []
}

function idempotencyKey() {
  if (typeof globalThis.crypto?.randomUUID === 'function') return `label-${globalThis.crypto.randomUUID()}`
  return `label-${Date.now()}-${Math.floor(Math.random() * 1_000_000)}`
}

function requestMessage(error: unknown, fallback: string) {
  return error instanceof ApiRequestError ? error.message : fallback
}

export default function AtlasLabelPrint({ assets, csrfToken }: { assets: readonly PrintableAsset[]; csrfToken: string }) {
  const [open, setOpen] = useState(false)
  const [identifiers, setIdentifiers] = useState<PrintableIdentifier[]>([])
  const [templates, setTemplates] = useState<LabelTemplate[]>([])
  const [selected, setSelected] = useState<string[]>([])
  const [templateId, setTemplateId] = useState('')
  const [output, setOutput] = useState<'svg' | 'pdf'>('svg')
  const [testPrint, setTestPrint] = useState(true)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')
  const [confirmed, setConfirmed] = useState(false)
  const [artifact, setArtifact] = useState<Artifact | null>(null)
  const errorRef = useRef<HTMLDivElement>(null)
  const controllerRef = useRef<AbortController | null>(null)
  const objectUrlRef = useRef('')
  const generationRef = useRef(0)
  const loadRef = useRef(0)
  const requestKeyRef = useRef({ signature: '', key: '' })

  const selectedIdentifiers = selected.map((id) => identifiers.find((item) => item.id === id)).filter((item): item is PrintableIdentifier => Boolean(item))
  const selectedFormats = new Set(selectedIdentifiers.map((item) => item.symbology))
  const selectedFormat = selectedFormats.size === 1 ? selectedIdentifiers[0]?.symbology : undefined
  const compatibleTemplates = templates.filter((template) => template.symbology === selectedFormat)
  const selectedTemplate = compatibleTemplates.find((template) => template.id === templateId) ?? compatibleTemplates[0]
  const svgBatchInvalid = output === 'svg' && selected.length !== 1

  function releaseArtifact() {
    if (objectUrlRef.current) URL.revokeObjectURL(objectUrlRef.current)
    objectUrlRef.current = ''
    setArtifact(null)
    setConfirmed(false)
  }

  function invalidateRequest() {
    controllerRef.current?.abort()
    controllerRef.current = null
    generationRef.current += 1
    requestKeyRef.current = { signature: '', key: '' }
    releaseArtifact()
    setError('')
    setMessage('')
    if (busy === 'preview') setBusy('')
  }

  useEffect(() => () => {
    controllerRef.current?.abort()
    loadRef.current += 1
    if (objectUrlRef.current) URL.revokeObjectURL(objectUrlRef.current)
  }, [])

  useEffect(() => {
    setSelected((current) => current.filter((id) => identifiers.some((item) => item.id === id)))
  }, [identifiers])

  useEffect(() => {
    if (!selectedTemplate && compatibleTemplates.length > 0) setTemplateId(compatibleTemplates[0].id)
  }, [compatibleTemplates, selectedTemplate])

  async function openPanel() {
    setOpen(true)
    setError('')
    setMessage('')
    setIdentifiers([])
    setTemplates([])
    setSelected([])
    setTemplateId('')
    const load = ++loadRef.current
    setBusy('loading')
    try {
      const [templateResponse, ...identifierResponses] = await Promise.all([
        requestJSON('/api/v1/asset-label-templates'),
        ...assets.map((asset) => requestJSON(`/api/v1/assets/${encodeURIComponent(asset.id)}/identifiers`)),
      ])
      if (loadRef.current !== load) return
      const templateItems = readItems(templateResponse).filter(isLabelTemplate)
      if (templateItems.length === 0) throw new Error('no label templates')
      const candidates = identifierResponses.flatMap((response, index) => {
        const asset = assets[index]
        return readItems(response).filter(isIdentifier).filter((item) => item.status === 'active' && item.assetId === asset.id).map((item) => ({ ...item, assetName: asset.name, assetTag: asset.assetTag }))
      })
      const bounded = candidates.slice(0, maximumCandidates)
      setTemplates(templateItems)
      setIdentifiers(bounded)
      setSelected(bounded.length > 0 ? [bounded[0].id] : [])
      if (candidates.length > maximumCandidates) setMessage(`Showing the first ${maximumCandidates} active identifiers. Narrow the visible Atlas asset set to print others.`)
    } catch (requestError) {
      if (loadRef.current !== load) return
      setError(requestMessage(requestError, 'Printable identifiers and label templates could not be loaded.'))
      queueMicrotask(() => errorRef.current?.focus())
    } finally {
      if (loadRef.current === load) setBusy('')
    }
  }

  function toggleIdentifier(id: string) {
    if (!selected.includes(id) && selected.length >= maximumBatchSize) {
      setError(`A label batch can contain at most ${maximumBatchSize} identifiers.`)
      queueMicrotask(() => errorRef.current?.focus())
      return
    }
    invalidateRequest()
    const next = selected.includes(id) ? selected.filter((item) => item !== id) : [...selected, id]
    if (next.length > 1 && output === 'svg') setOutput('pdf')
    setSelected(next)
  }

  function cancelGeneration() {
    controllerRef.current?.abort()
    controllerRef.current = null
    generationRef.current += 1
    setBusy('')
    setMessage('Label generation cancelled. Retry is safe and uses the same request key.')
  }

  async function generatePreview() {
    if (!selectedTemplate || selected.length < 1 || selected.length > maximumBatchSize || selectedFormats.size !== 1 || svgBatchInvalid) {
      const detail = selectedFormats.size > 1
        ? 'Select identifiers that use the same code format for one label batch.'
        : svgBatchInvalid ? 'Browser SVG printing supports one physical label. Choose Vector PDF for a selected batch.'
          : 'Select between 1 and 50 active identifiers and a compatible template.'
      setError(detail)
      queueMicrotask(() => errorRef.current?.focus())
      return
    }
    const body = { templateId: selectedTemplate.id, templateVersion: selectedTemplate.version, identifierIds: selected, output, testPrint }
    const signature = JSON.stringify(body)
    if (requestKeyRef.current.signature !== signature) requestKeyRef.current = { signature, key: idempotencyKey() }
    controllerRef.current?.abort()
    const controller = new AbortController()
    controllerRef.current = controller
    const generation = ++generationRef.current
    setBusy('preview')
    setError('')
    setMessage('')
    releaseArtifact()
    try {
      const response = await requestArtifact('/api/v1/asset-label-batches', {
        method: 'POST', signal: controller.signal,
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken, 'Idempotency-Key': requestKeyRef.current.key },
        body: JSON.stringify(body),
      })
      const blob = await response.blob()
      if (controller.signal.aborted || generationRef.current !== generation || requestKeyRef.current.signature !== signature) return
      const batchId = response.headers.get('X-Label-Batch-ID') ?? 'atlas-label-batch'
      const mediaType = response.headers.get('Content-Type')?.split(';', 1)[0]?.trim().toLowerCase()
      const widthMm = Number(response.headers.get('X-Label-Width-MM'))
      const heightMm = Number(response.headers.get('X-Label-Height-MM'))
      const itemCount = Number(response.headers.get('X-Label-Item-Count'))
      if (!batchIDPattern.test(batchId) || mediaType !== artifactMediaTypes[output] || blob.size < 1 || blob.size > maximumArtifactBytes
        || !Number.isFinite(widthMm) || widthMm <= 0 || !Number.isFinite(heightMm) || heightMm <= 0 || itemCount !== selected.length) {
        throw new Error('invalid label artifact metadata')
      }
      let previewSrc: string | undefined
      let objectUrl: string | undefined
      if (output === 'svg') previewSrc = `data:image/svg+xml;charset=utf-8,${encodeURIComponent(await blob.text())}`
      else {
        objectUrl = URL.createObjectURL(blob)
        objectUrlRef.current = objectUrl
      }
      if (controller.signal.aborted || generationRef.current !== generation) {
        if (objectUrl) URL.revokeObjectURL(objectUrl)
        return
      }
      setArtifact({ batchId, output, itemCount, widthMm, heightMm, previewSrc, objectUrl, fileName: `${batchId}.${output}`, replay: response.headers.get('X-Idempotent-Replay') === 'true' })
      setMessage(testPrint ? 'Test-print preview ready. Measure the output at 100% scale before printing the full batch.' : 'Label output ready for operator review.')
    } catch (requestError) {
      if (controller.signal.aborted || generationRef.current !== generation) return
      setError(requestMessage(requestError, 'Label output could not be generated. Review the dimensions and retry.'))
      queueMicrotask(() => errorRef.current?.focus())
    } finally {
      if (generationRef.current === generation) {
        controllerRef.current = null
        setBusy('')
      }
    }
  }

  function deliverArtifact() {
    if (!artifact || !confirmed) return
    if (artifact.output === 'svg') {
      window.print()
      setMessage('The browser print dialog was opened. Choose and confirm the printer, media, scale, and quantity there.')
    } else {
      window.open(artifact.objectUrl ?? '', '_blank', 'noopener,noreferrer')
      setMessage('The PDF viewer was requested. If the browser blocked it, allow the new tab and retry; otherwise use the viewer’s print command to choose and confirm a printer.')
    }
  }

  if (assets.length === 0) return null

  return <>
    {artifact?.previewSrc && createPortal(<div aria-hidden="true" className="atlas-label-print-sheet"><img alt="" src={artifact.previewSrc} /></div>, document.body)}
    <section aria-labelledby="atlas-label-print-heading" className={`${subpanelClass} min-w-0 overflow-hidden`} data-feature="inventory.identifiers" data-requirement="REQ-ATLAS-CODES-001">
    <div className="flex flex-wrap items-start justify-between gap-3 p-4">
      <div className="min-w-0"><h3 className="font-semibold" id="atlas-label-print-heading">Atlas Codes — Label printing</h3><p className="mt-1 max-w-3xl text-sm leading-5 text-steward-mist-muted">Select active identifiers across the visible Atlas assets, preview one label or a bounded batch, then explicitly open an operator-controlled output path. StewardMesh never chooses or contacts a printer silently.</p></div>
      <button className={secondaryButtonClass} onClick={() => { if (open) { loadRef.current += 1; invalidateRequest(); setOpen(false) } else void openPanel() }} type="button">{open ? 'Close label printing' : 'Print labels'}</button>
    </div>
    {open && <div className="border-t border-steward-ink-800 p-4">
      {error && <div className="rounded-lg border border-red-400/50 bg-red-950/50 p-3 text-sm" ref={errorRef} role="alert" tabIndex={-1}>{error}</div>}
      {message && <p className="mt-3 rounded-lg border border-steward-green/40 bg-steward-green/10 p-3 text-sm" role="status">{message}</p>}
      {busy === 'loading' ? <p className="mt-3 text-sm text-steward-mist-muted" role="status">Loading printable identifiers and immutable templates…</p> : identifiers.length === 0 ? <p className="mt-3 rounded-lg border border-steward-ink-800 p-4 text-sm text-steward-mist-muted">No active identifiers are available on the visible assets.</p> : <>
        <fieldset className="mt-3 min-w-0" disabled={busy === 'preview'}>
          <legend className="font-semibold">1. Select active identifiers</legend>
          <p className="mt-1 text-xs leading-5 text-steward-mist-muted">{selected.length} of {maximumBatchSize} selected across {assets.length} visible asset{assets.length === 1 ? '' : 's'}. A batch uses one code format and creates no associations.</p>
          <div className="mt-2 grid min-w-0 gap-2 sm:grid-cols-2">{identifiers.map((identifier) => <label className="flex min-w-0 items-start gap-3 rounded-lg border border-steward-ink-800 p-3 text-sm" key={identifier.id}><input checked={selected.includes(identifier.id)} className="mt-0.5 h-5 w-5 shrink-0 accent-steward-teal" disabled={!selected.includes(identifier.id) && selected.length >= maximumBatchSize} onChange={() => toggleIdentifier(identifier.id)} type="checkbox" /><span className="min-w-0"><span className="block break-all font-mono text-steward-mist">{identifier.displayValue || identifier.normalizedValue}</span><span className="mt-1 block break-words text-steward-mist-muted">{identifier.assetName}{identifier.assetTag ? ` · ${identifier.assetTag}` : ''} · {identifier.symbology === 'code128' ? 'Code 128' : 'QR'}</span></span></label>)}</div>
        </fieldset>
        <fieldset className="mt-5 min-w-0">
          <legend className="font-semibold">2. Choose template and output</legend>
          <div className="mt-2 grid min-w-0 gap-3 sm:grid-cols-2">
            <label className="min-w-0 text-sm font-semibold text-steward-mist-muted">Versioned label template<select className={inputClass} disabled={busy === 'preview' || compatibleTemplates.length === 0} onChange={(event) => { invalidateRequest(); setTemplateId(event.target.value) }} value={selectedTemplate?.id ?? ''}>{compatibleTemplates.length === 0 && <option value="">Select one code format</option>}{compatibleTemplates.map((template) => <option key={template.id} value={template.id}>{template.name} · v{template.version} · {template.widthMm} × {template.heightMm} mm</option>)}</select></label>
            <label className="min-w-0 text-sm font-semibold text-steward-mist-muted">Output path<select className={inputClass} disabled={busy === 'preview'} onChange={(event) => { invalidateRequest(); setOutput(event.target.value as 'svg' | 'pdf') }} value={output}><option value="svg">Browser/OS print (single vector SVG)</option><option value="pdf">Vector PDF (single or batch)</option></select></label>
          </div>
          {selectedTemplate && <dl className="mt-3 grid gap-2 rounded-lg border border-steward-ink-800 p-3 text-sm sm:grid-cols-2"><div><dt className="font-semibold text-steward-mist-muted">Physical label</dt><dd>{selectedTemplate.widthMm} × {selectedTemplate.heightMm} mm · {selectedTemplate.marginMm} mm margins</dd></div><div><dt className="font-semibold text-steward-mist-muted">Code safety</dt><dd>{selectedTemplate.quietZoneMm} mm quiet zone · human-readable text included</dd></div><div><dt className="font-semibold text-steward-mist-muted">Immutable definition</dt><dd>Pattern {selectedTemplate.patternTemplateId}, version {selectedTemplate.patternVersion}</dd></div><div><dt className="font-semibold text-steward-mist-muted">Payload</dt><dd>{selectedTemplate.payloadSource === 'organization_route' ? 'Credential-free app route' : 'Existing identifier value'}</dd></div></dl>}
          <label className="mt-3 flex min-h-11 items-start gap-3 text-sm font-semibold text-steward-mist-muted"><input checked={testPrint} className="mt-0.5 h-5 w-5 shrink-0 accent-steward-teal" disabled={busy === 'preview'} onChange={(event) => { invalidateRequest(); setTestPrint(event.target.checked) }} type="checkbox" /><span>Test print first <span className="block font-normal">Adds a calibration border. Print at 100% scale and measure before a production batch.</span></span></label>
          {svgBatchInvalid && <p className="mt-2 rounded-lg border border-amber-300/40 bg-amber-950/30 p-3 text-sm">Browser SVG preserves one physical label. Choose Vector PDF for a selected batch.</p>}
          <div className="mt-4 flex flex-wrap gap-2"><button className={buttonClass} disabled={busy !== '' || !selectedTemplate || selected.length < 1 || selected.length > maximumBatchSize || selectedFormats.size !== 1 || svgBatchInvalid} onClick={() => void generatePreview()} type="button">{busy === 'preview' ? 'Generating…' : artifact ? 'Regenerate preview' : testPrint ? 'Generate test-print preview' : 'Generate preview'}</button>{busy === 'preview' && <button className={secondaryButtonClass} onClick={cancelGeneration} type="button">Cancel generation</button>}{error && busy === '' && <button className={secondaryButtonClass} onClick={() => void generatePreview()} type="button">Retry generation</button>}</div>
        </fieldset>
      </>}
      {artifact && <section aria-labelledby="atlas-label-review-heading" className="mt-5 min-w-0 border-t border-steward-ink-800 pt-5"><h4 className="font-semibold" id="atlas-label-review-heading">3. Review and confirm</h4><p className="mt-1 break-words text-sm text-steward-mist-muted">{artifact.itemCount} label{artifact.itemCount === 1 ? '' : 's'} · {artifact.widthMm} × {artifact.heightMm} mm each · {artifact.output.toUpperCase()}{artifact.replay ? ' · safe retry replay' : ''}</p>{artifact.previewSrc && <div className="mt-3 max-w-full overflow-auto rounded-lg bg-white p-3"><img alt={`Generated preview of ${artifact.itemCount} Atlas Codes label${artifact.itemCount === 1 ? '' : 's'}`} className="atlas-label-print-preview block h-auto max-w-full bg-white" src={artifact.previewSrc} /></div>}{artifact.objectUrl && artifact.output === 'pdf' && <p className="mt-3 text-sm text-steward-mist-muted">The PDF is ready. Confirm the dimensions, count, and test-print state below before opening its browser viewer.</p>}<label className="mt-4 flex min-h-11 items-start gap-3 rounded-lg border border-steward-blue/35 p-3 text-sm font-semibold"><input checked={confirmed} className="mt-0.5 h-5 w-5 shrink-0 accent-steward-teal" onChange={(event) => setConfirmed(event.target.checked)} type="checkbox" /><span>I reviewed the {artifact.widthMm} × {artifact.heightMm} mm dimensions, selected count, code format, and test-print state. <span className="mt-1 block font-normal text-steward-mist-muted">I will choose and confirm the printer, media, scale, and quantity in the browser/OS or controlled device workflow.</span></span></label><button className={`${buttonClass} mt-3`} disabled={!confirmed} onClick={deliverArtifact} type="button">{artifact.output === 'svg' ? 'Open browser print dialog' : 'Open PDF for printing'}</button></section>}
    </div>}
    </section>
  </>
}
