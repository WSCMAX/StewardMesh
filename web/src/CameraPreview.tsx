import { type RefObject } from 'react'
import { cx } from './ui'

// Requirement: REQ-ATLAS-CODES-001. Feature: inventory.identifiers.

type CameraPreviewProps = {
  active: boolean
  videoRef: RefObject<HTMLVideoElement | null>
  label?: string
  compact?: boolean
}

export default function CameraPreview({ active, compact = false, label = 'Live barcode camera preview', videoRef }: CameraPreviewProps) {
  return (
    <div className={active ? cx('steward-scan-viewfinder mt-3 ring-1 ring-inset ring-white/12', compact && 'steward-scan-viewfinder-compact mt-2') : 'hidden'}>
      <video aria-label={label} muted playsInline ref={videoRef} />
      {active && (
        <>
          <div aria-hidden="true" className="steward-scan-overlay">
            <span className="absolute -right-px -top-px h-[1.15rem] w-[1.15rem] border-t-2 border-r-2 border-steward-teal" />
            <span className="absolute -bottom-px -left-px h-[1.15rem] w-[1.15rem] border-b-2 border-l-2 border-steward-teal" />
          </div>
          <div aria-hidden="true" className="steward-scan-line" />
          <p className="absolute left-3 top-3 inline-flex items-center gap-1.5 rounded-sm border border-steward-teal/35 bg-steward-ink-950/80 px-1.5 py-0.5 text-[11px] font-medium text-[#98eab9]">
            <span className="steward-pulse size-1.5 rounded-full bg-steward-teal" />
            Live · stays in this browser
          </p>
        </>
      )}
    </div>
  )
}
