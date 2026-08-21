import { createGraphForceSimulation, type SimulationControls } from './graphForceSimulation'
import { runSteppedSimulation } from './graphForceRunner'
import { packWorkerInit, workerInitTransfer, type GraphWorkerIncoming, type GraphWorkerOutgoing } from './graphForceProtocol'
import { snapshotPositions, type GraphLayoutProfile, type LayoutLink, type LayoutNode } from './graphLayout'

// Requirement: REQ-DIRECTORY-EXPANSION-008. Feature: threads.relationships.

export type GraphSimulationHandle = {
  tune: (gravity: number, repel: number, spacing: number, groupedKinds: readonly string[], alpha: number) => void
  dragStart: (index: number) => void
  drag: (index: number, x: number, y: number) => void
  dragEnd: (index: number, x: number, y: number) => void
  stop: () => void
}

export type GraphSimulationOptions = {
  nodes: LayoutNode[]
  links: { id: string; kind: string; source: number; target: number }[]
  width: number
  height: number
  layout: GraphLayoutProfile
  maxDegree: number
  gravity: number
  repel: number
  spacing: number
  groupedKinds: readonly string[]
  onTick: (positions: Float32Array) => void
  onEnd: (positions: Float32Array) => void
}

function workerURL() {
  return new URL('./graphForceWorker.ts', import.meta.url)
}

let pooledWorker: Worker | null = null

function tryCreateWorker() {
  try {
    if (typeof Worker === 'undefined') return null
    return new Worker(workerURL(), { type: 'module' })
  } catch {
    return null
  }
}

function acquireWorker() {
  if (pooledWorker) return pooledWorker
  pooledWorker = tryCreateWorker()
  return pooledWorker
}

function startWorkerSimulation(worker: Worker, options: GraphSimulationOptions): GraphSimulationHandle {
  const send = (message: GraphWorkerIncoming, transfer?: Transferable[]) => {
    if (transfer?.length) worker.postMessage(message, transfer)
    else worker.postMessage(message)
  }
  worker.onmessage = (event: MessageEvent<GraphWorkerOutgoing>) => {
    if (event.data.type === 'end') options.onEnd(event.data.positions)
    else options.onTick(event.data.positions)
  }
  worker.onerror = () => {
    if (pooledWorker === worker) pooledWorker = null
  }
  const payload = packWorkerInit({
    nodes: options.nodes,
    links: options.links,
    width: options.width,
    height: options.height,
    layout: options.layout,
    maxDegree: options.maxDegree,
    gravity: options.gravity,
    repel: options.repel,
    spacing: options.spacing,
    groupedKinds: options.groupedKinds,
  })
  send({ type: 'init', payload }, workerInitTransfer(payload))
  return {
    tune(gravity, repel, spacing, groupedKinds, alpha) {
      send({ type: 'tune', gravity, repel, spacing, groupedKinds: [...groupedKinds], alpha })
    },
    dragStart(index) {
      send({ type: 'dragStart', index })
    },
    drag(index, x, y) {
      send({ type: 'drag', index, x, y })
    },
    dragEnd(index, x, y) {
      send({ type: 'dragEnd', index, x, y })
    },
    stop() {
      send({ type: 'stop' })
      worker.onmessage = null
    },
  }
}

function startLocalSimulation(options: GraphSimulationOptions): GraphSimulationHandle {
  const controls: SimulationControls = {
    gravity: options.gravity,
    repel: options.repel,
    spacing: options.spacing,
    groupedKinds: [...options.groupedKinds],
  }
  const links: LayoutLink[] = options.links.map((link) => ({
    id: link.id,
    kind: link.kind,
    source: options.nodes[link.source] ?? link.source,
    target: options.nodes[link.target] ?? link.target,
  }))
  const simulation = createGraphForceSimulation({
    nodes: options.nodes,
    links,
    width: options.width,
    height: options.height,
    layout: options.layout,
    maxDegree: options.maxDegree,
    controls,
  })
  const runner = runSteppedSimulation(
    simulation,
    options.layout,
    () => options.onTick(snapshotPositions(options.nodes)),
    () => options.onEnd(snapshotPositions(options.nodes)),
  )
  runner.restart(options.layout.alphaStart)
  return {
    tune(gravity, repel, spacing, groupedKinds, alpha) {
      controls.gravity = gravity
      controls.repel = repel
      controls.spacing = spacing
      controls.groupedKinds = [...groupedKinds]
      runner.restart(alpha)
    },
    dragStart(index) {
      const node = options.nodes[index]
      if (!node) return
      node.fx = node.x
      node.fy = node.y
      runner.setAlphaTarget(0.18)
    },
    drag(index, x, y) {
      const node = options.nodes[index]
      if (!node) return
      node.fx = x
      node.fy = y
      node.x = x
      node.y = y
    },
    dragEnd(index, x, y) {
      const node = options.nodes[index]
      if (!node) return
      node.fx = x
      node.fy = y
      node.x = x
      node.y = y
      runner.setAlphaTarget(0)
    },
    stop() {
      runner.stop()
    },
  }
}

export function startGraphSimulation(options: GraphSimulationOptions): GraphSimulationHandle {
  const worker = acquireWorker()
  if (worker) return startWorkerSimulation(worker, options)
  return startLocalSimulation(options)
}
