import axe from 'axe-core'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { expect, test, vi } from 'vitest'
import GuideExperience, { GuideInvitation, type GuideDestination } from './GuideExperience'
import { resolveBranding } from './guide'

// Requirements: REQ-HORIZON-001, A11Y-001, DOC-001, DOC-002. Features: lifecycle.planning, experience.help.

const branding = resolveBranding({})

function renderGuide(overrides: Partial<Parameters<typeof GuideExperience>[0]> = {}) {
  const props = {
    branding,
    destination: { view: 'help', topic: 'workspace' } as GuideDestination,
    issuesUrl: 'https://github.com/WSCMAX/StewardMesh/issues',
    onClose: vi.fn(),
    onNavigate: vi.fn(),
    onWalkthroughStatus: vi.fn(),
    open: true,
    permissions: ['assets.read'],
    roles: ['Asset reader'],
    version: '1.2.3',
    ...overrides,
  }
  return { ...render(<GuideExperience {...props} />), props }
}

test('renders accessible contextual help with labels, examples, and documentation', async () => {
  const { container } = renderGuide({ destination: { view: 'help', topic: 'atlas' } })
  expect(screen.getByRole('heading', { name: 'What you can do here' })).toBeInTheDocument()
  expect(screen.getByText('Example')).toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'Read Atlas documentation' })).toHaveAttribute('href', expect.stringContaining('/docs/features/atlas.md'))
  expect((await axe.run(container)).violations).toEqual([])
})

test('keeps public help discoverable before protected workspace access is available', () => {
  renderGuide({ destination: { view: 'help', topic: 'guard' }, permissions: [], roles: [] })
  expect(screen.getByRole('combobox', { name: 'Help topic' })).toHaveValue('guard')
  expect(screen.getByRole('link', { name: 'Read Guard documentation' })).toBeInTheDocument()
})

test('keeps walkthrough steps permission-aware, skippable, replayable, and completable', () => {
  const onNavigate = vi.fn()
  const onWalkthroughStatus = vi.fn()
  renderGuide({ destination: { view: 'walkthrough', topic: 'workspace' }, onNavigate, onWalkthroughStatus })

  expect(screen.getByText('Step 1 of 3')).toBeInTheDocument()
  expect(screen.queryByText(/Guard — Authentication and authorization/)).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Next' }))
  expect(onNavigate).toHaveBeenLastCalledWith({ view: 'walkthrough', topic: 'atlas' })
  fireEvent.click(screen.getByRole('button', { name: 'Next' }))
  expect(onNavigate).toHaveBeenLastCalledWith({ view: 'walkthrough', topic: 'guide' })
  fireEvent.click(screen.getByRole('button', { name: 'Finish walkthrough' }))
  expect(onWalkthroughStatus).toHaveBeenCalledWith('completed')
  expect(screen.getByRole('status')).toHaveTextContent('Walkthrough completed')
  fireEvent.click(screen.getByRole('button', { name: 'Skip walkthrough' }))
  expect(onWalkthroughStatus).toHaveBeenCalledWith('skipped')
  fireEvent.click(screen.getByRole('button', { name: 'Replay from start' }))
  expect(onNavigate).toHaveBeenLastCalledWith({ view: 'walkthrough', topic: 'workspace' })
})

test('announces unsafe branding fallback and lists blocked contrast checks', () => {
  const unsafe = resolveBranding({ darkCanvas: '#FFFFFF', darkSurface: '#FFFFFF', textOnDark: '#FFFFFF' })
  renderGuide({ branding: unsafe, destination: { view: 'accessibility', topic: 'guide' } })
  expect(screen.getByRole('status')).toHaveTextContent('Unsafe branding was blocked')
  expect(screen.getAllByText(/Blocked/).length).toBeGreaterThan(0)
  expect(screen.getByText(/Never use color alone/)).toBeInTheDocument()
})

test('prepares a sanitized issue URL without identity or session data', () => {
  window.history.replaceState(null, '', '/people?email=private@example.test')
  renderGuide({ destination: { view: 'report', topic: 'people' }, roles: ['Administrator'] })
  const report = new URL(screen.getByRole('link', { name: 'Review issue before submitting' }).getAttribute('href') ?? '')
  const body = report.searchParams.get('body') ?? ''
  expect(body).toContain('Page: /people')
  expect(body).toContain('Component: People')
  expect(body).not.toContain('private@example.test')
  expect(body).not.toContain('Administrator')
  expect(body).not.toContain('csrf')
})

test('closes on Escape and restores focus to the opener', async () => {
  const opener = document.createElement('button')
  opener.textContent = 'Open Guide'
  document.body.append(opener)
  opener.focus()
  const onClose = vi.fn()
  renderGuide({ onClose })
  await waitFor(() => expect(screen.getByRole('button', { name: 'Close Guide' })).toHaveFocus())
  fireEvent.keyDown(window, { key: 'Escape' })
  expect(onClose).toHaveBeenCalledOnce()
  await waitFor(() => expect(opener).toHaveFocus())
  opener.remove()
})

test('closes contextual help and focuses the selected in-page section', async () => {
  const target = document.createElement('div')
  target.id = 'guide-atlas'
  document.body.append(target)
  target.scrollIntoView = vi.fn()
  const onClose = vi.fn()
  renderGuide({ destination: { view: 'help', topic: 'atlas' }, onClose })
  fireEvent.click(screen.getByRole('link', { name: 'Go to Atlas' }))
  expect(onClose).toHaveBeenCalledOnce()
  await waitFor(() => expect(target).toHaveFocus())
  expect(target.scrollIntoView).toHaveBeenCalledWith({ block: 'start' })
  target.remove()
})

test('offers a nonblocking invitation that can be started or skipped', () => {
  const onNavigate = vi.fn()
  const onWalkthroughStatus = vi.fn()
  render(<GuideInvitation onNavigate={onNavigate} onWalkthroughStatus={onWalkthroughStatus} roles={['Administrator']} status="new" />)
  fireEvent.click(screen.getByRole('button', { name: 'Start walkthrough' }))
  expect(onNavigate).toHaveBeenCalledWith({ view: 'walkthrough', topic: 'workspace' })
  fireEvent.click(screen.getByRole('button', { name: 'Skip for now' }))
  expect(onWalkthroughStatus).toHaveBeenCalledWith('skipped')
})
