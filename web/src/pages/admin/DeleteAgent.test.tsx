import { MockedProvider, type MockedResponse } from '@apollo/client/testing'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import { DeleteAgentDocument } from '@/graphql/graphql'
import { DeleteAgent } from './Tokens'

const agent = (host: { id: string; hostname: string; namespace: string; agentVersion: string | null } | null) => ({
  __typename: 'Agent' as const, id: 'a1', createdAt: '2026-11-04T16:53:03Z', lastSeenAt: null, revokedAt: null,
  tokenLabel: null, conflictAt: null, conflictHostnames: null, host: host && { __typename: 'AgentHost' as const, ...host },
})

test.each([
  { name: 'an agent with a host asks for its hostname', host: { id: 'h1', hostname: 'web-1', namespace: 'acme', agentVersion: '0.0.7' }, typed: 'web-1' },
  { name: 'an agent without host is deleted on confirmation', host: null, typed: null },
])('$name', async ({ host, typed }) => {
  const deleted = vi.fn(() => ({ data: { deleteAgent: true } }))
  const close = vi.fn()
  const mocks: MockedResponse[] = [{ request: { query: DeleteAgentDocument, variables: { id: 'a1' } }, result: deleted }]
  render(<MockedProvider mocks={mocks}><DeleteAgent agent={agent(host)} onClose={close} /></MockedProvider>)
  const submit = screen.getByRole('button', { name: 'Supprimer', hidden: true }) as HTMLButtonElement
  if (typed) {
    expect(screen.getByText(/avec son host web-1/)).toBeTruthy()
    expect(submit.disabled).toBe(true)
    const input = screen.getByLabelText('Tapez le nom du host pour confirmer')
    fireEvent.change(input, { target: { value: 'web-2' } })
    expect(submit.disabled).toBe(true)
    fireEvent.change(input, { target: { value: typed } })
  }
  expect(submit.disabled).toBe(false)
  fireEvent.click(submit)
  await waitFor(() => expect(close).toHaveBeenCalled())
  expect(deleted).toHaveBeenCalled()
})
