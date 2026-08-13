import axe from 'axe-core'
import { fireEvent, render, screen, within } from '@testing-library/react'
import { expect, test } from 'vitest'
import DocumentationSite from './DocumentationSite'

// Requirements: A11Y-001, DOC-001. Feature: experience.help.

test('renders a complete local Atlas guide without GitHub documentation links', async () => {
  const { container } = render(<DocumentationSite topicID="atlas" />)

  expect(screen.getByRole('heading', { level: 1, name: 'Atlas' })).toBeInTheDocument()
  expect(screen.getByRole('link', { name: /Open Atlas/ })).toHaveAttribute('href', '#workspace-atlas')
  expect(screen.getByRole('heading', { name: 'Reuse product models' })).toBeInTheDocument()
  expect(screen.getByRole('heading', { name: 'Associate barcodes and QR codes' })).toBeInTheDocument()
  expect([...container.querySelectorAll('a')].every((link) => new URL(link.href).origin === window.location.origin)).toBe(true)
  expect((await axe.run(container)).violations).toEqual([])
})

test('filters the documentation navigation with product vocabulary', () => {
  render(<DocumentationSite topicID="overview" />)
  fireEvent.click(screen.getByRole('button', { name: 'Browse documentation' }))
  const topicNavigation = screen.getByRole('complementary', { name: 'Documentation topics' })
  fireEvent.change(within(topicNavigation).getByRole('searchbox', { name: 'Search documentation' }), { target: { value: 'budget' } })

  expect(within(topicNavigation).getByRole('link', { name: /Ledger/ })).toHaveAttribute('href', '#docs/ledger')
  expect(within(topicNavigation).queryByRole('link', { name: /Atlas/ })).not.toBeInTheDocument()
})

test('announces an empty documentation search result', () => {
  render(<DocumentationSite topicID="overview" />)
  fireEvent.click(screen.getByRole('button', { name: 'Browse documentation' }))
  fireEvent.change(screen.getByRole('searchbox', { name: 'Search documentation' }), { target: { value: 'definitely-not-a-stewardmesh-topic' } })
  expect(screen.getByRole('status')).toHaveTextContent('No documentation matches')
})
