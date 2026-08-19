import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { expect, test, vi } from 'vitest'
import InteractiveRelationshipGraph, { applyMassedRepel, centerPull, chargeStrength, denseGraphNodeThreshold, edgeVisualWidth, graphCanvasHeight, graphLayoutProfile, gravityStrength, initialSimNodeRadius, isDenseGraph, isLargeGraph, hubRepelMagnitude, isGroupingHub, labelDegreeThreshold, largeGraphNodeThreshold, linkForces, nodeMass, nodeVisualRadius, unlinkedRepelMagnitude, viewportGraphHeight } from './InteractiveRelationshipGraph'

// Requirement: REQ-DIRECTORY-EXPANSION-008. Feature: threads.relationships.

test('increases layout weight with the number of edges on a node', () => {
  expect(nodeVisualRadius(12)).toBeGreaterThan(nodeVisualRadius(1))
  expect(nodeMass(12)).toBeGreaterThan(nodeMass(4))
  expect(nodeMass(4)).toBeGreaterThan(nodeMass(1))
  expect(nodeMass(200) / nodeMass(12)).toBeLessThan(3)
  const hub = linkForces(12, 1, 90, 12)
  const leaf = linkForces(1, 1, 90, 12)
  expect(hub.strength).toBeLessThan(leaf.strength)
  expect(hub.distance).toBeGreaterThan(leaf.distance)
  expect(linkForces(400, 1, 90, 400).distance).toBeLessThan(hub.distance * 2)
})

test('repels grouping hubs from each other without pushing leaves out of a cluster', () => {
  expect(isGroupingHub(12, 12)).toBe(true)
  expect(isGroupingHub(1, 12)).toBe(false)
  const hubPair = hubRepelMagnitude(12, 10, 80, 220, 90, 12)
  const hubAndLeaf = hubRepelMagnitude(12, 1, 80, 220, 90, 12)
  const farHubs = hubRepelMagnitude(12, 10, 800, 220, 90, 12)
  expect(hubPair).toBeGreaterThan(0)
  expect(hubAndLeaf).toBe(0)
  expect(farHubs).toBe(0)
  expect(hubRepelMagnitude(12, 10, 40, 400, 90, 12)).toBeGreaterThan(hubRepelMagnitude(12, 10, 40, 120, 90, 12))
})

test('gives dense graphs more room while limiting default label density', () => {
  expect(graphCanvasHeight(100)).toBeGreaterThan(graphCanvasHeight(10))
  expect(graphCanvasHeight(1000)).toBe(1100)
  expect(viewportGraphHeight(1000, 1200)).toBeLessThan(graphCanvasHeight(1000))
  expect(viewportGraphHeight(10, 800)).toBeGreaterThanOrEqual(480)
  expect(labelDegreeThreshold([1, 1, 1])).toBe(0)
  expect(labelDegreeThreshold([...Array(80).fill(1), ...Array(20).fill(8)])).toBe(8)
  expect(edgeVisualWidth('contains', 8, 1)).toBeGreaterThan(edgeVisualWidth('tagged_with', 1, 1))
})

test('keeps unconnected records from sitting on top of each other', () => {
  expect(unlinkedRepelMagnitude(true, 20, 140, 320)).toBe(0)
  expect(unlinkedRepelMagnitude(false, 20, 140, 320)).toBeGreaterThan(0)
  expect(unlinkedRepelMagnitude(false, 400, 140, 320)).toBe(0)
  expect(unlinkedRepelMagnitude(false, 20, 140, 320, 12, 8)).toBeGreaterThan(unlinkedRepelMagnitude(false, 20, 140, 320, 1, 1))
})

test('keeps dense-graph cooling while still applying local repel', () => {
  expect(isDenseGraph(denseGraphNodeThreshold)).toBe(false)
  expect(isDenseGraph(denseGraphNodeThreshold + 1)).toBe(true)
  expect(isLargeGraph(largeGraphNodeThreshold)).toBe(true)
  expect(isLargeGraph(largeGraphNodeThreshold - 1)).toBe(false)
  const compact = graphLayoutProfile(400)
  const dense = graphLayoutProfile(2500)
  const large = graphLayoutProfile(5000)
  expect(compact.skipPairwiseForces).toBe(false)
  expect(dense.skipPairwiseForces).toBe(false)
  expect(large.skipPairwiseForces).toBe(true)
  expect(large.tickBatch).toBeGreaterThan(dense.tickBatch)
  expect(dense.alphaDecay).toBeGreaterThan(compact.alphaDecay)
  expect(dense.alphaMin).toBeGreaterThan(compact.alphaMin)
  const hub = initialSimNodeRadius(0, 2500, 40, 40, 960, 1100, true)
  const leaf = initialSimNodeRadius(900, 2500, 1, 40, 960, 1100, true)
  expect(Math.hypot(hub.x - 480, hub.y - 550)).toBeLessThan(Math.hypot(leaf.x - 480, leaf.y - 550))
})

test('splits center pull from charge so gravity and repel can be tuned apart', () => {
  expect(centerPull(400)).toBeGreaterThan(centerPull(80))
  expect(centerPull(0)).toBe(0)
  expect(gravityStrength(120, 12)).toBeGreaterThan(gravityStrength(120, 1))
  expect(gravityStrength(120, 12)).toBeLessThan(centerPull(120) * 2)
  expect(gravityStrength(0, 12)).toBe(0)
  expect(chargeStrength(500, 8)).toBeLessThan(chargeStrength(120, 8))
  expect(chargeStrength(400, 12)).toBeLessThan(chargeStrength(400, 1))
  const large = graphLayoutProfile(5000)
  const compact = graphLayoutProfile(400)
  expect(large.gravityScale * gravityStrength(120, 1)).toBeLessThan(compact.gravityScale * gravityStrength(120, 1))
})

test('lets heavier hubs resist pairwise repel more than lightly attached leaves', () => {
  const hub = { degree: 12, vx: 0, vy: 0 }
  const leaf = { degree: 1, vx: 0, vy: 0 }
  applyMassedRepel(hub, leaf, 40, 0, 40, 80)
  expect(Math.abs(leaf.vx)).toBeGreaterThan(Math.abs(hub.vx))
  expect(hub.vx).toBeLessThan(0)
  expect(leaf.vx).toBeGreaterThan(0)
})

test('lets operators pick type colors from the legend pills', async () => {
  const onKindColorChange = vi.fn()
  render(
    <InteractiveRelationshipGraph
      colorMode="type"
      edges={[{ id: '1', from: 'organization:org', to: 'asset:a', kind: 'contains' }]}
      focusNodeID=""
      nodes={[
        { id: 'organization:org', kind: 'organization', label: 'Org' },
        { id: 'asset:a', kind: 'asset', label: 'A' },
      ]}
      onKindColorChange={onKindColorChange}
      onSelectNode={() => undefined}
      selectedNodeID=""
    />,
  )

  const legend = screen.getByRole('list', { name: 'Record type legend' })
  fireEvent.click(within(legend).getByRole('button', { name: 'Change color for Asset' }))
  fireEvent.click(screen.getByRole('option', { name: 'Mint' }))
  expect(onKindColorChange).toHaveBeenCalledWith('asset', 'mint')
})

test('draws larger circles for highly connected records', async () => {
  render(
    <InteractiveRelationshipGraph
      edges={[
        { id: '1', from: 'organization:org', to: 'asset:a', kind: 'contains' },
        { id: '2', from: 'organization:org', to: 'asset:b', kind: 'contains' },
        { id: '3', from: 'organization:org', to: 'asset:c', kind: 'contains' },
      ]}
      focusNodeID=""
      nodes={[
        { id: 'organization:org', kind: 'organization', label: 'Org' },
        { id: 'asset:a', kind: 'asset', label: 'A' },
        { id: 'asset:b', kind: 'asset', label: 'B' },
        { id: 'asset:c', kind: 'asset', label: 'C' },
      ]}
      onSelectNode={() => undefined}
      selectedNodeID=""
    />,
  )

  expect(screen.getByRole('img', { name: 'Interactive relationship graph' })).toBeInTheDocument()
  await waitFor(() => {
    const hub = document.querySelector('[data-node-id="organization:org"] circle.node-dot')
    const leaf = document.querySelector('[data-node-id="asset:a"] circle.node-dot')
    expect(hub).not.toBeNull()
    expect(leaf).not.toBeNull()
    expect(Number(hub?.getAttribute('r'))).toBeGreaterThan(Number(leaf?.getAttribute('r')))
  })
})

test('highlights a hovered neighborhood and dims unrelated records and edges', async () => {
  render(
    <InteractiveRelationshipGraph
      edges={[
        { id: 'related', from: 'organization:org', to: 'asset:a', kind: 'contains' },
        { id: 'unrelated', from: 'asset:b', to: 'asset:c', kind: 'tagged_with' },
      ]}
      focusNodeID=""
      nodes={[
        { id: 'organization:org', kind: 'organization', label: 'Org' },
        { id: 'asset:a', kind: 'asset', label: 'A' },
        { id: 'asset:b', kind: 'asset', label: 'B' },
        { id: 'asset:c', kind: 'asset', label: 'C' },
      ]}
      onSelectNode={() => undefined}
      selectedNodeID=""
    />,
  )

  await waitFor(() => expect(document.querySelector('[data-node-id="organization:org"]')).not.toBeNull())
  fireEvent.mouseEnter(document.querySelector('[data-node-id="organization:org"]') as Element)
  const groups = [...document.querySelectorAll<SVGGElement>('g.nodes > g')]
  expect(groups.find((group) => group.dataset.nodeId === 'asset:a')?.getAttribute('opacity')).toBe('1')
  expect(groups.find((group) => group.dataset.nodeId === 'asset:b')?.getAttribute('opacity')).toBe('0.12')
  const lines = [...document.querySelectorAll<SVGLineElement>('g.links line')]
  expect(lines[0].getAttribute('stroke-opacity')).toBe('1')
  expect(lines[1].getAttribute('stroke-opacity')).toBe('0.05')
  expect(lines[0].getAttribute('stroke')).toBe('#7af0d8')
  expect(Number(lines[0].getAttribute('stroke-width'))).toBeGreaterThan(Number(lines[1].getAttribute('stroke-width')))
  expect(groups.find((group) => group.dataset.nodeId === 'asset:a')?.querySelector('.node-label')?.getAttribute('opacity')).toBe('1')
  expect(groups.find((group) => group.dataset.nodeId === 'asset:b')?.querySelector('.node-label')?.getAttribute('opacity')).toBe('0')
})

test('highlights every record of a type selected from the legend key', async () => {
  render(
    <InteractiveRelationshipGraph
      edges={[
        { id: 'related', from: 'organization:org', to: 'asset:a', kind: 'contains' },
        { id: 'unrelated', from: 'asset:b', to: 'asset:c', kind: 'tagged_with' },
      ]}
      focusNodeID=""
      nodes={[
        { id: 'organization:org', kind: 'organization', label: 'Org' },
        { id: 'asset:a', kind: 'asset', label: 'A' },
        { id: 'asset:b', kind: 'asset', label: 'B' },
        { id: 'asset:c', kind: 'asset', label: 'C' },
      ]}
      onSelectNode={() => undefined}
      selectedNodeID=""
    />,
  )

  await waitFor(() => expect(document.querySelector('[data-node-id="organization:org"]')).not.toBeNull())
  fireEvent.click(screen.getByRole('button', { name: 'Highlight Asset records' }))
  const groups = [...document.querySelectorAll<SVGGElement>('g.nodes > g')]
  expect(groups.find((group) => group.dataset.nodeId === 'asset:a')?.getAttribute('opacity')).toBe('1')
  expect(groups.find((group) => group.dataset.nodeId === 'asset:b')?.classList.contains('is-kind-highlight')).toBe(true)
  expect(groups.find((group) => group.dataset.nodeId === 'organization:org')?.getAttribute('opacity')).toBe('0.12')
  fireEvent.click(screen.getByRole('button', { name: 'Highlight Asset records' }))
  expect(groups.find((group) => group.dataset.nodeId === 'organization:org')?.getAttribute('opacity')).toBe('1')
})

test('opens a fullscreen graph with the key and layout sliders', async () => {
  render(
    <InteractiveRelationshipGraph
      edges={[{ id: '1', from: 'organization:org', to: 'asset:a', kind: 'contains' }]}
      focusNodeID=""
      nodes={[
        { id: 'organization:org', kind: 'organization', label: 'Org' },
        { id: 'asset:a', kind: 'asset', label: 'A' },
      ]}
      onSelectNode={() => undefined}
      selectedNodeID=""
    />,
  )

  fireEvent.click(screen.getByRole('button', { name: 'Fullscreen' }))
  const dialog = screen.getByRole('dialog', { name: 'Fullscreen relationship graph' })
  expect(within(dialog).getByLabelText(/Zoom/)).toBeInTheDocument()
  expect(within(dialog).getByLabelText(/Spacing/)).toBeInTheDocument()
  expect(within(dialog).getByLabelText(/Gravity/)).toBeInTheDocument()
  expect(within(dialog).getByRole('list', { name: 'Record type legend' })).toBeInTheDocument()
  expect(within(dialog).getByRole('img', { name: 'Interactive relationship graph' })).toBeInTheDocument()
  fireEvent.keyDown(window, { key: 'Escape' })
  expect(screen.queryByRole('dialog', { name: 'Fullscreen relationship graph' })).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Fullscreen' })).toBeInTheDocument()
})

test('hides a type from the legend and can restore it', async () => {
  const onHideKind = vi.fn()
  const onShowKind = vi.fn()
  render(
    <InteractiveRelationshipGraph
      edges={[{ id: '1', from: 'organization:org', to: 'asset:a', kind: 'contains' }]}
      focusNodeID=""
      hiddenKinds={[{ kind: 'person', label: 'Person' }]}
      nodes={[
        { id: 'organization:org', kind: 'organization', label: 'Org' },
        { id: 'asset:a', kind: 'asset', label: 'A' },
      ]}
      onHideKind={onHideKind}
      onSelectNode={() => undefined}
      onShowKind={onShowKind}
      selectedNodeID=""
    />,
  )

  fireEvent.click(screen.getByRole('button', { name: 'Hide Asset from the graph' }))
  expect(onHideKind).toHaveBeenCalledWith('asset')
  fireEvent.click(screen.getByRole('button', { name: 'Show Person' }))
  expect(onShowKind).toHaveBeenCalledWith('person')
})
