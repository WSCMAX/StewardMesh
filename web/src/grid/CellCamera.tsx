import { useEffect } from 'react'
import { createPortal } from 'react-dom'
import BarcodeCameraCapture from '../BarcodeCameraCapture'
import { cx, menuSurfaceClass } from '../ui'
import { useAnchoredPanelStyle, type AnchoredBox } from './anchoredPanel'

// Requirements: REQ-ATLAS-CODES-001, A11Y-001. Feature: experience.grid.

// A camera preview anchored to the cell being edited so a serial number, asset
// tag, or model barcode can be scanned without leaving the grid. The panel is
// opaque so the spreadsheet does not show through. Placement stays inside the
// viewport, including last rows whose camera would otherwise open below the
// scrollport.

export default function CellCamera({ anchor, onCapture, onClose }: {
  anchor: AnchoredBox | HTMLElement | null
  onCapture: (value: string) => void
  onClose: () => void
}) {
  const { ref, style } = useAnchoredPanelStyle(anchor)

  useEffect(() => {
    function handlePointerDown(event: PointerEvent) {
      if (!ref.current?.contains(event.target as Node)) onClose()
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
  }, [onClose, ref])

  return createPortal(
    <div
      className={cx(menuSurfaceClass, 'fixed z-50 w-80 overflow-y-auto p-3 shadow-2xl steward-scrollbar')}
      ref={ref}
      role="dialog"
      aria-label="Scan a barcode into this cell"
      style={style}
    >
      <p className="text-xs font-semibold text-steward-mist">Scan into this cell</p>
      <p className="mt-1 text-xs leading-5 text-steward-mist-muted">Point at the printed serial, asset tag, or model barcode. Frames stay in this browser.</p>
      <div className="mt-3 border-t border-white/10 pt-3">
        <BarcodeCameraCapture autoStart onCapture={(code) => onCapture(code.value)} />
      </div>
    </div>,
    document.body,
  )
}
