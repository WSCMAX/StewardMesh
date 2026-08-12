import { fireEvent, render, screen } from '@testing-library/react'
import { expect, test, vi } from 'vitest'
import WorkspaceShell, { workspaceAreaFromHash, workspaceHash, type WorkspaceArea } from './WorkspaceShell'

// Requirement: REQ-WORKSPACE-001. Feature: experience.workspace.

test('accepts only fixed Workspace deep links', () => {
  expect(workspaceAreaFromHash('#workspace-atlas')).toBe('atlas')
  expect(workspaceAreaFromHash('#workspace-guard')).toBe('guard')
  expect(workspaceAreaFromHash('#workspace-unknown')).toBe('overview')
  expect(workspaceAreaFromHash('#guide-atlas')).toBe('overview')
  expect(workspaceHash('people')).toBe('#workspace-people')
})

test('uses ordinary navigation links and reports the requested focused area', () => {
  const onNavigate = vi.fn()
  const areas: WorkspaceArea[] = [
    { id: 'overview', name: 'Overview', descriptor: 'Work queue', summary: 'Start here.', content: <p>Overview content</p> },
    { id: 'atlas', name: 'Atlas', descriptor: 'Asset inventory', summary: 'Manage assets.', permission: 'assets.read', content: <p>Atlas content</p> },
  ]
  render(<WorkspaceShell activeArea="overview" areas={areas} assetCount={0} healthLabel="Connected" onNavigate={onNavigate} onOpenHelp={() => undefined} onReportIssue={() => undefined} roles={['Administrator']} visitedAreas={new Set(['overview'])} />)

  const atlas = screen.getByRole('link', { name: 'Atlas — Asset inventory' })
  expect(atlas).toHaveAttribute('href', '#workspace-atlas')
  fireEvent.click(atlas)
  expect(onNavigate).toHaveBeenCalledWith('atlas')
  expect(screen.getByRole('link', { name: 'Overview — Work queue' })).toHaveAttribute('aria-current', 'page')
})
