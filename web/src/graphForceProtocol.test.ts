import { expect, test } from 'vitest'
import { packWorkerInit, unpackWorkerGraph } from './graphForceProtocol'
import { graphLayoutProfile } from './graphLayout'

// Requirement: REQ-DIRECTORY-EXPANSION-008. Feature: threads.relationships.

test('packs simulation input as transferable typed arrays without labels', () => {
  const packed = packWorkerInit({
    nodes: [
      { id: 'organization:org', kind: 'organization', label: 'Org', degree: 2, x: 10, y: 20, fx: 10, fy: 20 },
      { id: 'asset:a', kind: 'asset', label: 'A', degree: 1, x: 30, y: 40 },
    ],
    links: [{ source: 0, target: 1 }],
    width: 800,
    height: 600,
    layout: graphLayoutProfile(2),
    maxDegree: 2,
    gravity: 200,
    repel: 260,
    spacing: 140,
    groupedKinds: ['asset'],
  })
  expect(packed.xy).toEqual(new Float32Array([10, 20, 30, 40]))
  expect(packed.degrees[0]).toBe(2)
  expect(packed.linkEnds).toEqual(new Uint32Array([0, 1]))
  const unpacked = unpackWorkerGraph(packed)
  expect(unpacked.nodes[0].kind).toBe('organization')
  expect(unpacked.nodes[0].x).toBe(10)
  expect(unpacked.nodes[0].fx).toBe(10)
  expect(unpacked.nodes[1].fx).toBeNull()
  expect(typeof unpacked.links[0].source).toBe('object')
})
