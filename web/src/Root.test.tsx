import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import Root from './Root'

// Requirements: A11Y-001, DOC-001. Feature: experience.help.

beforeEach(() => {
  window.history.replaceState(null, '', '/')
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

test('serves local documentation before authentication without API requests', () => {
  const fetchMock = vi.fn()
  vi.stubGlobal('fetch', fetchMock)
  window.history.replaceState(null, '', '/#docs/guard')

  render(<Root />)

  expect(screen.getByRole('heading', { level: 1, name: 'Guard' })).toBeInTheDocument()
  expect(screen.getByRole('link', { name: /Open Guard/ })).toHaveAttribute('href', '#workspace-guard')
  expect(fetchMock).not.toHaveBeenCalled()
})

test('updates local documentation when its fixed hash route changes', () => {
  window.history.replaceState(null, '', '/#docs/workspace')
  render(<Root />)
  expect(screen.getByRole('heading', { level: 1, name: 'Workspace' })).toBeInTheDocument()

  window.history.replaceState(null, '', '/#docs/people')
  fireEvent(window, new Event('hashchange'))
  expect(screen.getByRole('heading', { level: 1, name: 'People' })).toBeInTheDocument()
})
