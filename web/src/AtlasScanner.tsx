import { type FormEvent, type KeyboardEvent, useEffect, useRef, useState } from 'react'
import { ApiRequestError, requestJSON } from './api'
import { identityBarcodeFormats, isAtlasCodeSymbology, symbologyFromFormat, type CapturedSymbology } from './barcodeCapture'
import RecordSearchPicker from './RecordSearchPicker'
import IdentityScanMapper, { type IdentityCatalogMatch } from './IdentityScanMapper'
import {
  applyIdentityScan,
  assignIdentityField,
  capturedValuesFromScan,
  emptyDeviceIdentity,
  identityFieldLabels,
  identityFromCaptured,
  identityValuesEqual,
  mergeCapturedValues,
  modelNumberDecisionNeeded,
  type CapturedIdentityValue,
  type DeviceIdentity,
  type IdentityField,
} from './scanIdentity'
import { buttonClass, cx, inputClass, labelClass, panelClass, plainButtonClass, secondaryButtonClass, StatusBadge, subpanelClass } from './ui'
import CameraPreview from './CameraPreview'

// Requirement: REQ-ATLAS-CODES-001, REQ-ATLAS-001. Features: inventory.identifiers, inventory.assets.

type Symbology = 'code128' | 'qr'
type ScanMode = 'find' | 'labels' | 'associate'
type Terminator = 'Enter' | 'Tab'

type AssetSummary = {
  id: string
  name: string
  assetTag?: string
  serialNumber?: string
  modelId?: string
  modelNumber?: string
  modelLabel?: string
}

type ModelSummary = {
  id: string
  manufacturer: string
  name: string
  modelNumber?: string
  status?: string
}

type AtlasScannerProps = {
  active?: boolean
  canWrite: boolean
  csrfToken: string
  onAssociated: () => void
  onResolveAsset: (assetId: string) => Promise<void>
  onClearAsset?: () => void
  onLinkAssetModel?: (modelId: string) => Promise<void>
  onSetupScannedModel?: (input: { modelNumber: string; modelId?: string }) => void
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
const identityFields: { key: IdentityField; label: string }[] = [
  { key: 'serialNumber', label: 'Manufacturer serial' },
  { key: 'assetTag', label: 'Asset tag' },
  { key: 'modelNumber', label: 'Model number' },
]

const workflowOptions: { id: ScanMode; label: string; hint: string; write?: boolean }[] = [
  { id: 'find', label: 'Find an asset', hint: 'Match a code, serial, or tag' },
  { id: 'labels', label: 'Capture serial, tag, and model', hint: 'Read the device stickers' },
  { id: 'associate', label: 'Attach a code to an asset', hint: 'Bind Code 128 or QR', write: true },
]

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

function readAssetSummaries(value: unknown): AssetSummary[] {
  if (typeof value !== 'object' || value === null) return []
  const items = (value as Record<string, unknown>).items
  if (!Array.isArray(items)) return []
  return items.flatMap((item) => {
    if (typeof item !== 'object' || item === null) return []
    const record = item as Record<string, unknown>
    if (typeof record.id !== 'string' || typeof record.name !== 'string') return []
    return [{
      id: record.id,
      name: record.name,
      assetTag: typeof record.assetTag === 'string' ? record.assetTag : '',
      serialNumber: typeof record.serialNumber === 'string' ? record.serialNumber : '',
    }]
  })
}

function readModelSummaries(value: unknown): ModelSummary[] {
  if (typeof value !== 'object' || value === null) return []
  const items = (value as Record<string, unknown>).items
  if (!Array.isArray(items)) return []
  return items.flatMap((item) => {
    if (typeof item !== 'object' || item === null) return []
    const record = item as Record<string, unknown>
    if (typeof record.id !== 'string' || typeof record.name !== 'string') return []
    return [{
      id: record.id,
      name: record.name,
      manufacturer: typeof record.manufacturer === 'string' ? record.manufacturer : '',
      modelNumber: typeof record.modelNumber === 'string' ? record.modelNumber : '',
      status: typeof record.status === 'string' ? record.status : '',
    }]
  })
}

function modelSummaryLabel(model: ModelSummary) {
  return `${model.manufacturer} ${model.name}`.trim() || model.name
}

function identitySearchValues(identity: DeviceIdentity, extra = '') {
  return [identity.serialNumber, identity.assetTag, extra].map((value) => value.trim()).filter(Boolean)
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

export default function AtlasScanner({
  active, canWrite, csrfToken, onAssociated, onClearAsset, onLinkAssetModel, onResolveAsset, onSetupScannedModel, selectedAsset,
}: AtlasScannerProps) {
  const [open, setOpen] = useState(false)
  const [mode, setMode] = useState<ScanMode>('find')
  const [symbology, setSymbology] = useState<Symbology>('code128')
  const [terminator, setTerminator] = useState<Terminator>('Enter')
  const [burstWindow, setBurstWindow] = useState(500)
  const [value, setValue] = useState('')
  const [identity, setIdentity] = useState<DeviceIdentity>(emptyDeviceIdentity)
  const [captured, setCaptured] = useState<CapturedIdentityValue[]>([])
  const [matches, setMatches] = useState<IdentityCatalogMatch[]>([])
  const [mismatchOpen, setMismatchOpen] = useState(false)
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
  const seenCodesRef = useRef(new Set<string>())
  const identityRef = useRef(identity)
  const capturedRef = useRef(captured)
  const dismissedMismatchRef = useRef('')
  identityRef.current = identity
  capturedRef.current = captured

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
    setIdentity(emptyDeviceIdentity())
    setCaptured([])
    setMatches([])
    setMismatchOpen(false)
    dismissedMismatchRef.current = ''
    setError('')
    setMessage('Scanning cancelled. No asset or identifier was changed.')
    setRetry(false)
    burstStartRef.current = 0
    lastKeystrokeRef.current = 0
    seenCodesRef.current = new Set()
  }

  async function searchAsset(query: string) {
    const response = await requestJSON(`/api/v1/assets?q=${encodeURIComponent(query)}&limit=20`)
    const items = readAssetSummaries(response)
    const needle = query.trim().toLowerCase()
    return items.find((item) => item.id.toLowerCase() === needle
      || item.name.toLowerCase() === needle
      || ('assetTag' in item && String(item.assetTag).toLowerCase() === needle)
      || ('serialNumber' in item && String(item.serialNumber).toLowerCase() === needle))
      ?? items[0]
      ?? null
  }

  async function searchModels(query: string) {
    const params = new URLSearchParams({ limit: '20', includeRetired: 'true', q: query })
    const response = await requestJSON(`/api/v1/asset-models?${params.toString()}`)
    return readModelSummaries(response)
  }

  async function refreshMatches(nextIdentity: DeviceIdentity) {
    const found: IdentityCatalogMatch[] = []
    async function matchAsset(field: 'serialNumber' | 'assetTag', value: string) {
      if (!value) return
      try {
        const items = readAssetSummaries(await requestJSON(`/api/v1/assets?q=${encodeURIComponent(value)}&limit=20`))
        const match = items.find((item) => identityValuesEqual(item[field] ?? '', value))
        if (match) {
          found.push({ field, value, recordId: match.id, label: match.name, kind: field })
        }
      } catch {
        // Match highlighting is advisory; lookup failures must not block scanning.
      }
    }
    await matchAsset('serialNumber', nextIdentity.serialNumber)
    await matchAsset('assetTag', nextIdentity.assetTag)
    if (nextIdentity.modelNumber) {
      try {
        const models = await searchModels(nextIdentity.modelNumber)
        const match = models.find((item) => identityValuesEqual(item.modelNumber ?? '', nextIdentity.modelNumber))
        if (match) {
          found.push({
            field: 'modelNumber',
            value: nextIdentity.modelNumber,
            recordId: match.id,
            label: modelSummaryLabel(match),
            kind: 'modelNumber',
          })
        }
      } catch {
        // Same as asset lookup: a failed catalog search still leaves the scanned value.
      }
    }
    setMatches(found)
  }

  function adoptCaptured(next: CapturedIdentityValue[]) {
    setCaptured(next)
    const mapped = identityFromCaptured(next)
    setIdentity(mapped)
    void refreshMatches(mapped)
    return mapped
  }

  function adoptScans(rawValues: string[]) {
    const next = mergeCapturedValues(capturedRef.current, rawValues.flatMap((raw) => capturedValuesFromScan(raw)))
    const mapped = adoptCaptured(next)
    if (next.length > 1) {
      setMessage('Multiple values captured. Map each barcode, then find the matching asset.')
    }
    return { captured: next, identity: mapped }
  }

  function fieldMatch(field: IdentityField, value: string) {
    if (!value) return undefined
    return matches.find((item) => item.field === field && identityValuesEqual(item.value, value))
  }

  async function resolveIdentifier(detectedSymbology: Symbology, scanned: string) {
    const response = await requestJSON('/api/v1/asset-identifiers/resolve', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ symbology: detectedSymbology, value: scanned }),
    })
    const assetID = resolvedAssetID(response)
    if (!assetID) throw new Error('invalid identifier resolution response')
    return assetID
  }

  async function findAsset(scanned: string, detectedSymbology: CapturedSymbology | Symbology, currentIdentity = identityRef.current) {
    const queries = identitySearchValues(currentIdentity, scanned)
    if (scanned && isAtlasCodeSymbology(detectedSymbology)) {
      try {
        const assetID = await resolveIdentifier(detectedSymbology === 'qr' ? 'qr' : 'code128', scanned)
        await onResolveAsset(assetID)
        // Atlas Codes are identifiers, not manufacturer stickers. Drop the
        // misclassified capture so a later unknown code is a new lookup
        // instead of a second barcode in the label-mapping flow.
        capturedRef.current = []
        identityRef.current = emptyDeviceIdentity()
        setCaptured([])
        setIdentity(emptyDeviceIdentity())
        setMatches([])
        setMessage('Identifier matched. The authorized asset is shown below.')
        return true
      } catch (requestError) {
        if (!(requestError instanceof ApiRequestError) || requestError.status !== 404) throw requestError
      }
    }
    for (const query of queries) {
      const match = await searchAsset(query)
      if (match) {
        await onResolveAsset(match.id)
        setMessage(`Matched ${match.name} from the scanned serial or asset tag.`)
        return true
      }
    }
    setError('No authorized asset matched that serial, asset tag, or Atlas Code.')
    setRetry(true)
    return false
  }

  useEffect(() => {
    if (!selectedAsset || !identity.modelNumber) {
      setMismatchOpen(false)
      return
    }
    if (!modelNumberDecisionNeeded(identity.modelNumber, selectedAsset.modelNumber)) {
      setMismatchOpen(false)
      return
    }
    const key = `${selectedAsset.id}:${identity.modelNumber.toLowerCase()}`
    if (dismissedMismatchRef.current === key) return
    setMismatchOpen(true)
  }, [identity.modelNumber, selectedAsset])

  function dismissMismatch() {
    if (selectedAsset && identity.modelNumber) {
      dismissedMismatchRef.current = `${selectedAsset.id}:${identity.modelNumber.toLowerCase()}`
    }
    setMismatchOpen(false)
  }

  function handleWrongItem() {
    dismissMismatch()
    onClearAsset?.()
    setMessage('Cleared the matched asset. Scan the labels on the intended item.')
  }

  async function handleEnterModelOnItem() {
    if (!selectedAsset || !identity.modelNumber) return
    if (!onLinkAssetModel) {
      setError('Asset write access is required to enter a model number on this item.')
      return
    }
    setBusy(true)
    setError('')
    try {
      const models = await searchModels(identity.modelNumber)
      const exact = models.filter((item) => identityValuesEqual(item.modelNumber ?? '', identity.modelNumber) && item.status !== 'retired')
      if (exact.length === 1) {
        await onLinkAssetModel(exact[0].id)
        dismissMismatch()
        setMessage(`Entered ${identity.modelNumber} on ${selectedAsset.name} by linking ${modelSummaryLabel(exact[0])}.`)
        return
      }
      setError(exact.length === 0
        ? 'No catalog model uses that number yet. Associate it with the model setup to record it there.'
        : 'Several catalog models use that number. Associate it with the model setup to choose one.')
    } catch (requestError) {
      setError(requestMessage(requestError, 'The catalog model could not be linked to this asset.'))
    } finally {
      setBusy(false)
    }
  }

  function handleAssociateModelSetup() {
    if (!identity.modelNumber) return
    onSetupScannedModel?.({ modelNumber: identity.modelNumber, modelId: selectedAsset?.modelId })
    dismissMismatch()
    setMessage('Opened Models so you can record the scanned model number on the catalog record.')
  }

  async function submitValue(candidate = value, detectedSymbology: CapturedSymbology | Symbology = symbology) {
    const scanned = candidate.trim()
    setError('')
    setMessage('')
    setRetry(false)
    if (mode === 'labels') {
      if (!scanned && !identity.serialNumber && !identity.assetTag) {
        setError('Scan or enter the manufacturer serial, internal asset tag, or both.')
        return
      }
      const next = scanned ? adoptScans([scanned]).identity : identity
      setValue('')
      if (!next.serialNumber && !next.assetTag) {
        setMessage('Captured the model number. Scan the serial or asset tag, map the barcodes, then find the matching asset.')
        return
      }
      const duplicateKey = `labels:${next.serialNumber}:${next.assetTag}:${next.modelNumber}`
      const now = Date.now()
      if (lastCompletedRef.current.key === duplicateKey && now - lastCompletedRef.current.at < duplicateWindowMilliseconds) {
        setMessage('Duplicate scan ignored. The first scan already completed.')
        return
      }
      setBusy(true)
      try {
        const found = await findAsset(scanned, detectedSymbology, next)
        if (found) lastCompletedRef.current = { key: duplicateKey, at: Date.now() }
      } catch (requestError) {
        setError(requestMessage(requestError, 'The scanned labels could not be matched to an asset.'))
        setRetry(true)
      } finally {
        setBusy(false)
        inputRef.current?.focus()
      }
      return
    }
    if (mode === 'find' && !scanned && (identity.serialNumber || identity.assetTag)) {
      setBusy(true)
      try {
        const found = await findAsset('', detectedSymbology, identity)
        if (found) lastCompletedRef.current = { key: `find-identity:${identity.serialNumber}:${identity.assetTag}`, at: Date.now() }
      } catch (requestError) {
        setError(requestMessage(requestError, 'The identifier could not be resolved.'))
        setRetry(true)
      } finally {
        setBusy(false)
        inputRef.current?.focus()
      }
      return
    }
    const validation = isAtlasCodeSymbology(detectedSymbology)
      ? validateScannedValue(detectedSymbology, scanned)
      : { value: scanned, error: scanned ? '' : 'Enter or scan an identifier.' }
    setValue(validation.value)
    if (validation.error) {
      setError(validation.error)
      return
    }
    if (mode === 'associate' && (!canWrite || !selectedAsset)) {
      setError(canWrite ? 'Choose an asset before scanning an identifier to associate.' : 'Asset write access is required to associate an identifier.')
      return
    }
    if (mode === 'associate' && !isAtlasCodeSymbology(detectedSymbology)) {
      setError('Atlas Code association accepts Code 128 and QR. Capture a serial, asset tag, or model with the label workflow, or scan those fields in the Assets grid.')
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
        const incoming = capturedValuesFromScan(validation.value)
        const nextCaptured = mergeCapturedValues(capturedRef.current, incoming)
        const next = incoming.length > 0 ? adoptCaptured(nextCaptured) : applyIdentityScan(identity, validation.value)
        if (nextCaptured.length > 1) {
          setValue('')
          setMessage('Multiple values captured. Map each barcode, then find the matching asset.')
          return
        }
        const found = await findAsset(validation.value, detectedSymbology, next)
        if (found) lastCompletedRef.current = { key: duplicateKey, at: Date.now() }
        else return
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
    seenCodesRef.current = new Set()
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
      setMessage(mode === 'labels'
        ? 'Camera active. Point at the serial, asset tag, and model barcodes. Frames stay in this browser.'
        : 'Camera active. Frames stay in this browser and are not uploaded or retained.')
      const detector = new Detector({ formats: [...identityBarcodeFormats] })
      const detect = async () => {
        if (scanGenerationRef.current !== generation || !streamRef.current || !videoRef.current) return
        try {
          const detections = await detector.detect(videoRef.current)
          const codes = detections.flatMap((item) => {
            const detected = symbologyFromFormat(item.format)
            const raw = item.rawValue?.trim() ?? ''
            if (!detected || !raw || seenCodesRef.current.has(raw)) return []
            return [{ value: raw, symbology: detected }]
          })
          if (codes.length > 0) {
            for (const code of codes) seenCodesRef.current.add(code.value)
            if (mode === 'associate') {
              const first = codes[0]
              const detected = first.symbology === 'qr' ? 'qr' : first.symbology === 'code128' ? 'code128' : first.symbology
              if (!isAtlasCodeSymbology(first.symbology)) {
                setError('That barcode format is not an Atlas Code. Use Capture serial, tag, and model, or scan Code 128 or QR.')
              } else {
                if (detected === 'code128' || detected === 'qr') setSymbology(detected)
                stopCamera('Code captured. Checking the identifier…')
                await submitValue(first.value, first.symbology)
                return
              }
            } else {
              const adopted = adoptScans(codes.map((code) => code.value))
              if (mode === 'labels' || adopted.captured.length > 1) {
                if (adopted.captured.length > 1) {
                  setMessage('Multiple values captured. Map each barcode, then find the matching asset.')
                }
                if (mode === 'find' && adopted.captured.length > 1) {
                  stopCamera('Multiple values captured. Map each barcode, then find the matching asset.')
                  return
                }
                frameRef.current = requestAnimationFrame(() => { void detect() })
                return
              }
              const first = codes[0]
              const detected = first.symbology === 'qr' ? 'qr' : first.symbology === 'code128' ? 'code128' : first.symbology
              if (detected === 'code128' || detected === 'qr') setSymbology(detected)
              stopCamera('Code captured. Checking the identifier…')
              await submitValue(first.value, first.symbology)
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

  const showIdentity = mode === 'labels' || Boolean(identity.serialNumber || identity.assetTag || identity.modelNumber)
  const findLabel = mode === 'labels' ? 'Find matching asset' : mode === 'find' ? 'Find asset' : 'Associate identifier'
  const busyLabel = mode === 'associate' ? 'Associating…' : 'Finding…'
  const workflowHint = workflowOptions.find((item) => item.id === mode)?.hint ?? ''

  return <section aria-labelledby="atlas-scanner-heading" className={`${panelClass} overflow-hidden`} data-feature="inventory.identifiers" data-requirement="REQ-ATLAS-CODES-001">
    <div className="border-b border-white/[0.08] px-5 py-4 sm:px-6">
      <div className="flex min-w-0 flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 max-w-3xl">
          <p className="text-[13px] font-medium text-steward-slate">Atlas Codes</p>
          <h3 className="mt-1 text-xl font-semibold text-steward-mist" id="atlas-scanner-heading">Atlas Codes — Scan</h3>
          <p className="mt-1.5 text-sm leading-6 text-steward-mist-muted">Find an asset, capture manufacturer labels, or search for an asset here and attach a Code 128 or QR to it. Scanner input cannot act while this surface is closed.</p>
        </div>
        <button className={open ? plainButtonClass : buttonClass} onClick={() => { if (open) closeScanner(); else { setOpen(true); setMessage('Scan mode active. Choose a workflow and input method.'); queueMicrotask(() => inputRef.current?.focus()) } }} type="button">{open ? 'Cancel scanning' : 'Open scanner'}</button>
      </div>
      {message && <p className="mt-4 rounded-md border border-steward-success/35 bg-steward-success/10 px-3 py-2.5 text-sm text-[#98eab9]" role="status">{message}</p>}
      {error && <p className="mt-4 rounded-md border border-steward-danger/45 bg-steward-danger/10 px-3 py-2.5 text-sm text-[#ffccd1]" role="alert">{error}</p>}
      {!open && !message && <p className="mt-4 rounded-md border border-dashed border-white/12 bg-steward-ink-950/40 px-4 py-5 text-sm leading-6 text-steward-mist-muted">Scanner is idle. Open it to capture with a keyboard wedge, paste, or the camera. Nothing is written until a scan completes.</p>}
    </div>
    {open && <form aria-label="Scan an Atlas Code" className="min-w-0 p-5 sm:p-6" onSubmit={onSubmit}>
      <div className="flex min-w-0 flex-wrap gap-2" role="group" aria-label="Scan workflow">
        {workflowOptions.filter((item) => !item.write || canWrite).map((item) => (
          <button
            aria-pressed={mode === item.id}
            className={cx(
              'min-h-11 w-full min-w-0 max-w-full rounded-md px-3.5 py-2 text-left text-sm transition sm:w-auto',
              mode === item.id
                ? 'border border-steward-teal/45 bg-steward-teal/12 font-semibold text-steward-mist'
                : 'border border-white/12 bg-transparent font-medium text-steward-mist-muted hover:border-white/20 hover:bg-white/[0.04] hover:text-steward-mist',
            )}
            key={item.id}
            onClick={() => { setMode(item.id); setRetry(false); setError('') }}
            type="button"
          >
            <span className="block">{item.label}</span>
            <span className="mt-0.5 block text-xs font-normal text-steward-mist-muted">{item.hint}</span>
          </button>
        ))}
      </div>
      <label className="sr-only">Workflow
        <select className="h-px w-px min-h-0 min-w-0 max-w-px overflow-hidden" onChange={(event) => { setMode(event.target.value as ScanMode); setRetry(false); setError('') }} value={mode}>
          <option value="find">Find an asset</option>
          <option value="labels">Capture serial, tag, and model</option>
          {canWrite && <option value="associate">Attach a code to an asset</option>}
        </select>
      </label>
      <p className="mt-2 text-sm text-steward-mist-muted">{workflowHint}</p>

      <div className={cx(subpanelClass, 'mt-5 p-4')}>
        <div className="flex min-w-0 flex-wrap items-center justify-between gap-2">
          <p className="text-sm font-medium text-steward-mist">Capture</p>
          <StatusBadge tone={cameraActive ? 'success' : 'neutral'}>{cameraActive ? 'Camera live' : 'Manual or camera'}</StatusBadge>
        </div>
        <div className="mt-3 grid gap-3 sm:grid-cols-[minmax(0,1fr)_10.5rem]">
          <label className={labelClass}>Scanned or entered value
            <input autoCapitalize="none" autoComplete="off" autoCorrect="off" className={cx(inputClass, 'font-mono tracking-wide')} maxLength={512} onChange={(event) => { setValue(event.target.value); setRetry(false); setError('') }} onKeyDown={onInputKeyDown} onPaste={() => { burstStartRef.current = 0; lastKeystrokeRef.current = 0 }} placeholder="Scan, paste, or type" ref={inputRef} spellCheck={false} value={value} />
          </label>
          <label className={labelClass}>Symbology
            <select className={inputClass} onChange={(event) => { setSymbology(event.target.value as Symbology); setRetry(false); setError('') }} value={symbology}><option value="code128">Code 128</option><option value="qr">QR</option></select>
          </label>
        </div>
        <p className="mt-2 text-xs text-steward-mist-muted">Wedge scanners submit with {terminator}. Burst window {burstWindow < 1000 ? `${burstWindow} ms` : `${burstWindow / 1000}s`}.</p>
        <div className="mt-4 flex flex-wrap gap-2">
          <button className={buttonClass} disabled={busy || (mode === 'associate' && !selectedAsset)} type="submit">{busy ? busyLabel : retry ? 'Retry scan' : findLabel}</button>
          {!cameraActive && <button className={secondaryButtonClass} disabled={busy} onClick={() => void startCamera()} type="button">Use camera</button>}
          {cameraActive && <button className={secondaryButtonClass} onClick={() => stopCamera('Camera stopped. Manual and keyboard-scanner input remain available.')} type="button">Stop camera</button>}
        </div>
        <CameraPreview active={cameraActive} videoRef={videoRef} />
      </div>

      {showIdentity && <div className="mt-4 grid gap-3 md:grid-cols-3">
        {identityFields.map((field) => {
          const match = fieldMatch(field.key, identity[field.key])
          const filled = Boolean(identity[field.key])
          return (
            <div className={cx(subpanelClass, 'p-3', filled && 'border-steward-teal/25')} key={field.key}>
              <label className={labelClass}>{field.label}
                <input
                  autoCapitalize="none"
                  autoComplete="off"
                  autoCorrect="off"
                  className={cx(inputClass, 'font-mono')}
                  maxLength={field.key === 'serialNumber' ? 255 : field.key === 'assetTag' ? 128 : 120}
                  onChange={(event) => {
                    const next = assignIdentityField(identity, field.key, event.target.value)
                    setIdentity(next)
                    void refreshMatches(next)
                  }}
                  placeholder={field.key === 'serialNumber' ? 'PF41ER5H' : field.key === 'assetTag' ? '43188' : '20W5S51T00'}
                  spellCheck={false}
                  value={identity[field.key]}
                />
              </label>
              {match && captured.length <= 1 && (
                <p className="mt-2">
                  <StatusBadge tone="success">Matches existing {identityFieldLabels[match.kind].toLowerCase()} on {match.label}</StatusBadge>
                </p>
              )}
            </div>
          )
        })}
      </div>}
      {captured.length > 1 && (
        <IdentityScanMapper
          matches={matches}
          onChange={(values) => { adoptCaptured(values) }}
          values={captured}
        />
      )}
      {mismatchOpen && selectedAsset && identity.modelNumber && (
        <div aria-labelledby="scanned-model-mismatch-heading" className="mt-4 rounded-md border border-steward-warning/50 bg-steward-warning/10 p-4" role="region">
          <h4 className="font-semibold text-[#ffd596]" id="scanned-model-mismatch-heading">Check the scanned model number</h4>
          <p className="mt-2 text-sm leading-6 text-steward-mist">
            {selectedAsset.modelNumber
              ? `Scanned ${identity.modelNumber} does not match ${selectedAsset.name}'s catalog model number ${selectedAsset.modelNumber}${selectedAsset.modelLabel ? ` (${selectedAsset.modelLabel})` : ''}.`
              : `${selectedAsset.name} has no catalog model number. Scanned ${identity.modelNumber}.`}
          </p>
          <p className="mt-2 text-sm text-steward-mist-muted">Are you scanning the right item, should this number be entered on the asset, or should it be recorded on the model setup?</p>
          <div className="mt-3 flex flex-wrap gap-2">
            <button className={secondaryButtonClass} onClick={handleWrongItem} type="button">This is the wrong item</button>
            {canWrite && <button className={secondaryButtonClass} disabled={busy} onClick={() => void handleEnterModelOnItem()} type="button">Enter this model number on the item</button>}
            {canWrite && <button className={secondaryButtonClass} onClick={handleAssociateModelSetup} type="button">Associate with model setup</button>}
          </div>
        </div>
      )}
      {mode === 'labels' && <p className="mt-4 text-sm leading-6 text-steward-mist-muted">A typical laptop underside has a manufacturer serial, a machine-type model number, and a separate internal asset tag. Scan each barcode, map them if several appear at once, then find the matching record. On the Assets tab, Scan on those cells writes the values into the row.</p>}
      {mode === 'associate' && <div className={cx(subpanelClass, 'mt-4 p-4')}>
        <RecordSearchPicker
          help="Search by name, asset tag, or serial on this tab. The chosen record is the one the next Code 128 or QR will be attached to."
          kind="asset"
          label="Asset to attach this code to"
          multiple={false}
          onChange={(records) => {
            const chosen = records[0]
            if (chosen) void onResolveAsset(chosen.id)
            else onClearAsset?.()
          }}
          selected={selectedAsset ? [{ id: selectedAsset.id, label: selectedAsset.name, detail: [selectedAsset.assetTag, selectedAsset.serialNumber].filter(Boolean).join(' · ') || undefined }] : []}
        />
        <p className="mt-3 text-sm text-steward-mist-muted">{selectedAsset ? `Ready to attach a Code 128 or QR to ${selectedAsset.name}.` : 'Choose an asset above, or use Find an asset first, then come back to this workflow.'}</p>
      </div>}

      <details className="mt-5">
        <summary className="cursor-pointer text-sm font-medium text-steward-mist-muted hover:text-steward-mist">Scanner settings</summary>
        <div className="mt-3 grid gap-3 sm:grid-cols-2">
          <label className={labelClass}>Scanner terminator
            <select className={inputClass} onChange={(event) => setTerminator(event.target.value as Terminator)} value={terminator}><option value="Enter">Enter</option><option value="Tab">Tab</option></select>
          </label>
          <label className={labelClass}>Scanner burst window
            <select className={inputClass} onChange={(event) => setBurstWindow(Number(event.target.value))} value={burstWindow}><option value={250}>250 ms</option><option value={500}>500 ms</option><option value={1000}>1 second</option><option value={2000}>2 seconds</option></select>
          </label>
        </div>
      </details>
      <p className="mt-4 text-xs leading-5 text-steward-mist-muted">Atlas Codes stay Code 128 and QR. Serial, asset tag, and model capture also reads Code 39 and other manufacturer barcodes. Camera frames stay local. Duplicate completed scans are suppressed briefly; conflicts and denied records do not reveal hidden asset details.</p>
    </form>}
  </section>
}
