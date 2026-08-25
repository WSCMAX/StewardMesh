import { fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { expect, test } from 'vitest'
import QueryBuilder from './QueryBuilder'
import { emptyQuery, encodeQuery, type QueryField, type QueryValueOption } from './queryLanguage'

// Requirements: REQ-ATLAS-001, REQ-WORKSPACE-001. Feature: experience.grid.

const fields: QueryField[] = [
  { key: 'name', header: 'Asset name', kind: 'text' },
  { key: 'modelId', header: 'Model', kind: 'lookup' },
]

const modelOptions: QueryValueOption[] = [
  { value: 'm1', label: 'Lenovo ThinkPad T14', count: 3 },
  { value: 'm2', label: 'Apple MacBook Air', count: 1 },
]

function Harness({ onEncoded }: { onEncoded: (encoded: string) => void }) {
  const [model, setModel] = useState(emptyQuery)
  const [encoded, setEncoded] = useState('')
  return (
    <QueryBuilder
      encoded={encoded}
      fields={fields}
      model={model}
      onEncodedChange={(value) => { setEncoded(value); onEncoded(value) }}
      onModelChange={(next) => {
        setModel(next)
        const value = encodeQuery(next)
        setEncoded(value)
        onEncoded(value)
      }}
      optionsForField={(field) => field.key === 'modelId' ? modelOptions : []}
    />
  )
}

function renderBuilder() {
  let encoded = ''
  render(<Harness onEncoded={(value) => { encoded = value }} />)
  fireEvent.change(screen.getByLabelText('Field'), { target: { value: 'modelId' } })
  return { encoded: () => encoded }
}

test('the "is" value box suggests unique values and stores the picked canonical value', () => {
  const { encoded } = renderBuilder()
  const valueBox = screen.getByRole('combobox', { name: 'Value for Model' })
  fireEvent.focus(valueBox)
  fireEvent.change(valueBox, { target: { value: 'think' } })
  // Free text still applies while typing.
  expect(encoded()).toBe('modelId=think')
  expect(screen.queryByRole('option', { name: /MacBook/ })).toBeNull()
  fireEvent.click(screen.getByRole('option', { name: /Lenovo ThinkPad T14/ }))
  expect(encoded()).toBe('modelId=m1')
  expect(valueBox).toHaveValue('Lenovo ThinkPad T14')
})

test('the "is one of" picker searches the unique list and builds the value set', () => {
  const { encoded } = renderBuilder()
  fireEvent.change(screen.getByLabelText('Operator'), { target: { value: 'in' } })
  fireEvent.click(screen.getByRole('button', { name: 'Add to Values for Model' }))
  const search = screen.getByLabelText('Search values')
  fireEvent.change(search, { target: { value: 'apple' } })
  expect(screen.queryByRole('option', { name: /ThinkPad/ })).toBeNull()
  fireEvent.click(screen.getByRole('option', { name: /Apple MacBook Air/ }))
  expect(encoded()).toBe('modelIdINm2')
  fireEvent.change(search, { target: { value: '' } })
  fireEvent.click(screen.getByRole('option', { name: /Lenovo ThinkPad T14/ }))
  expect(encoded()).toBe('modelIdIN"m2, m1"')
  // Chips show labels and can be removed.
  fireEvent.click(screen.getByRole('button', { name: 'Remove Apple MacBook Air' }))
  expect(encoded()).toBe('modelIdINm1')
})
