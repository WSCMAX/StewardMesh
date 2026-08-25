import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import ScanField from './ScanField'

// Requirements: REQ-ATLAS-CODES-001, REQ-ATLAS-MODELS-001. Features: inventory.identifiers, inventory.models.

beforeEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

test('asks which captured barcode is the model number when several are in view', async () => {
  const onChange = vi.fn()
  const stop = vi.fn()
  vi.stubGlobal('navigator', Object.assign(Object.create(navigator), {
    mediaDevices: { getUserMedia: vi.fn(async () => ({ getTracks: () => [{ stop }] }) as unknown as MediaStream) },
  }))
  vi.stubGlobal('BarcodeDetector', class {
    detect = vi.fn(async () => [
      { format: 'code_39', rawValue: '20W5S51T00' },
      { format: 'code_128', rawValue: 'PF41ER5H' },
      { format: 'code_39', rawValue: '43188' },
    ])
  })
  vi.stubGlobal('requestAnimationFrame', vi.fn((callback: FrameRequestCallback) => {
    queueMicrotask(() => callback(1))
    return 21
  }))
  vi.stubGlobal('cancelAnimationFrame', vi.fn())
  vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined)

  function Field() {
    return <ScanField label="Model number" onChange={onChange} parseValue={(value) => value.trim()} value="" />
  }
  render(<Field />)
  fireEvent.click(screen.getByRole('button', { name: 'Scan Model number' }))
  expect(await screen.findByText('Choose the model number')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Use 20W5S51T00' }))
  expect(onChange).toHaveBeenCalledWith('20W5S51T00')
})
