import { fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { AddPicker } from './AddPicker'

const items = Array.from({ length: 15 }, (_, i) => ({ id: `r${i}`, label: `team-${i}-viewer`, detail: i === 3 ? 'Lecture de billing' : null }))

function Picker({ initial = [] as string[], onSubmit = () => {} }) {
  const [selected, setSelected] = useState(initial)
  return (
    <form onSubmit={(e) => { e.preventDefault(); onSubmit() }}>
      <AddPicker label="Rôles" placeholder="Chercher un rôle" items={items} selected={selected} onChange={setSelected} empty="Aucun rôle." />
    </form>
  )
}

test('the search lists 10 roles at most, by code or description', () => {
  render(<Picker />)
  expect(screen.getByText('Aucun rôle.')).toBeTruthy()
  const input = screen.getByRole('combobox')
  fireEvent.focus(input)
  expect(screen.getAllByRole('option')).toHaveLength(10)
  expect(screen.getByRole('status').textContent).toContain('10 affichés sur 15')
  fireEvent.change(input, { target: { value: 'billing' } })
  expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual(['team-3-viewerLecture de billingAjouter'])
})

test('a pick joins the list, Retirer takes it out, Enter never submits', () => {
  const submit = vi.fn()
  render(<Picker initial={['r1']} onSubmit={submit} />)
  expect(screen.getByRole('button', { name: 'Retirer team-1-viewer' })).toBeTruthy()
  const input = screen.getByRole('combobox')
  fireEvent.change(input, { target: { value: 'team-12' } })
  fireEvent.keyDown(input, { key: 'Enter' })
  expect(submit).not.toHaveBeenCalled()
  expect(screen.getByRole('button', { name: 'Retirer team-12-viewer' })).toBeTruthy()
  fireEvent.change(input, { target: { value: 'team-1-' } })
  expect(screen.getByText('Aucun ne correspond.')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Retirer team-1-viewer' }))
  expect(screen.queryByRole('button', { name: 'Retirer team-1-viewer' })).toBeNull()
  fireEvent.mouseDown(screen.getByRole('option', { name: /team-1-viewer/ }))
  expect(screen.getByRole('button', { name: 'Retirer team-1-viewer' })).toBeTruthy()
})
