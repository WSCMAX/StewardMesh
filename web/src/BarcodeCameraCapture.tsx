import { useEffect, useRef, useState } from 'react'
import { secondaryButtonClass } from './ui'

// Requirement: REQ-ATLAS-CODES-001. Feature: inventory.identifiers.

export type CapturedCode = { value: string; symbology: 'code128' | 'qr' }

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
  onCapture: (code: CapturedCode) => void
  /** Starts the camera as soon as the control mounts, used by the in-cell scanner. */
  autoStart?: boolean
}

function barcodeDetectorConstructor() {
  return (globalThis as typeof globalThis & { BarcodeDetector?: BarcodeDetectorConstructor }).BarcodeDetector
}

export default function BarcodeCameraCapture({ disabled, onCapture, autoStart = false }: BarcodeCameraCaptureProps) {
  const [cameraActive, setCameraActive] = useState(false)
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')
  const videoRef = useRef<HTMLVideoElement>(null)
  const streamRef = useRef<MediaStream | null>(null)
  const frameRef = useRef<number | null>(null)
  const scanGenerationRef = useRef(0)

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
              stopCamera('Code captured. Review the value, then save to associate it.')
              onCapture({ value: detection.rawValue, symbology: detected })
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

  return (
    <div>
      <div className="flex flex-wrap gap-2">
        {!cameraActive && <button className={secondaryButtonClass} disabled={disabled} onClick={() => void startCamera()} type="button">Scan with camera</button>}
        {cameraActive && <button className={secondaryButtonClass} onClick={() => stopCamera('Camera stopped. Manual and keyboard-scanner input remain available.')} type="button">Stop camera</button>}
      </div>
      {message && <p className="mt-3 rounded-lg border border-steward-green/40 bg-steward-green/10 p-3 text-sm" role="status">{message}</p>}
      {error && <p className="mt-3 rounded-lg border border-red-400/50 bg-red-950/50 p-3 text-sm" role="alert">{error}</p>}
      <video aria-label="Live barcode camera preview" className={`${cameraActive ? 'mt-3 block' : 'hidden'} max-h-56 w-full rounded-xl bg-black object-contain`} muted playsInline ref={videoRef} />
    </div>
  )
}
