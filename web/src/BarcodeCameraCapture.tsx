import { useEffect, useRef, useState } from 'react'
import { identityBarcodeFormats, symbologyFromFormat, type CapturedSymbology } from './barcodeCapture'
import CameraPreview from './CameraPreview'
import { secondaryButtonClass } from './ui'

// Requirement: REQ-ATLAS-CODES-001. Feature: inventory.identifiers.

export type CapturedCode = { value: string; symbology: CapturedSymbology }

type BarcodeDetection = {
  format?: string
  rawValue?: string
}

type BarcodeDetectorLike = {
  detect: (source: HTMLVideoElement) => Promise<BarcodeDetection[]>
}

type BarcodeDetectorConstructor = new (options?: { formats?: string[] }) => BarcodeDetectorLike

type BarcodeCameraCaptureProps = {
  disabled?: boolean
  onCapture?: (code: CapturedCode) => void
  /** Receives every distinct code in the current frame so a sticker sheet can fill several fields. */
  onCaptures?: (codes: CapturedCode[]) => void
  /** Starts the camera as soon as the control mounts, used by the in-cell scanner. */
  autoStart?: boolean
  /** Keeps decoding after a capture so serial, tag, and model can be read in one pass. */
  continuous?: boolean
}

function barcodeDetectorConstructor() {
  return (globalThis as typeof globalThis & { BarcodeDetector?: BarcodeDetectorConstructor }).BarcodeDetector
}

export default function BarcodeCameraCapture({ disabled, onCapture, onCaptures, autoStart = false, continuous = false }: BarcodeCameraCaptureProps) {
  const [cameraActive, setCameraActive] = useState(false)
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')
  const videoRef = useRef<HTMLVideoElement>(null)
  const streamRef = useRef<MediaStream | null>(null)
  const frameRef = useRef<number | null>(null)
  const scanGenerationRef = useRef(0)
  const seenRef = useRef(new Set<string>())

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
    if (autoStart && !disabled) void startCamera()
    // startCamera is a local function; auto-start is a mount-time behavior.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [autoStart, disabled])

  async function startCamera() {
    setError('')
    setMessage('')
    seenRef.current = new Set()
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
      setMessage(continuous
        ? 'Camera active. Point at the serial, asset tag, and model barcodes. Frames stay in this browser.'
        : 'Camera active. Frames stay in this browser and are not uploaded or retained.')
      const detector = new Detector({ formats: [...identityBarcodeFormats] })
      const detect = async () => {
        if (scanGenerationRef.current !== generation || !streamRef.current || !videoRef.current) return
        try {
          const detections = await detector.detect(videoRef.current)
          const codes = detections.flatMap((item) => {
            const symbology = symbologyFromFormat(item.format)
            const value = item.rawValue?.trim() ?? ''
            if (!symbology || !value || seenRef.current.has(value)) return []
            return [{ value, symbology }]
          })
          if (codes.length > 0) {
            for (const code of codes) seenRef.current.add(code.value)
            onCaptures?.(codes)
            if (!onCaptures) {
              for (const code of codes) onCapture?.(code)
            }
            if (!continuous) {
              stopCamera(codes.length > 1 ? 'Codes captured. Review the values, then save.' : 'Code captured. Review the value, then save to associate it.')
              return
            }
            setMessage(`${codes.length === 1 ? 'Code' : `${codes.length} codes`} captured. Keep scanning or stop the camera when finished.`)
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

  return (
    <div className="min-w-0">
      <div className="flex flex-wrap items-center gap-2">
        {!cameraActive && <button className={secondaryButtonClass} disabled={disabled} onClick={() => void startCamera()} type="button">Scan with camera</button>}
        {cameraActive && <button className={secondaryButtonClass} onClick={() => stopCamera('Camera stopped. Manual and keyboard-scanner input remain available.')} type="button">Stop camera</button>}
      </div>
      {message && <p className="mt-2 rounded-md border border-steward-success/35 bg-steward-success/10 px-3 py-2 text-sm text-[#98eab9]" role="status">{message}</p>}
      {error && <p className="mt-2 rounded-md border border-steward-danger/45 bg-steward-danger/10 px-3 py-2 text-sm text-[#ffccd1]" role="alert">{error}</p>}
      <CameraPreview active={cameraActive} compact videoRef={videoRef} />
    </div>
  )
}
