import { useLayoutEffect, useRef, useState, type CSSProperties } from 'react'

export type AnchoredBox = { left: number; top: number; right: number; bottom: number }
export type AnchoredSize = { width: number; height: number }
export type AnchoredPlacement = { x: number; y: number; maxHeight: number }

const defaultMargin = 8

export function anchoredBox(anchor: AnchoredBox | HTMLElement | null | undefined): AnchoredBox {
  if (!anchor) return { left: defaultMargin, top: defaultMargin, right: defaultMargin, bottom: defaultMargin }
  if (anchor instanceof HTMLElement) {
    const box = anchor.getBoundingClientRect()
    return { left: box.left, top: box.top, right: box.right, bottom: box.bottom }
  }
  return anchor
}

/** Places a floating panel next to a cell and keeps every edge inside the viewport. */
export function placeAnchoredPanel(
  anchor: AnchoredBox | HTMLElement | null | undefined,
  size: AnchoredSize,
  margin = defaultMargin,
): AnchoredPlacement {
  const box = anchoredBox(anchor)
  const viewportWidth = typeof window === 'undefined' ? size.width : window.innerWidth
  const viewportHeight = typeof window === 'undefined' ? size.height : window.innerHeight
  const maxWidth = Math.max(0, viewportWidth - margin * 2)
  const maxHeight = Math.max(0, viewportHeight - margin * 2)
  const width = Math.min(Math.max(size.width, 0), maxWidth)
  const height = Math.min(Math.max(size.height, 0), maxHeight)
  const x = Math.max(margin, Math.min(box.left, viewportWidth - width - margin))
  const below = box.bottom + 4
  const above = box.top - size.height - 4
  const fitsBelow = below + size.height <= viewportHeight - margin
  const fitsAbove = above >= margin
  const y = Math.max(
    margin,
    Math.min(
      fitsBelow ? below : fitsAbove ? above : margin,
      viewportHeight - height - margin,
    ),
  )
  return { x, y, maxHeight }
}

export function useAnchoredPanelStyle(anchor: AnchoredBox | HTMLElement | null | undefined) {
  const ref = useRef<HTMLDivElement | null>(null)
  const [style, setStyle] = useState<CSSProperties>({ left: defaultMargin, top: defaultMargin })

  useLayoutEffect(() => {
    const element = ref.current
    if (!element) return
    const place = () => {
      const size = element.getBoundingClientRect()
      const next = placeAnchoredPanel(anchor, size)
      setStyle({ left: next.x, top: next.y, maxHeight: next.maxHeight })
    }
    place()
    const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(place)
    observer?.observe(element)
    window.addEventListener('resize', place)
    window.addEventListener('scroll', place, true)
    return () => {
      observer?.disconnect()
      window.removeEventListener('resize', place)
      window.removeEventListener('scroll', place, true)
    }
  }, [anchor])

  return { ref, style }
}
