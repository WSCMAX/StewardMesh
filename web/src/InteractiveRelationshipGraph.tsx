import { drag } from 'd3-drag'
import { select } from 'd3-selection'
import { zoom, zoomIdentity, type ZoomBehavior, type ZoomTransform } from 'd3-zoom'
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { configureGraphCanvas, drawGraphCanvas } from './graphCanvasRender'
import {
  applyMassedRepel,
  applyPositions,
  centerPull,
  chargeStrength,
  denseGraphNodeThreshold,
  edgeVisualWidth,
  graphCanvasHeight,
  graphLayoutProfile,
  graphRenderSurface,
  gravityStrength,
  hitTestNodeIndex,
  hubRepelMagnitude,
  initialSimNodeRadius,
  isDenseGraph,
  isGroupingHub,
  isLargeGraph,
  labelDegreeThreshold,
  largeGraphNodeThreshold,
  linkForces,
  nodeMass,
  nodeVisualRadius,
  truncateLabel,
  unlinkedRepelMagnitude,
  viewportGraphHeight,
  type LayoutLink,
  type LayoutNode,
} from './graphLayout'
import { colorsForNode, defaultKindColorKey, displayType, graphPaletteColor, graphTypePalette, sourceForKind, sourceLabels, type GraphColorMode, type GraphPaletteKey } from './graphModel'
import { startGraphSimulation, type GraphSimulationHandle } from './graphSimulationClient'
import { lockScroll } from './scrollLock'
import { cx, menuSurfaceClass, secondaryButtonClass } from './ui'

// Requirement: REQ-DIRECTORY-EXPANSION-008. Feature: threads.relationships.

export type GraphNode = {
  id: string
  kind: string
  label: string
  attributes?: Record<string, string>
}

export type GraphEdge = {
  id: string
  from: string
  to: string
  kind: string
  attributes?: Record<string, string>
}

type SimNode = LayoutNode
type SimLink = LayoutLink & { source: SimNode; target: SimNode }

type NodePosition = { x: number; y: number; fx?: number | null; fy?: number | null }

type InteractiveRelationshipGraphProps = {
  nodes: GraphNode[]
  edges: GraphEdge[]
  selectedNodeID: string
  focusNodeID: string
  onSelectNode: (nodeID: string) => void
  colorMode?: GraphColorMode
  kindColorOverrides?: Readonly<Record<string, GraphPaletteKey>>
  onKindColorChange?: (kind: string, colorKey: GraphPaletteKey) => void
  groupedKinds?: readonly string[]
  inspector?: ReactNode
  onHideKind?: (kind: string) => void
  hiddenKinds?: readonly { kind: string; label: string }[]
  onShowKind?: (kind: string) => void
}

const emptyGroupedKinds: readonly string[] = []

export {
  applyMassedRepel,
  centerPull,
  chargeStrength,
  denseGraphNodeThreshold,
  edgeVisualWidth,
  graphCanvasHeight,
  graphLayoutProfile,
  graphRenderSurface,
  gravityStrength,
  hubRepelMagnitude,
  initialSimNodeRadius,
  isDenseGraph,
  isGroupingHub,
  isLargeGraph,
  labelDegreeThreshold,
  largeGraphNodeThreshold,
  linkForces,
  nodeMass,
  nodeVisualRadius,
  unlinkedRepelMagnitude,
  viewportGraphHeight,
}

function linkEnds(link: SimLink) {
  const sourceID = typeof link.source === 'object' ? link.source.id : String(link.source)
  const targetID = typeof link.target === 'object' ? link.target.id : String(link.target)
  const sourceKind = typeof link.source === 'object' ? link.source.kind : ''
  const targetKind = typeof link.target === 'object' ? link.target.kind : ''
  return { sourceID, targetID, sourceKind, targetKind }
}

function applyGraphHighlight(
  svgElement: SVGSVGElement,
  activeNodeID: string,
  highlightedKind: string,
  labelThreshold: number,
  colorMode: GraphColorMode,
  kindColorOverrides?: Readonly<Record<string, GraphPaletteKey>>,
) {
  const active = activeNodeID.trim()
  const kind = active ? '' : highlightedKind.trim()
  const neighborIDs = new Set<string>()
  if (active) neighborIDs.add(active)

  const links = select(svgElement).selectAll<SVGLineElement, SimLink>('g.links line')
  if (active) {
    links.each((link) => {
      const { sourceID, targetID } = linkEnds(link)
      if (sourceID === active) neighborIDs.add(targetID)
      if (targetID === active) neighborIDs.add(sourceID)
    })
  }

  const linkRelated = (link: SimLink) => {
    const ends = linkEnds(link)
    if (active) return ends.sourceID === active || ends.targetID === active
    if (kind) return ends.sourceKind === kind || ends.targetKind === kind
    return false
  }

  links
    .attr('stroke', (link) => {
      if (!active && !kind) return '#6b7c90'
      return linkRelated(link) ? '#7af0d8' : '#2d3a48'
    })
    .attr('stroke-opacity', (link) => {
      if (!active && !kind) return 0.28
      return linkRelated(link) ? 1 : 0.05
    })
    .attr('stroke-width', (link) => {
      const base = edgeVisualWidth(link.kind, linkDegree(link.source), linkDegree(link.target))
      if (!active && !kind) return base
      return linkRelated(link) ? Math.max(2.4, base + 1.8) : Math.max(0.4, base * 0.45)
    })

  const nodeGroups = select(svgElement).selectAll<SVGGElement, SimNode>('g.nodes g')
  const emphasized = (node: SimNode) => {
    if (active) return neighborIDs.has(node.id)
    if (kind) return node.kind === kind
    return true
  }
  const kindCount = kind ? nodeGroups.data().filter((node) => node.kind === kind).length : 0

  nodeGroups
    .attr('opacity', (node) => emphasized(node) ? 1 : 0.12)
    .classed('is-active', (node) => Boolean(active) && node.id === active)
    .classed('is-kind-highlight', (node) => Boolean(kind) && node.kind === kind)
    .classed('is-neighbor', (node) => Boolean(active) && neighborIDs.has(node.id) && node.id !== active)

  nodeGroups.select<SVGCircleElement>('.node-dot')
    .attr('stroke', (node) => (active && node.id === active) || (kind && node.kind === kind) ? '#ffffff' : paintNode(node, active, colorMode, kindColorOverrides).stroke)
    .attr('stroke-width', (node) => (active && node.id === active) || (kind && node.kind === kind) ? 2.4 : emphasized(node) && (active || kind) ? 1.8 : 1)
    .attr('filter', (node) => emphasized(node) && (active || kind) ? 'url(#graph-node-glow)' : null)

  nodeGroups.select<SVGTextElement>('.node-label')
    .attr('opacity', (node) => {
      if (active) {
        if (node.id === active) return 1
        if (neighborIDs.size <= 24 && neighborIDs.has(node.id)) return 1
        return 0
      }
      if (kind) {
        if (node.kind !== kind) return 0
        return kindCount <= 36 || node.degree >= labelThreshold ? 1 : 0
      }
      return node.degree >= labelThreshold ? 0.92 : 0
    })

  nodeGroups.select<SVGTextElement>('.node-kind')
    .attr('opacity', (node) => {
      if (active) return node.id === active ? 0.9 : 0
      if (kind) return node.kind === kind && kindCount <= 12 ? 0.9 : 0
      return 0
    })
}

function legendPillStyle(colors: { fill: string; stroke: string }) {
  return {
    backgroundColor: `${colors.fill}e6`,
    borderColor: `${colors.stroke}66`,
    boxShadow: `inset 0 0 0 1px ${colors.stroke}22`,
  } as const
}

function LegendColorPicker({
  kind,
  activeKey,
  onChange,
}: {
  kind: string
  activeKey: GraphPaletteKey
  onChange: (kind: string, colorKey: GraphPaletteKey) => void
}) {
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLDivElement>(null)
  const label = displayType(kind)
  const active = graphPaletteColor(activeKey)

  useEffect(() => {
    if (!open) return
    function close(event: MouseEvent) {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', close)
    return () => document.removeEventListener('mousedown', close)
  }, [open])

  return (
    <div className="relative" ref={rootRef}>
      <button
        aria-expanded={open}
        aria-haspopup="listbox"
        aria-label={`Change color for ${label}`}
        className="size-2.5 shrink-0 rounded-full ring-1 ring-white/20 transition hover:ring-white/40"
        onClick={() => setOpen((current) => !current)}
        style={{ background: active.stroke }}
        type="button"
      />
      {open && (
        <div
          aria-label={`Colors for ${label}`}
          className={`${menuSurfaceClass} absolute left-0 top-full z-20 mt-1 grid w-36 grid-cols-4 gap-1.5 p-2`}
          role="listbox"
        >
          {graphTypePalette.map((entry) => (
            <button
              aria-label={entry.label}
              aria-selected={entry.id === activeKey}
              className="size-5 rounded-full ring-1 ring-white/15 transition hover:scale-110 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-steward-teal"
              key={entry.id}
              onClick={() => {
                onChange(kind, entry.id)
                setOpen(false)
              }}
              role="option"
              style={{ background: entry.stroke }}
              type="button"
            />
          ))}
        </div>
      )}
    </div>
  )
}

function GraphSlider({
  id,
  max,
  min,
  name,
  onChange,
  step,
  value,
  valueLabel,
}: {
  id: string
  max: number
  min: number
  name: string
  onChange: (value: number) => void
  step: number
  value: number
  valueLabel: string
}) {
  return (
    <div className="min-w-30 flex-1">
      <label className="flex items-center justify-between gap-2 text-xs font-medium text-steward-mist" htmlFor={id}>
        <span>{name}</span>
        <span className="tabular-nums text-steward-mist-muted">{valueLabel}</span>
      </label>
      <input
        aria-valuemax={max}
        aria-valuemin={min}
        aria-valuenow={value}
        className="mt-1 w-full accent-steward-teal"
        id={id}
        max={max}
        min={min}
        onChange={(event) => onChange(Number(event.target.value))}
        step={step}
        type="range"
        value={value}
      />
    </div>
  )
}

function ExpandIcon() {
  return (
    <svg aria-hidden="true" className="size-4" fill="none" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="1.8" viewBox="0 0 24 24">
      <path d="M8 4H4v4M16 4h4v4M8 20H4v-4M16 20h4v-4" />
    </svg>
  )
}

function CollapseIcon() {
  return (
    <svg aria-hidden="true" className="size-4" fill="none" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="1.8" viewBox="0 0 24 24">
      <path d="M9 4v5H4M15 4v5h5M9 20v-5H4M15 20v-5h5" />
    </svg>
  )
}

function visibleIDs(nodes: readonly GraphNode[], edges: readonly GraphEdge[], focusNodeID: string) {
  if (!focusNodeID) return new Set(nodes.map((node) => node.id))
  const ids = new Set([focusNodeID])
  for (const edge of edges) {
    if (edge.from === focusNodeID) ids.add(edge.to)
    if (edge.to === focusNodeID) ids.add(edge.from)
  }
  return ids
}

function degrees(nodes: readonly GraphNode[], edges: readonly GraphEdge[]) {
  const degreeByID = new Map<string, number>()
  for (const node of nodes) degreeByID.set(node.id, 0)
  for (const edge of edges) {
    degreeByID.set(edge.from, (degreeByID.get(edge.from) ?? 0) + 1)
    degreeByID.set(edge.to, (degreeByID.get(edge.to) ?? 0) + 1)
  }
  return degreeByID
}

function linkDegree(end: SimNode | string | number) {
  return typeof end === 'object' ? end.degree : 1
}

function paintNode(
  simNode: SimNode,
  selectedNodeID: string,
  colorMode: GraphColorMode,
  kindColorOverrides?: Readonly<Record<string, GraphPaletteKey>>,
) {
  const colors = colorsForNode(simNode.kind, simNode.attributes, colorMode, kindColorOverrides)
  return {
    fill: colors.stroke,
    stroke: simNode.id === selectedNodeID ? '#ffffff' : simNode.degree === 0 ? '#f0b429' : '#071018',
    width: simNode.id === selectedNodeID ? 2.4 : 1.15,
    dash: simNode.degree === 0 ? '3 3' : null,
  }
}

function topologyKey(nodes: readonly GraphNode[], edges: readonly GraphEdge[], focusNodeID: string, width: number, height: number) {
  return [
    focusNodeID,
    `${width}x${height}`,
    nodes.map((node) => node.id).join('\n'),
    edges.map((edge) => `${edge.id}\t${edge.from}\t${edge.to}`).join('\n'),
  ].join('\n---\n')
}

function neighborhood(nodeID: string, links: readonly SimLink[]) {
  const ids = new Set<string>()
  if (!nodeID) return ids
  ids.add(nodeID)
  for (const link of links) {
    const sourceID = typeof link.source === 'object' ? link.source.id : String(link.source)
    const targetID = typeof link.target === 'object' ? link.target.id : String(link.target)
    if (sourceID === nodeID) ids.add(targetID)
    if (targetID === nodeID) ids.add(sourceID)
  }
  return ids
}

export default function InteractiveRelationshipGraph({
  nodes,
  edges,
  selectedNodeID,
  focusNodeID,
  onSelectNode,
  colorMode = 'type',
  kindColorOverrides,
  onKindColorChange,
  groupedKinds = emptyGroupedKinds,
  inspector,
  onHideKind,
  hiddenKinds,
  onShowKind,
}: InteractiveRelationshipGraphProps) {
  const svgRef = useRef<SVGSVGElement>(null)
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const rootRef = useRef<HTMLDivElement>(null)
  const canvasWrapRef = useRef<HTMLDivElement>(null)
  const fullscreenButtonRef = useRef<HTMLButtonElement>(null)
  const exitButtonRef = useRef<HTMLButtonElement>(null)
  const openerRef = useRef<HTMLElement | null>(null)
  const simulationClientRef = useRef<GraphSimulationHandle | null>(null)
  const transformRef = useRef<ZoomTransform>(zoomIdentity)
  const zoomBehaviorRef = useRef<ZoomBehavior<Element, unknown> | null>(null)
  const zoomTargetRef = useRef<Element | null>(null)
  const positionsRef = useRef(new Map<string, NodePosition>())
  const viewNodesRef = useRef<SimNode[]>([])
  const hoveredNodeIDRef = useRef('')
  const labelThresholdRef = useRef(0)
  const redrawRef = useRef(() => {})
  const onSelectRef = useRef(onSelectNode)
  const colorModeRef = useRef(colorMode)
  const kindColorOverridesRef = useRef(kindColorOverrides)
  const selectedRef = useRef(selectedNodeID)
  const highlightedKindRef = useRef('')
  const gravityRef = useRef(100)
  const repelRef = useRef(420)
  const spacingRef = useRef(180)
  const groupedRef = useRef(new Set(groupedKinds))
  const nodesRef = useRef(nodes)
  const edgesRef = useRef(edges)
  const layoutReadyRef = useRef(false)
  const desiredHeight = viewportGraphHeight(nodes.length)
  const [size, setSize] = useState(() => ({ width: 960, height: desiredHeight }))
  const [zoomLevel, setZoomLevel] = useState(1)
  const [gravity, setGravity] = useState(100)
  const [repel, setRepel] = useState(420)
  const [spacing, setSpacing] = useState(180)
  const [highlightedKind, setHighlightedKind] = useState('')
  const [fullscreen, setFullscreen] = useState(false)

  onSelectRef.current = onSelectNode
  colorModeRef.current = colorMode
  kindColorOverridesRef.current = kindColorOverrides
  selectedRef.current = selectedNodeID
  highlightedKindRef.current = highlightedKind
  gravityRef.current = gravity
  repelRef.current = repel
  spacingRef.current = spacing
  groupedRef.current = new Set(groupedKinds)
  nodesRef.current = nodes
  edgesRef.current = edges

  const groupedKey = groupedKinds.join('\n')
  const graphKey = topologyKey(nodes, edges, focusNodeID, size.width, size.height)
  const focusedIDs = useMemo(() => visibleIDs(nodes, edges, focusNodeID), [edges, focusNodeID, nodes])
  const legend = useMemo(() => {
    const plotted = nodes.filter((node) => focusedIDs.has(node.id))
    const counts = new Map<string, number>()
    for (const node of plotted) counts.set(node.kind, (counts.get(node.kind) ?? 0) + 1)
    return [...counts.entries()]
      .sort((left, right) => displayType(left[0]).localeCompare(displayType(right[0])))
      .map(([kind, count]) => ({
        kind,
        count,
        colors: colorsForNode(kind, plotted.find((node) => node.kind === kind)?.attributes, colorMode, kindColorOverrides),
      }))
  }, [colorMode, focusedIDs, kindColorOverrides, nodes])

  useEffect(() => {
    const element = canvasWrapRef.current
    if (!element || typeof ResizeObserver === 'undefined') return
    const observer = new ResizeObserver((entries) => {
      const entry = entries[0]
      if (!entry) return
      const width = Math.max(320, Math.floor(entry.contentRect.width) || 960)
      const height = Math.max(320, Math.floor(entry.contentRect.height) || desiredHeight)
      setSize((current) => (current.width === width && current.height === height ? current : { width, height }))
    })
    observer.observe(element)
    return () => observer.disconnect()
  }, [desiredHeight, fullscreen])

  const closeFullscreen = useCallback(() => {
    setFullscreen(false)
    queueMicrotask(() => (openerRef.current ?? fullscreenButtonRef.current)?.focus())
  }, [])

  useEffect(() => {
    if (!fullscreen) return undefined
    openerRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
    const releaseScroll = lockScroll()
    queueMicrotask(() => exitButtonRef.current?.focus())
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        event.preventDefault()
        closeFullscreen()
        return
      }
      if (event.key !== 'Tab') return
      const focusable = Array.from(rootRef.current?.querySelectorAll<HTMLElement>('a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])') ?? [])
      if (focusable.length === 0) return
      const first = focusable[0]
      const last = focusable[focusable.length - 1]
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault()
        last.focus()
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault()
        first.focus()
      }
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => {
      releaseScroll()
      window.removeEventListener('keydown', handleKeyDown)
    }
  }, [closeFullscreen, fullscreen])

  useEffect(() => {
    const currentNodes = nodesRef.current
    const currentEdges = edgesRef.current
    if (currentNodes.length === 0) return

    const ids = visibleIDs(currentNodes, currentEdges, focusNodeID)
    const degreeByID = degrees(currentNodes, currentEdges)
    const visibleNodes = currentNodes.filter((node) => ids.has(node.id))
    const layout = graphLayoutProfile(visibleNodes.length)
    const maxDegree = visibleNodes.reduce((highest, node) => Math.max(highest, degreeByID.get(node.id) ?? 0), 1)
    const simNodes: SimNode[] = visibleNodes.map((node, index) => {
      const prior = positionsRef.current.get(node.id)
      const degree = degreeByID.get(node.id) ?? 0
      const seeded = initialSimNodeRadius(index, visibleNodes.length, degree, maxDegree, size.width, size.height, layout.dense)
      return {
        ...node,
        index,
        degree,
        x: prior?.x ?? seeded.x,
        y: prior?.y ?? seeded.y,
        fx: prior?.fx ?? null,
        fy: prior?.fy ?? null,
      }
    })
    const nodeByID = new Map(simNodes.map((node) => [node.id, node]))
    const simLinks: SimLink[] = currentEdges.flatMap((edge) => {
      const source = nodeByID.get(edge.from)
      const target = nodeByID.get(edge.to)
      if (!source || !target) return []
      return [{ id: edge.id, kind: edge.kind, source, target }]
    })
    const workerLinks = simLinks.flatMap((link) => {
      const source = typeof link.source === 'object' ? link.source.index : undefined
      const target = typeof link.target === 'object' ? link.target.index : undefined
      if (source == null || target == null) return []
      return [{ id: link.id, kind: link.kind, source, target }]
    })
    if (simNodes.length === 0) return

    viewNodesRef.current = simNodes
    const labelThreshold = labelDegreeThreshold(simNodes.map((node) => node.degree))
    labelThresholdRef.current = labelThreshold
    hoveredNodeIDRef.current = ''

    const persistGraphPositions = () => {
      for (const node of simNodes) {
        positionsRef.current.set(node.id, { x: node.x, y: node.y, fx: node.fx, fy: node.fy })
      }
    }

    const canvasElement = canvasRef.current
    const canvasContext = graphRenderSurface(simNodes.length) === 'canvas' && canvasElement
      ? configureGraphCanvas(canvasElement, size.width, size.height)
      : null
    if (canvasContext && canvasElement) {
      zoomTargetRef.current = canvasElement
      let frame = 0
      const paintCanvas = () => {
        drawGraphCanvas(canvasContext, simNodes, simLinks, size.width, size.height, transformRef.current, {
          selectedNodeID: selectedRef.current,
          hoveredNodeID: hoveredNodeIDRef.current,
          highlightedKind: highlightedKindRef.current,
          neighborIDs: neighborhood(hoveredNodeIDRef.current || (highlightedKindRef.current ? '' : selectedRef.current), simLinks),
          labelThreshold,
        }, colorModeRef.current, kindColorOverridesRef.current)
      }
      const requestCanvasPaint = () => {
        if (frame) return
        frame = requestAnimationFrame(() => {
          frame = 0
          paintCanvas()
        })
      }
      redrawRef.current = requestCanvasPaint
      let dragIndex = -1
      let dragMoved = false
      const worldPoint = (event: { clientX: number; clientY: number }) => {
        const rect = canvasElement.getBoundingClientRect()
        const point = transformRef.current.invert([event.clientX - rect.left, event.clientY - rect.top])
        return { x: point[0], y: point[1] }
      }
      const setHover = (nodeID: string, cursor: string, title: string) => {
        canvasElement.style.cursor = cursor
        canvasElement.title = title
        if (hoveredNodeIDRef.current === nodeID) return
        hoveredNodeIDRef.current = nodeID
        requestCanvasPaint()
      }
      const onPointerDown = (event: PointerEvent) => {
        const { x, y } = worldPoint(event)
        const index = hitTestNodeIndex(simNodes, x, y)
        if (index < 0) return
        event.stopPropagation()
        dragIndex = index
        dragMoved = false
        canvasElement.setPointerCapture(event.pointerId)
        simulationClientRef.current?.dragStart(index)
        const node = simNodes[index]
        setHover(node.id, 'grabbing', `${node.label} (${displayType(node.kind)})`)
      }
      const onPointerMove = (event: PointerEvent) => {
        const { x, y } = worldPoint(event)
        if (dragIndex < 0) {
          const index = hitTestNodeIndex(simNodes, x, y)
          if (index < 0) {
            setHover('', 'default', '')
            return
          }
          const node = simNodes[index]
          setHover(node.id, 'grab', `${node.label} (${displayType(node.kind)})`)
          return
        }
        dragMoved = true
        const node = simNodes[dragIndex]
        node.x = x
        node.y = y
        node.fx = x
        node.fy = y
        simulationClientRef.current?.drag(dragIndex, x, y)
        requestCanvasPaint()
      }
      const onPointerUp = (event: PointerEvent) => {
        if (dragIndex < 0) return
        const { x, y } = worldPoint(event)
        const node = simNodes[dragIndex]
        simulationClientRef.current?.dragEnd(dragIndex, x, y)
        positionsRef.current.set(node.id, { x, y, fx: x, fy: y })
        if (!dragMoved) {
          setHighlightedKind('')
          onSelectRef.current(node.id)
        }
        dragIndex = -1
        canvasElement.style.cursor = 'grab'
      }
      const onPointerLeave = () => {
        if (dragIndex >= 0) return
        setHover('', 'default', '')
      }
      canvasElement.addEventListener('pointerdown', onPointerDown)
      canvasElement.addEventListener('pointermove', onPointerMove)
      canvasElement.addEventListener('pointerup', onPointerUp)
      canvasElement.addEventListener('pointerleave', onPointerLeave)
      const zoomBehavior = zoom<HTMLCanvasElement, unknown>()
        .scaleExtent([0.2, 3])
        .filter((event) => {
          if (event.type === 'wheel' || event.type === 'touchstart' || event.type === 'touchmove') return true
          if (event.type === 'mousedown' || event.type === 'pointerdown') {
            const { x, y } = worldPoint(event)
            return hitTestNodeIndex(simNodes, x, y) < 0
          }
          return false
        })
        .on('zoom', (event) => {
          transformRef.current = event.transform
          requestCanvasPaint()
          const next = Number(event.transform.k.toFixed(2))
          setZoomLevel((current) => (current === next ? current : next))
        })
      zoomBehaviorRef.current = zoomBehavior as unknown as ZoomBehavior<Element, unknown>
      select(canvasElement).call(zoomBehavior as never)
      select(canvasElement).call(zoomBehavior.transform as never, transformRef.current)
      paintCanvas()
      const simulation = startGraphSimulation({
        nodes: simNodes,
        links: workerLinks,
        width: size.width,
        height: size.height,
        layout,
        maxDegree,
        gravity: gravityRef.current,
        repel: repelRef.current,
        spacing: spacingRef.current,
        groupedKinds: [...groupedRef.current],
        onTick(positions) {
          applyPositions(simNodes, positions)
          requestCanvasPaint()
          if (layout.savePositionsEveryTick) persistGraphPositions()
        },
        onEnd(positions) {
          applyPositions(simNodes, positions)
          paintCanvas()
          persistGraphPositions()
        },
      })
      simulationClientRef.current = simulation
      return () => {
        simulation.stop()
        simulationClientRef.current = null
        if (frame) cancelAnimationFrame(frame)
        canvasElement.removeEventListener('pointerdown', onPointerDown)
        canvasElement.removeEventListener('pointermove', onPointerMove)
        canvasElement.removeEventListener('pointerup', onPointerUp)
        canvasElement.removeEventListener('pointerleave', onPointerLeave)
        zoomBehaviorRef.current = null
        zoomTargetRef.current = null
      }
    }

    const svgElement = svgRef.current
    if (!svgElement) return
    zoomTargetRef.current = svgElement
    const svg = select(svgElement)
    svg.selectAll('*').remove()
    const glow = svg.append('defs').append('filter')
      .attr('id', 'graph-node-glow')
      .attr('x', '-60%')
      .attr('y', '-60%')
      .attr('width', '220%')
      .attr('height', '220%')
    glow.append('feGaussianBlur').attr('in', 'SourceGraphic').attr('stdDeviation', '2.6').attr('result', 'blur')
    const merge = glow.append('feMerge')
    merge.append('feMergeNode').attr('in', 'blur')
    merge.append('feMergeNode').attr('in', 'SourceGraphic')

    const root = svg.append('g').attr('class', 'graph-root')
    root.attr('transform', transformRef.current.toString())

    const linkLayer = root.append('g').attr('class', 'links')
    const nodeLayer = root.append('g').attr('class', 'nodes')

    const linkSelection = linkLayer
      .selectAll('line')
      .data(simLinks, (link) => (link as SimLink).id)
      .join('line')
      .attr('stroke', '#6b7c90')
      .attr('stroke-opacity', 0.28)
      .attr('stroke-linecap', 'round')
      .attr('stroke-width', (link) => edgeVisualWidth((link as SimLink).kind, linkDegree((link as SimLink).source), linkDegree((link as SimLink).target)))

    const nodeSelection = nodeLayer
      .selectAll('g')
      .data(simNodes, (node) => (node as SimNode).id)
      .join('g')
      .attr('cursor', 'grab')
      .attr('data-node-id', (node) => (node as SimNode).id)

    nodeSelection.append('circle')
      .attr('class', 'node-hit')
      .attr('r', (node) => Math.max(14, nodeVisualRadius((node as SimNode).degree) + 8))
      .attr('fill', 'transparent')
      .attr('stroke', 'none')

    nodeSelection.append('circle')
      .attr('class', 'node-dot')
      .attr('r', (node) => nodeVisualRadius((node as SimNode).degree))
      .attr('fill', (node) => paintNode(node as SimNode, selectedRef.current, colorModeRef.current, kindColorOverridesRef.current).fill)
      .attr('stroke', (node) => paintNode(node as SimNode, selectedRef.current, colorModeRef.current, kindColorOverridesRef.current).stroke)
      .attr('stroke-width', (node) => paintNode(node as SimNode, selectedRef.current, colorModeRef.current, kindColorOverridesRef.current).width)
      .attr('stroke-dasharray', (node) => paintNode(node as SimNode, selectedRef.current, colorModeRef.current, kindColorOverridesRef.current).dash)

    nodeSelection.append('title').text((node) => `${(node as SimNode).label} (${displayType((node as SimNode).kind)})`)

    nodeSelection.append('text')
      .attr('class', 'node-label')
      .attr('text-anchor', 'middle')
      .attr('y', (node) => nodeVisualRadius((node as SimNode).degree) + 14)
      .attr('fill', '#e7edf3')
      .attr('font-size', 11)
      .attr('paint-order', 'stroke')
      .attr('stroke', '#071018')
      .attr('stroke-width', 3)
      .attr('pointer-events', 'none')
      .attr('opacity', (node) => (node as SimNode).degree >= labelThreshold ? 0.92 : 0)
      .text((node) => truncateLabel((node as SimNode).label))

    nodeSelection.append('text')
      .attr('class', 'node-kind')
      .attr('text-anchor', 'middle')
      .attr('y', (node) => nodeVisualRadius((node as SimNode).degree) + 28)
      .attr('fill', '#8aa0b5')
      .attr('font-size', 9)
      .attr('paint-order', 'stroke')
      .attr('stroke', '#071018')
      .attr('stroke-width', 3)
      .attr('pointer-events', 'none')
      .attr('opacity', 0)
      .text((node) => displayType((node as SimNode).kind))

    const renderGraphPositions = () => {
      linkSelection
        .attr('x1', (link) => (typeof link.source === 'object' ? link.source.x : 0))
        .attr('y1', (link) => (typeof link.source === 'object' ? link.source.y : 0))
        .attr('x2', (link) => (typeof link.target === 'object' ? link.target.x : 0))
        .attr('y2', (link) => (typeof link.target === 'object' ? link.target.y : 0))
      nodeSelection.attr('transform', (node) => `translate(${node.x},${node.y})`)
    }

    const dragBehavior = drag<SVGGElement, SimNode>()
      .on('start', (_event, node) => {
        const index = node.index ?? simNodes.indexOf(node)
        simulationClientRef.current?.dragStart(index)
        node.fx = node.x
        node.fy = node.y
      })
      .on('drag', (event, node) => {
        const index = node.index ?? simNodes.indexOf(node)
        node.fx = event.x
        node.fy = event.y
        node.x = event.x
        node.y = event.y
        simulationClientRef.current?.drag(index, event.x, event.y)
        renderGraphPositions()
      })
      .on('end', (event, node) => {
        const index = node.index ?? simNodes.indexOf(node)
        node.fx = event.x
        node.fy = event.y
        node.x = event.x
        node.y = event.y
        simulationClientRef.current?.dragEnd(index, event.x, event.y)
        positionsRef.current.set(node.id, { x: node.x, y: node.y, fx: node.fx, fy: node.fy })
      })

    nodeSelection.call(dragBehavior as never)

    nodeSelection.on('click', (event, node) => {
      event.stopPropagation()
      setHighlightedKind('')
      onSelectRef.current(node.id)
    })
    nodeSelection
      .on('mouseenter', (_event, node) => applyGraphHighlight(svgElement, node.id, highlightedKindRef.current, labelThreshold, colorModeRef.current, kindColorOverridesRef.current))
      .on('mouseleave', () => applyGraphHighlight(svgElement, highlightedKindRef.current ? '' : selectedRef.current, highlightedKindRef.current, labelThreshold, colorModeRef.current, kindColorOverridesRef.current))

    redrawRef.current = () => applyGraphHighlight(svgElement, highlightedKindRef.current ? '' : selectedRef.current, highlightedKindRef.current, labelThreshold, colorModeRef.current, kindColorOverridesRef.current)

    const zoomBehavior = zoom<SVGSVGElement, unknown>()
      .scaleExtent([0.2, 3])
      .filter((event) => {
        if (event.type === 'wheel' || event.type === 'touchstart' || event.type === 'touchmove') return true
        return event.target === svgElement
      })
      .on('zoom', (event) => {
        transformRef.current = event.transform
        root.attr('transform', event.transform.toString())
        const next = Number(event.transform.k.toFixed(2))
        setZoomLevel((current) => (current === next ? current : next))
      })
    zoomBehaviorRef.current = zoomBehavior as unknown as ZoomBehavior<Element, unknown>
    svg.call(zoomBehavior as never)
    svg.call(zoomBehavior.transform as never, transformRef.current)

    let tickCount = 0
    const simulation = startGraphSimulation({
      nodes: simNodes,
      links: workerLinks,
      width: size.width,
      height: size.height,
      layout,
      maxDegree,
      gravity: gravityRef.current,
      repel: repelRef.current,
      spacing: spacingRef.current,
      groupedKinds: [...groupedRef.current],
      onTick(positions) {
        applyPositions(simNodes, positions)
        tickCount += 1
        if (layout.tickSkip > 1 && tickCount % layout.tickSkip !== 0) return
        renderGraphPositions()
        if (layout.savePositionsEveryTick) persistGraphPositions()
      },
      onEnd(positions) {
        applyPositions(simNodes, positions)
        renderGraphPositions()
        persistGraphPositions()
      },
    })
    simulationClientRef.current = simulation
    applyGraphHighlight(svgElement, highlightedKindRef.current ? '' : selectedRef.current, highlightedKindRef.current, labelThreshold, colorModeRef.current, kindColorOverridesRef.current)

    return () => {
      simulation.stop()
      simulationClientRef.current = null
      zoomBehaviorRef.current = null
      zoomTargetRef.current = null
    }
  }, [focusNodeID, graphKey, size.height, size.width])

  useEffect(() => {
    if (!layoutReadyRef.current) {
      layoutReadyRef.current = true
      return
    }
    const profile = graphLayoutProfile(viewNodesRef.current.length || nodesRef.current.length)
    simulationClientRef.current?.tune(gravity, repel, spacing, [...groupedRef.current], profile.alphaRetune)
  }, [gravity, groupedKey, repel, spacing])

  useEffect(() => {
    if (canvasRef.current && graphRenderSurface(viewNodesRef.current.length) === 'canvas') {
      redrawRef.current()
      return
    }
    const svgElement = svgRef.current
    if (!svgElement) return
    const renderedNodes = select(svgElement).selectAll<SVGGElement, SimNode>('g.nodes g').data()
    const threshold = labelDegreeThreshold(renderedNodes.map((node) => node.degree))
    select(svgElement)
      .selectAll<SVGCircleElement, SimNode>('g.nodes g circle.node-dot')
      .attr('fill', (node) => paintNode(node, selectedNodeID, colorMode, kindColorOverrides).fill)
      .attr('stroke', (node) => paintNode(node, selectedNodeID, colorMode, kindColorOverrides).stroke)
      .attr('stroke-width', (node) => paintNode(node, selectedNodeID, colorMode, kindColorOverrides).width)
      .attr('stroke-dasharray', (node) => paintNode(node, selectedNodeID, colorMode, kindColorOverrides).dash)
    applyGraphHighlight(svgElement, highlightedKind ? '' : selectedNodeID, highlightedKind, threshold, colorMode, kindColorOverrides)
  }, [colorMode, highlightedKind, kindColorOverrides, selectedNodeID])

  useEffect(() => {
    const latest = new Map(nodes.map((node) => [node.id, node]))
    for (const simNode of viewNodesRef.current) {
      const node = latest.get(simNode.id)
      if (!node) continue
      simNode.label = node.label
      simNode.kind = node.kind
      simNode.attributes = node.attributes
    }
    const svgElement = svgRef.current
    if (svgElement) {
      select(svgElement)
        .selectAll<SVGGElement, SimNode>('g.nodes g')
        .each(function updateLabels(simNode) {
          const node = latest.get(simNode.id)
          if (!node) return
          const group = select(this)
          group.select('.node-label').text(truncateLabel(node.label))
          group.select('.node-kind').text(displayType(node.kind))
        })
    }
    if (canvasRef.current && graphRenderSurface(viewNodesRef.current.length) === 'canvas') redrawRef.current()
  }, [nodes])

  function applyZoom(next: number) {
    const target = zoomTargetRef.current
    const behavior = zoomBehaviorRef.current
    if (!target || !behavior) {
      setZoomLevel(next)
      return
    }
    const current = transformRef.current
    select(target).call(behavior.transform as never, zoomIdentity.translate(current.x, current.y).scale(next))
  }

  if (nodes.length === 0) {
    if (!hiddenKinds?.length) return null
    return (
      <div className="flex min-w-0 flex-col gap-3">
        <ul aria-label="Hidden record types" className="flex flex-wrap items-center gap-1.5">
          <li className="text-xs text-steward-mist-muted">Hidden</li>
          {hiddenKinds.map((item) => (
            <li key={item.kind}>
              <button
                className="inline-flex items-center gap-1.5 rounded-full border border-dashed border-white/15 px-2.5 py-1 text-xs text-steward-mist-muted transition hover:border-white/30 hover:text-steward-mist"
                onClick={() => onShowKind?.(item.kind)}
                type="button"
              >
                Show {item.label}
              </button>
            </li>
          ))}
        </ul>
        <p className="text-sm text-steward-mist-muted">All record types are hidden. Show a type to restore the graph.</p>
      </div>
    )
  }

  const denseLayout = isDenseGraph(focusedIDs.size)
  const legendCaption = colorMode === 'source' ? 'Colored by product' : colorMode === 'status' ? 'Colored by status' : 'Colored by record type'
  const statusText = [
    focusNodeID ? `Focused on one record and its ${focusedIDs.size - 1} direct connections.` : `Showing ${focusedIDs.size} records.`,
    denseLayout ? (isLargeGraph(focusedIDs.size)
      ? 'Very large graphs settle in time-sliced batches off the UI thread so the first layout appears faster.'
      : 'Large graph mode draws on canvas, runs layout off the UI thread, and uses a spatial hash for local repel.') : '',
  ].filter(Boolean).join(' ')

  const controls = (
    <div className="flex flex-wrap items-end gap-3">
      <div className="flex min-w-0 flex-1 flex-wrap items-end gap-3">
        <GraphSlider id="graph-zoom" max={3} min={0.2} name="Zoom" onChange={applyZoom} step={0.05} value={zoomLevel} valueLabel={`${zoomLevel.toFixed(2)}×`} />
        <GraphSlider id="graph-spacing" max={360} min={70} name="Spacing" onChange={setSpacing} step={5} value={spacing} valueLabel={String(spacing)} />
        <GraphSlider id="graph-gravity" max={600} min={0} name="Gravity" onChange={setGravity} step={10} value={gravity} valueLabel={String(gravity)} />
        <GraphSlider id="graph-repel" max={900} min={40} name="Repel" onChange={setRepel} step={10} value={repel} valueLabel={String(repel)} />
      </div>
      {fullscreen ? (
        <button className={`${secondaryButtonClass} min-h-10 shrink-0 px-3`} onClick={closeFullscreen} ref={exitButtonRef} type="button">
          <CollapseIcon /> Exit fullscreen
        </button>
      ) : (
        <button className={`${secondaryButtonClass} min-h-10 shrink-0 px-3`} onClick={() => setFullscreen(true)} ref={fullscreenButtonRef} type="button">
          <ExpandIcon /> Fullscreen
        </button>
      )}
    </div>
  )

  const legendList = legend.length > 0 ? (
    <ul aria-label="Record type legend" className="flex flex-wrap gap-1.5">
      <li className="sr-only">{legendCaption}. Select a type to highlight those records in the graph.</li>
      {legend.map((item) => {
        const label = colorMode === 'source'
          ? (sourceLabels[sourceForKind(item.kind)] ?? displayType(item.kind))
          : displayType(item.kind)
        const activeColorKey = kindColorOverrides?.[item.kind] ?? defaultKindColorKey(item.kind)
        const configurable = colorMode === 'type' && Boolean(onKindColorChange)
        const pressed = highlightedKind === item.kind
        return (
          <li key={item.kind}>
            <div
              className={cx(
                'relative inline-flex items-center gap-1.5 rounded-full border py-0.5 pl-1.5 pr-1 text-xs text-steward-mist',
                pressed && 'ring-2 ring-steward-teal ring-offset-1 ring-offset-steward-ink-950',
              )}
              style={legendPillStyle(item.colors)}
            >
              {configurable ? (
                <LegendColorPicker
                  activeKey={activeColorKey}
                  kind={item.kind}
                  onChange={onKindColorChange!}
                />
              ) : (
                <span aria-hidden="true" className="size-2.5 shrink-0 rounded-full ring-1 ring-white/15" style={{ background: item.colors.stroke }} />
              )}
              <button
                aria-label={`Highlight ${label} records`}
                aria-pressed={pressed}
                className="rounded-full px-1.5 py-0.5 text-left font-medium transition hover:text-white"
                onClick={() => setHighlightedKind((current) => current === item.kind ? '' : item.kind)}
                title="Highlight these records in the graph"
                type="button"
              >
                {label}
                <span className="ml-1.5 font-normal text-steward-mist-muted">{item.count}</span>
              </button>
              {onHideKind && (
                <button
                  aria-label={`Hide ${label} from the graph`}
                  className="rounded-full px-1.5 py-0.5 text-[11px] font-medium text-steward-mist-muted transition hover:text-steward-mist"
                  onClick={() => {
                    if (highlightedKind === item.kind) setHighlightedKind('')
                    onHideKind(item.kind)
                  }}
                  type="button"
                >Hide</button>
              )}
            </div>
          </li>
        )
      })}
    </ul>
  ) : null
  const hiddenList = hiddenKinds && hiddenKinds.length > 0 ? (
    <ul aria-label="Hidden record types" className="flex flex-wrap items-center gap-1.5">
      <li className="text-xs text-steward-mist-muted">Hidden</li>
      {hiddenKinds.map((item) => (
        <li key={item.kind}>
          <button
            className="inline-flex items-center gap-1.5 rounded-full border border-dashed border-white/15 px-2.5 py-1 text-xs text-steward-mist-muted transition hover:border-white/30 hover:text-steward-mist"
            onClick={() => onShowKind?.(item.kind)}
            type="button"
          >
            Show {item.label}
          </button>
        </li>
      ))}
    </ul>
  ) : null

  return (
    <div
      aria-label={fullscreen ? 'Fullscreen relationship graph' : undefined}
      aria-modal={fullscreen ? true : undefined}
      className={cx(
        'flex min-w-0 flex-col gap-3',
        fullscreen && 'fixed inset-0 z-60 bg-steward-ink-950 p-4 sm:p-5',
      )}
      ref={rootRef}
      role={fullscreen ? 'dialog' : undefined}
    >
      {fullscreen && (
        <div className="flex items-center justify-between gap-3">
          <p className="text-sm font-semibold text-steward-mist" id="graph-fullscreen-title">Relationship graph</p>
          <p className="sr-only">{statusText}</p>
        </div>
      )}
      {controls}
      {legendList}
      {hiddenList}
      {!fullscreen && <p className="sr-only">{statusText} Drag nodes to rearrange. Hover a node or select a type in the key to highlight those records.</p>}
      <div
        className={cx('relative min-h-0 overflow-hidden rounded-xl border border-white/[0.08] bg-steward-ink-950/40', fullscreen && 'flex-1')}
        ref={canvasWrapRef}
        style={fullscreen ? undefined : { height: `${desiredHeight}px` }}
      >
        {inspector && <div className="pointer-events-none absolute inset-0 z-10 flex justify-end p-3 sm:p-4"><div className="pointer-events-auto max-h-full">{inspector}</div></div>}
        {graphRenderSurface(focusedIDs.size) === 'canvas' ? (
          <canvas
            aria-label="Interactive relationship graph"
            className="h-full w-full touch-none"
            ref={canvasRef}
            role="img"
          />
        ) : (
          <svg
            aria-label="Interactive relationship graph"
            className="h-full w-full touch-none"
            ref={svgRef}
            role="img"
            viewBox={`0 0 ${size.width} ${size.height}`}
          />
        )}
      </div>
    </div>
  )
}
