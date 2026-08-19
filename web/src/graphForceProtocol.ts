import type { GraphLayoutProfile, LayoutLink, LayoutNode } from './graphLayout'

// Requirement: REQ-DIRECTORY-EXPANSION-008. Feature: threads.relationships.

export type GraphWorkerInit = {
  kinds: string[]
  xy: Float32Array
  pins: Float32Array
  degrees: Uint16Array
  kindIndex: Uint16Array
  linkEnds: Uint32Array
  width: number
  height: number
  layout: GraphLayoutProfile
  maxDegree: number
  gravity: number
  repel: number
  spacing: number
  groupedKinds: string[]
}

export type GraphWorkerIncoming =
  | { type: 'init'; payload: GraphWorkerInit }
  | { type: 'tune'; gravity: number; repel: number; spacing: number; groupedKinds: string[]; alpha: number }
  | { type: 'dragStart'; index: number }
  | { type: 'drag'; index: number; x: number; y: number }
  | { type: 'dragEnd'; index: number; x: number; y: number }
  | { type: 'stop' }

export type GraphWorkerOutgoing =
  | { type: 'tick'; positions: Float32Array }
  | { type: 'end'; positions: Float32Array }

export function packWorkerInit(input: {
  nodes: readonly LayoutNode[]
  links: readonly { source: number; target: number }[]
  width: number
  height: number
  layout: GraphLayoutProfile
  maxDegree: number
  gravity: number
  repel: number
  spacing: number
  groupedKinds: readonly string[]
}): GraphWorkerInit {
  const kinds: string[] = []
  const kindLookup = new Map<string, number>()
  const xy = new Float32Array(input.nodes.length * 2)
  const pins = new Float32Array(input.nodes.length * 2)
  const degrees = new Uint16Array(input.nodes.length)
  const kindIndex = new Uint16Array(input.nodes.length)
  for (let i = 0; i < input.nodes.length; i += 1) {
    const node = input.nodes[i]
    xy[i * 2] = node.x
    xy[i * 2 + 1] = node.y
    pins[i * 2] = node.fx == null ? Number.NaN : node.fx
    pins[i * 2 + 1] = node.fy == null ? Number.NaN : node.fy
    degrees[i] = Math.min(65535, Math.max(0, node.degree))
    let kind = kindLookup.get(node.kind)
    if (kind == null) {
      kind = kinds.length
      kindLookup.set(node.kind, kind)
      kinds.push(node.kind)
    }
    kindIndex[i] = kind
  }
  const linkEnds = new Uint32Array(input.links.length * 2)
  for (let i = 0; i < input.links.length; i += 1) {
    linkEnds[i * 2] = input.links[i].source
    linkEnds[i * 2 + 1] = input.links[i].target
  }
  return {
    kinds,
    xy,
    pins,
    degrees,
    kindIndex,
    linkEnds,
    width: input.width,
    height: input.height,
    layout: input.layout,
    maxDegree: input.maxDegree,
    gravity: input.gravity,
    repel: input.repel,
    spacing: input.spacing,
    groupedKinds: [...input.groupedKinds],
  }
}

export function workerInitTransfer(payload: GraphWorkerInit): Transferable[] {
  return [payload.xy.buffer, payload.pins.buffer, payload.degrees.buffer, payload.kindIndex.buffer, payload.linkEnds.buffer]
}

export function unpackWorkerGraph(payload: GraphWorkerInit): { nodes: LayoutNode[]; links: LayoutLink[] } {
  const count = payload.degrees.length
  const nodes: LayoutNode[] = new Array(count)
  for (let i = 0; i < count; i += 1) {
    const fx = payload.pins[i * 2]
    const fy = payload.pins[i * 2 + 1]
    nodes[i] = {
      id: String(i),
      kind: payload.kinds[payload.kindIndex[i]] ?? '',
      label: '',
      degree: payload.degrees[i],
      x: payload.xy[i * 2],
      y: payload.xy[i * 2 + 1],
      vx: 0,
      vy: 0,
      fx: Number.isFinite(fx) ? fx : null,
      fy: Number.isFinite(fy) ? fy : null,
      index: i,
    }
  }
  const edgeCount = Math.floor(payload.linkEnds.length / 2)
  const links: LayoutLink[] = new Array(edgeCount)
  for (let i = 0; i < edgeCount; i += 1) {
    const source = payload.linkEnds[i * 2]
    const target = payload.linkEnds[i * 2 + 1]
    links[i] = {
      id: String(i),
      kind: '',
      source: nodes[source] ?? source,
      target: nodes[target] ?? target,
    }
  }
  return { nodes, links }
}
