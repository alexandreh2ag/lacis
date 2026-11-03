import { MockedProvider, type MockedResponse } from '@apollo/client/testing'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { afterEach, vi } from 'vitest'
import { config } from '@/config'
import { AdminUserOAuthGrantsDocument, OAuthGrantsDocument, RevokeOAuthGrantDocument, RevokeUserOAuthGrantDocument } from '@/graphql/graphql'
import { Assistants } from './Assistants'
import { UserGrants } from './admin/Users'

const grant = { __typename: 'OAuthGrant', id: 'g1', clientName: 'Claude', createdAt: '2026-11-01T10:00:00Z',
  lastUsedAt: '2026-11-02T10:00:00Z', expiresAt: '2026-12-01T10:00:00Z' }

afterEach(() => { config.mcp = { enabled: false, url: '' } })

test('the tab of the profile tells how to connect, lists and revokes the assistants', async () => {
  config.mcp = { enabled: true, url: 'https://lacis.acme.example/mcp' }
  const grants = OAuthGrantsDocument
  const revoked = vi.fn(() => ({ data: { revokeOAuthGrant: true } }))
  const mocks: MockedResponse[] = [
    { request: { query: grants }, result: { data: { oauthGrants: [grant] } } },
    { request: { query: RevokeOAuthGrantDocument, variables: { id: 'g1' } }, result: revoked },
    { request: { query: grants }, result: { data: { oauthGrants: [] } } },
  ]
  render(<MockedProvider mocks={mocks}><MemoryRouter><Assistants /></MemoryRouter></MockedProvider>)
  expect(screen.getByTitle('https://lacis.acme.example/mcp')).toBeTruthy()
  expect(screen.getByTitle('claude mcp add --transport http lacis https://lacis.acme.example/mcp')).toBeTruthy()
  expect(await screen.findByText('Claude')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Retirer l\'accès' }))
  await waitFor(() => expect(revoked).toHaveBeenCalled())
  expect(await screen.findByText('Aucun assistant autorisé.')).toBeTruthy()
})

test('the tab says when the MCP server is off', async () => {
  const grants = OAuthGrantsDocument
  render(<MockedProvider mocks={[{ request: { query: grants }, result: { data: { oauthGrants: [] } } }]}><MemoryRouter><Assistants /></MemoryRouter></MockedProvider>)
  expect(screen.getByText(/Le serveur MCP est désactivé/)).toBeTruthy()
  expect(await screen.findByText('Aucun assistant autorisé.')).toBeTruthy()
})

const user = { __typename: 'Account', id: 'u1', username: 'bob', firstName: null, lastName: null, local: true, oidc: false, createdAt: '2026-10-01T00:00:00Z', roles: [] }
const userGrants = AdminUserOAuthGrantsDocument

test('an administrator who reads the users sees the assistants of one, without revoking', async () => {
  render(<MockedProvider mocks={[{ request: { query: userGrants, variables: { userId: 'u1' } }, result: { data: { userOAuthGrants: [grant] } } }]}>
    <UserGrants user={user} write={false} onClose={() => {}} />
  </MockedProvider>)
  expect(await screen.findByText('Claude')).toBeTruthy()
  expect(screen.getByRole('heading', { name: 'Accès MCP de bob', hidden: true })).toBeTruthy()
  expect(screen.queryByRole('button', { name: 'Retirer l\'accès', hidden: true })).toBeNull()
})

test('an administrator who writes the users revokes the access of one', async () => {
  const revoked = vi.fn(() => ({ data: { revokeUserOAuthGrant: true } }))
  const mocks: MockedResponse[] = [
    { request: { query: userGrants, variables: { userId: 'u1' } }, result: { data: { userOAuthGrants: [grant] } } },
    { request: { query: RevokeUserOAuthGrantDocument, variables: { userId: 'u1', id: 'g1' } }, result: revoked },
    { request: { query: userGrants, variables: { userId: 'u1' } }, result: { data: { userOAuthGrants: [] } } },
  ]
  render(<MockedProvider mocks={mocks}><UserGrants user={user} write onClose={() => {}} /></MockedProvider>)
  fireEvent.click(await screen.findByRole('button', { name: 'Retirer l\'accès', hidden: true }))
  await waitFor(() => expect(revoked).toHaveBeenCalled())
  expect(await screen.findByText('Aucun assistant autorisé.')).toBeTruthy()
})

test('a failed revocation says so', async () => {
  const mocks: MockedResponse[] = [
    { request: { query: userGrants, variables: { userId: 'u1' } }, result: { data: { userOAuthGrants: [grant] } } },
    { request: { query: RevokeUserOAuthGrantDocument, variables: { userId: 'u1', id: 'g1' } },
      result: { errors: [{ message: 'not found', extensions: { code: 'NOT_FOUND' } }] } },
  ]
  render(<MockedProvider mocks={mocks}><UserGrants user={user} write onClose={() => {}} /></MockedProvider>)
  fireEvent.click(await screen.findByRole('button', { name: 'Retirer l\'accès', hidden: true }))
  expect((await screen.findByRole('alert', { hidden: true })).textContent).toBeTruthy()
})
