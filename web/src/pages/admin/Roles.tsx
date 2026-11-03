import { useMutation, useQuery } from '@apollo/client'
import { useState } from 'react'
import { Dialog, Field, useAction } from '@/components/Dialog'
import { FilterBar } from '@/components/FilterBar'
import { Pager, SearchInput, SortHeader, useTable } from '@/components/Listing'
import { graphql } from '@/graphql'
import type { AdminRolesQuery, GlobalGrant, GrantAction, NamespaceGrant } from '@/graphql/graphql'
import { t as text, useT } from '@/i18n'

const RolesDoc = graphql(`
  query AdminRoles {
    roles {
      id code description builtin users oidcGroups
      permissions { namespace { id slug } resource action }
      globalPermissions { resource action }
    }
    namespaceChoices { id slug name }
  }
`)

const CreateRoleDoc = graphql(`
  mutation CreateRole($input: RoleInput!) {
    createRole(input: $input) { id }
  }
`)

const UpdateRoleDoc = graphql(`
  mutation UpdateRole($id: ID!, $input: RoleInput!) {
    updateRole(id: $id, input: $input) { id }
  }
`)

const DeleteRoleDoc = graphql(`
  mutation DeleteRole($id: ID!) {
    deleteRole(id: $id)
  }
`)

type Role = AdminRolesQuery['roles'][number]
type Namespace = AdminRolesQuery['namespaceChoices'][number]

const refetch = { refetchQueries: ['AdminRoles', 'AdminUsers'] }

const namespaceGrants: NamespaceGrant[] = ['INVENTORY', 'METADATA', 'ALL']
const globalGrants: GlobalGrant[] = ['NAMESPACES', 'TOKENS', 'USERS', 'ROLES', 'ICONS', 'TASKS', 'ALL']

// grantText says a permission in a few words: « acme · Inventaire lecture ».
export function grantText(p: { namespace?: { slug: string } | null; resource: string; action: GrantAction }, global = false): string {
  const r = text().admin.roles
  const resource = global ? r.globalGrants[p.resource] : r.namespaceGrants[p.resource]
  const where = global ? r.global : p.namespace?.slug ?? r.allNamespaces
  return `${where} · ${resource} ${p.action === 'ALL' ? r.allActions : r.actions[p.action]}`
}

// Roles lists the roles with their permissions, and creates, edits and
// deletes them when the user may write them.
export function Roles({ write }: { write: boolean }) {
  const { data, error } = useQuery(RolesDoc)
  const [dialog, setDialog] = useState<{ kind: 'create' } | { kind: 'edit' | 'delete'; role: Role } | null>(null)
  const t = useT()
  const r = t.admin.roles
  const tbl = useTable(data?.roles ?? [], (r) => [r.code, r.description ?? '', ...r.oidcGroups, ...r.permissions.map((p) => p.namespace?.slug ?? '')],
    (r, by) => by === 'users' ? r.users : r.code, 'code')
  const head = (label: string, by: string, num?: boolean) => <SortHeader label={label} by={by} sort={tbl.sort.by} desc={tbl.sort.desc} onSort={tbl.onSort} num={num} />

  if (error) return <p role="alert">{r.loadError}{error.message}</p>
  if (!data) return <span className="lc-muted">{t.admin.loading}</span>
  return (
    <>
      <div className="flex flex-wrap items-center gap-2">
        <div className="min-w-0 flex-1">
          <FilterBar search={<SearchInput value={tbl.q} onChange={tbl.setQ} placeholder={r.search} width={300} />}
            fields={[]} count={r.count(tbl.list.length)} />
        </div>
        {write ? <button type="button" className="lc-btn lc-btn--primary" onClick={() => setDialog({ kind: 'create' })}>{r.create}</button> : null}
      </div>
      <div className="overflow-x-auto rounded-lg border border-border">
        <table className="lc-table">
          <thead><tr>
            {head(r.role, 'code')}<th>{r.permissions}</th><th>{r.oidcGroups}</th>{head(r.users, 'users', true)}
            {write ? <th><span className="sr-only">{t.admin.actions}</span></th> : null}
          </tr></thead>
          <tbody>
            {tbl.shown.map((x) => (
              <tr key={x.id} className="align-top">
                <td style={{ whiteSpace: 'normal', minWidth: 180 }}>
                  <span className="lc-mono font-semibold">{x.code}</span>
                  {x.builtin ? <span className="lc-badge lc-badge--plain ml-2">{r.builtin}</span> : null}
                  {x.description ? <div className="lc-muted text-[12.5px] leading-[18px]">{x.description}</div> : null}
                </td>
                <td style={{ whiteSpace: 'normal', minWidth: 240 }}>
                  {x.permissions.length + x.globalPermissions.length === 0 ? <span className="lc-muted">{r.none}</span> : (
                    <ul className="m-0 flex list-none flex-col gap-0.5 p-0 text-[13px]">
                      {x.permissions.map((p, i) => <li key={'n' + i}>{grantText(p)}</li>)}
                      {x.globalPermissions.map((p, i) => <li key={'g' + i}>{grantText(p, true)}</li>)}
                    </ul>
                  )}
                </td>
                <td style={{ whiteSpace: 'normal' }}>{x.oidcGroups.length
                  ? <span className="lc-tags">{x.oidcGroups.map((g) => <span key={g} className="lc-tag pr-2">{g}</span>)}</span>
                  : <span className="lc-muted">—</span>}</td>
                <td className="lc-num">{x.users}</td>
                {write ? (
                  <td className="lc-num">
                    <span className="inline-flex gap-1">
                      <button type="button" className="lc-btn lc-btn--small" onClick={() => setDialog({ kind: 'edit', role: x })}>{t.admin.edit}</button>
                      <button type="button" className="lc-btn lc-btn--small lc-btn--danger" disabled={x.builtin}
                        title={x.builtin ? r.adminUndeletable : undefined}
                        onClick={() => setDialog({ kind: 'delete', role: x })}>{t.admin.delete}</button>
                    </span>
                  </td>
                ) : null}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Pager page={tbl.page} size={tbl.size} total={tbl.list.length} noun={r.noun} onPage={tbl.setPage} onSize={tbl.setSize} />
      {dialog?.kind === 'create' ? <RoleForm namespaces={data.namespaceChoices} onClose={() => setDialog(null)} /> : null}
      {dialog?.kind === 'edit' ? <RoleForm role={dialog.role} namespaces={data.namespaceChoices} onClose={() => setDialog(null)} /> : null}
      {dialog?.kind === 'delete' ? <DeleteRole role={dialog.role} onClose={() => setDialog(null)} /> : null}
    </>
  )
}

interface NsGrant { namespace: string; resource: NamespaceGrant; action: GrantAction }
interface GlobalRow { resource: GlobalGrant; action: GrantAction }

const actions: GrantAction[] = ['READ', 'WRITE', 'ALL']

function RoleForm({ role, namespaces, onClose }: { role?: Role; namespaces: Namespace[]; onClose: () => void }) {
  const [create] = useMutation(CreateRoleDoc, refetch)
  const [update] = useMutation(UpdateRoleDoc, refetch)
  const t = useT()
  const r = t.admin.roles
  const a = useAction()
  const [code, setCode] = useState(role?.code ?? '')
  const [description, setDescription] = useState(role?.description ?? '')
  const [groups, setGroups] = useState<string[]>(role?.oidcGroups ?? [])
  const [group, setGroup] = useState('')
  const [ns, setNs] = useState<NsGrant[]>(role?.permissions.map((p) => ({ namespace: p.namespace?.id ?? '', resource: p.resource, action: p.action })) ?? [])
  const [global, setGlobal] = useState<GlobalRow[]>(role?.globalPermissions.map((p) => ({ resource: p.resource, action: p.action })) ?? [])
  const builtin = !!role?.builtin
  const sorted = [...namespaces].sort((x, y) => x.slug.localeCompare(y.slug))

  const addGroup = () => {
    const g = group.trim()
    if (g && !groups.includes(g)) setGroups([...groups, g])
    setGroup('')
  }
  const save = () => {
    // A group typed but not added yet is added too.
    const all = group.trim() && !groups.includes(group.trim()) ? [...groups, group.trim()] : groups
    const input = {
      code, description, oidcGroups: all,
      ...(builtin ? {} : {
        permissions: ns.map((p) => ({ namespace: p.namespace || null, resource: p.resource, action: p.action })),
        globalPermissions: global,
      }),
    }
    return role ? update({ variables: { id: role.id, input } }) : create({ variables: { input } })
  }
  const setNsRow = (i: number, change: Partial<NsGrant>) => setNs(ns.map((p, j) => (j === i ? { ...p, ...change } : p)))
  const setGlobalRow = (i: number, change: Partial<GlobalRow>) => setGlobal(global.map((p, j) => (j === i ? { ...p, ...change } : p)))

  return (
    <Dialog title={role ? r.editTitle(role.code) : r.create} submit={role ? t.admin.save : t.admin.create} wide busy={a.busy} error={a.error}
      onClose={onClose} onSubmit={() => void a.run(save, onClose)}>
      <Field label={r.code} hint={builtin ? r.adminCode : r.codeHint}>
        {(id) => <input id={id} className="lc-input lc-mono" required readOnly={builtin} autoCapitalize="none" spellCheck={false} value={code} onChange={(e) => setCode(e.target.value)} />}
      </Field>
      <Field label={t.admin.description}>
        {(id) => <textarea id={id} className="lc-input" rows={2} value={description} onChange={(e) => setDescription(e.target.value)} />}
      </Field>

      <div className="lc-field">
        <span className="lc-label">{r.namespacePermissions}</span>
        {builtin ? <span className="lc-field__hint">{r.adminNamespaces}</span> : (
          <div className="lc-grants">
            {ns.map((p, i) => (
              <div key={i} className="lc-grant">
                <select className="lc-select" aria-label={r.namespace} value={p.namespace} onChange={(e) => setNsRow(i, { namespace: e.target.value })}>
                  <option value="">{r.allNamespacesOption}</option>
                  {sorted.map((n) => <option key={n.id} value={n.id}>{n.slug}</option>)}
                </select>
                <select className="lc-select" aria-label={r.resource} value={p.resource} onChange={(e) => setNsRow(i, { resource: e.target.value as NamespaceGrant })}>
                  {namespaceGrants.map((g) => <option key={g} value={g}>{r.namespaceGrants[g]}</option>)}
                </select>
                <select className="lc-select" aria-label={r.action} value={p.action} onChange={(e) => setNsRow(i, { action: e.target.value as GrantAction })}>
                  {actions.map((x) => <option key={x} value={x}>{r.actions[x]}</option>)}
                </select>
                <button type="button" className="lc-btn lc-btn--ghost lc-btn--icon" aria-label={r.removePermission} onClick={() => setNs(ns.filter((_, j) => j !== i))}>×</button>
              </div>
            ))}
            <button type="button" className="lc-btn lc-btn--small self-start"
              onClick={() => setNs([...ns, { namespace: sorted[0]?.id ?? '', resource: 'INVENTORY', action: 'READ' }])}>{r.addPermission}</button>
            <span className="lc-field__hint">{r.grantsHint}</span>
          </div>
        )}
      </div>

      <div className="lc-field">
        <span className="lc-label">{r.globalPermissions}</span>
        {builtin ? <span className="lc-field__hint">{r.adminGlobal}</span> : (
          <div className="lc-grants">
            {global.map((p, i) => (
              <div key={i} className="lc-grant lc-grant--global">
                <select className="lc-select" aria-label={r.resource} value={p.resource} onChange={(e) => setGlobalRow(i, { resource: e.target.value as GlobalGrant })}>
                  {globalGrants.map((g) => <option key={g} value={g}>{r.globalGrants[g]}</option>)}
                </select>
                <select className="lc-select" aria-label={r.action} value={p.action} onChange={(e) => setGlobalRow(i, { action: e.target.value as GrantAction })}>
                  {actions.map((x) => <option key={x} value={x}>{r.actions[x]}</option>)}
                </select>
                <button type="button" className="lc-btn lc-btn--ghost lc-btn--icon" aria-label={r.removePermission} onClick={() => setGlobal(global.filter((_, j) => j !== i))}>×</button>
              </div>
            ))}
            <button type="button" className="lc-btn lc-btn--small self-start" onClick={() => setGlobal([...global, { resource: 'TOKENS', action: 'READ' }])}>{r.addPermission}</button>
          </div>
        )}
      </div>

      <Field label={r.oidcGroups} hint={r.groupsHint}>
        {(id) => (
          <div className="flex flex-col gap-2">
            {groups.length ? (
              <span className="lc-tags">{groups.map((g) => (
                <span key={g} className="lc-tag">{g}<button type="button" aria-label={r.removeGroup(g)} onClick={() => setGroups(groups.filter((x) => x !== g))}>×</button></span>
              ))}</span>
            ) : null}
            <span className="flex gap-2">
              <input id={id} className="lc-input flex-1" placeholder={r.groupName} value={group} onChange={(e) => setGroup(e.target.value)}
                onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); addGroup() } }} />
              <button type="button" className="lc-btn" onClick={addGroup} disabled={!group.trim()}>{t.admin.add}</button>
            </span>
          </div>
        )}
      </Field>
    </Dialog>
  )
}

function DeleteRole({ role, onClose }: { role: Role; onClose: () => void }) {
  const [remove] = useMutation(DeleteRoleDoc, refetch)
  const t = useT()
  const a = useAction()
  return (
    <Dialog title={t.admin.roles.deleteTitle(role.code)} submit={t.admin.delete} danger busy={a.busy} error={a.error} onClose={onClose}
      onSubmit={() => void a.run(() => remove({ variables: { id: role.id } }), onClose)}>
      <p className="m-0">{t.admin.roles.deleteText(role.users, role.oidcGroups)}</p>
    </Dialog>
  )
}
