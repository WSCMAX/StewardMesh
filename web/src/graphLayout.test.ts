import { expect, test } from 'vitest'
import {
  applyMassedRepel,
  assignLocalWell,
  assignLocalWellRegions,
  buildSpatialHash,
  graphLayoutProfile,
  graphRenderSurface,
  hitTestNodeIndex,
  hubSeparation,
  isUniversalHub,
  linkForces,
  localWellCount,
  nearbyIndices,
  nodeMass,
  packedPair,
  selectLocalWellIndices,
  spokeRadius,
  unlinkedCellSize,
  unlinkedQueryRange,
  unlinkedRepelMagnitude,
  wellSeparation,
  type LayoutNode,
} from './graphLayout'

// Requirement: REQ-DIRECTORY-EXPANSION-008. Feature: threads.relationships.

test('spatial hash only visits nodes in nearby cells', () => {
  const points = [
    { x: 10, y: 10 },
    { x: 18, y: 12 },
    { x: 4000, y: 4000 },
  ]
  const hash = buildSpatialHash(points, 48)
  const nearby = nearbyIndices(hash, 10, 10, 1)
  expect(nearby).toContain(0)
  expect(nearby).toContain(1)
  expect(nearby).not.toContain(2)
})

test('spatial hash keeps unlinked repel local while still separating close records', () => {
  expect(unlinkedQueryRange(140, 12)).toBeGreaterThanOrEqual(1)
  expect(unlinkedCellSize(140)).toBeGreaterThan(40)
  const close = unlinkedRepelMagnitude(false, 20, 140, 320, 1, 1)
  const far = unlinkedRepelMagnitude(false, 400, 140, 320, 1, 1)
  expect(close).toBeGreaterThan(0)
  expect(far).toBe(0)
})

test('packs undirected pairs in one order', () => {
  expect(packedPair(3, 9)).toBe(packedPair(9, 3))
  expect(packedPair(3, 9)).not.toBe(packedPair(3, 8))
})

test('uses canvas for dense graphs and a lighter simulation above 5000 records', () => {
  expect(graphRenderSurface(400)).toBe('svg')
  expect(graphRenderSurface(2500)).toBe('canvas')
  const compact = graphLayoutProfile(400)
  const dense = graphLayoutProfile(2500)
  const large = graphLayoutProfile(5000)
  expect(dense.dense).toBe(true)
  expect(dense.skipPairwiseForces).toBe(false)
  expect(dense.chargeScale).toBeLessThan(compact.chargeScale)
  expect(dense.chargeDistance).toBeLessThan(compact.chargeDistance)
  expect(large.large).toBe(true)
  expect(large.skipPairwiseForces).toBe(true)
  expect(large.gravityScale).toBeLessThan(compact.gravityScale)
  expect(large.centerStrength).toBeLessThanOrEqual(compact.centerStrength + 0.002)
  expect(large.chargeScale).toBeGreaterThan(0.6)
  expect(large.tickBatch).toBeGreaterThan(dense.tickBatch)
  expect(large.tickYieldMs).toBe(0)
  expect(hubSeparation(40, 40, 140)).toBeLessThan(210)
  expect(hubSeparation(12, 10, 140)).toBeLessThan(hubSeparation(12, 10, 280))
})

test('hit testing prefers the topmost overlapping node', () => {
  const nodes = [
    { id: 'a', kind: 'asset', label: 'A', degree: 1, x: 0, y: 0 },
    { id: 'b', kind: 'asset', label: 'B', degree: 1, x: 2, y: 0 },
  ] as LayoutNode[]
  expect(hitTestNodeIndex(nodes, 2, 0)).toBe(1)
})

test('massed repel still yields for leaves next to hubs', () => {
  const hub = { degree: 12, vx: 0, vy: 0 }
  const leaf = { degree: 1, vx: 0, vy: 0 }
  applyMassedRepel(hub, leaf, 40, 0, 40, 80)
  expect(Math.abs(leaf.vx)).toBeGreaterThan(Math.abs(hub.vx))
})

test('local wells are the top connected tenth of the graph', () => {
  expect(localWellCount(12)).toBe(2)
  expect(localWellCount(80)).toBe(8)
  expect(localWellCount(2000)).toBe(48)
  const degrees = [1, 8, 3, 1, 12, 2, 1, 1, 4, 1]
  expect(selectLocalWellIndices(degrees)).toEqual([4, 1])
  expect(assignLocalWell(9, [1], degrees, new Set([1, 4]))).toBe(1)
  expect(assignLocalWell(1, [9], degrees, new Set([1, 4]))).toBe(1)
})

test('saturates campus-scale hubs so they cannot dominate layout', () => {
  expect(nodeMass(40) - nodeMass(4)).toBeGreaterThan(nodeMass(400) - nodeMass(40))
  expect(linkForces(400, 1, 140, 400).distance).toBeLessThan(linkForces(12, 1, 140, 400).distance * 1.8)
  expect(spokeRadius(400, 140)).toBe(spokeRadius(141, 140))
  expect(isUniversalHub(80, 100)).toBe(true)
  expect(isUniversalHub(12, 100)).toBe(false)
  const degrees = [80, 18, 14, ...Array(97).fill(1)]
  expect(selectLocalWellIndices(degrees)).not.toContain(0)
  expect(selectLocalWellIndices(degrees)[0]).toBe(1)
})

test('relationship regions grow around the nearest well and keep clusters apart', () => {
  const neighbors = [
    [1, 2],
    [0],
    [0, 3],
    [2],
    [5],
    [4],
  ]
  const regions = assignLocalWellRegions(neighbors, [0, 4])
  expect(Array.from(regions)).toEqual([0, 0, 0, 0, 4, 4])
  expect(spokeRadius(20, 140)).toBeGreaterThan(spokeRadius(4, 140))
  expect(wellSeparation(20, 20, 140)).toBeGreaterThan(spokeRadius(20, 140) * 2)
})
