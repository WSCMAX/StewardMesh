import { expect, test } from 'vitest'
import { documentationByID, documentationHref, documentationPages, documentationTopicFromHash, searchDocumentation } from './documentation'

// Requirements: A11Y-001, DOC-001. Feature: experience.help.

test('creates fixed same-host documentation deep links', () => {
  expect(documentationHref('atlas')).toBe('#docs/atlas')
  expect(documentationTopicFromHash('#docs/guard')).toBe('guard')
  expect(documentationTopicFromHash('#docs/not-a-topic')).toBe('overview')
  expect(documentationTopicFromHash('#workspace-atlas')).toBeNull()
})

test('keeps every local documentation page complete and connected', () => {
  expect(documentationPages).toHaveLength(12)
  for (const page of documentationPages) {
    expect(page.sections.length).toBeGreaterThan(0)
    expect(page.related.length).toBeGreaterThan(0)
    expect(page.appHref).toMatch(/^#workspace-/)
    expect(page.related.every((related) => Boolean(documentationByID[related]))).toBe(true)
  }
})

test('searches titles, summaries, and product vocabulary', () => {
  expect(searchDocumentation('budget').map((page) => page.id)).toContain('ledger')
  expect(searchDocumentation('entitlement').map((page) => page.id)).toContain('stack')
  expect(searchDocumentation('renewal').map((page) => page.id)).toContain('signals')
  expect(searchDocumentation('barcode').map((page) => page.id)).toEqual(['atlas'])
  expect(searchDocumentation('scoped grants').map((page) => page.id)).toContain('guard')
  expect(searchDocumentation('')).toEqual(documentationPages)
})
