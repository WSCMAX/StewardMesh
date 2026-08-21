import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import BarcodeCameraCapture from '../BarcodeCameraCapture'
import { cx, subpanelClass } from '../ui'

// Requirements: REQ-ATLAS-CODES-001, A11Y-001. Feature: experience.grid.

// A camera preview anchored to the cell being edited so a serial number can be
// scanned without leaving the grid or covering the surrounding rows.

export default function CellCamera({ anchor, onCapture, onClose }: {
  anchor: DOMRect
  onCapture: (value: string) => void
  onClose: () => void
}) {
  const panelRef = useRef<HTMLDivElement | null>(null)
  const [position, setPosition] = useState({ x: anchor.left, y: anchor.bottom + 4 })

  useLayoutEffect(() => {
    const element = panelRef.current
    if (!element) return
    const { width, height } = element.getBoundingClientRect()
    const margin = 8
    setPosition({
      x: Math.max(margin, Math.min(anchor.left, window.innerWidth - width - margin)),
      y: anchor.bottom + 4 + height > window.innerHeight - margin
        ? Math.max(margin, anchor.top - height - 4)
        : anchor.bottom + 4,
    })
  }, [anchor.left, anchor.top, anchor.bottom])

  useEffect(() => {
    function handlePointerDown(event: PointerEvent) {
      if (!panelRef.current?.contains(event.target as Node)) onClose()
    }
    function handleKey(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        onClose()
        event.preventDefault()
        event.stopPropagation()
      }
    }
    document.addEventListener('pointerdown', handlePointerDown, true)
    window.addEventListener('keydown', handleKey, true)
    return () => {
      document.removeEventListener('pointerdown', handlePointerDown, true)
      window.removeEventListener('keydown', handleKey, true)
    }
  }, [onClose])

  return createPortal(
    <div
      className={cx(subpanelClass, 'fixed z-50 w-80 bg-steward-ink-900 p-3 shadow-2xl')}
      ref={panelRef}
      role="dialog"
      aria-label="Scan a barcode into this cell"
      style={{ left: position.x, top: position.y }}
    >
      <p className="text-xs font-semibold text-steward-mist">Scan into this cell</p>
      <p className="mt-1 text-xs text-steward-mist-muted">The preview stays next to the serial number. Frames stay in this browser.</p>
      <div className="mt-2">
        <BarcodeCameraCapture autoStart onCapture={(code) => onCapture(code.value)} />
      </div>
    </div>,
    document.body,
  )
}
