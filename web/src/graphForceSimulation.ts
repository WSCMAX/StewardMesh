import { forceCenter, forceCollide, forceLink, forceManyBody, forceSimulation, forceX, forceY, type Force, type Simulation } from 'd3-force'
import {
  applyMassedRepel,
  assignLocalWellRegions,
  buildSpatialHash,
  chargeStrength,
  clusterFoci,
  collideRadius,
  forEachNearbyIndex,
  gravityStrength,
  hubRepelMagnitude,
  isGroupingHub,
  linkForces,
  localGravityStrength,
  nodeMass,
  packedPair,
  pairSeparation,
  selectLocalWellIndices,
  spokeRadius,
  unlinkedCellSize,
  unlinkedQueryRange,
  unlinkedRepelMagnitude,
  wellSeparation,
  type GraphLayoutProfile,
  type LayoutLink,
  type LayoutNode,
} from './graphLayout'

// Requirement: REQ-DIRECTORY-EXPANSION-008. Feature: threads.relationships.

export type SimulationControls = {
  gravity: number
  repel: number
  spacing: number
  groupedKinds: string[]
}

export type GraphForceInput = {
  nodes: LayoutNode[]
  links: LayoutLink[]
  width: number
  height: number
  layout: GraphLayoutProfile
  maxDegree: number
  controls: SimulationControls
}

function linkDegree(end: LayoutNode | string | number) {
  return typeof end === 'object' ? end.degree : 1
}

function nodeIndex(node: LayoutNode | string | number, indexByID: Map<string, number>) {
  if (typeof node === 'object') return node.index ?? indexByID.get(node.id)
  if (typeof node === 'number') return node
  return indexByID.get(node)
}

function globalGravityStrength(gravity: number, degree: number, scale: number, well: number, assigned: number) {
  if (well) return gravityStrength(gravity, degree) * scale
  if (assigned >= 0) return 0
  return gravityStrength(gravity, degree) * scale * 0.28
}

function createLocalGravityForce(
  links: readonly LayoutLink[],
  getGravity: () => number,
  getSpacing: () => number,
  getRepel: () => number,
  isWell: Uint8Array,
  wellOf: Int32Array,
): Force<LayoutNode, undefined> {
  let nodes: LayoutNode[] = []
  let wells: number[] = []
  let members = new Int32Array(0)
  let radii = new Float32Array(0)
  const force = ((alpha: number) => {
    if (nodes.length === 0 || wells.length === 0) return
    const gravity = localGravityStrength(getGravity()) * alpha
    const spacing = getSpacing()
    const wellPush = Math.max(0, getRepel()) * 0.016 * alpha
    for (let i = 0; i < wells.length; i += 1) {
      const left = nodes[wells[i]]
      for (let j = i + 1; j < wells.length; j += 1) {
        const right = nodes[wells[j]]
        const desired = wellSeparation(members[wells[i]] ?? 1, members[wells[j]] ?? 1, spacing)
        const { dx, dy, distance } = pairSeparation(left, right)
        if (distance >= desired) continue
        const overlap = (desired - distance) / desired
        applyMassedRepel(left, right, dx, dy, distance, wellPush * Math.sqrt(nodeMass(left.degree) * nodeMass(right.degree)) * overlap)
      }
    }
    if (gravity === 0) return
    for (let i = 0; i < nodes.length; i += 1) {
      const wellIndex = wellOf[i]
      if (wellIndex < 0 || wellIndex === i) continue
      const node = nodes[i]
      const well = nodes[wellIndex]
      if (!well) continue
      let dx = (node.x ?? 0) - (well.x ?? 0)
      let dy = (node.y ?? 0) - (well.y ?? 0)
      let distance = Math.hypot(dx, dy)
      if (distance < 0.01) {
        dx = (Math.random() - 0.5) * 0.02
        dy = (Math.random() - 0.5) * 0.02
        distance = Math.hypot(dx, dy)
      }
      const radius = radii[wellIndex] ?? spacing
      const unitX = dx / distance
      const unitY = dy / distance
      const shift = (radius - distance) * gravity
      const spin = (((i * 13) % 11) - 5) * 0.45 * alpha
      node.vx = (node.vx ?? 0) + unitX * shift - unitY * spin
      node.vy = (node.vy ?? 0) + unitY * shift + unitX * spin
    }
  }) as Force<LayoutNode, undefined>
  force.initialize = (next) => {
    nodes = next
    isWell.fill(0)
    wellOf.fill(-1)
    members = new Int32Array(nodes.length)
    radii = new Float32Array(nodes.length)
    const degrees = nodes.map((node) => node.degree)
    wells = selectLocalWellIndices(degrees)
    for (const index of wells) isWell[index] = 1
    const indexByID = new Map(nodes.map((node, index) => [node.id, index]))
    const neighbors: number[][] = nodes.map(() => [])
    for (const link of links) {
      const source = nodeIndex(link.source, indexByID)
      const target = nodeIndex(link.target, indexByID)
      if (source == null || target == null) continue
      neighbors[source].push(target)
      neighbors[target].push(source)
    }
    const regions = assignLocalWellRegions(neighbors, wells)
    wellOf.set(regions)
    for (let i = 0; i < nodes.length; i += 1) {
      const assigned = wellOf[i]
      if (assigned >= 0) members[assigned] += 1
    }
    for (const index of wells) radii[index] = spokeRadius(members[index] ?? 1, getSpacing())
  }
  return force
}

function createHubRepelForce(
  getRepel: () => number,
  getSpacing: () => number,
  maxDegree: number,
): Force<LayoutNode, undefined> {
  let hubs: LayoutNode[] = []
  const force = ((alpha: number) => {
    if (hubs.length < 2) return
    const repel = getRepel()
    const spacing = getSpacing()
    for (let i = 0; i < hubs.length; i += 1) {
      const left = hubs[i]
      for (let j = i + 1; j < hubs.length; j += 1) {
        const right = hubs[j]
        const { dx, dy, distance } = pairSeparation(left, right)
        applyMassedRepel(left, right, dx, dy, distance, hubRepelMagnitude(left.degree, right.degree, distance, repel, spacing, maxDegree) * alpha)
      }
    }
  }) as Force<LayoutNode, undefined>
  force.initialize = (next) => {
    hubs = next.filter((node) => isGroupingHub(node.degree, maxDegree))
  }
  return force
}

function createUnlinkedRepelForce(
  links: readonly LayoutLink[],
  getRepel: () => number,
  getSpacing: () => number,
  maxDegree: number,
): Force<LayoutNode, undefined> {
  let nodes: LayoutNode[] = []
  let connected = new Set<number>()
  const force = ((alpha: number) => {
    if (nodes.length < 2) return
    const repel = getRepel()
    const spacing = getSpacing()
    const hash = buildSpatialHash(nodes, unlinkedCellSize(spacing))
    const range = unlinkedQueryRange(spacing, maxDegree)
    for (let i = 0; i < nodes.length; i += 1) {
      const left = nodes[i]
      forEachNearbyIndex(hash, left.x ?? 0, left.y ?? 0, range, (j) => {
        if (j <= i) return
        const right = nodes[j]
        const { dx, dy, distance } = pairSeparation(left, right)
        applyMassedRepel(left, right, dx, dy, distance, unlinkedRepelMagnitude(connected.has(packedPair(i, j)), distance, spacing, repel, left.degree, right.degree) * alpha)
      })
    }
  }) as Force<LayoutNode, undefined>
  force.initialize = (next) => {
    nodes = next
    const indexByID = new Map<string, number>()
    for (let i = 0; i < nodes.length; i += 1) indexByID.set(nodes[i].id, i)
    connected = new Set()
    for (const link of links) {
      const sourceID = typeof link.source === 'object' ? link.source.id : String(link.source)
      const targetID = typeof link.target === 'object' ? link.target.id : String(link.target)
      const left = indexByID.get(sourceID)
      const right = indexByID.get(targetID)
      if (left == null || right == null) continue
      connected.add(packedPair(left, right))
    }
  }
  return force
}

export function createGraphForceSimulation(input: GraphForceInput): Simulation<LayoutNode, LayoutLink> {
  const { nodes, links, width, height, layout, maxDegree, controls } = input
  let groupedKey = ''
  let groupedSet = new Set<string>()
  let fociKey = ''
  let fociMap = clusterFoci(controls.groupedKinds, width, height)
  const grouped = () => {
    const key = controls.groupedKinds.join('\n')
    if (key !== groupedKey) {
      groupedKey = key
      groupedSet = new Set(controls.groupedKinds)
    }
    return groupedSet
  }
  const foci = () => {
    const key = `${controls.groupedKinds.join('\n')}|${width}x${height}`
    if (key !== fociKey) {
      fociKey = key
      fociMap = clusterFoci(controls.groupedKinds, width, height)
    }
    return fociMap
  }
  const linkDistance = new Float64Array(links.length)
  const linkStrength = new Float64Array(links.length)
  let cachedSpacing = Number.NaN
  const refreshLinks = () => {
    if (cachedSpacing === controls.spacing) return
    cachedSpacing = controls.spacing
    for (let i = 0; i < links.length; i += 1) {
      const forces = linkForces(linkDegree(links[i].source), linkDegree(links[i].target), controls.spacing, maxDegree)
      linkDistance[i] = forces.distance
      linkStrength[i] = forces.strength
    }
  }
  const collide = nodes.map((node) => collideRadius(node.degree))
  const chargeWeight = nodes.map((node) => chargeStrength(1, node.degree))
  const isWell = new Uint8Array(nodes.length)
  const wellOf = new Int32Array(nodes.length)
  const simulation = forceSimulation(nodes)
    .force('link', forceLink<LayoutNode, LayoutLink>(links)
      .id((node) => node.id)
      .distance((_, index) => {
        refreshLinks()
        return linkDistance[index] ?? 80
      })
      .strength((_, index) => {
        refreshLinks()
        return linkStrength[index] ?? 0.1
      }))
    .force('charge', forceManyBody<LayoutNode>().strength((_, index) => (chargeWeight[index] ?? -1) * controls.repel * layout.chargeScale).distanceMin(8).distanceMax(Math.min(width, height) * layout.chargeDistance).theta(layout.chargeTheta))
    .force('center', forceCenter(width / 2, height / 2).strength(layout.centerStrength))
    .force('gravityX', forceX<LayoutNode>(width / 2).strength((node, index) => globalGravityStrength(controls.gravity, node.degree, layout.gravityScale, isWell[index], wellOf[index])))
    .force('gravityY', forceY<LayoutNode>(height / 2).strength((node, index) => globalGravityStrength(controls.gravity, node.degree, layout.gravityScale, isWell[index], wellOf[index])))
    .force('local', createLocalGravityForce(links, () => controls.gravity, () => controls.spacing, () => controls.repel, isWell, wellOf))
    .force('collide', forceCollide<LayoutNode>().radius((_, index) => collide[index] ?? 10).strength(0.95).iterations(layout.collideIterations))
    .force('clusterX', forceX<LayoutNode>((node) => (grouped().has(node.kind) ? (foci().get(node.kind)?.x ?? width / 2) : width / 2)).strength((node) => (grouped().has(node.kind) ? 0.12 : 0)))
    .force('clusterY', forceY<LayoutNode>((node) => (grouped().has(node.kind) ? (foci().get(node.kind)?.y ?? height / 2) : height / 2)).strength((node) => (grouped().has(node.kind) ? 0.12 : 0)))
    .velocityDecay(layout.velocityDecay)
    .alphaDecay(layout.alphaDecay)
    .alphaMin(layout.alphaMin)
  if (layout.skipPairwiseForces) {
    simulation.force('repel', null).force('spread', null)
  } else {
    simulation
      .force('repel', createHubRepelForce(() => controls.repel, () => controls.spacing, maxDegree))
      .force('spread', createUnlinkedRepelForce(links, () => controls.repel, () => controls.spacing, maxDegree))
  }
  return simulation
}
