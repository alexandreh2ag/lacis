import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { vi } from 'vitest'
import { RouteBoundary } from './ErrorBoundary'

function Broken({ resources }: { resources: unknown }): React.ReactNode {
  return (resources as { cpu: { cores: number } }).cpu.cores
}

test('a page failing to render says so instead of blanking the front', () => {
  const quiet = vi.spyOn(console, 'error').mockImplementation(() => {})
  render(<MemoryRouter><p>menu</p><RouteBoundary><Broken resources="garbage" /></RouteBoundary></MemoryRouter>)
  expect(screen.getByRole('alert').textContent).toContain('Cette page n\'a pas pu s\'afficher.')
  expect(screen.getByText('menu')).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Réessayer' })).toBeTruthy()
  quiet.mockRestore()
})
