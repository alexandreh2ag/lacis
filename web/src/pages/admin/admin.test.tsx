import { fireEvent, render, screen } from '@testing-library/react'
import { Dialog, errorMessage } from '@/components/Dialog'
import { t } from '@/i18n'
import { family, moved, unused } from '../Settings'
import { canAdminister } from './Admin'
import { iconRows } from './Icons'
import { grantText } from './Roles'
import { outputText, pending } from './Tasks'
import { agentDrift, tokenState, versionOrder, versionSatisfies } from './Tokens'
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
  const refusal = (reason: string, args = {}, code = 'BAD_USER_INPUT') => ({ graphQLErrors: [{ message: 'refused', extensions: { code, reason, args } }] })
  expect(errorMessage(refusal('HOST_NOT_IN_RANGE', { host: 'web-1', network: '10-0-1-0-24' }))).toBe('web-1 n\'a pas d\'IP dans le range de 10-0-1-0-24.')
  expect(errorMessage(refusal('HOST_ALREADY_IN', { host: 'web-1', network: 'lan-b' }))).toBe('web-1 est déjà dans lan-b.')
  expect(errorMessage(refusal('HOST_NOT_IN', { host: 'web-1', network: 'lan-b' }))).toBe('web-1 n\'est pas dans lan-b.')
  expect(errorMessage(refusal('NETWORK_EXISTS', { network: 'lan-b' }, 'CONFLICT'))).toBe('Un réseau porte déjà ce nom.')
  expect(errorMessage(refusal('NETWORK_NOT_SPLIT'))).toContain('Seul un réseau scindé')
})

test('a project or an env is deleted once nothing names it', () => {
  expect(unused({ hosts: 0, apps: 0 })).toBe(true)
  expect(unused({ hosts: 1, apps: 0 })).toBe(false)
  expect(unused({ hosts: 0, apps: 2 })).toBe(false)
  const st = t().admin.settings
  expect(st.used(0, 0)).toBe('inutilisé')
  expect(st.used(2, 1)).toBe('2 hosts · 1 app')
  const refusal = { graphQLErrors: [{ message: 'still in use', extensions: { code: 'CONFLICT', reason: 'ENVIRONMENT_IN_USE', args: { hosts: 2, apps: 1 } } }] }
  expect(errorMessage(refusal)).toBe('Encore utilisé par 2 host(s) et 1 app(s) : supprimez-les ou changez leur label d\'abord.')
  expect(outputText({ projects: 3 })).toBe('3 projets supprimés')
})

test('errorMessage tells the refusal of the server', () => {
  expect(errorMessage({ graphQLErrors: [{ message: 'invalid input: code "X" must hold…', extensions: { code: 'BAD_USER_INPUT', reason: 'INVALID_ROLE_CODE' } }] })).toContain('Code invalide')
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
  const refusal = (message: string, code = 'BAD_USER_INPUT', reason?: string, args = {}) => ({ graphQLErrors: [{ message, extensions: { code, reason, args } }] })
  expect(errorMessage(refusal('invalid input: the password needs at least 8 characters', 'BAD_USER_INPUT', 'PASSWORD_TOO_SHORT', { min: 8 }))).toBe('Le mot de passe doit faire au moins 8 caractères.')
  expect(errorMessage(refusal('already exists: user bob', 'CONFLICT', 'USER_EXISTS'))).toBe('Ce nom d\'utilisateur est déjà pris.')
  expect(errorMessage(refusal('the last user holding the admin role keeps it', 'CONFLICT', 'LAST_ADMIN'))).toContain('dernier utilisateur')
  // Without a reason, the message of the server shows, not guessed from its words.
  expect(errorMessage(refusal('invalid input: the password needs at least 8 characters'))).toBe('the password needs at least 8 characters')
  expect(errorMessage(refusal('invalid input: the current password is wrong', 'BAD_USER_INPUT', 'CURRENT_PASSWORD_WRONG'))).toBe('Le mot de passe actuel est incorrect.')
  expect(errorMessage(refusal('invalid input: the password holds 1024 characters at most', 'BAD_USER_INPUT', 'PASSWORD_TOO_LONG', { max: 1024 }))).toBe('Le mot de passe fait au plus 1024 caractères.')
  expect(errorMessage(refusal('forbidden: you do not hold users write', 'FORBIDDEN'))).toBe('Vous n\'avez pas le droit de faire cette modification.')
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
  expect(Object.keys(t().admin.tasks.kinds)).toEqual(expect.arrayContaining(['purge_snapshots', 'purge_removed_entities', 'purge_expired_tokens', 'detect_token_conflicts', 'purge_history', 'purge_inactive_hosts']))
})

test('agent versions compare by their numbers', () => {
  expect(versionOrder('0.0.10', '0.0.9')).toBe(1)
  expect(versionOrder('v0.0.6', '0.0.6')).toBe(0)
  expect(versionSatisfies('0.0.5', 'LT', '0.0.6')).toBe(true)
  expect(versionSatisfies('0.0.6', 'LT', '0.0.6')).toBe(false)
  expect(versionSatisfies('0.0.6', 'GTE', '0.0')).toBe(true)
  expect(versionSatisfies('0.1.0', 'GT', '0.0.6')).toBe(true)
  expect(versionSatisfies('dev', 'LT', '0.0.6')).toBe(false)
  expect(versionSatisfies('0.0.1', 'LT', '')).toBe(true)
})

test('agentDrift tells how an agent stands to the server', () => {
  expect(agentDrift('0.0.6', '0.0.6')).toBe('same')
  expect(agentDrift('v0.0.6', '0.0.6')).toBe('same')
  expect(agentDrift('0.0.5', '0.0.6')).toBe('older')
  expect(agentDrift('0.1.0', '0.0.6')).toBe('newer')
  expect(agentDrift('0.0.6', 'dev')).toBe('other')
  expect(agentDrift('dev', 'dev')).toBe('same')
  expect(agentDrift(null, '0.0.6')).toBe('unknown')
})
