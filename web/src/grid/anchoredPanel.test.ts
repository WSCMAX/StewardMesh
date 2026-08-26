import { expect, test } from 'vitest'
import { placeAnchoredPanel } from './anchoredPanel'

// Requirements: REQ-ATLAS-CODES-001, A11Y-001. Feature: experience.grid.

test('places a short panel below the cell when it fits in the viewport', () => {
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: 1200 })
  Object.defineProperty(window, 'innerHeight', { configurable: true, value: 800 })
  const placed = placeAnchoredPanel({ left: 40, top: 80, right: 160, bottom: 112 }, { width: 320, height: 120 })
  expect(placed).toEqual({ x: 40, y: 116, maxHeight: 784 })
})

test('flips a tall camera panel above a last-row cell instead of opening below the viewport', () => {
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: 1200 })
  Object.defineProperty(window, 'innerHeight', { configurable: true, value: 400 })
  const placed = placeAnchoredPanel({ left: 24, top: 340, right: 180, bottom: 372 }, { width: 320, height: 280 })
  expect(placed.y).toBe(56)
  expect(placed.y + 280).toBeLessThanOrEqual(400)
  expect(placed.maxHeight).toBe(384)
})

test('clamps a panel taller than the viewport so it stays fully reachable', () => {
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: 800 })
  Object.defineProperty(window, 'innerHeight', { configurable: true, value: 300 })
  const placed = placeAnchoredPanel({ left: 16, top: 250, right: 120, bottom: 290 }, { width: 320, height: 420 })
  expect(placed.y).toBe(8)
  expect(placed.y + placed.maxHeight).toBeLessThanOrEqual(300)
  expect(placed.maxHeight).toBe(284)
})
