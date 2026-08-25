import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import WorkspaceShell, { workspaceAreaFromHash, type WorkspaceArea } from './WorkspaceShell'

// Requirement: REQ-WORKSPACE-001. Feature: experience.workspace.

beforeEach(() => {
  window.localStorage.clear()
})

test('accepts only fixed Workspace deep links', () => {
  expect(workspaceAreaFromHash('#workspace-atlas')).toBe('atlas')
  expect(workspaceAreaFromHash('#workspace-guard')).toBe('guard')
  expect(workspaceAreaFromHash('#workspace-signals')).toBe('signals')
  expect(workspaceAreaFromHash('#workspace-exchange')).toBe('exchange')
  expect(workspaceAreaFromHash('#workspace-unknown')).toBe('overview')
  expect(workspaceAreaFromHash('#guide-atlas')).toBe('overview')
  expect(workspaceAreaFromHash('#workspace-people')).toBe('people')
  expect(workspaceAreaFromHash('#workspace-mesh')).toBe('mesh')
  expect(workspaceAreaFromHash('#workspace-mesh?node=asset%3Alab-1')).toBe('mesh')
})

test('uses ordinary navigation links and reports the requested focused area', () => {
  const onNavigate = vi.fn()
  const areas: WorkspaceArea[] = [
    { id: 'overview', name: 'Overview', descriptor: 'Work queue', summary: 'Start here.', content: <p>Overview content</p> },
    { id: 'atlas', name: 'Inventory', descriptor: 'Atlas · assets and equipment', summary: 'Manage assets.', permission: 'assets.read', content: <p>Atlas content</p> },
  ]
  render(<WorkspaceShell activeArea="overview" areas={areas} assetCount={0} healthLabel="Connected" onNavigate={onNavigate} onOpenHelp={() => undefined} onReportIssue={() => undefined} roles={['Administrator']} visitedAreas={new Set(['overview'])} />)

  const atlas = screen.getByRole('link', { name: 'Inventory — Atlas · assets and equipment' })
  expect(atlas).toHaveAttribute('href', '#workspace-atlas')
  fireEvent.click(atlas)
  expect(onNavigate).toHaveBeenCalledWith('atlas')
  expect(screen.getByRole('link', { name: 'Overview — Work queue' })).toHaveAttribute('aria-current', 'page')
})

test('opens mobile navigation as a modal and restores focus after Escape', async () => {
  const areas: WorkspaceArea[] = [
    { id: 'overview', name: 'Overview', descriptor: 'Work queue', summary: 'Start here.', content: <p>Overview content</p> },
    { id: 'atlas', name: 'Inventory', descriptor: 'Atlas · assets and equipment', summary: 'Manage assets.', permission: 'assets.read', content: <p>Atlas content</p> },
  ]
  render(<WorkspaceShell activeArea="overview" areas={areas} assetCount={0} healthLabel="Connected" onNavigate={() => undefined} onOpenHelp={() => undefined} onReportIssue={() => undefined} roles={['Administrator']} visitedAreas={new Set(['overview'])} />)

  const opener = screen.getByRole('button', { name: 'Open workspace navigation' })
  fireEvent.click(opener)
  const dialog = screen.getByRole('dialog', { name: 'Workspace navigation' })
  expect(dialog).toHaveAttribute('aria-modal', 'true')
  const close = screen.getByRole('button', { name: 'Close workspace navigation' })
  const report = screen.getAllByRole('button', { name: 'Report an issue' }).at(-1)
  await waitFor(() => expect(close).toHaveFocus())
  fireEvent.keyDown(window, { key: 'Tab', shiftKey: true })
  expect(report).toHaveFocus()
  fireEvent.keyDown(window, { key: 'Tab' })
  expect(close).toHaveFocus()

  fireEvent.keyDown(window, { key: 'Escape' })
  expect(screen.queryByRole('dialog', { name: 'Workspace navigation' })).not.toBeInTheDocument()
  await waitFor(() => expect(opener).toHaveFocus())
})

test('collapses desktop navigation to an icon rail and remembers the choice', () => {
  const areas: WorkspaceArea[] = [
    { id: 'overview', name: 'Overview', descriptor: 'Work queue', summary: 'Start here.', content: <p>Overview content</p> },
    { id: 'atlas', name: 'Inventory', descriptor: 'Atlas · assets and equipment', summary: 'Manage assets.', permission: 'assets.read', content: <p>Atlas content</p> },
  ]
  render(<WorkspaceShell activeArea="overview" areas={areas} assetCount={0} healthLabel="Connected" onNavigate={() => undefined} onOpenHelp={() => undefined} onReportIssue={() => undefined} roles={['Administrator']} visitedAreas={new Set(['overview'])} />)

  expect(screen.getByRole('heading', { name: 'Overview' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Expand workspace navigation' })).toHaveAttribute('aria-expanded', 'false')
  expect(screen.getByRole('link', { name: 'Inventory — Atlas · assets and equipment' })).toBeInTheDocument()

  fireEvent.click(screen.getByRole('button', { name: 'Expand workspace navigation' }))
  expect(screen.getByRole('button', { name: 'Collapse workspace navigation' })).toHaveAttribute('aria-expanded', 'true')
  expect(window.localStorage.getItem('stewardmesh.workspace.navExpanded')).toBe('1')

  fireEvent.click(screen.getByRole('button', { name: 'Collapse workspace navigation' }))
  expect(window.localStorage.getItem('stewardmesh.workspace.navExpanded')).toBe('0')
})
