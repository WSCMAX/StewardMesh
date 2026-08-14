import { beforeEach, expect, test } from 'vitest'
import {
  buildIssueReportUrl,
  collectIssueContext,
  contrastRatio,
  defaultBrandTheme,
  detectBrowser,
  detectSystem,
  readWalkthroughStatus,
  resolveBranding,
  writeWalkthroughStatus,
} from './guide'

// Requirements: REQ-WORKSPACE-001, REQ-HORIZON-001, A11Y-001, DOC-001, DOC-002. Features: experience.workspace, lifecycle.planning, experience.help.

beforeEach(() => {
  localStorage.clear()
  window.history.replaceState(null, '', '/assets?token=private#details')
})

test('validates WCAG contrast and keeps the verified default palette active', () => {
  expect(contrastRatio('#000000', '#FFFFFF')).toBeCloseTo(21, 4)
  const branding = resolveBranding({})
  expect(branding.usedFallback).toBe(false)
  expect(branding.validation.blocked).toBe(false)
  expect(branding.validation.checks.every((check) => check.passed)).toBe(true)
})

test('blocks invalid and low-contrast branding before it is applied', () => {
  const branding = resolveBranding({
    darkCanvas: '#FFFFFF',
    darkSurface: '#FFFFFF',
    textOnDark: '#FFFFFF',
    primary: 'not-a-color',
  })
  expect(branding.validation.blocked).toBe(true)
  expect(branding.validation.invalidColors).toContain('primary')
  expect(branding.usedFallback).toBe(true)
  expect(branding.appliedTheme).toEqual(defaultBrandTheme)
  expect(branding.validation.warnings[0]).toMatch(/Never use color alone/)
})

test('builds a prefilled report from allow-listed sanitized context', () => {
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: 320 })
  Object.defineProperty(window, 'innerHeight', { configurable: true, value: 640 })
  const context = collectIssueContext('People <private@example.test>', '1.2.3<script>', 'request-123')
  const report = new URL(buildIssueReportUrl('https://github.com/WSCMAX/StewardMesh/issues', context))
  const body = report.searchParams.get('body') ?? ''

  expect(context.page).toBe('/assets')
  expect(context.component).toBe('Workspace')
  expect(context.version).toBe('development')
  expect(context.viewport).toBe('320x640')
  expect(report.pathname).toBe('/WSCMAX/StewardMesh/issues/new')
  expect(body).toContain('Correlation ID: request-123')
  expect(body).not.toContain('token=private')
  expect(body).not.toContain('private@example.test')
  expect(body).not.toContain('<script>')
})

test('rejects unsafe correlation values and reports coarse browser and system versions', () => {
  expect(collectIssueContext('Guide', 'development', 'secret value\nCookie: abc').correlationId).toBe('Unavailable')
  expect(detectBrowser('Mozilla/5.0 Chrome/140.0.0.0 Safari/537.36')).toBe('Chrome 140')
  expect(detectSystem('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)')).toBe('macOS')
})

test('persists only the walkthrough preference states', () => {
  expect(readWalkthroughStatus()).toBe('new')
  writeWalkthroughStatus('skipped')
  expect(readWalkthroughStatus()).toBe('skipped')
  localStorage.setItem('stewardmesh.guide.walkthrough.v1', 'unexpected')
  expect(readWalkthroughStatus()).toBe('new')
})
