import { render, screen } from '@testing-library/react'
import { expect, test } from 'vitest'
import App from './App'

test('renders StewardMesh modules', () => {
  render(<App />)
  expect(screen.getByRole('heading', { name: 'StewardMesh' })).toBeInTheDocument()
  expect(screen.getByText('Atlas — Asset inventory')).toBeInTheDocument()
})
