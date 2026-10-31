import { fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { chipsField, FilterBar } from './FilterBar'

const envs = [{ value: 'production', label: 'production' }, { value: 'staging', label: 'staging' }]

function Bar() {
  const [env, setEnv] = useState<string[]>([])
  const [status, setStatus] = useState<string[]>(['STALE'])
  return (
    <FilterBar
      fields={[
        chipsField({ label: 'Environnement', short: 'Env', options: envs, selected: env, onChange: setEnv }),
        chipsField({ label: 'Statut', options: [{ value: 'STALE', label: 'stale' }], selected: status, onChange: setStatus }),
      ]}
      onClearAll={() => { setEnv([]); setStatus([]) }}
      count="3 hosts"
    />
  )
}

test('the panel is closed until asked; a filter set shows as a pill', () => {
  render(<Bar />)
  expect(screen.queryByRole('dialog')).toBeNull()
  expect(screen.getByText('Statut :').parentElement?.textContent).toContain('stale')
  const button = screen.getByRole('button', { name: /Filtres/ })
  expect(button.textContent).toContain('1')
  fireEvent.click(button)
  fireEvent.click(screen.getByRole('button', { name: 'production' }))
  expect(screen.getByText('Env :').parentElement?.textContent).toContain('production')
  expect(button.textContent).toContain('2')
  fireEvent.keyDown(document, { key: 'Escape' })
  expect(screen.queryByRole('dialog')).toBeNull()
})

test('a pill takes its filter out, and all of them go at once', () => {
  render(<Bar />)
  fireEvent.click(screen.getByRole('button', { name: 'Retirer le filtre Statut' }))
  expect(screen.queryByText('Statut :')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: /Filtres/ }))
  fireEvent.click(screen.getByRole('button', { name: 'staging' }))
  fireEvent.click(screen.getByRole('button', { name: 'production' }))
  fireEvent.click(screen.getAllByRole('button', { name: 'Tout effacer' })[0])
  expect(screen.queryByText('Env :')).toBeNull()
})
