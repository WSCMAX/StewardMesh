import { expect, test } from 'vitest'
import {
  atlasInventoryCounts,
  annotateCampusAttributes,
  colorsForNode,
  defaultKindColorKey,
  denseGraphNodeThreshold,
  formatAtlasInventorySummary,
  graphPaletteColor,
  graphRecordLimits,
  graphTypePalette,
  identityKindHubsFor,
  kindGraphColor,
  meshKindColorKeys,
  nodePassesCampusFilters,
  occupancyRoleHubsFor,
  occupancyRolesFor,
  parseKindColorOverrides,
  applyOverlayGroups,
  sourceGroupKind,
  sourceHubID,
  sourceHubsFor,
  withoutUnlinkedNodes,
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

test('withoutUnlinkedNodes keeps only records that appear on an edge', () => {
  const result = withoutUnlinkedNodes(
    [
      { id: 'purchase_order:po-1' },
      { id: 'vendor:vendor-1' },
      { id: 'person:ada' },
    ],
    [{ from: 'purchase_order:po-1', to: 'vendor:vendor-1' }],
  )
  expect(result.nodes.map((node) => node.id)).toEqual(['purchase_order:po-1', 'vendor:vendor-1'])
  expect(result.edges).toHaveLength(1)
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

test('campus attributes color people by occupancy with a gradient and assets by model', () => {
  const nodes = annotateCampusAttributes(
    [
      { id: 'person:ada', kind: 'person', label: 'Ada', attributes: { status: 'active' } as Record<string, string> },
      { id: 'asset:mac', kind: 'asset', label: 'Lab Mac', attributes: { status: 'active' } as Record<string, string> },
      { id: 'model:m1', kind: 'model', label: 'MacBook Pro 14', attributes: { status: 'active' } as Record<string, string> },
      { id: 'room:studio', kind: 'room', label: 'Studio' },
      { id: 'room:hall', kind: 'room', label: 'Hall' },
    ],
    [
      { id: 'e1', from: 'person:ada', to: 'room:studio', kind: 'teaches_in' },
      { id: 'e2', from: 'person:ada', to: 'room:hall', kind: 'attends_class' },
      { id: 'e3', from: 'asset:mac', to: 'model:m1', kind: 'modeled_as' },
    ],
  )
  const ada = nodes.find((node) => node.id === 'person:ada')
  const asset = nodes.find((node) => node.id === 'asset:mac')
  expect(occupancyRolesFor('person:ada', [
    { from: 'person:ada', to: 'room:studio', kind: 'teaches_in' },
    { from: 'person:ada', to: 'room:hall', kind: 'attends_class' },
  ])).toEqual(['instructor', 'student'])
  expect(ada?.attributes?.roles).toBe('instructor,student')
  expect(asset?.attributes?.model).toBe('MacBook Pro 14')
  const paint = colorsForNode('person', ada?.attributes, 'campus')
  expect(paint.fills).toHaveLength(2)
  expect(paint.fills?.[0]).toBe(graphPaletteColor('violet').stroke)
  expect(paint.fills?.[1]).toBe(graphPaletteColor('sky').stroke)
  expect(colorsForNode('asset', asset?.attributes, 'campus').stroke).not.toBe(graphPaletteColor(meshKindColorKeys.asset).stroke)
})

test('occupancy and identity hubs group people by user type', () => {
  const nodes = [
    { id: 'person:ada', kind: 'person', label: 'Ada' },
    { id: 'person:linus', kind: 'person', label: 'Linus' },
    { id: 'shared:help', kind: 'shared', label: 'Help Desk' },
  ]
  const edges = [
    { id: 'e1', from: 'person:ada', to: 'room:studio', kind: 'teaches_in' },
    { id: 'e2', from: 'person:linus', to: 'room:hall', kind: 'attends_class' },
  ]
  const roles = occupancyRoleHubsFor(nodes, edges)
  expect(roles.map((group) => group.label).sort()).toEqual(['Instructors', 'Students'])
  expect(identityKindHubsFor(nodes).map((group) => group.label).sort()).toEqual(['Person', 'Shared identity'])
})

test('campus status filters hide inactive people and retired assets', () => {
  expect(nodePassesCampusFilters({ kind: 'person', attributes: { status: 'inactive' } }, { hideInactivePeople: true })).toBe(false)
  expect(nodePassesCampusFilters({ kind: 'person', attributes: { status: 'active' } }, { hideInactivePeople: true })).toBe(true)
  expect(nodePassesCampusFilters({ kind: 'asset', attributes: { status: 'retired' } }, { hideInactiveAssets: true })).toBe(false)
  expect(nodePassesCampusFilters({ kind: 'asset', attributes: { status: 'active' } }, { hideInactiveAssets: true })).toBe(true)
})
