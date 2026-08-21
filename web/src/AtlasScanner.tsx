import { type FormEvent, type KeyboardEvent, useEffect, useRef, useState } from 'react'
import { ApiRequestError, requestJSON } from './api'
import { buttonClass, inputClass, labelClass, plainButtonClass, secondaryButtonClass, subpanelClass } from './ui'

// Requirement: REQ-ATLAS-CODES-001. Feature: inventory.identifiers.

type Symbology = 'code128' | 'qr'
type ScanMode = 'find' | 'associate'
type Terminator = 'Enter' | 'Tab'

type AssetSummary = {
  id: string
  name: string
}

type AtlasScannerProps = {
  active?: boolean
  canWrite: boolean
  csrfToken: string
  onAssociated: () => void
  onResolveAsset: (assetId: string) => Promise<void>
  selectedAsset: AssetSummary | null
}

type BarcodeDetection = {
  format?: string
  rawValue?: string
}

type BarcodeDetectorLike = {
  detect: (source: HTMLVideoElement) => Promise<BarcodeDetection[]>
}

type BarcodeDetectorConstructor = new (options?: { formats?: string[] }) => BarcodeDetectorLike

const duplicateWindowMilliseconds = 1500

function barcodeDetectorConstructor() {
  return (globalThis as typeof globalThis & { BarcodeDetector?: BarcodeDetectorConstructor }).BarcodeDetector
}

function operationCreated(value: unknown) {
  if (typeof value !== 'object' || value === null) return null
  const record = value as Record<string, unknown>
  if (typeof record.identifier !== 'object' || record.identifier === null) return null
  const identifier = record.identifier as Record<string, unknown>
  if (typeof identifier.assetId !== 'string') return null
  return { assetId: identifier.assetId, created: record.created !== false }
}

function resolvedAssetID(value: unknown) {
  if (typeof value !== 'object' || value === null) return ''
  const assetID = (value as Record<string, unknown>).assetId
  return typeof assetID === 'string' ? assetID : ''
}

export function validateScannedValue(symbology: Symbology, rawValue: string) {
  const value = rawValue.trim()
  if (!value) return { value: '', error: 'Enter or scan an identifier.' }
  if (symbology === 'code128') {
    if (value.length > 128 || !/^[\x20-\x7e]+$/.test(value)) {
      return { value, error: 'Code 128 values must be 1–128 printable ASCII characters.' }
    }
    return { value, error: '' }
  }
  if (/[\u0000-\u001f\u007f]/.test(value) || new TextEncoder().encode(value).length > 512) {
    return { value, error: 'QR values must be control-free UTF-8 no longer than 512 bytes.' }
  }
  return { value, error: '' }
}

function requestMessage(error: unknown, fallback: string) {
  return error instanceof ApiRequestError ? error.message : fallback
}

export default function AtlasScanner({ active, canWrite, csrfToken, onAssociated, onResolveAsset, selectedAsset }: AtlasScannerProps) {
  const [open, setOpen] = useState(false)
  const [mode, setMode] = useState<ScanMode>('find')
  const [symbology, setSymbology] = useState<Symbology>('code128')
  const [terminator, setTerminator] = useState<Terminator>('Enter')
  const [burstWindow, setBurstWindow] = useState(500)
  const [value, setValue] = useState('')
  const [busy, setBusy] = useState(false)
  const [cameraActive, setCameraActive] = useState(false)
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')
  const [retry, setRetry] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)
  const videoRef = useRef<HTMLVideoElement>(null)
  const streamRef = useRef<MediaStream | null>(null)
  const frameRef = useRef<number | null>(null)
  const scanGenerationRef = useRef(0)
  const lastCompletedRef = useRef({ key: '', at: 0 })
  const burstStartRef = useRef(0)
  const lastKeystrokeRef = useRef(0)

  function stopCamera(status = '') {
    scanGenerationRef.current += 1
    if (frameRef.current !== null) cancelAnimationFrame(frameRef.current)
    frameRef.current = null
    streamRef.current?.getTracks().forEach((track) => track.stop())
    streamRef.current = null
    if (videoRef.current) videoRef.current.srcObject = null
    setCameraActive(false)
    if (status) setMessage(status)
  }

  useEffect(() => () => {
    scanGenerationRef.current += 1
    if (frameRef.current !== null) cancelAnimationFrame(frameRef.current)
    streamRef.current?.getTracks().forEach((track) => track.stop())
    streamRef.current = null
  }, [])

  useEffect(() => {
    if (active !== true && active !== false) return
    if (!active) {
      scanGenerationRef.current += 1
      if (frameRef.current !== null) cancelAnimationFrame(frameRef.current)
      frameRef.current = null
      streamRef.current?.getTracks().forEach((track) => track.stop())
      streamRef.current = null
      if (videoRef.current) videoRef.current.srcObject = null
      setCameraActive(false)
      setOpen(false)
      burstStartRef.current = 0
      lastKeystrokeRef.current = 0
      return
    }
    setOpen(true)
    setMessage((current) => current || 'Scan mode active. Choose a workflow and input method.')
    queueMicrotask(() => inputRef.current?.focus())
  }, [active])

  function closeScanner() {
    stopCamera()
    setOpen(false)
    setValue('')
    setError('')
    setMessage('Scanning cancelled. No asset or identifier was changed.')
    setRetry(false)
    burstStartRef.current = 0
    lastKeystrokeRef.current = 0
  }

  async function submitValue(candidate = value, detectedSymbology: Symbology = symbology) {
    const validation = validateScannedValue(detectedSymbology, candidate)
    setValue(validation.value)
    setError(validation.error)
    setMessage('')
    setRetry(false)
    if (validation.error) return
    if (mode === 'associate' && (!canWrite || !selectedAsset)) {
      setError(canWrite ? 'Choose an asset before scanning an identifier to associate.' : 'Asset write access is required to associate an identifier.')
      return
    }
    const duplicateKey = `${mode}:${detectedSymbology}:${validation.value}`
    const now = Date.now()
    if (lastCompletedRef.current.key === duplicateKey && now - lastCompletedRef.current.at < duplicateWindowMilliseconds) {
      setMessage('Duplicate scan ignored. The first scan already completed.')
      return
    }
    setBusy(true)
    try {
      if (mode === 'find') {
        const response = await requestJSON('/api/v1/asset-identifiers/resolve', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ symbology: detectedSymbology, value: validation.value }),
        })
        const assetID = resolvedAssetID(response)
        if (!assetID) throw new Error('invalid identifier resolution response')
        await onResolveAsset(assetID)
        setMessage('Identifier matched. The authorized asset is shown below.')
      } else {
        const response = await requestJSON(`/api/v1/assets/${encodeURIComponent(selectedAsset!.id)}/identifiers`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
          body: JSON.stringify({
            symbology: detectedSymbology,
            value: validation.value,
            displayValue: validation.value,
            source: 'user_entered',
            primary: false,
          }),
        })
        const operation = operationCreated(response)
        if (!operation || operation.assetId !== selectedAsset!.id) throw new Error('invalid identifier association response')
        onAssociated()
        setMessage(operation.created ? `Identifier associated with ${selectedAsset!.name}.` : `That identifier is already associated with ${selectedAsset!.name}.`)
      }
      lastCompletedRef.current = { key: duplicateKey, at: Date.now() }
      burstStartRef.current = 0
      lastKeystrokeRef.current = 0
      setValue('')
      inputRef.current?.focus()
    } catch (requestError) {
      setError(requestMessage(requestError, mode === 'find' ? 'The identifier could not be resolved.' : 'The identifier could not be associated.'))
      setRetry(true)
    } finally {
      setBusy(false)
    }
  }

  async function startCamera() {
    setError('')
    setMessage('')
    const Detector = barcodeDetectorConstructor()
    if (!Detector || !navigator.mediaDevices?.getUserMedia) {
      setError('Camera scanning is not available in this browser. Use a keyboard scanner, paste, or manual entry.')
      return
    }
    stopCamera()
    const generation = scanGenerationRef.current
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: { ideal: 'environment' } }, audio: false })
      if (scanGenerationRef.current !== generation) {
        stream.getTracks().forEach((track) => track.stop())
        return
      }
      streamRef.current = stream
      const video = videoRef.current
      if (!video) {
        stream.getTracks().forEach((track) => track.stop())
        return
      }
      video.srcObject = stream
      await video.play()
      setCameraActive(true)
      setMessage('Camera active. Frames stay in this browser and are not uploaded or retained.')
      const detector = new Detector({ formats: ['code_128', 'qr_code'] })
      const detect = async () => {
        if (scanGenerationRef.current !== generation || !streamRef.current || !videoRef.current) return
        try {
          const detections = await detector.detect(videoRef.current)
          const detection = detections.find((item) => typeof item.rawValue === 'string' && item.rawValue.length > 0)
          if (detection?.rawValue) {
            const detected = detection.format === 'qr_code' ? 'qr' : detection.format === 'code_128' ? 'code128' : null
            if (!detected) {
              setError('That barcode format is unsupported. Use Code 128 or QR.')
            } else {
              setSymbology(detected)
              stopCamera('Code captured. Checking the identifier…')
              await submitValue(detection.rawValue, detected)
              return
            }
          }
        } catch {
          setError('The camera frame could not be decoded. Try again or use manual entry.')
        }
        frameRef.current = requestAnimationFrame(() => { void detect() })
      }
      frameRef.current = requestAnimationFrame(() => { void detect() })
    } catch {
      stopCamera()
      setError('Camera access was denied or unavailable. Use a keyboard scanner, paste, or manual entry.')
    }
  }

  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    void submitValue()
  }

  function onInputKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    const now = Date.now()
    if (event.key !== terminator) {
      if (event.key.length === 1) {
        if (!burstStartRef.current || now - lastKeystrokeRef.current > burstWindow) burstStartRef.current = now
        lastKeystrokeRef.current = now
      }
      return
    }
    if (busy) return
    event.preventDefault()
    if (burstStartRef.current && now - burstStartRef.current > burstWindow) {
      setError(`Keyboard-scanner input exceeded the ${burstWindow} ms burst window. Review the retained value and use the action button for manual entry.`)
      burstStartRef.current = 0
      lastKeystrokeRef.current = 0
      return
    }
    void submitValue(event.currentTarget.value)
  }

  return <section aria-labelledby="atlas-scanner-heading" className={`${subpanelClass} overflow-hidden p-5 sm:p-6`} data-feature="inventory.identifiers" data-requirement="REQ-ATLAS-CODES-001">
    <div>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h3 className="text-lg font-semibold" id="atlas-scanner-heading">Atlas Codes — Scan</h3>
          <p className="mt-1 max-w-3xl text-sm leading-6 text-steward-mist-muted">Use an explicit find or associate mode with a keyboard-wedge scanner, camera, paste, or manual entry. Scanner input cannot act while this surface is closed.</p>
        </div>
        <button className={open ? plainButtonClass : secondaryButtonClass} onClick={() => { if (open) closeScanner(); else { setOpen(true); setMessage('Scan mode active. Choose a workflow and input method.'); queueMicrotask(() => inputRef.current?.focus()) } }} type="button">{open ? 'Cancel scanning' : 'Open scanner'}</button>
      </div>
      {message && <p className="mt-3 rounded-lg border border-steward-green/40 bg-steward-green/10 p-3 text-sm" role="status">{message}</p>}
      {error && <p className="mt-3 rounded-lg border border-red-400/50 bg-red-950/50 p-3 text-sm" role="alert">{error}</p>}
      {open && <form aria-label="Scan an Atlas Code" className="mt-4" onSubmit={onSubmit}>
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-5">
          <label className={labelClass}>Workflow
            <select className={inputClass} onChange={(event) => { setMode(event.target.value as ScanMode); setRetry(false); setError('') }} value={mode}>
              <option value="find">Find an asset</option>
              {canWrite && <option value="associate">Associate with selected asset</option>}
            </select>
          </label>
          <label className={labelClass}>Symbology
            <select className={inputClass} onChange={(event) => { setSymbology(event.target.value as Symbology); setRetry(false); setError('') }} value={symbology}><option value="code128">Code 128</option><option value="qr">QR</option></select>
          </label>
          <label className={labelClass}>Scanner terminator
            <select className={inputClass} onChange={(event) => setTerminator(event.target.value as Terminator)} value={terminator}><option value="Enter">Enter</option><option value="Tab">Tab</option></select>
          </label>
          <label className={labelClass}>Scanner burst window
            <select className={inputClass} onChange={(event) => setBurstWindow(Number(event.target.value))} value={burstWindow}><option value={250}>250 ms</option><option value={500}>500 ms</option><option value={1000}>1 second</option><option value={2000}>2 seconds</option></select>
          </label>
          <label className={labelClass}>Scanned or entered value
            <input autoCapitalize="none" autoComplete="off" autoCorrect="off" className={inputClass} maxLength={512} onChange={(event) => { setValue(event.target.value); setRetry(false); setError('') }} onKeyDown={onInputKeyDown} onPaste={() => { burstStartRef.current = 0; lastKeystrokeRef.current = 0 }} placeholder="Scan, paste, or type" ref={inputRef} spellCheck={false} value={value} />
          </label>
        </div>
        {mode === 'associate' && <p className="mt-3 text-sm text-steward-mist-muted">{selectedAsset ? `New identifiers will be associated with ${selectedAsset.name}.` : 'Choose an asset from the inventory before associating a code.'}</p>}
        <div className="mt-4 flex flex-wrap gap-3">
          <button className={buttonClass} disabled={busy} type="submit">{busy ? (mode === 'find' ? 'Finding…' : 'Associating…') : retry ? 'Retry scan' : mode === 'find' ? 'Find asset' : 'Associate identifier'}</button>
          {!cameraActive && <button className={secondaryButtonClass} disabled={busy} onClick={() => void startCamera()} type="button">Use camera</button>}
          {cameraActive && <button className={secondaryButtonClass} onClick={() => stopCamera('Camera stopped. Manual and keyboard-scanner input remain available.')} type="button">Stop camera</button>}
        </div>
        <video aria-label="Live barcode camera preview" className={`${cameraActive ? 'mt-4 block' : 'hidden'} max-h-72 w-full rounded-xl bg-black object-contain`} muted playsInline ref={videoRef} />
        <p className="mt-3 text-xs leading-5 text-steward-mist-muted">Only Code 128 and QR are accepted. Camera frames stay local. Duplicate completed scans are suppressed briefly; conflicts and denied records do not reveal hidden asset details.</p>
      </form>}
    </div>
  </section>
}
