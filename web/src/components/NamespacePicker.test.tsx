import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { matching, NamespacePicker, PickerLimit } from './NamespacePicker'

const many = Array.from({ length: 14 }, (_, i) => ({ id: String(i), slug: `client-${i + 1}`, name: `Client ${i + 1}` }))

test('matching', () => {
  const ns = [{ id: '1', slug: 'acme', name: 'Acme Corp' }, { id: '2', slug: 'globex', name: 'Globex' }]
  expect(matching(ns, '')).toEqual(ns)
  expect(matching(ns, ' CORP ')).toEqual([ns[0]])
  expect(matching(ns, 'glob')).toEqual([ns[1]])
  expect(matching(ns, 'nope')).toEqual([])
})

test('lists the first namespaces and asks for a search past the limit', () => {
  render(<MemoryRouter><NamespacePicker namespaces={many} current="client-1" /></MemoryRouter>)
  fireEvent.click(screen.getByRole('button', { name: /client-1/ }))
  expect(screen.getAllByRole('option')).toHaveLength(PickerLimit)
  expect(screen.getByRole('status').textContent).toContain('10 namespaces affichés sur 14')

  fireEvent.change(screen.getByRole('combobox'), { target: { value: 'client-1' } })
  expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual(
    ['client-1Client 1', 'client-10Client 10', 'client-11Client 11', 'client-12Client 12', 'client-13Client 13', 'client-14Client 14'])
  expect(screen.queryByRole('status')).toBeNull()
})

test('says nothing of a limit it does not reach', () => {
  render(<MemoryRouter><NamespacePicker namespaces={many.slice(0, 3)} /></MemoryRouter>)
  fireEvent.click(screen.getByRole('button', { name: /namespace/ }))
  expect(screen.getAllByRole('option')).toHaveLength(3)
  expect(screen.queryByRole('status')).toBeNull()
})
