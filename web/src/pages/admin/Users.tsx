import { useMutation, useQuery } from '@apollo/client'
import { useState } from 'react'
import { AddPicker } from '@/components/AddPicker'
import { CopyButton } from '@/components/Copy'
import { Dialog, Field, useAction } from '@/components/Dialog'
import { chipsField, FilterBar } from '@/components/FilterBar'
import { Pager, SearchInput, SortHeader, useTable } from '@/components/Listing'
import { graphql } from '@/graphql'
import type { AdminUsersQuery } from '@/graphql/graphql'
import { formatDate, plural } from '@/lib/format'
import { useSession } from '@/session'

const UsersDoc = graphql(`
  query AdminUsers {
    users { id username firstName lastName local oidc createdAt roles { id code } }
    roleChoices { id code description }
  }
`)

const CreateUserDoc = graphql(`
  mutation CreateUser($input: CreateUserInput!) {
    createUser(input: $input) { id }
  }
`)

const UpdateUserDoc = graphql(`
  mutation UpdateUser($id: ID!, $input: UpdateUserInput!) {
    updateUser(id: $id, input: $input) { id firstName lastName }
  }
`)

const SetUserRolesDoc = graphql(`
  mutation SetUserRoles($id: ID!, $roles: [ID!]!) {
    setUserRoles(id: $id, roles: $roles) { id roles { id code } }
  }
`)

const DeleteUserDoc = graphql(`
  mutation DeleteUser($id: ID!) {
    deleteUser(id: $id)
  }
`)

type User = AdminUsersQuery['users'][number]
type Role = AdminUsersQuery['roleChoices'][number]

const refetch = { refetchQueries: ['AdminUsers', 'AdminRoles'] }

function fullName(u: User) {
  return [u.firstName, u.lastName].filter(Boolean).join(' ')
}

const logins = [{ value: 'local', label: 'Mot de passe' }, { value: 'oidc', label: 'SSO' }]

// Users lists the users, and creates, edits and deletes them when the
// user may write them.
export function Users({ write }: { write: boolean }) {
  const { data, error } = useQuery(UsersDoc)
  const me = useSession().me
  const [roles, setRoles] = useState<string[]>([])
  const [login, setLogin] = useState<string[]>([])
  const [dialog, setDialog] = useState<{ kind: 'create' } | { kind: 'edit' | 'delete'; user: User } | null>(null)
  const users = (data?.users ?? []).filter((u) => (!roles.length || u.roles.some((r) => roles.includes(r.code)))
    && (!login.length || (login.includes('local') && u.local) || (login.includes('oidc') && u.oidc)))
  const t = useTable(users, (u) => [u.username, fullName(u), ...u.roles.map((r) => r.code)],
    (u, by) => by === 'name' ? fullName(u) : by === 'created' ? u.createdAt : u.username, 'username')
  const head = (label: string, by: string) => <SortHeader label={label} by={by} sort={t.sort.by} desc={t.sort.desc} onSort={t.onSort} />
  const choices = data?.roleChoices ?? []

  if (error) return <p role="alert">Les utilisateurs n'ont pas pu être chargés : {error.message}</p>
  if (!data) return <span className="lc-muted">Chargement…</span>
  return (
    <>
      <div className="flex flex-wrap items-center gap-2">
        <div className="min-w-0 flex-1">
          <FilterBar search={<SearchInput value={t.q} onChange={t.setQ} placeholder="Chercher un utilisateur, un rôle" width={300} />}
            fields={[
              chipsField({ label: 'Rôle', options: choices.map((r) => ({ value: r.code, label: r.code })), selected: roles, onChange: setRoles }),
              chipsField({ label: 'Connexion', options: logins, selected: login, onChange: setLogin }),
            ]}
            count={plural(t.list.length, 'utilisateur')} />
        </div>
        {write ? <button type="button" className="lc-btn lc-btn--primary" onClick={() => setDialog({ kind: 'create' })}>Nouvel utilisateur</button> : null}
      </div>
      <div className="overflow-x-auto rounded-lg border border-border">
        <table className="lc-table">
          <thead><tr>
            {head('Utilisateur', 'username')}{head('Nom', 'name')}<th>Connexion</th><th>Rôles</th>{head('Créé le', 'created')}
            {write ? <th><span className="sr-only">Actions</span></th> : null}
          </tr></thead>
          <tbody>
            {t.shown.map((u) => (
              <tr key={u.id}>
                <td><span className="lc-copyable"><span className="lc-mono font-semibold">{u.username}</span><CopyButton value={u.username} /></span>
                  {u.id === me.id ? <span className="lc-muted"> (vous)</span> : null}</td>
                <td>{fullName(u) || <span className="lc-muted">—</span>}</td>
                <td><span className="flex gap-1.5">
                  {u.local ? <span className="lc-badge lc-badge--plain">Mot de passe</span> : null}
                  {u.oidc ? <span className="lc-badge lc-badge--plain">SSO</span> : null}
                </span></td>
                <td>{u.roles.length ? <span className="lc-tags">{u.roles.map((r) => <span key={r.id} className="lc-tag pr-2">{r.code}</span>)}</span>
                  : <span className="lc-muted">aucun</span>}</td>
                <td>{formatDate(u.createdAt)}</td>
                {write ? (
                  <td className="lc-num">
                    <span className="inline-flex gap-1">
                      <button type="button" className="lc-btn lc-btn--small" onClick={() => setDialog({ kind: 'edit', user: u })}>Modifier</button>
                      <button type="button" className="lc-btn lc-btn--small lc-btn--danger" disabled={u.id === me.id}
                        title={u.id === me.id ? 'Vous ne pouvez pas supprimer votre propre compte' : undefined}
                        onClick={() => setDialog({ kind: 'delete', user: u })}>Supprimer</button>
                    </span>
                  </td>
                ) : null}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Pager page={t.page} size={t.size} total={t.list.length} noun="utilisateurs" onPage={t.setPage} onSize={t.setSize} />
      {dialog?.kind === 'create' ? <CreateUser roles={choices} onClose={() => setDialog(null)} /> : null}
      {dialog?.kind === 'edit' ? <EditUser user={dialog.user} roles={choices} onClose={() => setDialog(null)} /> : null}
      {dialog?.kind === 'delete' ? <DeleteUser user={dialog.user} onClose={() => setDialog(null)} /> : null}
    </>
  )
}

// RolePicker adds roles by search, as there may be many, and lists those
// given with a button taking each out.
function RolePicker({ roles, selected, onChange, hint }: { roles: Role[]; selected: string[]; onChange: (ids: string[]) => void; hint?: string }) {
  return (
    <div className="flex flex-col gap-1.5">
      <AddPicker label="Rôles" placeholder="Chercher un rôle à ajouter" empty="Aucun rôle : l'utilisateur ne voit rien."
        items={[...roles].sort((a, b) => a.code.localeCompare(b.code, 'fr', { numeric: true })).map((r) => ({ id: r.id, label: r.code, detail: r.description }))} selected={selected} onChange={onChange} />
      {hint ? <span className="lc-field__hint">{hint}</span> : null}
    </div>
  )
}

export const passwordMismatch = 'Les deux mots de passe ne sont pas identiques.'

// PasswordFields asks the password twice; the confirmation says when it
// differs.
export function PasswordFields({ label, hint, required, password, confirm, onPassword, onConfirm }: {
  label: string; hint: string; required?: boolean; password: string; confirm: string
  onPassword: (v: string) => void; onConfirm: (v: string) => void
}) {
  const differs = confirm !== '' && confirm !== password
  return (
    <div className="lc-fields">
      <Field label={label} hint={hint}>
        {(id) => <input id={id} className="lc-input" type="password" required={required} minLength={8} autoComplete="new-password"
          value={password} onChange={(e) => onPassword(e.target.value)} />}
      </Field>
      <Field label="Confirmer le mot de passe" hint={differs ? <span className="text-danger">{passwordMismatch}</span> : undefined}>
        {(id) => <input id={id} className="lc-input" type="password" required={required || password !== ''} autoComplete="new-password"
          aria-invalid={differs} value={confirm} onChange={(e) => onConfirm(e.target.value)} />}
      </Field>
    </div>
  )
}

function CreateUser({ roles, onClose }: { roles: Role[]; onClose: () => void }) {
  const [create] = useMutation(CreateUserDoc, refetch)
  const a = useAction()
  const [form, setForm] = useState({ username: '', firstName: '', lastName: '', password: '', roles: [] as string[] })
  const [confirm, setConfirm] = useState('')
  const set = (k: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement>) => setForm({ ...form, [k]: e.target.value })
  const save = async () => {
    if (confirm !== form.password) throw new Error(passwordMismatch)
    await create({ variables: { input: form } })
  }
  return (
    <Dialog title="Nouvel utilisateur" submit="Créer" busy={a.busy} error={a.error} onClose={onClose}
      onSubmit={() => void a.run(save, onClose)}>
      <Field label="Nom d'utilisateur" hint="Lettres, chiffres et . _ @ + - : une adresse email convient.">
        {(id) => <input id={id} className="lc-input" required autoComplete="off" autoCapitalize="none" spellCheck={false} value={form.username} onChange={set('username')} />}
      </Field>
      <div className="lc-fields">
        <Field label="Prénom">{(id) => <input id={id} className="lc-input" value={form.firstName} onChange={set('firstName')} />}</Field>
        <Field label="Nom">{(id) => <input id={id} className="lc-input" value={form.lastName} onChange={set('lastName')} />}</Field>
      </div>
      <PasswordFields label="Mot de passe" hint="8 caractères au moins." required password={form.password} confirm={confirm}
        onPassword={(v) => setForm({ ...form, password: v })} onConfirm={setConfirm} />
      <RolePicker roles={roles} selected={form.roles} onChange={(ids) => setForm({ ...form, roles: ids })} />
    </Dialog>
  )
}

function EditUser({ user, roles, onClose }: { user: User; roles: Role[]; onClose: () => void }) {
  const [update] = useMutation(UpdateUserDoc, refetch)
  const [setRoles] = useMutation(SetUserRolesDoc, refetch)
  const a = useAction()
  const initial = user.roles.map((r) => r.id)
  const [form, setForm] = useState({ firstName: user.firstName ?? '', lastName: user.lastName ?? '', password: '', roles: initial })
  const [confirm, setConfirm] = useState('')
  const set = (k: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement>) => setForm({ ...form, [k]: e.target.value })
  const save = async () => {
    if (confirm !== form.password) throw new Error(passwordMismatch)
    await update({ variables: { id: user.id, input: { firstName: form.firstName, lastName: form.lastName, ...(form.password ? { password: form.password } : {}) } } })
    if ([...form.roles].sort().join() !== [...initial].sort().join()) {
      await setRoles({ variables: { id: user.id, roles: form.roles } })
    }
  }
  return (
    <Dialog title={`Modifier ${user.username}`} busy={a.busy} error={a.error} onClose={onClose} onSubmit={() => void a.run(save, onClose)}>
      <div className="lc-fields">
        <Field label="Prénom">{(id) => <input id={id} className="lc-input" value={form.firstName} onChange={set('firstName')} />}</Field>
        <Field label="Nom">{(id) => <input id={id} className="lc-input" value={form.lastName} onChange={set('lastName')} />}</Field>
      </div>
      {user.local ? (
        <PasswordFields label="Nouveau mot de passe" hint="Laisser vide pour le garder. Un nouveau mot de passe ferme les autres sessions de l'utilisateur."
          password={form.password} confirm={confirm} onPassword={(v) => setForm({ ...form, password: v })} onConfirm={setConfirm} />
      ) : (
        <p className="lc-muted m-0 text-[13px]">Cet utilisateur se connecte par SSO : il n'a pas de mot de passe dans Lacis.</p>
      )}
      <RolePicker roles={roles} selected={form.roles} onChange={(ids) => setForm({ ...form, roles: ids })}
        hint={user.oidc ? 'Les rôles donnés par ses groupes OIDC s\'ajoutent à ceux-ci.' : undefined} />
    </Dialog>
  )
}

function DeleteUser({ user, onClose }: { user: User; onClose: () => void }) {
  const [remove] = useMutation(DeleteUserDoc, refetch)
  const a = useAction()
  return (
    <Dialog title={`Supprimer ${user.username} ?`} submit="Supprimer" danger busy={a.busy} error={a.error} onClose={onClose}
      onSubmit={() => void a.run(() => remove({ variables: { id: user.id } }), onClose)}>
      <p className="m-0">L'utilisateur perd ses rôles et ses sessions sont fermées. {user.oidc
        ? 'Il sera recréé, sans rôle, à sa prochaine connexion SSO.' : 'Cette action est définitive.'}</p>
    </Dialog>
  )
}
