import { createGraphForceSimulation, type SimulationControls } from './graphForceSimulation'
import { runSteppedSimulation, type SimulationRunner } from './graphForceRunner'
import { snapshotPositions, type LayoutNode } from './graphLayout'
import { unpackWorkerGraph, type GraphWorkerIncoming, type GraphWorkerOutgoing } from './graphForceProtocol'

// Requirement: REQ-DIRECTORY-EXPANSION-008. Feature: threads.relationships.

let runner: SimulationRunner | null = null
let nodes: LayoutNode[] = []
const controls: SimulationControls = { gravity: 100, repel: 420, spacing: 180, groupedKinds: [] }
let active = false

function emit(type: 'tick' | 'end') {
  if (!active || nodes.length === 0) return
  const positions = snapshotPositions(nodes)
  ;(postMessage as (message: GraphWorkerOutgoing, transfer: Transferable[]) => void)({ type, positions }, [positions.buffer])
}

function reset() {
  runner?.stop()
  runner = null
  nodes = []
  active = false
}

self.onmessage = (event: MessageEvent<GraphWorkerIncoming>) => {
  const message = event.data
  if (message.type === 'stop') {
    reset()
    return
  }
  if (message.type === 'init') {
    reset()
    const payload = message.payload
    controls.gravity = payload.gravity
    controls.repel = payload.repel
    controls.spacing = payload.spacing
    controls.groupedKinds = payload.groupedKinds
    const unpacked = unpackWorkerGraph(payload)
    nodes = unpacked.nodes
    const simulation = createGraphForceSimulation({
      nodes,
      links: unpacked.links,
      width: payload.width,
      height: payload.height,
      layout: payload.layout,
      maxDegree: payload.maxDegree,
      controls,
    })
    active = true
    runner = runSteppedSimulation(simulation, payload.layout, () => emit('tick'), () => emit('end'))
    runner.restart(payload.layout.alphaStart)
    return
  }
  if (!runner || nodes.length === 0) return
  if (message.type === 'tune') {
    controls.gravity = message.gravity
    controls.repel = message.repel
    controls.spacing = message.spacing
    controls.groupedKinds = message.groupedKinds
    runner.restart(message.alpha)
    return
  }
  const node = nodes[message.index]
  if (!node) return
  if (message.type === 'dragStart') {
    node.fx = node.x
    node.fy = node.y
    runner.setAlphaTarget(0.18)
    return
  }
  if (message.type === 'drag') {
    node.fx = message.x
    node.fy = message.y
    node.x = message.x
    node.y = message.y
    return
  }
  node.fx = message.x
  node.fy = message.y
  node.x = message.x
  node.y = message.y
  runner.setAlphaTarget(0)
}
