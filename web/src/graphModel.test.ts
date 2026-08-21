import { expect, test } from 'vitest'
import {
  atlasInventoryCounts,
  colorsForNode,
  defaultKindColorKey,
  denseGraphNodeThreshold,
  formatAtlasInventorySummary,
  graphPaletteColor,
  graphRecordLimits,
  graphTypePalette,
  kindGraphColor,
  meshKindColorKeys,
  parseKindColorOverrides,
  applyOverlayGroups,
  sourceGroupKind,
  sourceHubID,
  sourceHubsFor,
} from './graphModel'

// Requirement: REQ-DIRECTORY-EXPANSION-008. Feature: threads.relationships.

test('graph type palette entries expose fill and stroke pairs', () => {
  for (const entry of graphTypePalette) {
    expect(entry.fill).toMatch(/^#[0-9a-f]{6}$/i)
    expect(entry.stroke).toMatch(/^#[0-9a-f]{6}$/i)
    expect(entry.label.length).toBeGreaterThan(0)
  }
})

test('mesh kinds resolve default palette colors and accept overrides', () => {
  expect(defaultKindColorKey('purchase_order')).toBe(meshKindColorKeys.purchase_order)
  expect(kindGraphColor('purchase_order').stroke).toBe(graphPaletteColor('orange').stroke)
  expect(colorsForNode('purchase_order', undefined, 'type', { purchase_order: 'mint' }).stroke).toBe(graphPaletteColor('mint').stroke)
})

test('parseKindColorOverrides keeps only known palette keys', () => {
  expect(parseKindColorOverrides({ asset: 'blue', vendor: 'invalid', person: 4 })).toEqual({ asset: 'blue' })
})

test('graph record limits include 2000 and dense layout starts above that threshold', () => {
  expect(graphRecordLimits).toContain('2000')
  expect(denseGraphNodeThreshold).toBe(2000)
})

test('atlas inventory summaries count assets and models from loaded graph nodes', () => {
  const counts = atlasInventoryCounts([
    { kind: 'asset' },
    { kind: 'asset' },
    { kind: 'model' },
    { kind: 'person' },
  ])
  expect(counts).toEqual({ assets: 2, models: 1 })
  expect(formatAtlasInventorySummary(counts)).toBe('2 assets, 1 model')
})

test('product hubs and chart groups overlay as extra nodes and edges', () => {
  const nodes = [
    { id: 'asset:a', kind: 'asset', label: 'A', attributes: { source: 'atlas' } },
    { id: 'person:ada', kind: 'person', label: 'Ada', attributes: { source: 'people' } },
  ]
  const hubs = sourceHubsFor(nodes, ['atlas'])
  expect(hubs).toHaveLength(1)
  expect(hubs[0].label).toBe('Atlas')
  const overlaid = applyOverlayGroups(nodes, [], hubs, sourceGroupKind)
  expect(overlaid.nodes.some((node) => node.id === sourceHubID('atlas'))).toBe(true)
  expect(overlaid.edges).toHaveLength(1)
  expect(overlaid.edges[0].to).toBe(sourceHubID('atlas'))
})
