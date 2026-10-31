import { useMutation, useQuery } from '@apollo/client'
import { useState } from 'react'
import { Dialog, Field, useAction } from '@/components/Dialog'
import { FilterBar } from '@/components/FilterBar'
import { Pager, SearchInput, SortHeader, useTable } from '@/components/Listing'
import { graphql } from '@/graphql'
import type { AdminRolesQuery, GlobalGrant, GrantAction, NamespaceGrant } from '@/graphql/graphql'
import { plural } from '@/lib/format'

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

export const namespaceGrantFr: Record<NamespaceGrant, string> = { INVENTORY: 'Inventaire', METADATA: 'Métadonnées', ALL: 'Tout' }
export const globalGrantFr: Record<GlobalGrant, string> = { NAMESPACES: 'Namespaces', TOKENS: 'Tokens et agents', USERS: 'Utilisateurs', ROLES: 'Rôles', ICONS: 'Logos des technos', TASKS: 'Tâches planifiées', ALL: 'Tout' }
export const actionFr: Record<GrantAction, string> = { READ: 'lecture', WRITE: 'écriture', ALL: 'tout' }

// grantText says a permission in a few words: « acme · Inventaire lecture ».
export function grantText(p: { namespace?: { slug: string } | null; resource: string; action: GrantAction }, global = false): string {
  const resource = global ? globalGrantFr[p.resource as GlobalGrant] : namespaceGrantFr[p.resource as NamespaceGrant]
  const where = global ? 'global' : p.namespace?.slug ?? 'tous les namespaces'
  return `${where} · ${resource} ${p.action === 'ALL' ? '(tout)' : actionFr[p.action]}`
}

// Roles lists the roles with their permissions, and creates, edits and
// deletes them when the user may write them.
export function Roles({ write }: { write: boolean }) {
  const { data, error } = useQuery(RolesDoc)
  const [dialog, setDialog] = useState<{ kind: 'create' } | { kind: 'edit' | 'delete'; role: Role } | null>(null)
  const t = useTable(data?.roles ?? [], (r) => [r.code, r.description ?? '', ...r.oidcGroups, ...r.permissions.map((p) => p.namespace?.slug ?? '')],
    (r, by) => by === 'users' ? r.users : r.code, 'code')
  const head = (label: string, by: string, num?: boolean) => <SortHeader label={label} by={by} sort={t.sort.by} desc={t.sort.desc} onSort={t.onSort} num={num} />

  if (error) return <p role="alert">Les rôles n'ont pas pu être chargés : {error.message}</p>
  if (!data) return <span className="lc-muted">Chargement…</span>
  return (
    <>
      <div className="flex flex-wrap items-center gap-2">
        <div className="min-w-0 flex-1">
          <FilterBar search={<SearchInput value={t.q} onChange={t.setQ} placeholder="Chercher un rôle, un groupe, un namespace" width={300} />}
            fields={[]} count={plural(t.list.length, 'rôle')} />
        </div>
        {write ? <button type="button" className="lc-btn lc-btn--primary" onClick={() => setDialog({ kind: 'create' })}>Nouveau rôle</button> : null}
      </div>
      <div className="overflow-x-auto rounded-lg border border-border">
        <table className="lc-table">
          <thead><tr>
            {head('Rôle', 'code')}<th>Permissions</th><th>Groupes OIDC</th>{head('Utilisateurs', 'users', true)}
            {write ? <th><span className="sr-only">Actions</span></th> : null}
          </tr></thead>
          <tbody>
            {t.shown.map((r) => (
              <tr key={r.id} className="align-top">
                <td style={{ whiteSpace: 'normal', minWidth: 180 }}>
                  <span className="lc-mono font-semibold">{r.code}</span>
                  {r.builtin ? <span className="lc-badge lc-badge--plain ml-2">intégré</span> : null}
                  {r.description ? <div className="lc-muted text-[12.5px] leading-[18px]">{r.description}</div> : null}
                </td>
                <td style={{ whiteSpace: 'normal', minWidth: 240 }}>
                  {r.permissions.length + r.globalPermissions.length === 0 ? <span className="lc-muted">aucune</span> : (
                    <ul className="m-0 flex list-none flex-col gap-0.5 p-0 text-[13px]">
                      {r.permissions.map((p, i) => <li key={'n' + i}>{grantText(p)}</li>)}
                      {r.globalPermissions.map((p, i) => <li key={'g' + i}>{grantText(p, true)}</li>)}
                    </ul>
                  )}
                </td>
                <td style={{ whiteSpace: 'normal' }}>{r.oidcGroups.length
                  ? <span className="lc-tags">{r.oidcGroups.map((g) => <span key={g} className="lc-tag pr-2">{g}</span>)}</span>
                  : <span className="lc-muted">—</span>}</td>
                <td className="lc-num">{r.users}</td>
                {write ? (
                  <td className="lc-num">
                    <span className="inline-flex gap-1">
                      <button type="button" className="lc-btn lc-btn--small" onClick={() => setDialog({ kind: 'edit', role: r })}>Modifier</button>
                      <button type="button" className="lc-btn lc-btn--small lc-btn--danger" disabled={r.builtin}
                        title={r.builtin ? 'Le rôle admin ne peut pas être supprimé' : undefined}
                        onClick={() => setDialog({ kind: 'delete', role: r })}>Supprimer</button>
                    </span>
                  </td>
                ) : null}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Pager page={t.page} size={t.size} total={t.list.length} noun="rôles" onPage={t.setPage} onSize={t.setSize} />
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
    <Dialog title={role ? `Modifier le rôle ${role.code}` : 'Nouveau rôle'} submit={role ? 'Enregistrer' : 'Créer'} wide busy={a.busy} error={a.error}
      onClose={onClose} onSubmit={() => void a.run(save, onClose)}>
      <Field label="Code" hint={builtin ? 'Le rôle admin garde son code.' : 'Lettres minuscules, chiffres et . _ -, par ex. acme-viewer.'}>
        {(id) => <input id={id} className="lc-input lc-mono" required readOnly={builtin} autoCapitalize="none" spellCheck={false} value={code} onChange={(e) => setCode(e.target.value)} />}
      </Field>
      <Field label="Description">
        {(id) => <textarea id={id} className="lc-input" rows={2} value={description} onChange={(e) => setDescription(e.target.value)} />}
      </Field>

      <div className="lc-field">
        <span className="lc-label">Permissions sur les namespaces</span>
        {builtin ? <span className="lc-field__hint">Le rôle admin garde toutes les permissions sur tous les namespaces.</span> : (
          <div className="lc-grants">
            {ns.map((p, i) => (
              <div key={i} className="lc-grant">
                <select className="lc-select" aria-label="Namespace" value={p.namespace} onChange={(e) => setNsRow(i, { namespace: e.target.value })}>
                  <option value="">Tous les namespaces</option>
                  {sorted.map((n) => <option key={n.id} value={n.id}>{n.slug}</option>)}
                </select>
                <select className="lc-select" aria-label="Ressource" value={p.resource} onChange={(e) => setNsRow(i, { resource: e.target.value as NamespaceGrant })}>
                  {(Object.keys(namespaceGrantFr) as NamespaceGrant[]).map((r) => <option key={r} value={r}>{namespaceGrantFr[r]}</option>)}
                </select>
                <select className="lc-select" aria-label="Action" value={p.action} onChange={(e) => setNsRow(i, { action: e.target.value as GrantAction })}>
                  {actions.map((x) => <option key={x} value={x}>{actionFr[x]}</option>)}
                </select>
                <button type="button" className="lc-btn lc-btn--ghost lc-btn--icon" aria-label="Retirer la permission" onClick={() => setNs(ns.filter((_, j) => j !== i))}>×</button>
              </div>
            ))}
            <button type="button" className="lc-btn lc-btn--small self-start"
              onClick={() => setNs([...ns, { namespace: sorted[0]?.id ?? '', resource: 'INVENTORY', action: 'READ' }])}>Ajouter une permission</button>
            <span className="lc-field__hint">Inventaire : hosts, apps, graphe, suppression d'un host. Métadonnées : noms, descriptions, logos, ordre des envs, seuils.
              L'écriture n'inclut pas la lecture : ajoutez les deux.</span>
          </div>
        )}
      </div>

      <div className="lc-field">
        <span className="lc-label">Permissions globales</span>
        {builtin ? <span className="lc-field__hint">Le rôle admin garde toutes les permissions globales.</span> : (
          <div className="lc-grants">
            {global.map((p, i) => (
              <div key={i} className="lc-grant lc-grant--global">
                <select className="lc-select" aria-label="Ressource" value={p.resource} onChange={(e) => setGlobalRow(i, { resource: e.target.value as GlobalGrant })}>
                  {(Object.keys(globalGrantFr) as GlobalGrant[]).map((r) => <option key={r} value={r}>{globalGrantFr[r]}</option>)}
                </select>
                <select className="lc-select" aria-label="Action" value={p.action} onChange={(e) => setGlobalRow(i, { action: e.target.value as GrantAction })}>
                  {actions.map((x) => <option key={x} value={x}>{actionFr[x]}</option>)}
                </select>
                <button type="button" className="lc-btn lc-btn--ghost lc-btn--icon" aria-label="Retirer la permission" onClick={() => setGlobal(global.filter((_, j) => j !== i))}>×</button>
              </div>
            ))}
            <button type="button" className="lc-btn lc-btn--small self-start" onClick={() => setGlobal([...global, { resource: 'TOKENS', action: 'READ' }])}>Ajouter une permission</button>
          </div>
        )}
      </div>

      <Field label="Groupes OIDC" hint="Les membres de ces groupes reçoivent le rôle à leur connexion SSO.">
        {(id) => (
          <div className="flex flex-col gap-2">
            {groups.length ? (
              <span className="lc-tags">{groups.map((g) => (
                <span key={g} className="lc-tag">{g}<button type="button" aria-label={`Retirer le groupe ${g}`} onClick={() => setGroups(groups.filter((x) => x !== g))}>×</button></span>
              ))}</span>
            ) : null}
            <span className="flex gap-2">
              <input id={id} className="lc-input flex-1" placeholder="Nom du groupe" value={group} onChange={(e) => setGroup(e.target.value)}
                onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); addGroup() } }} />
              <button type="button" className="lc-btn" onClick={addGroup} disabled={!group.trim()}>Ajouter</button>
            </span>
          </div>
        )}
      </Field>
    </Dialog>
  )
}

function DeleteRole({ role, onClose }: { role: Role; onClose: () => void }) {
  const [remove] = useMutation(DeleteRoleDoc, refetch)
  const a = useAction()
  return (
    <Dialog title={`Supprimer le rôle ${role.code} ?`} submit="Supprimer" danger busy={a.busy} error={a.error} onClose={onClose}
      onSubmit={() => void a.run(() => remove({ variables: { id: role.id } }), onClose)}>
      <p className="m-0">{role.users > 0 ? `Les ${role.users} utilisateurs qui le détiennent le perdent` : 'Aucun utilisateur ne le détient'}
        {role.oidcGroups.length ? `, ainsi que les membres des groupes ${role.oidcGroups.join(', ')}` : ''}. Cette action est définitive.</p>
    </Dialog>
  )
}
