import { displayType, colorsForNode, type GraphColorMode, type GraphPaletteKey } from './graphModel'
import { edgeVisualWidth, nodeVisualRadius, truncateLabel, type LayoutLink, type LayoutNode } from './graphLayout'
import type { ZoomTransform } from 'd3-zoom'

// Requirement: REQ-DIRECTORY-EXPANSION-008. Feature: threads.relationships.

export type CanvasHighlight = {
  selectedNodeID: string
  hoveredNodeID: string
  highlightedKind: string
  neighborIDs: ReadonlySet<string>
  labelThreshold: number
}

function paintNode(
  node: LayoutNode,
  selectedNodeID: string,
  colorMode: GraphColorMode,
  kindColorOverrides?: Readonly<Record<string, GraphPaletteKey>>,
) {
  const colors = colorsForNode(node.kind, node.attributes, colorMode, kindColorOverrides)
  return {
    fill: colors.stroke,
    stroke: node.id === selectedNodeID ? '#ffffff' : node.degree === 0 ? '#f0b429' : '#071018',
    width: node.id === selectedNodeID ? 2.4 : 1.15,
    dash: node.degree === 0,
  }
}

function linkEnds(link: LayoutLink) {
  const source = typeof link.source === 'object' ? link.source : undefined
  const target = typeof link.target === 'object' ? link.target : undefined
  return {
    source,
    target,
    sourceID: source?.id ?? String(link.source),
    targetID: target?.id ?? String(link.target),
    sourceKind: source?.kind ?? '',
    targetKind: target?.kind ?? '',
    sourceDegree: source?.degree ?? 1,
    targetDegree: target?.degree ?? 1,
  }
}

function emphasized(node: LayoutNode, highlight: CanvasHighlight, active: string, kind: string) {
  if (active) return highlight.neighborIDs.has(node.id)
  if (kind) return node.kind === kind
  return true
}

export function configureGraphCanvas(canvas: HTMLCanvasElement, width: number, height: number) {
  const dpr = Math.max(1, typeof window === 'undefined' ? 1 : window.devicePixelRatio || 1)
  canvas.width = Math.max(1, Math.floor(width * dpr))
  canvas.height = Math.max(1, Math.floor(height * dpr))
  canvas.style.width = `${width}px`
  canvas.style.height = `${height}px`
  return canvas.getContext('2d')
}

export function visibleWorldBounds(transform: ZoomTransform, width: number, height: number, pad = 48) {
  const topLeft = transform.invert([0, 0])
  const bottomRight = transform.invert([width, height])
  return {
    left: Math.min(topLeft[0], bottomRight[0]) - pad,
    top: Math.min(topLeft[1], bottomRight[1]) - pad,
    right: Math.max(topLeft[0], bottomRight[0]) + pad,
    bottom: Math.max(topLeft[1], bottomRight[1]) + pad,
  }
}

export function drawGraphCanvas(
  ctx: CanvasRenderingContext2D,
  nodes: readonly LayoutNode[],
  links: readonly LayoutLink[],
  width: number,
  height: number,
  transform: ZoomTransform,
  highlight: CanvasHighlight,
  colorMode: GraphColorMode,
  kindColorOverrides?: Readonly<Record<string, GraphPaletteKey>>,
) {
  const dpr = Math.max(1, typeof window === 'undefined' ? 1 : window.devicePixelRatio || 1)
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
  ctx.clearRect(0, 0, width, height)
  ctx.setTransform(dpr * transform.k, 0, 0, dpr * transform.k, dpr * transform.x, dpr * transform.y)

  const view = visibleWorldBounds(transform, width, height)
  const kindCount = highlight.highlightedKind ? nodes.reduce((count, node) => count + (node.kind === highlight.highlightedKind ? 1 : 0), 0) : 0
  const active = highlight.hoveredNodeID || (highlight.highlightedKind ? '' : highlight.selectedNodeID)
  const kind = active ? '' : highlight.highlightedKind

  ctx.lineCap = 'round'
  for (const link of links) {
    const ends = linkEnds(link)
    if (!ends.source || !ends.target) continue
    const x1 = ends.source.x ?? 0
    const y1 = ends.source.y ?? 0
    const x2 = ends.target.x ?? 0
    const y2 = ends.target.y ?? 0
    if ((x1 < view.left && x2 < view.left) || (x1 > view.right && x2 > view.right) || (y1 < view.top && y2 < view.top) || (y1 > view.bottom && y2 > view.bottom)) continue
    const related = active
      ? ends.sourceID === active || ends.targetID === active
      : kind
        ? ends.sourceKind === kind || ends.targetKind === kind
        : false
    const base = edgeVisualWidth(link.kind, ends.sourceDegree, ends.targetDegree)
    ctx.beginPath()
    ctx.moveTo(x1, y1)
    ctx.lineTo(x2, y2)
    ctx.strokeStyle = !active && !kind ? '#6b7c90' : related ? '#7af0d8' : '#2d3a48'
    ctx.globalAlpha = !active && !kind ? 0.28 : related ? 1 : 0.05
    ctx.lineWidth = !active && !kind ? base : related ? Math.max(2.4, base + 1.8) : Math.max(0.4, base * 0.45)
    ctx.stroke()
  }
  ctx.globalAlpha = 1

  for (const node of nodes) {
    const x = node.x ?? 0
    const y = node.y ?? 0
    const radius = nodeVisualRadius(node.degree)
    if (x + radius < view.left || x - radius > view.right || y + radius < view.top || y - radius > view.bottom) continue
    const paint = paintNode(node, highlight.selectedNodeID, colorMode, kindColorOverrides)
    const isEmphasized = emphasized(node, highlight, active, kind)
    const selectedOrKind = (Boolean(active) && node.id === active) || (Boolean(kind) && node.kind === kind)
    ctx.beginPath()
    ctx.arc(x, y, radius, 0, Math.PI * 2)
    ctx.globalAlpha = isEmphasized ? 1 : 0.12
    ctx.fillStyle = paint.fill
    ctx.fill()
    ctx.strokeStyle = selectedOrKind ? '#ffffff' : paint.stroke
    ctx.lineWidth = selectedOrKind ? 2.4 : isEmphasized && (active || kind) ? 1.8 : paint.width
    ctx.setLineDash(paint.dash ? [3, 3] : [])
    if (isEmphasized && (active || kind)) {
      ctx.shadowColor = paint.fill
      ctx.shadowBlur = 8 / Math.max(transform.k, 0.4)
    }
    ctx.stroke()
    ctx.shadowBlur = 0
    ctx.setLineDash([])
    ctx.globalAlpha = 1

    let labelOpacity = 0
    if (active) {
      if (node.id === active) labelOpacity = 1
      else if (highlight.neighborIDs.size <= 24 && highlight.neighborIDs.has(node.id)) labelOpacity = 1
    } else if (kind) {
      if (node.kind === kind && (kindCount <= 36 || node.degree >= highlight.labelThreshold)) labelOpacity = 1
    } else if (node.degree >= highlight.labelThreshold) {
      labelOpacity = 0.92
    }
    if (labelOpacity <= 0) continue
    ctx.globalAlpha = labelOpacity
    ctx.font = '11px "Segoe UI", "Helvetica Neue", sans-serif'
    ctx.textAlign = 'center'
    ctx.textBaseline = 'top'
    ctx.lineWidth = 3
    ctx.strokeStyle = '#071018'
    ctx.fillStyle = '#e7edf3'
    const label = truncateLabel(node.label)
    ctx.strokeText(label, x, y + radius + 8)
    ctx.fillText(label, x, y + radius + 8)
    const showKind = (Boolean(active) && node.id === active) || (Boolean(kind) && node.kind === kind && kindCount <= 12)
    if (showKind) {
      ctx.globalAlpha = 0.9
      ctx.font = '9px "Segoe UI", "Helvetica Neue", sans-serif'
      ctx.fillStyle = '#8aa0b5'
      ctx.strokeText(displayType(node.kind), x, y + radius + 22)
      ctx.fillText(displayType(node.kind), x, y + radius + 22)
    }
    ctx.globalAlpha = 1
  }
}
