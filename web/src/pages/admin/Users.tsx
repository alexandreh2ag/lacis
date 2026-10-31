import { useMutation, useQuery } from '@apollo/client'
import { useState } from 'react'
import { AddPicker } from '@/components/AddPicker'
import { CopyButton } from '@/components/Copy'
import { Dialog, Field, useAction } from '@/components/Dialog'
import { chipsField, FilterBar } from '@/components/FilterBar'
import { Pager, SearchInput, SortHeader, useTable } from '@/components/Listing'
import { graphql } from '@/graphql'
import type { AdminUsersQuery } from '@/graphql/graphql'
import { useT } from '@/i18n'
import { formatDate } from '@/lib/format'
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

// Users lists the users, and creates, edits and deletes them when the
// user may write them.
export function Users({ write }: { write: boolean }) {
  const { data, error } = useQuery(UsersDoc)
  const me = useSession().me
  const t = useT()
  const u = t.admin.users
  const [roles, setRoles] = useState<string[]>([])
  const [login, setLogin] = useState<string[]>([])
  const [dialog, setDialog] = useState<{ kind: 'create' } | { kind: 'edit' | 'delete'; user: User } | null>(null)
  const users = (data?.users ?? []).filter((u) => (!roles.length || u.roles.some((r) => roles.includes(r.code)))
    && (!login.length || (login.includes('local') && u.local) || (login.includes('oidc') && u.oidc)))
  const tbl = useTable(users, (u) => [u.username, fullName(u), ...u.roles.map((r) => r.code)],
    (u, by) => by === 'name' ? fullName(u) : by === 'created' ? u.createdAt : u.username, 'username')
  const head = (label: string, by: string) => <SortHeader label={label} by={by} sort={tbl.sort.by} desc={tbl.sort.desc} onSort={tbl.onSort} />
  const choices = data?.roleChoices ?? []

  if (error) return <p role="alert">{u.loadError}{error.message}</p>
  if (!data) return <span className="lc-muted">{t.admin.loading}</span>
  return (
    <>
      <div className="flex flex-wrap items-center gap-2">
        <div className="min-w-0 flex-1">
          <FilterBar search={<SearchInput value={tbl.q} onChange={tbl.setQ} placeholder={u.search} width={300} />}
            fields={[
              chipsField({ label: u.role, options: choices.map((r) => ({ value: r.code, label: r.code })), selected: roles, onChange: setRoles }),
              chipsField({ label: u.login, options: (['local', 'oidc'] as const).map((value) => ({ value, label: u.logins[value] })), selected: login, onChange: setLogin }),
            ]}
            count={u.count(tbl.list.length)} />
        </div>
        {write ? <button type="button" className="lc-btn lc-btn--primary" onClick={() => setDialog({ kind: 'create' })}>{u.create}</button> : null}
      </div>
      <div className="overflow-x-auto rounded-lg border border-border">
        <table className="lc-table">
          <thead><tr>
            {head(u.user, 'username')}{head(t.admin.name, 'name')}<th>{u.login}</th><th>{u.roles}</th>{head(t.admin.createdOn, 'created')}
            {write ? <th><span className="sr-only">{t.admin.actions}</span></th> : null}
          </tr></thead>
          <tbody>
            {tbl.shown.map((x) => (
              <tr key={x.id}>
                <td><span className="lc-copyable"><span className="lc-mono font-semibold">{x.username}</span><CopyButton value={x.username} /></span>
                  {x.id === me.id ? <span className="lc-muted">{u.you}</span> : null}</td>
                <td>{fullName(x) || <span className="lc-muted">—</span>}</td>
                <td><span className="flex gap-1.5">
                  {x.local ? <span className="lc-badge lc-badge--plain">{u.logins.local}</span> : null}
                  {x.oidc ? <span className="lc-badge lc-badge--plain">{u.logins.oidc}</span> : null}
                </span></td>
                <td>{x.roles.length ? <span className="lc-tags">{x.roles.map((r) => <span key={r.id} className="lc-tag pr-2">{r.code}</span>)}</span>
                  : <span className="lc-muted">{u.none}</span>}</td>
                <td>{formatDate(x.createdAt)}</td>
                {write ? (
                  <td className="lc-num">
                    <span className="inline-flex gap-1">
                      <button type="button" className="lc-btn lc-btn--small" onClick={() => setDialog({ kind: 'edit', user: x })}>{t.admin.edit}</button>
                      <button type="button" className="lc-btn lc-btn--small lc-btn--danger" disabled={x.id === me.id}
                        title={x.id === me.id ? u.cannotDeleteSelf : undefined}
                        onClick={() => setDialog({ kind: 'delete', user: x })}>{t.admin.delete}</button>
                    </span>
                  </td>
                ) : null}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Pager page={tbl.page} size={tbl.size} total={tbl.list.length} noun={u.noun} onPage={tbl.setPage} onSize={tbl.setSize} />
      {dialog?.kind === 'create' ? <CreateUser roles={choices} onClose={() => setDialog(null)} /> : null}
      {dialog?.kind === 'edit' ? <EditUser user={dialog.user} roles={choices} onClose={() => setDialog(null)} /> : null}
      {dialog?.kind === 'delete' ? <DeleteUser user={dialog.user} onClose={() => setDialog(null)} /> : null}
    </>
  )
}

// RolePicker adds roles by search, as there may be many, and lists those
// given with a button taking each out.
function RolePicker({ roles, selected, onChange, hint }: { roles: Role[]; selected: string[]; onChange: (ids: string[]) => void; hint?: string }) {
  const u = useT().admin.users
  return (
    <div className="flex flex-col gap-1.5">
      <AddPicker label={u.roles} placeholder={u.rolePlaceholder} empty={u.noRole}
        items={[...roles].sort((a, b) => a.code.localeCompare(b.code, 'fr', { numeric: true })).map((r) => ({ id: r.id, label: r.code, detail: r.description }))} selected={selected} onChange={onChange} />
      {hint ? <span className="lc-field__hint">{hint}</span> : null}
    </div>
  )
}

// PasswordFields asks the password twice; the confirmation says when it
// differs.
export function PasswordFields({ label, hint, required, password, confirm, onPassword, onConfirm }: {
  label: string; hint: string; required?: boolean; password: string; confirm: string
  onPassword: (v: string) => void; onConfirm: (v: string) => void
}) {
  const u = useT().admin.users
  const differs = confirm !== '' && confirm !== password
  return (
    <div className="lc-fields">
      <Field label={label} hint={hint}>
        {(id) => <input id={id} className="lc-input" type="password" required={required} minLength={8} autoComplete="new-password"
          value={password} onChange={(e) => onPassword(e.target.value)} />}
      </Field>
      <Field label={u.confirmPassword} hint={differs ? <span className="text-danger">{u.passwordMismatch}</span> : undefined}>
        {(id) => <input id={id} className="lc-input" type="password" required={required || password !== ''} autoComplete="new-password"
          aria-invalid={differs} value={confirm} onChange={(e) => onConfirm(e.target.value)} />}
      </Field>
    </div>
  )
}

function CreateUser({ roles, onClose }: { roles: Role[]; onClose: () => void }) {
  const [create] = useMutation(CreateUserDoc, refetch)
  const t = useT()
  const u = t.admin.users
  const a = useAction()
  const [form, setForm] = useState({ username: '', firstName: '', lastName: '', password: '', roles: [] as string[] })
  const [confirm, setConfirm] = useState('')
  const set = (k: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement>) => setForm({ ...form, [k]: e.target.value })
  const save = async () => {
    if (confirm !== form.password) throw new Error(u.passwordMismatch)
    await create({ variables: { input: form } })
  }
  return (
    <Dialog title={u.create} submit={t.admin.create} busy={a.busy} error={a.error} onClose={onClose}
      onSubmit={() => void a.run(save, onClose)}>
      <Field label={u.username} hint={u.usernameHint}>
        {(id) => <input id={id} className="lc-input" required autoComplete="off" autoCapitalize="none" spellCheck={false} value={form.username} onChange={set('username')} />}
      </Field>
      <div className="lc-fields">
        <Field label={u.firstName}>{(id) => <input id={id} className="lc-input" value={form.firstName} onChange={set('firstName')} />}</Field>
        <Field label={u.lastName}>{(id) => <input id={id} className="lc-input" value={form.lastName} onChange={set('lastName')} />}</Field>
      </div>
      <PasswordFields label={u.password} hint={u.passwordHint} required password={form.password} confirm={confirm}
        onPassword={(v) => setForm({ ...form, password: v })} onConfirm={setConfirm} />
      <RolePicker roles={roles} selected={form.roles} onChange={(ids) => setForm({ ...form, roles: ids })} />
    </Dialog>
  )
}

function EditUser({ user, roles, onClose }: { user: User; roles: Role[]; onClose: () => void }) {
  const [update] = useMutation(UpdateUserDoc, refetch)
  const [setRoles] = useMutation(SetUserRolesDoc, refetch)
  const u = useT().admin.users
  const a = useAction()
  const initial = user.roles.map((r) => r.id)
  const [form, setForm] = useState({ firstName: user.firstName ?? '', lastName: user.lastName ?? '', password: '', roles: initial })
  const [confirm, setConfirm] = useState('')
  const set = (k: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement>) => setForm({ ...form, [k]: e.target.value })
  const save = async () => {
    if (confirm !== form.password) throw new Error(u.passwordMismatch)
    await update({ variables: { id: user.id, input: { firstName: form.firstName, lastName: form.lastName, ...(form.password ? { password: form.password } : {}) } } })
    if ([...form.roles].sort().join() !== [...initial].sort().join()) {
      await setRoles({ variables: { id: user.id, roles: form.roles } })
    }
  }
  return (
    <Dialog title={u.editTitle(user.username)} busy={a.busy} error={a.error} onClose={onClose} onSubmit={() => void a.run(save, onClose)}>
      <div className="lc-fields">
        <Field label={u.firstName}>{(id) => <input id={id} className="lc-input" value={form.firstName} onChange={set('firstName')} />}</Field>
        <Field label={u.lastName}>{(id) => <input id={id} className="lc-input" value={form.lastName} onChange={set('lastName')} />}</Field>
      </div>
      {user.local ? (
        <PasswordFields label={u.newPassword} hint={u.newPasswordHint}
          password={form.password} confirm={confirm} onPassword={(v) => setForm({ ...form, password: v })} onConfirm={setConfirm} />
      ) : (
        <p className="lc-muted m-0 text-[13px]">{u.ssoOnly}</p>
      )}
      <RolePicker roles={roles} selected={form.roles} onChange={(ids) => setForm({ ...form, roles: ids })}
        hint={user.oidc ? u.oidcRoles : undefined} />
    </Dialog>
  )
}

function DeleteUser({ user, onClose }: { user: User; onClose: () => void }) {
  const [remove] = useMutation(DeleteUserDoc, refetch)
  const t = useT()
  const u = t.admin.users
  const a = useAction()
  return (
    <Dialog title={u.deleteTitle(user.username)} submit={t.admin.delete} danger busy={a.busy} error={a.error} onClose={onClose}
      onSubmit={() => void a.run(() => remove({ variables: { id: user.id } }), onClose)}>
      <p className="m-0">{u.deleteText}{user.oidc ? u.deleteOidc : u.deleteFinal}</p>
    </Dialog>
  )
}
