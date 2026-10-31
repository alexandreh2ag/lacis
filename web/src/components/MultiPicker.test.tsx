import { fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { MultiPicker, PickerShown } from './MultiPicker'

const projects = Array.from({ length: 14 }, (_, i) => `projet-${i + 1}`)

function Picker({ initial = [] as string[] }) {
  const [selected, setSelected] = useState(initial)
  return <MultiPicker label="Projet" options={projects} selected={selected} onChange={setSelected} />
}

test('lists ten options and asks for a search past them', () => {
  render(<Picker />)
  fireEvent.click(screen.getByRole('button', { name: /Projet/ }))
  expect(screen.getAllByRole('option')).toHaveLength(PickerShown)
  expect(screen.getByRole('status').textContent).toContain('10 affichés sur 14')
  fireEvent.change(screen.getByRole('combobox'), { target: { value: 'projet-1' } })
  expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual(['projet-1', 'projet-10', 'projet-11', 'projet-12', 'projet-13', 'projet-14'])
})

test('adds what is picked as a chip, and takes it out', () => {
  render(<Picker initial={['projet-2']} />)
  fireEvent.click(screen.getByRole('button', { name: /^Projet/ }))
  const search = screen.getByRole('combobox')
  fireEvent.change(search, { target: { value: '-3' } })
  fireEvent.keyDown(search, { key: 'Enter' })
  expect(screen.getByRole('button', { name: 'Retirer projet-3' })).toBeTruthy()
  fireEvent.change(search, { target: { value: '' } })
  expect(screen.getAllByRole('option').map((o) => o.textContent)).not.toContain('projet-2')
  fireEvent.click(screen.getByRole('button', { name: 'Retirer projet-2' }))
  expect(screen.queryByRole('button', { name: 'Retirer projet-2' })).toBeNull()
})
