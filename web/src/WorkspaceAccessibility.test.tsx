import axe from 'axe-core'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, test } from 'vitest'
import WorkspaceShell, { type WorkspaceArea } from './WorkspaceShell'

// Requirements: REQ-WORKSPACE-001, A11Y-001. Feature: experience.workspace.

afterEach(cleanup)

const scenarios = [
  {
    name: 'populated',
    health: 'Connected',
    content: <section aria-labelledby="populated-heading"><h3 id="populated-heading">Visible records</h3><ul><li>Managed laptop</li></ul></section>,
    expected: 'Managed laptop',
  },
  {
    name: 'empty',
    health: 'Connected',
    content: <p role="status">No records match this view.</p>,
    expected: 'No records match this view.',
  },
  {
    name: 'permission denied',
    health: 'Connected',
    content: <section aria-labelledby="denied-heading"><h3 id="denied-heading">Atlas data is protected</h3><p>Ask an administrator for <code>assets.read</code>.</p></section>,
    expected: 'Atlas data is protected',
  },
  {
    name: 'feature error',
    health: 'Unavailable',
    content: <div role="alert">The asset list could not be loaded.</div>,
    expected: 'The asset list could not be loaded.',
  },
] as const

describe.each(scenarios)('Workspace $name state', ({ content, expected, health }) => {
  test('keeps state text explicit and has no automated accessibility violations', async () => {
    const areas: WorkspaceArea[] = [
      {
        id: 'atlas',
        name: 'Atlas',
        descriptor: 'Asset inventory',
        summary: 'Register and locate organization-owned assets.',
        permission: 'assets.read',
        writePermission: 'assets.write',
        content,
      },
    ]
    const { container } = render(
      <WorkspaceShell
        activeArea="atlas"
        areas={areas}
        assetCount={health === 'Connected' ? 1 : 0}
        healthLabel={health}
        onNavigate={() => undefined}
        onOpenHelp={() => undefined}
        onReportIssue={() => undefined}
        roles={['Reader with an intentionally long role name that must reflow at narrow widths']}
        visitedAreas={new Set(['atlas'])}
      />,
    )

    expect(screen.getByText(expected)).toBeVisible()
    if (health === 'Unavailable') expect(screen.getByText('Service unavailable.')).toBeVisible()
    expect((await axe.run(container)).violations).toEqual([])
  })
})
