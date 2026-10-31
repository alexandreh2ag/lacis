import { useMutation, useQuery } from '@apollo/client'
import { useEffect, useState } from 'react'
import { Navigate, useParams } from 'react-router'
import { AddPicker } from '@/components/AddPicker'
import { CopyButton } from '@/components/Copy'
import { Dialog, Field, useAction } from '@/components/Dialog'
import { FilterBar } from '@/components/FilterBar'
import { Page } from '@/components/Layout'
import { Pager, SearchInput, SortHeader, useTable } from '@/components/Listing'
import { Tabs, useTab } from '@/components/Tabs'
import { graphql } from '@/graphql'
import type { NamespaceSettingsQuery } from '@/graphql/graphql'
import { useT } from '@/i18n'
import { parseDuration, writeDuration } from '@/lib/format'

const SettingsDoc = graphql(`
  query NamespaceSettings($ns: String!) {
    namespace(slug: $ns) {
      id slug name description staleAfter inactiveAfter snapshotRetention canEdit
      projects { id slug name description }
      environments { id slug name sortOrder }
      networks { id slug name kind description cidrs splitFrom { id slug name } hosts { id hostname } }
    }
  }
`)

const UpdateNamespaceDoc = graphql(`
  mutation UpdateNamespace($ns: String!, $input: NamespaceInput!) {
    updateNamespace(namespace: $ns, input: $input) { id name description staleAfter inactiveAfter snapshotRetention }
  }
`)

const UpdateProjectDoc = graphql(`
  mutation UpdateProject($ns: String!, $slug: String!, $input: ProjectInput!) {
    updateProject(namespace: $ns, slug: $slug, input: $input) { id name description }
  }
`)

const UpdateEnvironmentDoc = graphql(`
  mutation UpdateEnvironment($ns: String!, $slug: String!, $input: EnvironmentInput!) {
    updateEnvironment(namespace: $ns, slug: $slug, input: $input) { id name }
  }
`)

const ReorderEnvironmentsDoc = graphql(`
  mutation ReorderEnvironments($ns: String!, $slugs: [String!]!) {
    reorderEnvironments(namespace: $ns, slugs: $slugs) { id sortOrder }
  }
`)

const UpdateNetworkDoc = graphql(`
  mutation UpdateNetwork($ns: String!, $slug: String!, $input: NetworkInput!) {
    updateNetwork(namespace: $ns, slug: $slug, input: $input) { id name description kind }
  }
`)

const SplitNetworkDoc = graphql(`
  mutation SplitNetwork($ns: String!, $slug: String!, $name: String!, $hosts: [ID!]!) {
    splitNetwork(namespace: $ns, slug: $slug, name: $name, hosts: $hosts) { id slug }
  }
`)

const MoveHostsToNetworkDoc = graphql(`
  mutation MoveHostsToNetwork($ns: String!, $slug: String!, $hosts: [ID!]!) {
    moveHostsToNetwork(namespace: $ns, slug: $slug, hosts: $hosts) { id slug }
  }
`)

const DeleteNetworkDoc = graphql(`
  mutation DeleteNetwork($ns: String!, $slug: String!) {
    deleteNetwork(namespace: $ns, slug: $slug)
  }
`)

// The hosts change networks: every network is read again.
const refetchNetworks = { refetchQueries: ['NamespaceSettings'] }

type Namespace = NonNullable<NamespaceSettingsQuery['namespace']>
type Project = Namespace['projects'][number]
type Environment = Namespace['environments'][number]
type Network = Namespace['networks'][number]

const tabIds = ['general', 'projects', 'environments', 'networks'] as const

// Settings changes what people say of a namespace: names, descriptions,
// the order of the envs and the freshness thresholds.
export function Settings() {
  const { ns = '' } = useParams()
  const { data, error } = useQuery(SettingsDoc, { variables: { ns } })
  const t = useT()
  const st = t.admin.settings
  const tabs = tabIds.map((id) => ({ id, label: st.tabs[id] }))
  const [tab, setTab] = useTab(tabs, 'general')
  const n = data?.namespace
  if (n && !n.canEdit) return <Navigate to={`/${ns}`} replace />
  return (
    <Page title={st.title} meta={n ? <span>{n.name} · <span className="lc-mono">{n.slug}</span></span> : null}>
      {error ? <p role="alert">{st.loadError}{error.message}</p> : null}
      {data && !n ? <p role="alert">{st.notFound}</p> : null}
      {n ? (
        <Tabs label={st.title} current={tab} onChange={setTab}
          tabs={tabs.map((x) => ({ ...x, count: x.id === 'projects' ? n.projects.length : x.id === 'environments' ? n.environments.length : x.id === 'networks' ? n.networks.length : undefined }))}>
          {tab === 'general' ? <General n={n} /> : null}
          {tab === 'projects' ? <Projects ns={ns} projects={n.projects} /> : null}
          {tab === 'environments' ? <Environments ns={ns} environments={n.environments} /> : null}
          {tab === 'networks' ? <Networks ns={ns} networks={n.networks} /> : null}
        </Tabs>
      ) : !error && !data ? <span className="lc-muted">{t.admin.loading}</span> : null}
    </Page>
  )
}

function General({ n }: { n: Namespace }) {
  const [update] = useMutation(UpdateNamespaceDoc)
  const a = useAction()
  const initial = () => ({ name: n.name, description: n.description ?? '',
    staleAfter: writeDuration(n.staleAfter), inactiveAfter: writeDuration(n.inactiveAfter), snapshotRetention: writeDuration(n.snapshotRetention) })
  const [form, setForm] = useState(initial)
  const [saved, setSaved] = useState(false)
  useEffect(() => setSaved(false), [form])
  const set = (k: keyof ReturnType<typeof initial>) => (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => setForm({ ...form, [k]: e.target.value })
  const t = useT()
  const st = t.admin.settings
  const durations = st.durations

  const save = async () => {
    const seconds: Record<string, number> = {}
    for (const [k, label] of Object.entries(durations)) {
      const s = parseDuration(form[k as keyof typeof durations])
      if (s === null) throw new Error(st.badDuration(label))
      seconds[k] = s
    }
    await update({ variables: { ns: n.slug, input: { name: form.name, description: form.description, ...seconds } } })
    setSaved(true)
  }
  return (
    <form className="flex max-w-[720px] flex-col gap-4" onSubmit={(e) => { e.preventDefault(); void a.run(save) }}>
      {a.error ? <div className="lc-alert" role="alert">{a.error}</div> : null}
      <Field label={t.admin.name}>{(id) => <input id={id} className="lc-input" required value={form.name} onChange={set('name')} />}</Field>
      <Field label={t.admin.description}>{(id) => <textarea id={id} className="lc-input" rows={3} value={form.description} onChange={set('description')} />}</Field>
      <div className="lc-fields">
        <Field label={durations.staleAfter} hint={st.staleHint}>
          {(id) => <input id={id} className="lc-input lc-mono" required value={form.staleAfter} onChange={set('staleAfter')} />}
        </Field>
        <Field label={durations.inactiveAfter} hint={st.inactiveHint}>
          {(id) => <input id={id} className="lc-input lc-mono" required value={form.inactiveAfter} onChange={set('inactiveAfter')} />}
        </Field>
        <Field label={durations.snapshotRetention}>
          {(id) => <input id={id} className="lc-input lc-mono" required value={form.snapshotRetention} onChange={set('snapshotRetention')} />}
        </Field>
      </div>
      <span className="lc-field__hint -mt-2">{st.durationsHint}</span>
      <div className="flex items-center gap-3">
        <button type="submit" className="lc-btn lc-btn--primary" disabled={a.busy}>{a.busy ? st.saving : t.admin.save}</button>
        <button type="button" className="lc-btn lc-btn--ghost" onClick={() => setForm(initial())}>{st.undo}</button>
        {saved ? <span className="text-[13px] text-status-ok" role="status">{st.saved}</span> : null}
      </div>
    </form>
  )
}

function Projects({ ns, projects }: { ns: string; projects: Project[] }) {
  // The slug edited: the dialog follows the project as reloaded.
  const [editing, setEditing] = useState<string | null>(null)
  const edited = projects.find((p) => p.slug === editing)
  const t = useT()
  const st = t.admin.settings
  const tbl = useTable(projects, (p) => [p.slug, p.name, p.description ?? ''], (p, by) => by === 'name' ? p.name : p.slug, 'slug')
  const head = (label: string, by: string) => <SortHeader label={label} by={by} sort={tbl.sort.by} desc={tbl.sort.desc} onSort={tbl.onSort} />
  return (
    <>
      <FilterBar search={<SearchInput value={tbl.q} onChange={tbl.setQ} placeholder={st.searchProject} width={280} />} fields={[]} count={st.projects(tbl.list.length)} />
      <p className="lc-muted m-0 text-[13px]">{st.projectsIntro[0]}<code className="lc-mono">project</code>{st.projectsIntro[1]}</p>
      <div className="overflow-x-auto rounded-lg border border-border">
        <table className="lc-table">
          <thead><tr>{head(st.slug, 'slug')}{head(t.admin.name, 'name')}<th>{t.admin.description}</th><th><span className="sr-only">{t.admin.actions}</span></th></tr></thead>
          <tbody>
            {tbl.shown.map((p) => (
              <tr key={p.id}>
                <td><span className="lc-copyable"><span className="lc-mono font-semibold">{p.slug}</span><CopyButton value={p.slug} /></span></td>
                <td>{p.name}</td>
                <td className="text-ink-muted" style={{ whiteSpace: 'normal', minWidth: 220 }}>{p.description}</td>
                <td className="lc-num"><button type="button" className="lc-btn lc-btn--small" onClick={() => setEditing(p.slug)}>{t.admin.edit}</button></td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Pager page={tbl.page} size={tbl.size} total={tbl.list.length} noun={st.projectsNoun} onPage={tbl.setPage} onSize={tbl.setSize} />
      {edited ? <EditProject ns={ns} project={edited} onClose={() => setEditing(null)} /> : null}
    </>
  )
}

function EditProject({ ns, project, onClose }: { ns: string; project: Project; onClose: () => void }) {
  const [update] = useMutation(UpdateProjectDoc)
  const a = useAction()
  const [form, setForm] = useState({ name: project.name, description: project.description ?? '' })
  const set = (k: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => setForm({ ...form, [k]: e.target.value })
  const t = useT()
  return (
    <Dialog title={t.admin.settings.project(project.slug)} busy={a.busy} error={a.error} onClose={onClose}
      onSubmit={() => void a.run(() => update({ variables: { ns, slug: project.slug, input: form } }), onClose)}>
      <Field label={t.admin.name}>{(id) => <input id={id} className="lc-input" required value={form.name} onChange={set('name')} />}</Field>
      <Field label={t.admin.description}>{(id) => <textarea id={id} className="lc-input" rows={3} value={form.description} onChange={set('description')} />}</Field>
    </Dialog>
  )
}

// moved returns the list with the item i moved by delta.
export function moved<T>(list: T[], i: number, delta: number): T[] {
  const j = i + delta
  if (j < 0 || j >= list.length) return list
  const out = [...list]
  ;[out[i], out[j]] = [out[j], out[i]]
  return out
}

function Environments({ ns, environments }: { ns: string; environments: Environment[] }) {
  const sorted = [...environments].sort((a, b) => a.sortOrder - b.sortOrder || a.slug.localeCompare(b.slug))
  const [order, setOrder] = useState(sorted.map((e) => e.slug))
  const [renaming, setRenaming] = useState<Environment | null>(null)
  const [reorder] = useMutation(ReorderEnvironmentsDoc)
  const a = useAction()
  const bySlug = new Map(environments.map((e) => [e.slug, e]))
  const changed = order.join() !== sorted.map((e) => e.slug).join()
  const t = useT()
  const st = t.admin.settings
  return (
    <div className="flex max-w-[720px] flex-col gap-3">
      <p className="lc-muted m-0 text-[13px]">{st.envIntro[0]}<code className="lc-mono">env</code>{st.envIntro[1]}</p>
      {a.error ? <div className="lc-alert" role="alert">{a.error}</div> : null}
      <ol className="lc-order" aria-label={st.environments}>
        {order.map((slug, i) => {
          const e = bySlug.get(slug)
          if (!e) return null
          return (
            <li key={slug}>
              <span className="lc-order__n">{i + 1}</span>
              <span className="flex min-w-0 flex-1 flex-col leading-[18px]">
                <span className="font-semibold">{e.name}</span>
                <span className="lc-mono text-[12.5px] text-ink-muted">{e.slug}</span>
              </span>
              <button type="button" className="lc-btn lc-btn--small" onClick={() => setRenaming(e)}>{st.rename}</button>
              <button type="button" className="lc-btn lc-btn--icon lc-btn--small" aria-label={st.up(e.slug)} disabled={i === 0}
                onClick={() => setOrder(moved(order, i, -1))}>↑</button>
              <button type="button" className="lc-btn lc-btn--icon lc-btn--small" aria-label={st.down(e.slug)} disabled={i === order.length - 1}
                onClick={() => setOrder(moved(order, i, 1))}>↓</button>
            </li>
          )
        })}
      </ol>
      {order.length === 0 ? <span className="lc-muted">{st.noEnv}</span> : null}
      <div className="flex gap-3">
        <button type="button" className="lc-btn lc-btn--primary" disabled={!changed || a.busy}
          onClick={() => void a.run(() => reorder({ variables: { ns, slugs: order } }))}>{a.busy ? st.saving : st.saveOrder}</button>
        {changed ? <button type="button" className="lc-btn lc-btn--ghost" onClick={() => setOrder(sorted.map((e) => e.slug))}>{t.admin.cancel}</button> : null}
      </div>
      {renaming ? <RenameEnvironment ns={ns} env={renaming} onClose={() => setRenaming(null)} /> : null}
    </div>
  )
}

function RenameEnvironment({ ns, env, onClose }: { ns: string; env: Environment; onClose: () => void }) {
  const [update] = useMutation(UpdateEnvironmentDoc)
  const a = useAction()
  const [name, setName] = useState(env.name)
  const t = useT()
  return (
    <Dialog title={t.admin.settings.environment(env.slug)} busy={a.busy} error={a.error} onClose={onClose}
      onSubmit={() => void a.run(() => update({ variables: { ns, slug: env.slug, input: { name } } }), onClose)}>
      <Field label={t.admin.name}>{(id) => <input id={id} className="lc-input" required value={name} onChange={(e) => setName(e.target.value)} />}</Field>
    </Dialog>
  )
}

type NetworkDialog = { kind: 'edit' | 'split' | 'move' | 'delete'; network: Network }

// family are the networks of the range of n: the one its IPs give, and
// those split from it by hand.
export function family<T extends { id: string; splitFrom?: { id: string } | null }>(networks: T[], n: T): T[] {
  const root = n.splitFrom?.id ?? n.id
  return networks.filter((x) => x.id === root || x.splitFrom?.id === root)
}

function Networks({ ns, networks }: { ns: string; networks: Network[] }) {
  const [dialog, setDialog] = useState<NetworkDialog | null>(null)
  const open = (kind: NetworkDialog['kind'], network: Network) => () => setDialog({ kind, network })
  const t = useT()
  const st = t.admin.settings
  const tbl = useTable(networks, (x) => [x.slug, x.name, x.description ?? '', ...x.cidrs], (x, by) => by === 'name' ? x.name : by === 'kind' ? x.kind : x.slug, 'slug')
  const head = (label: string, by: string) => <SortHeader label={label} by={by} sort={tbl.sort.by} desc={tbl.sort.desc} onSort={tbl.onSort} />
  return (
    <>
      <FilterBar search={<SearchInput value={tbl.q} onChange={tbl.setQ} placeholder={st.searchNetwork} width={280} />} fields={[]} count={st.networks(tbl.list.length)} />
      <div className="overflow-x-auto rounded-lg border border-border">
        <table className="lc-table">
          <thead><tr>{head(st.slug, 'slug')}{head(t.admin.name, 'name')}{head(st.type, 'kind')}<th>CIDR</th><th className="lc-num">Hosts</th><th>{t.admin.description}</th><th><span className="sr-only">{t.admin.actions}</span></th></tr></thead>
          <tbody>
            {tbl.shown.map((x) => (
              <tr key={x.id}>
                <td><span className="lc-mono font-semibold">{x.slug}</span></td>
                <td>
                  <span className="flex flex-col leading-[18px]">
                    <span>{x.name}</span>
                    {x.splitFrom ? <span className="text-[12.5px] text-ink-muted">{st.splitFrom(x.splitFrom.name)}</span> : null}
                  </span>
                </td>
                <td>{st.kinds[x.kind] ?? x.kind}</td>
                <td><span className="lc-ips">{x.cidrs.map((c) => <span key={c} className="lc-copyable"><span className="lc-mono">{c}</span><CopyButton value={c} /></span>)}</span></td>
                <td className="lc-num">{x.hosts.length}</td>
                <td className="text-ink-muted" style={{ whiteSpace: 'normal', minWidth: 200 }}>{x.description}</td>
                <td className="lc-num">
                  <span className="inline-flex gap-1.5">
                    <button type="button" className="lc-btn lc-btn--small" onClick={open('edit', x)}>{t.admin.edit}</button>
                    {x.kind === 'private' ? (
                      <button type="button" className="lc-btn lc-btn--small" disabled={x.hosts.length === 0}
                        title={st.splitHint} onClick={open('split', x)}>{st.split}</button>
                    ) : null}
                    {family(networks, x).length > 1 ? <button type="button" className="lc-btn lc-btn--small" onClick={open('move', x)}>{st.attach}</button> : null}
                    {x.splitFrom ? <button type="button" className="lc-btn lc-btn--small lc-btn--danger" onClick={open('delete', x)}>{t.admin.delete}</button> : null}
                  </span>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Pager page={tbl.page} size={tbl.size} total={tbl.list.length} noun={st.networksNoun} onPage={tbl.setPage} onSize={tbl.setSize} />
      {dialog?.kind === 'edit' ? <EditNetwork ns={ns} network={dialog.network} onClose={() => setDialog(null)} /> : null}
      {dialog?.kind === 'split' ? <SplitNetwork ns={ns} network={dialog.network} onClose={() => setDialog(null)} /> : null}
      {dialog?.kind === 'move' ? <MoveHosts ns={ns} network={dialog.network} networks={networks} onClose={() => setDialog(null)} /> : null}
      {dialog?.kind === 'delete' ? <DeleteNetwork ns={ns} network={dialog.network} onClose={() => setDialog(null)} /> : null}
    </>
  )
}

function EditNetwork({ ns, network, onClose }: { ns: string; network: Network; onClose: () => void }) {
  const [update] = useMutation(UpdateNetworkDoc)
  const a = useAction()
  const [form, setForm] = useState({ name: network.name, description: network.description ?? '', kind: network.kind })
  const t = useT()
  const st = t.admin.settings
  return (
    <Dialog title={st.network(network.slug)} busy={a.busy} error={a.error} onClose={onClose}
      onSubmit={() => void a.run(() => update({ variables: { ns, slug: network.slug, input: form } }), onClose)}>
      <Field label={t.admin.name}>{(id) => <input id={id} className="lc-input" required value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />}</Field>
      <Field label={st.type}>
        {(id) => (
          <select id={id} className="lc-select" value={form.kind} onChange={(e) => setForm({ ...form, kind: e.target.value })}>
            <option value="private">{st.kinds.private}</option>
            <option value="public">{st.kinds.public}</option>
          </select>
        )}
      </Field>
      <Field label={t.admin.description}>{(id) => <textarea id={id} className="lc-input" rows={3} value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} />}</Field>
    </Dialog>
  )
}

// SplitNetwork moves hosts of a network into a new one: two private
// networks on the same range, which the IPs alone cannot tell apart.
function SplitNetwork({ ns, network, onClose }: { ns: string; network: Network; onClose: () => void }) {
  const [split] = useMutation(SplitNetworkDoc, refetchNetworks)
  const a = useAction()
  const [name, setName] = useState('')
  const [hosts, setHosts] = useState<string[]>([])
  const st = useT().admin.settings
  return (
    <Dialog title={st.splitTitle(network.name)} submit={st.split} busy={a.busy} error={a.error} onClose={onClose} wide
      onSubmit={() => void a.run(() => split({ variables: { ns, slug: network.slug, name, hosts } }), onClose)}>
      <p className="m-0 text-[13px] text-ink-muted">{st.splitText(network.cidrs.join(', '))}</p>
      <Field label={st.newName}>{(id) => <input id={id} className="lc-input" required value={name} onChange={(e) => setName(e.target.value)} />}</Field>
      <AddPicker label={st.hostsToMove} placeholder={st.searchNetworkHost} empty={st.noHostToMove}
        items={network.hosts.map((h) => ({ id: h.id, label: h.hostname }))} selected={hosts} onChange={setHosts} />
    </Dialog>
  )
}

// MoveHosts brings hosts of the other networks of the range into this one.
function MoveHosts({ ns, network, networks, onClose }: { ns: string; network: Network; networks: Network[]; onClose: () => void }) {
  const [move] = useMutation(MoveHostsToNetworkDoc, refetchNetworks)
  const a = useAction()
  const [hosts, setHosts] = useState<string[]>([])
  const st = useT().admin.settings
  const items = family(networks, network).filter((x) => x.id !== network.id)
    .flatMap((x) => x.hosts.map((h) => ({ id: h.id, label: h.hostname, detail: x.name })))
    .sort((p, q) => p.label.localeCompare(q.label, 'fr', { numeric: true }))
  return (
    <Dialog title={st.attachTitle(network.name)} submit={st.attachSubmit} busy={a.busy} error={a.error} onClose={onClose} wide
      onSubmit={() => void a.run(() => move({ variables: { ns, slug: network.slug, hosts } }), onClose)}>
      <p className="m-0 text-[13px] text-ink-muted">{st.attachText}</p>
      <AddPicker label={st.hostsToAttach} placeholder={st.searchRangeHost} empty={st.noHostChosen}
        items={items} selected={hosts} onChange={setHosts} />
    </Dialog>
  )
}

function DeleteNetwork({ ns, network, onClose }: { ns: string; network: Network; onClose: () => void }) {
  const [remove] = useMutation(DeleteNetworkDoc, refetchNetworks)
  const a = useAction()
  const t = useT()
  return (
    <Dialog title={t.admin.settings.deleteTitle(network.name)} submit={t.admin.delete} danger busy={a.busy} error={a.error} onClose={onClose}
      onSubmit={() => void a.run(() => remove({ variables: { ns, slug: network.slug } }), onClose)}>
      <p className="m-0">{t.admin.settings.deleteText(network.hosts.length, network.splitFrom?.name)}</p>
    </Dialog>
  )
}
