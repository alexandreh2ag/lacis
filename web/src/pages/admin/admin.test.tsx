import { fireEvent, render, screen } from '@testing-library/react'
import { Dialog, errorMessage } from '@/components/Dialog'
import { t } from '@/i18n'
import { family, moved } from '../Settings'
import { canAdminister } from './Admin'
import { iconRows } from './Icons'
import { grantText } from './Roles'
import { outputText, pending } from './Tasks'
import { tokenState } from './Tokens'
import { PasswordFields } from './Users'

const passwordMismatch = t().admin.users.passwordMismatch

const none = { read: false, write: false }

test('canAdminister needs a read right', () => {
  expect(canAdminister({ users: none, roles: none, tokens: none, icons: none, tasks: none })).toBe(false)
  expect(canAdminister({ users: none, roles: none, tokens: { read: true }, icons: none, tasks: none })).toBe(true)
  expect(canAdminister({ users: none, roles: none, tokens: none, icons: { read: true }, tasks: none })).toBe(true)
  expect(canAdminister({ users: none, roles: none, tokens: none, icons: none, tasks: { read: true } })).toBe(true)
})

test('tokenState', () => {
  const now = Date.parse('2026-10-30T12:00:00Z')
  expect(tokenState({ expiresAt: null }, now)).toBe('active')
  expect(tokenState({ expiresAt: '2026-10-31T00:00:00Z' }, now)).toBe('active')
  expect(tokenState({ expiresAt: '2026-10-29T00:00:00Z' }, now)).toBe('expired')
  expect(tokenState({ expiresAt: '2026-10-31T00:00:00Z', revokedAt: '2026-10-30T00:00:00Z' }, now)).toBe('revoked')
})

test('grantText', () => {
  expect(grantText({ namespace: { slug: 'acme' }, resource: 'INVENTORY', action: 'READ' })).toBe('acme · Inventaire lecture')
  expect(grantText({ namespace: null, resource: 'ALL', action: 'ALL' })).toBe('tous les namespaces · Tout (tout)')
  expect(grantText({ resource: 'USERS', action: 'WRITE' }, true)).toBe('global · Utilisateurs écriture')
})

test('moved', () => {
  expect(moved(['a', 'b', 'c'], 2, -1)).toEqual(['a', 'c', 'b'])
  expect(moved(['a', 'b', 'c'], 0, -1)).toEqual(['a', 'b', 'c'])
})

test('family gathers the networks of a range', () => {
  const lan = { id: 'lan', splitFrom: null }
  const b = { id: 'b', splitFrom: { id: 'lan' } }
  const c = { id: 'c', splitFrom: { id: 'lan' } }
  const other = { id: 'other', splitFrom: null }
  const all = [lan, b, c, other]
  expect(family(all, lan)).toEqual([lan, b, c])
  expect(family(all, c)).toEqual([lan, b, c])
  expect(family(all, other)).toEqual([other])
})

test('errorMessage says the refusals of a split in French', () => {
  const refusal = (message: string, code = 'BAD_USER_INPUT') => ({ graphQLErrors: [{ message, extensions: { code } }] })
  expect(errorMessage(refusal('invalid input: web-1 has no IP in the range of 10-0-1-0-24'))).toBe('web-1 n\'a pas d\'IP dans le range de 10-0-1-0-24.')
  expect(errorMessage(refusal('invalid input: web-1 is already in lan-b'))).toBe('web-1 est déjà dans lan-b.')
  expect(errorMessage(refusal('invalid input: web-1 is not in lan-b'))).toBe('web-1 n\'est pas dans lan-b.')
  expect(errorMessage(refusal('already exists: network lan-b', 'CONFLICT'))).toBe('Un réseau porte déjà ce nom.')
  expect(errorMessage(refusal('invalid input: only a network split by hand is deleted, the others come from the IPs'))).toContain('Seul un réseau scindé')
})

test('errorMessage tells the refusal of the server', () => {
  expect(errorMessage({ graphQLErrors: [{ message: 'invalid input: code "X" must hold…', extensions: { code: 'BAD_USER_INPUT' } }] })).toContain('Code invalide')
  expect(errorMessage({ graphQLErrors: [{ message: 'forbidden', extensions: { code: 'FORBIDDEN' } }] })).toContain('pas le droit')
  expect(errorMessage({ graphQLErrors: [], networkError: new Error('down') })).toBe('Le serveur ne répond pas.')
})

test('Dialog submits once, and closes', () => {
  const submit = vi.fn()
  const close = vi.fn()
  render(<Dialog title="Nouveau rôle" submit="Créer" onSubmit={submit} onClose={close} error="refusé"><input aria-label="Code" /></Dialog>)
  expect(screen.getByRole('alert', { hidden: true }).textContent).toBe('refusé')
  fireEvent.click(screen.getByRole('button', { name: 'Créer', hidden: true }))
  expect(submit).toHaveBeenCalledTimes(1)
  fireEvent.click(screen.getByRole('button', { name: 'Annuler', hidden: true }))
  expect(close).toHaveBeenCalledTimes(1)
})

test('errorMessage says the known refusals in French', () => {
  const refusal = (message: string, code = 'BAD_USER_INPUT') => ({ graphQLErrors: [{ message, extensions: { code } }] })
  expect(errorMessage(refusal('invalid input: the password needs at least 8 characters'))).toBe('Le mot de passe doit faire au moins 8 caractères.')
  expect(errorMessage(refusal('already exists: user bob', 'CONFLICT'))).toBe('Ce nom d\'utilisateur est déjà pris.')
  expect(errorMessage(refusal('the last user holding the admin role keeps it', 'CONFLICT'))).toContain('dernier utilisateur')
  expect(errorMessage(refusal('invalid input: something new'))).toBe('something new')
})

test('PasswordFields says when the confirmation differs', () => {
  const { rerender } = render(<PasswordFields label="Mot de passe" hint="8 caractères au moins." password="correct horse" confirm="" onPassword={() => {}} onConfirm={() => {}} />)
  expect(screen.queryByText(passwordMismatch)).toBeNull()
  rerender(<PasswordFields label="Mot de passe" hint="" password="correct horse" confirm="correct" onPassword={() => {}} onConfirm={() => {}} />)
  expect(screen.getByText(passwordMismatch)).toBeTruthy()
  expect(screen.getByLabelText('Confirmer le mot de passe').getAttribute('aria-invalid')).toBe('true')
  rerender(<PasswordFields label="Mot de passe" hint="" password="correct horse" confirm="correct horse" onPassword={() => {}} onConfirm={() => {}} />)
  expect(screen.queryByText(passwordMismatch)).toBeNull()
})

test('iconRows lists the technologies of the inventory, of the front and given a logo', () => {
  const rows = iconRows(['traefik', 'zabbix'], [{ tech: 'traefik', updatedAt: '2026-10-30T09:00:00Z', updatedBy: 'admin' }, { tech: 'gotenberg', updatedAt: '2026-10-30T09:00:00Z' }], ['newtech'])
  const by = new Map(rows.map((r) => [r.tech, r]))
  expect(by.get('traefik')).toMatchObject({ source: 'custom', inventory: true, updatedBy: 'admin' })
  expect(by.get('zabbix')).toMatchObject({ source: 'none', inventory: true })
  expect(by.get('gotenberg')).toMatchObject({ source: 'custom', inventory: false })
  expect(by.get('redis')).toMatchObject({ source: 'builtin', inventory: false })
  expect(by.get('newtech')).toMatchObject({ source: 'none', inventory: false })
})

test('outputText says what a run changed', () => {
  expect(outputText({ snapshots: 12 })).toBe('12 snapshots supprimés')
  expect(outputText({ tokens: 1, sessions: 0 })).toBe('1 token supprimé, 0 session supprimée')
  expect(outputText({ conflicts: 2 })).toBe('2 tokens partagés')
  expect(outputText({ other: 3 })).toBe('other : 3')
  expect(outputText(null)).toBe('')
  expect(pending('running')).toBe(true)
  expect(pending('completed')).toBe(false)
  expect(Object.keys(t().admin.tasks.kinds)).toEqual(expect.arrayContaining(['purge_snapshots', 'purge_removed_entities', 'purge_expired_tokens', 'detect_token_conflicts', 'purge_inactive_hosts']))
})
