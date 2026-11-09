import { MockedProvider } from '@apollo/client/testing'
import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { GraphFilters, readFolded } from './GraphFilters'
import { useGraphParams } from './useGraphParams'

function Panel() {
  const p = useGraphParams()
  return <GraphFilters ns="acme" p={p} envs={['production']} projects={[]} counts={null} />
}

const mount = () => render(<MockedProvider mocks={[]}><MemoryRouter><Panel /></MemoryRouter></MockedProvider>)

beforeEach(() => localStorage.clear())

test('the panel folds to a rail and unfolds, and remembers it', () => {
  mount()
  expect(screen.getByRole('button', { name: 'production' })).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Replier les filtres' }))
  expect(screen.queryByRole('button', { name: 'production' })).toBeNull()
  expect(readFolded()).toBe(true)

  fireEvent.click(screen.getByRole('button', { name: 'Déplier les filtres' }))
  expect(screen.getByRole('button', { name: 'production' })).toBeTruthy()
  expect(readFolded()).toBe(false)
})

test('a panel left folded stays folded', () => {
  localStorage.setItem('lacis-graph-filters', 'folded')
  mount()
  expect(screen.getByRole('button', { name: 'Déplier les filtres' }).getAttribute('aria-expanded')).toBe('false')
})
