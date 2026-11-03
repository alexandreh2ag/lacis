import { MockedProvider, type MockedResponse } from '@apollo/client/testing'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import { ChangePasswordDocument, UpdateProfileDocument } from '@/graphql/graphql'
import { General, Password } from './Profile'

const me = { __typename: 'User' as const, id: 'u1', username: 'bob', firstName: 'Bob', lastName: null, isAdmin: false, local: true, oidc: false }

test('the profile changes the names of the user', async () => {
  const updated = vi.fn(() => ({ data: { updateProfile: { __typename: 'User', id: 'u1', firstName: 'Robert', lastName: 'Martin' } } }))
  const mocks: MockedResponse[] = [
    { request: { query: UpdateProfileDocument, variables: { input: { firstName: 'Robert', lastName: 'Martin' } } }, result: updated },
  ]
  render(<MockedProvider mocks={mocks}><General me={me} /></MockedProvider>)
  expect((screen.getByLabelText(/Nom d'utilisateur/) as HTMLInputElement).readOnly).toBe(true)
  expect(screen.queryByText(/peut remplacer ces noms/)).toBeNull()
  fireEvent.change(screen.getByLabelText('Prénom'), { target: { value: 'Robert' } })
  fireEvent.change(screen.getByLabelText('Nom'), { target: { value: 'Martin' } })
  fireEvent.click(screen.getByRole('button', { name: 'Enregistrer' }))
  expect(await screen.findByText('Enregistré.')).toBeTruthy()
  expect(updated).toHaveBeenCalled()
})

test('an SSO user is told the identity provider may set the names again', () => {
  render(<MockedProvider mocks={[]}><General me={{ ...me, local: false, oidc: true }} /></MockedProvider>)
  expect(screen.getByText('SSO (fournisseur d\'identité)')).toBeTruthy()
  expect(screen.getByText(/peut remplacer ces noms/)).toBeTruthy()
})

function fill(current: string, password: string, confirm: string) {
  fireEvent.change(screen.getByLabelText('Mot de passe actuel'), { target: { value: current } })
  fireEvent.change(screen.getByLabelText('Nouveau mot de passe'), { target: { value: password } })
  fireEvent.change(screen.getByLabelText('Confirmer le nouveau mot de passe'), { target: { value: confirm } })
  fireEvent.click(screen.getByRole('button', { name: 'Changer le mot de passe' }))
}

test('the password changes given the current one, and the form empties', async () => {
  const changed = vi.fn(() => ({ data: { changePassword: true } }))
  const mocks: MockedResponse[] = [
    { request: { query: ChangePasswordDocument, variables: { currentPassword: 'old password', newPassword: 'new password' } }, result: changed },
  ]
  render(<MockedProvider mocks={mocks}><Password /></MockedProvider>)
  fill('old password', 'new password', 'new password')
  expect(await screen.findByText(/Mot de passe changé/)).toBeTruthy()
  expect(changed).toHaveBeenCalled()
  expect((screen.getByLabelText('Mot de passe actuel') as HTMLInputElement).value).toBe('')
})

test('a wrong current password is told', async () => {
  const mocks: MockedResponse[] = [
    { request: { query: ChangePasswordDocument, variables: { currentPassword: 'guess', newPassword: 'new password' } },
      result: { errors: [{ message: 'the current password is wrong', extensions: { code: 'BAD_USER_INPUT', reason: 'CURRENT_PASSWORD_WRONG' } }] } },
  ]
  render(<MockedProvider mocks={mocks}><Password /></MockedProvider>)
  fill('guess', 'new password', 'new password')
  expect((await screen.findByRole('alert')).textContent).not.toBe('')
})

test('two different passwords are refused before sending', async () => {
  render(<MockedProvider mocks={[]}><Password /></MockedProvider>)
  fill('old password', 'new password', 'other password')
  await waitFor(() => expect(screen.getByRole('alert').textContent).toBe('Les deux mots de passe ne sont pas identiques.'))
})
