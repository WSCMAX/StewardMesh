import { expect, test } from 'vitest'
import { documentationByID, documentationHref, documentationPages, documentationTopicFromHash, searchDocumentation } from './documentation'

// Requirements: A11Y-001, DOC-001, REQ-DIRECTORY-EXPANSION-005, REQ-DIRECTORY-EXPANSION-006, REQ-DIRECTORY-EXPANSION-007, REQ-DIRECTORY-EXPANSION-008, REQ-EXCHANGE-001. Features: experience.help, platform.foundation, integrations.protocols, threads.relationships, migration.packages.

test('creates fixed same-host documentation deep links', () => {
  expect(documentationHref('atlas')).toBe('#docs/atlas')
  expect(documentationTopicFromHash('#docs/guard')).toBe('guard')
  expect(documentationTopicFromHash('#docs/not-a-topic')).toBe('overview')
  expect(documentationTopicFromHash('#workspace-atlas')).toBeNull()
})

test('keeps every local documentation page complete and connected', () => {
  expect(documentationPages).toHaveLength(16)
  for (const page of documentationPages) {
    expect(page.sections.length).toBeGreaterThan(0)
    expect(page.related.length).toBeGreaterThan(0)
    expect(page.appHref).toMatch(/^#workspace-/)
    expect(page.related.every((related) => Boolean(documentationByID[related]))).toBe(true)
  }
})

test('documents the optional read-only Grouper workflow', () => {
  const results = searchDocumentation('grouper')
  expect(results.map((page) => page.id)).toContain('people')
  expect(documentationByID.people.sections.some((section) => section.id === 'grouper-sync')).toBe(true)
})

test('documents strict opt-in synthetic demo setup', () => {
  expect(searchDocumentation('synthetic demo').map((page) => page.id)).toContain('people')
  expect(documentationByID.people.sections.some((section) => section.id === 'synthetic-demo')).toBe(true)
})

test('documents the People spreadsheet and Mesh relationship graph', () => {
  expect(searchDocumentation('spreadsheet').map((page) => page.id)).toContain('people')
  expect(documentationByID.people.sections.some((section) => section.id === 'spreadsheet')).toBe(true)
  expect(searchDocumentation('relationship graph').map((page) => page.id)).toEqual(expect.arrayContaining(['people', 'mesh']))
  const section = documentationByID.people.sections.find((candidate) => candidate.id === 'relationship-graph')
  expect(section?.paragraphs?.join(' ')).toContain('Open Mesh')
  expect(section?.callout?.body).toContain('do not accept an organization')
  expect(documentationByID.people.sections.some((candidate) => candidate.id === 'location-references')).toBe(true)
  expect(searchDocumentation('dormitory').map((page) => page.id)).toContain('people')
})

test('documents the cross-product Mesh graph', () => {
  expect(searchDocumentation('mesh graph').map((page) => page.id)).toContain('mesh')
  expect(searchDocumentation('cross-product graph').map((page) => page.id)).toContain('mesh')
  expect(searchDocumentation('unlinked').map((page) => page.id)).toContain('mesh')
  expect(searchDocumentation('Show in Mesh').map((page) => page.id)).toContain('mesh')
  expect(searchDocumentation('occupancy').map((page) => page.id)).toContain('mesh')
  expect(documentationByID.mesh.sections.find((section) => section.id === 'graph')?.bullets?.join(' ')).toContain('Show unlinked records')
  expect(documentationByID.mesh.sections.find((section) => section.id === 'graph')?.callout?.body).toContain('finance-only')
})

test('documents the optional read-only PeopleSoft workflow', () => {
  const results = searchDocumentation('campus solutions')
  expect(results.map((page) => page.id)).toContain('people')
  expect(documentationByID.people.sections.some((section) => section.id === 'peoplesoft-sync')).toBe(true)
})

test('documents the Riverside campus demo workflow', () => {
  const results = searchDocumentation('campus demo')
  expect(results.map((page) => page.id)).toContain('people')
  expect(documentationByID.people.sections.some((section) => section.id === 'campus-demo')).toBe(true)
})

test('documents People checkout, reservations, and bulk loans', () => {
  expect(searchDocumentation('checkout').map((page) => page.id)).toContain('people')
  expect(searchDocumentation('reservation').map((page) => page.id)).toContain('people')
  expect(searchDocumentation('bulk checkout').map((page) => page.id)).toContain('people')
  expect(documentationByID.people.sections.find((section) => section.id === 'assignments')?.bullets?.join(' ')).toContain('group checkout')
})

test('documents Horizon replacement plans and forecast views', () => {
  expect(searchDocumentation('replacement plan').map((page) => page.id)).toContain('horizon')
  expect(searchDocumentation('due now').map((page) => page.id)).toContain('horizon')
  expect(documentationByID.horizon.sections.find((section) => section.id === 'forecast')?.steps?.some((step) => step.body.includes('named replacement plan'))).toBe(true)
})

test('searches titles, summaries, and product vocabulary', () => {
  expect(searchDocumentation('budget').map((page) => page.id)).toContain('ledger')
  expect(searchDocumentation('entitlement').map((page) => page.id)).toContain('stack')
  expect(searchDocumentation('renewal').map((page) => page.id)).toContain('signals')
  expect(searchDocumentation('barcode').map((page) => page.id)).toEqual(['atlas'])
  expect(searchDocumentation('scoped grants').map((page) => page.id)).toContain('guard')
  expect(searchDocumentation('openinventory').map((page) => page.id)).toContain('exchange')
  expect(searchDocumentation('')).toEqual(documentationPages)
})
