import { render, screen } from '@testing-library/react'
import { expect, test } from 'vitest'
import App, { resolvePublicUrl } from './App'

test('renders StewardMesh modules', () => {
  render(<App />)
  expect(screen.getByRole('heading', { name: 'StewardMesh' })).toBeInTheDocument()
  expect(screen.getByText('Atlas — Asset inventory')).toBeInTheDocument()
})

test('allows only safe configurable public links', () => {
  expect(resolvePublicUrl('javascript:alert(1)')).toBe('https://github.com/WSCMAX/StewardMesh/issues')
  expect(resolvePublicUrl('/support/issues')).toBe('/support/issues')
  expect(resolvePublicUrl('https://issues.example.org/project')).toBe('https://issues.example.org/project')
})
