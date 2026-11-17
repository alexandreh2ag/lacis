import { MockedProvider, type MockedResponse } from '@apollo/client/testing'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router'
import { vi } from 'vitest'
import { DeleteHostDocument, type HostStatus } from '@/graphql/graphql'
import { DeleteHost } from './HostPage'

test.each([
  { name: 'an active host asks for its hostname', status: 'ACTIVE' as HostStatus, typed: 'web-1' },
  { name: 'a stale host is deleted on confirmation', status: 'STALE' as HostStatus, typed: null },
])('$name', async ({ status, typed }) => {
  const deleted = vi.fn(() => ({ data: { deleteHost: true } }))
  const mocks: MockedResponse[] = [{ request: { query: DeleteHostDocument, variables: { ns: 'acme', id: 'h1' } }, result: deleted }]
  render(
    <MockedProvider mocks={mocks}>
      <MemoryRouter initialEntries={['/acme/hosts/h1']}>
        <Routes>
          <Route path="/acme/hosts/h1" element={<DeleteHost ns="acme" host={{ id: 'h1', hostname: 'web-1', status }} />} />
          <Route path="/acme/hosts" element={<p>liste</p>} />
        </Routes>
      </MemoryRouter>
    </MockedProvider>,
  )
  fireEvent.click(screen.getByRole('button', { name: 'Supprimer le host' }))
  expect(screen.getByText(/avec son agent/)).toBeTruthy()
  const submit = screen.getByRole('button', { name: 'Supprimer', hidden: true }) as HTMLButtonElement
  if (typed) {
    expect(screen.getByText(/Il est actif/)).toBeTruthy()
    expect(submit.disabled).toBe(true)
    const input = screen.getByLabelText('Tapez le nom du host pour confirmer')
    fireEvent.change(input, { target: { value: 'web-2' } })
    expect(submit.disabled).toBe(true)
    fireEvent.change(input, { target: { value: typed } })
  } else {
    expect(screen.queryByLabelText('Tapez le nom du host pour confirmer')).toBeNull()
  }
  expect(submit.disabled).toBe(false)
  fireEvent.click(submit)
  await waitFor(() => expect(screen.getByText('liste')).toBeTruthy())
  expect(deleted).toHaveBeenCalled()
})
