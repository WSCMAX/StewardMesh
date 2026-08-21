import { denseGraphNodeThreshold, largeGraphNodeThreshold } from './graphModel'

// Requirement: REQ-DIRECTORY-EXPANSION-008. Feature: threads.relationships.

export type LayoutNode = {
  id: string
  kind: string
  label: string
  attributes?: Record<string, string>
  degree: number
  x: number
  y: number
  vx?: number
  vy?: number
  fx?: number | null
  fy?: number | null
  index?: number
}

export type LayoutLink = {
  id: string
  kind: string
  source: LayoutNode | string | number
  target: LayoutNode | string | number
}

export type SpatialHash = {
  cellSize: number
  cells: Map<number, number[]>
}

export function softDegree(degree: number) {
  return Math.log2(1 + Math.max(0, degree))
}

export function nodeMass(degree: number) {
  const attached = Math.max(0, degree)
  return 1 + softDegree(attached) * 1.15 + Math.sqrt(Math.min(attached, 18)) * 0.4
}

export function nodeVisualRadius(degree: number) {
  return 3.6 + Math.min(9, Math.sqrt(Math.max(0, degree)) * 1.85)
}

export function isGroupingHub(degree: number, maxDegree: number) {
  return degree >= 3 && degree >= Math.max(3, maxDegree * 0.2)
}

export function linkForces(degreeA: number, degreeB: number, spacing: number, maxDegree: number) {
  const hub = Math.max(degreeA, degreeB, 1)
  const spoke = Math.min(6.2, softDegree(hub))
  const scale = maxDegree <= 1 ? 1 : Math.min(1, hub / maxDegree)
  return {
    strength: 0.07 + 0.18 / Math.max(1, spoke) + 0.04 * scale,
    distance: Math.max(72, spacing * (0.92 + 0.16 * spoke)),
  }
}

export function hubSeparation(degreeA: number, degreeB: number, spacing: number) {
  const desired = spacing * (0.7 + 0.1 * Math.sqrt(nodeMass(degreeA) * nodeMass(degreeB)))
  return Math.min(Math.max(80, desired), spacing * 1.45)
}

export function hubRepelMagnitude(degreeA: number, degreeB: number, distance: number, repel: number, spacing: number, maxDegree: number) {
  if (!isGroupingHub(degreeA, maxDegree) || !isGroupingHub(degreeB, maxDegree)) return 0
  const desired = hubSeparation(degreeA, degreeB, spacing)
  if (distance >= desired) return 0
  const overlap = (desired - Math.max(distance, 1)) / desired
  return Math.max(0, repel) * 0.035 * Math.sqrt(nodeMass(degreeA) * nodeMass(degreeB)) * overlap
}

export function unlinkedDesiredDistance(spacing: number, degreeA: number, degreeB: number) {
  const massSum = Math.min(22, nodeMass(degreeA) + nodeMass(degreeB))
  return Math.max(56, spacing * 0.95) * (0.92 + 0.03 * massSum)
}

export function unlinkedRepelMagnitude(linked: boolean, distance: number, spacing: number, repel: number, degreeA = 0, degreeB = 0) {
  if (linked) return 0
  const desired = unlinkedDesiredDistance(spacing, degreeA, degreeB)
  if (distance >= desired) return 0
  return Math.max(0, repel) * 0.007 * Math.sqrt(nodeMass(degreeA) * nodeMass(degreeB)) * ((desired - Math.max(distance, 1)) / desired)
}

export function unlinkedCellSize(spacing: number) {
  return Math.max(48, spacing * 0.85)
}

export function unlinkedQueryRange(spacing: number, maxDegree: number) {
  const cell = unlinkedCellSize(spacing)
  const farthest = unlinkedDesiredDistance(spacing, maxDegree, maxDegree)
  return Math.max(1, Math.ceil(farthest / cell))
}

export function centerPull(gravity: number) {
  return Math.max(0, gravity) / 2000
}

export function gravityStrength(gravity: number, degree: number) {
  return centerPull(gravity) * Math.min(1.7, 0.62 + 0.1 * nodeMass(degree))
}

export function localGravityStrength(gravity: number) {
  return Math.max(0, gravity) / 720
}

export function localWellCount(nodeCount: number) {
  return Math.max(2, Math.min(48, Math.round(Math.max(0, nodeCount) * 0.1)))
}

export function isUniversalHub(degree: number, nodeCount: number) {
  if (nodeCount < 12) return false
  return degree >= Math.max(24, (nodeCount - 1) * 0.4)
}

export function selectLocalWellIndices(degrees: readonly number[]) {
  const limit = localWellCount(degrees.length)
  const ranked = degrees.map((degree, index) => ({ degree, index })).sort((left, right) => right.degree - left.degree || left.index - right.index)
  const wells: number[] = []
  for (const entry of ranked) {
    if (entry.degree < 2) break
    if (isUniversalHub(entry.degree, degrees.length)) continue
    wells.push(entry.index)
    if (wells.length >= limit) break
  }
  return wells
}

export function assignLocalWell(nodeIndex: number, neighbors: readonly number[], degrees: readonly number[], wellSet: ReadonlySet<number>) {
  if (wellSet.has(nodeIndex)) return nodeIndex
  let best = -1
  let bestDegree = -1
  for (const neighbor of neighbors) {
    if (!wellSet.has(neighbor)) continue
    const degree = degrees[neighbor] ?? 0
    if (degree > bestDegree) {
      best = neighbor
      bestDegree = degree
    }
  }
  return best
}

export function spokeRadius(memberCount: number, spacing: number) {
  const around = Math.min(140, Math.max(0, memberCount - 1))
  return Math.max(spacing * 0.9, Math.sqrt(around) * Math.max(24, spacing * 0.3))
}

export function wellSeparation(membersA: number, membersB: number, spacing: number) {
  return spokeRadius(membersA, spacing) + spokeRadius(membersB, spacing) + spacing * 0.8
}

export function assignLocalWellRegions(neighbors: readonly (readonly number[])[], wellIndices: readonly number[]) {
  const wellOf = new Int32Array(neighbors.length)
  wellOf.fill(-1)
  const queue: number[] = []
  for (const well of wellIndices) {
    if (well < 0 || well >= neighbors.length || wellOf[well] >= 0) continue
    wellOf[well] = well
    queue.push(well)
  }
  let cursor = 0
  while (cursor < queue.length) {
    const node = queue[cursor]
    cursor += 1
    const owner = wellOf[node]
    for (const neighbor of neighbors[node] ?? []) {
      if (neighbor < 0 || neighbor >= wellOf.length || wellOf[neighbor] >= 0) continue
      wellOf[neighbor] = owner
      queue.push(neighbor)
    }
  }
  return wellOf
}

export function chargeStrength(repel: number, degree: number) {
  return -Math.max(0, repel) * (0.62 + Math.min(0.48, 0.04 * nodeMass(degree)))
}

export function applyMassedRepel(
  left: { degree: number; vx?: number; vy?: number },
  right: { degree: number; vx?: number; vy?: number },
  dx: number,
  dy: number,
  distance: number,
  magnitude: number,
) {
  if (magnitude === 0 || distance < 0.01) return
  const fx = (dx / distance) * magnitude
  const fy = (dy / distance) * magnitude
  const leftMass = nodeMass(left.degree)
  const rightMass = nodeMass(right.degree)
  left.vx = (left.vx ?? 0) - fx / leftMass
  left.vy = (left.vy ?? 0) - fy / leftMass
  right.vx = (right.vx ?? 0) + fx / rightMass
  right.vy = (right.vy ?? 0) + fy / rightMass
}

export function pairSeparation(left: LayoutNode, right: LayoutNode) {
  let dx = (right.x ?? 0) - (left.x ?? 0)
  let dy = (right.y ?? 0) - (left.y ?? 0)
  let distance = Math.hypot(dx, dy)
  if (distance < 0.01) {
    dx = (Math.random() - 0.5) * 0.02
    dy = (Math.random() - 0.5) * 0.02
    distance = Math.hypot(dx, dy)
  }
  return { dx, dy, distance }
}

export function packCell(cx: number, cy: number) {
  return ((cx + 32768) << 16) | ((cy + 32768) & 0xffff)
}

export function buildSpatialHash(points: readonly { x: number; y: number }[], cellSize: number): SpatialHash {
  const size = Math.max(1, cellSize)
  const cells = new Map<number, number[]>()
  for (let i = 0; i < points.length; i += 1) {
    const cx = Math.floor((points[i].x ?? 0) / size)
    const cy = Math.floor((points[i].y ?? 0) / size)
    const key = packCell(cx, cy)
    const bucket = cells.get(key)
    if (bucket) bucket.push(i)
    else cells.set(key, [i])
  }
  return { cellSize: size, cells }
}

export function forEachNearbyIndex(hash: SpatialHash, x: number, y: number, range: number, visit: (index: number) => void) {
  const cx = Math.floor(x / hash.cellSize)
  const cy = Math.floor(y / hash.cellSize)
  const span = Math.max(0, range)
  for (let ix = cx - span; ix <= cx + span; ix += 1) {
    for (let iy = cy - span; iy <= cy + span; iy += 1) {
      const bucket = hash.cells.get(packCell(ix, iy))
      if (!bucket) continue
      for (const index of bucket) visit(index)
    }
  }
}

export function nearbyIndices(hash: SpatialHash, x: number, y: number, range = 1) {
  const found: number[] = []
  forEachNearbyIndex(hash, x, y, range, (index) => found.push(index))
  return found
}

export function packedPair(left: number, right: number) {
  const a = left < right ? left : right
  const b = left < right ? right : left
  return a * 131071 + b
}

export function snapshotPositions(nodes: readonly LayoutNode[]) {
  const positions = new Float32Array(nodes.length * 2)
  for (let i = 0; i < nodes.length; i += 1) {
    positions[i * 2] = nodes[i].x ?? 0
    positions[i * 2 + 1] = nodes[i].y ?? 0
  }
  return positions
}

export function applyPositions(nodes: LayoutNode[], positions: Float32Array) {
  const count = Math.min(nodes.length, Math.floor(positions.length / 2))
  for (let i = 0; i < count; i += 1) {
    nodes[i].x = positions[i * 2]
    nodes[i].y = positions[i * 2 + 1]
  }
}

export function graphCanvasHeight(nodeCount: number) {
  return Math.round(Math.max(640, Math.min(1100, 520 + Math.sqrt(Math.max(0, nodeCount)) * 58)))
}

export function viewportGraphHeight(nodeCount: number, viewportHeight = typeof window === 'undefined' ? 800 : window.innerHeight) {
  return Math.max(480, Math.min(graphCanvasHeight(nodeCount), Math.round(viewportHeight * 0.58)))
}

export { denseGraphNodeThreshold, largeGraphNodeThreshold }

export function isDenseGraph(nodeCount: number) {
  return nodeCount > denseGraphNodeThreshold
}

export function isLargeGraph(nodeCount: number) {
  return nodeCount >= largeGraphNodeThreshold
}

export function graphRenderSurface(nodeCount: number): 'svg' | 'canvas' {
  return isDenseGraph(nodeCount) ? 'canvas' : 'svg'
}

export type GraphLayoutProfile = {
  dense: boolean
  large: boolean
  skipPairwiseForces: boolean
  velocityDecay: number
  alphaDecay: number
  alphaMin: number
  alphaStart: number
  alphaRetune: number
  collideIterations: number
  chargeTheta: number
  chargeScale: number
  chargeDistance: number
  centerStrength: number
  gravityScale: number
  tickSkip: number
  tickBatch: number
  tickBudgetMs: number
  tickYieldMs: number
  savePositionsEveryTick: boolean
}

export function graphLayoutProfile(nodeCount: number): GraphLayoutProfile {
  if (isLargeGraph(nodeCount)) {
    return {
      dense: true,
      large: true,
      skipPairwiseForces: true,
      velocityDecay: 0.42,
      alphaDecay: 0.07,
      alphaMin: 0.012,
      alphaStart: 0.72,
      alphaRetune: 0.4,
      collideIterations: 1,
      chargeTheta: 1.2,
      chargeScale: 1.05,
      chargeDistance: 0.68,
      centerStrength: 0.0035,
      gravityScale: 0.36,
      tickSkip: 1,
      tickBatch: 8,
      tickBudgetMs: 16,
      tickYieldMs: 0,
      savePositionsEveryTick: false,
    }
  }
  if (!isDenseGraph(nodeCount)) {
    return {
      dense: false,
      large: false,
      skipPairwiseForces: false,
      velocityDecay: 0.42,
      alphaDecay: 0.028,
      alphaMin: 0.001,
      alphaStart: 0.85,
      alphaRetune: 0.55,
      collideIterations: 3,
      chargeTheta: 0.86,
      chargeScale: 1,
      chargeDistance: 0.86,
      centerStrength: 0.0032,
      gravityScale: 0.78,
      tickSkip: 1,
      tickBatch: 1,
      tickBudgetMs: 8,
      tickYieldMs: 16,
      savePositionsEveryTick: true,
    }
  }
  return {
    dense: true,
    large: false,
    skipPairwiseForces: false,
    velocityDecay: 0.46,
    alphaDecay: 0.055,
    alphaMin: 0.008,
    alphaStart: 0.75,
    alphaRetune: 0.4,
    collideIterations: 1,
    chargeTheta: 1.05,
    chargeScale: 0.9,
    chargeDistance: 0.66,
    centerStrength: 0.0036,
    gravityScale: 0.48,
    tickSkip: 1,
    tickBatch: 4,
    tickBudgetMs: 12,
    tickYieldMs: 0,
    savePositionsEveryTick: false,
  }
}

export function initialSimNodeRadius(index: number, nodeCount: number, degree: number, maxDegree: number, width: number, height: number, dense: boolean) {
  const progress = (index + 0.5) / Math.max(nodeCount, 1)
  const angle = index * Math.PI * (3 - Math.sqrt(5))
  const hubness = maxDegree <= 1 ? 0.5 : degree / maxDegree
  const spread = dense ? 0.28 + 0.72 * (1 - hubness) : 0.22 + 0.52 * Math.sqrt(progress)
  const initialRadius = Math.min(width, height) * spread
  return {
    x: width / 2 + Math.cos(angle) * initialRadius,
    y: height / 2 + Math.sin(angle) * initialRadius,
  }
}

export function labelDegreeThreshold(nodeDegrees: readonly number[]) {
  if (nodeDegrees.length <= 30) return 0
  const descending = [...nodeDegrees].sort((left, right) => right - left)
  return Math.max(2, descending[Math.min(descending.length - 1, Math.ceil(descending.length * 0.16) - 1)] ?? 2)
}

export function edgeVisualWidth(kind: string, degreeA: number, degreeB: number) {
  const semanticWeight = kind === 'contains' || kind === 'belongs_to' || kind === 'modeled_as' ? 0.35 : 0
  return 0.55 + semanticWeight + Math.min(1.8, Math.sqrt(Math.max(degreeA, degreeB, 1)) * 0.28)
}

export function clusterFoci(kinds: readonly string[], width: number, height: number) {
  const unique = [...new Set(kinds)]
  const columns = Math.max(1, Math.ceil(Math.sqrt(unique.length)))
  const rows = Math.max(1, Math.ceil(unique.length / columns))
  const foci = new Map<string, { x: number; y: number }>()
  unique.forEach((kind, index) => {
    const column = index % columns
    const row = Math.floor(index / columns)
    foci.set(kind, {
      x: width * ((column + 1) / (columns + 1)),
      y: height * ((row + 1) / (rows + 1)),
    })
  })
  return foci
}

export function truncateLabel(label: string, maximum = 22) {
  const characters = [...label]
  if (characters.length <= maximum) return label
  return `${characters.slice(0, maximum - 1).join('')}…`
}

export function collideRadius(degree: number) {
  return nodeVisualRadius(degree) + 12 + Math.min(10, nodeMass(degree) * 0.25)
}

export function hitTestNodeIndex(nodes: readonly LayoutNode[], x: number, y: number) {
  let best = -1
  let bestDistance = Infinity
  for (let i = nodes.length - 1; i >= 0; i -= 1) {
    const radius = Math.max(14, nodeVisualRadius(nodes[i].degree) + 8)
    const dx = (nodes[i].x ?? 0) - x
    const dy = (nodes[i].y ?? 0) - y
    const distance = dx * dx + dy * dy
    if (distance <= radius * radius && distance < bestDistance) {
      best = i
      bestDistance = distance
    }
  }
  return best
}
