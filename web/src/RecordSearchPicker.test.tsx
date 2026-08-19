import { useState } from 'react'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, test, vi } from 'vitest'
import RecordSearchPicker, { type SearchableRecord } from './RecordSearchPicker'

beforeEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

test('lists departments immediately and can create one without a separate search', async () => {
  const onChange = vi.fn()
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    if (String(input) === '/api/v1/departments' && init?.method === 'POST') {
      return new Response(JSON.stringify({ id: 'dept-2', name: 'Studio Arts' }), { status: 201 })
    }
    return new Response(JSON.stringify({ items: [{ id: 'dept-1', name: 'Technology' }] }), { status: 200 })
  }))
  function Harness() {
    const [value, setValue] = useState<SearchableRecord[]>([])
    return <RecordSearchPicker
      create={{
        label: 'Add department',
        fields: [{ key: 'name', label: 'Department name', required: true }],
        submit: async (values) => {
          const response = await fetch('/api/v1/departments', { method: 'POST', body: JSON.stringify({ name: values.name }) })
          const body = await response.json() as { id: string; name: string }
          return { id: body.id, label: body.name }
        },
      }}
      kind="department"
      label="Department"
      multiple={false}
      onChange={(records) => { onChange(records); setValue(records) }}
      options={[{ id: 'dept-1', label: 'Technology' }]}
      selected={value}
    />
  }
  render(<Harness />)
  expect(screen.getByRole('option', { name: /Technology/ })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '+ Add department' }))
  fireEvent.change(screen.getByLabelText('Department name'), { target: { value: 'Studio Arts' } })
  fireEvent.click(screen.getByRole('button', { name: 'Add department' }))
  await waitFor(() => expect(onChange).toHaveBeenCalledWith([expect.objectContaining({ id: 'dept-2', label: 'Studio Arts' })]))
})

test('searches assets and keeps selected records', async () => {
  const onChange = vi.fn()
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    expect(String(input)).toBe('/api/v1/assets?q=lab&limit=20')
    return new Response(JSON.stringify({ items: [{ id: 'lab-1', name: 'Studio Arts Mac Lab Station 001', assetTag: 'RCC-LAB-0001', kind: 'desktop' }] }), { status: 200 })
  }))
  function Harness() {
    const [value, setValue] = useState<SearchableRecord[]>([])
    return <RecordSearchPicker kind="asset" label="Asset IDs" onChange={(records) => { onChange(records); setValue(records) }} selected={value} />
  }
  render(<Harness />)
  fireEvent.change(screen.getByLabelText('Asset IDs'), { target: { value: 'lab' } })
  fireEvent.click(screen.getByRole('button', { name: 'Search' }))
  fireEvent.click(await screen.findByRole('option', { name: /Studio Arts Mac Lab Station 001/ }))
  await waitFor(() => expect(onChange).toHaveBeenCalledWith([expect.objectContaining({ id: 'lab-1' })]))
  expect(screen.getAllByText('Studio Arts Mac Lab Station 001').length).toBeGreaterThan(0)
})
