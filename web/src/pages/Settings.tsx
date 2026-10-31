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
import { parseDuration, plural, writeDuration } from '@/lib/format'

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

const tabs = [
  { id: 'general', label: 'Général' },
  { id: 'projects', label: 'Projets' },
  { id: 'environments', label: 'Environnements' },
  { id: 'networks', label: 'Réseaux' },
]

// Settings changes what people say of a namespace: names, descriptions,
// the order of the envs and the freshness thresholds.
export function Settings() {
  const { ns = '' } = useParams()
  const { data, error } = useQuery(SettingsDoc, { variables: { ns } })
  const [tab, setTab] = useTab(tabs, 'general')
  const n = data?.namespace
  if (n && !n.canEdit) return <Navigate to={`/${ns}`} replace />
  return (
    <Page title="Paramètres" meta={n ? <span>{n.name} · <span className="lc-mono">{n.slug}</span></span> : null}>
      {error ? <p role="alert">Les paramètres n'ont pas pu être chargés : {error.message}</p> : null}
      {data && !n ? <p role="alert">Ce namespace n'existe pas, ou vous ne pouvez pas le lire.</p> : null}
      {n ? (
        <Tabs label="Paramètres" current={tab} onChange={setTab}
          tabs={tabs.map((t) => ({ ...t, count: t.id === 'projects' ? n.projects.length : t.id === 'environments' ? n.environments.length : t.id === 'networks' ? n.networks.length : undefined }))}>
          {tab === 'general' ? <General n={n} /> : null}
          {tab === 'projects' ? <Projects ns={ns} projects={n.projects} /> : null}
          {tab === 'environments' ? <Environments ns={ns} environments={n.environments} /> : null}
          {tab === 'networks' ? <Networks ns={ns} networks={n.networks} /> : null}
        </Tabs>
      ) : !error && !data ? <span className="lc-muted">Chargement…</span> : null}
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
  const durations = { staleAfter: 'Stale après', inactiveAfter: 'Inactif après', snapshotRetention: 'Snapshots gardés' } as const

  const save = async () => {
    const seconds: Record<string, number> = {}
    for (const [k, label] of Object.entries(durations)) {
      const s = parseDuration(form[k as keyof typeof durations])
      if (s === null) throw new Error(`« ${label} » doit être une durée comme 7d, 12h ou 1d12h.`)
      seconds[k] = s
    }
    await update({ variables: { ns: n.slug, input: { name: form.name, description: form.description, ...seconds } } })
    setSaved(true)
  }
  return (
    <form className="flex max-w-[720px] flex-col gap-4" onSubmit={(e) => { e.preventDefault(); void a.run(save) }}>
      {a.error ? <div className="lc-alert" role="alert">{a.error}</div> : null}
      <Field label="Nom">{(id) => <input id={id} className="lc-input" required value={form.name} onChange={set('name')} />}</Field>
      <Field label="Description">{(id) => <textarea id={id} className="lc-input" rows={3} value={form.description} onChange={set('description')} />}</Field>
      <div className="lc-fields">
        <Field label={durations.staleAfter} hint="Sans snapshot depuis, un host est stale.">
          {(id) => <input id={id} className="lc-input lc-mono" required value={form.staleAfter} onChange={set('staleAfter')} />}
        </Field>
        <Field label={durations.inactiveAfter} hint="Plus long que stale.">
          {(id) => <input id={id} className="lc-input lc-mono" required value={form.inactiveAfter} onChange={set('inactiveAfter')} />}
        </Field>
        <Field label={durations.snapshotRetention}>
          {(id) => <input id={id} className="lc-input lc-mono" required value={form.snapshotRetention} onChange={set('snapshotRetention')} />}
        </Field>
      </div>
      <span className="lc-field__hint -mt-2">Durées : 7d, 12h, 1d12h, 30m.</span>
      <div className="flex items-center gap-3">
        <button type="submit" className="lc-btn lc-btn--primary" disabled={a.busy}>{a.busy ? 'Enregistrement…' : 'Enregistrer'}</button>
        <button type="button" className="lc-btn lc-btn--ghost" onClick={() => setForm(initial())}>Annuler les changements</button>
        {saved ? <span className="text-[13px] text-status-ok" role="status">Enregistré.</span> : null}
      </div>
    </form>
  )
}

function Projects({ ns, projects }: { ns: string; projects: Project[] }) {
  // The slug edited: the dialog follows the project as reloaded.
  const [editing, setEditing] = useState<string | null>(null)
  const edited = projects.find((p) => p.slug === editing)
  const t = useTable(projects, (p) => [p.slug, p.name, p.description ?? ''], (p, by) => by === 'name' ? p.name : p.slug, 'slug')
  const head = (label: string, by: string) => <SortHeader label={label} by={by} sort={t.sort.by} desc={t.sort.desc} onSort={t.onSort} />
  return (
    <>
      <FilterBar search={<SearchInput value={t.q} onChange={t.setQ} placeholder="Chercher un projet" width={280} />} fields={[]} count={plural(t.list.length, 'projet')} />
      <p className="lc-muted m-0 text-[13px]">Les projets viennent du label <code className="lc-mono">project</code> des agents : leur slug ne change pas ici.</p>
      <div className="overflow-x-auto rounded-lg border border-border">
        <table className="lc-table">
          <thead><tr>{head('Slug', 'slug')}{head('Nom', 'name')}<th>Description</th><th><span className="sr-only">Actions</span></th></tr></thead>
          <tbody>
            {t.shown.map((p) => (
              <tr key={p.id}>
                <td><span className="lc-copyable"><span className="lc-mono font-semibold">{p.slug}</span><CopyButton value={p.slug} /></span></td>
                <td>{p.name}</td>
                <td className="text-ink-muted" style={{ whiteSpace: 'normal', minWidth: 220 }}>{p.description}</td>
                <td className="lc-num"><button type="button" className="lc-btn lc-btn--small" onClick={() => setEditing(p.slug)}>Modifier</button></td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Pager page={t.page} size={t.size} total={t.list.length} noun="projets" onPage={t.setPage} onSize={t.setSize} />
      {edited ? <EditProject ns={ns} project={edited} onClose={() => setEditing(null)} /> : null}
    </>
  )
}

function EditProject({ ns, project, onClose }: { ns: string; project: Project; onClose: () => void }) {
  const [update] = useMutation(UpdateProjectDoc)
  const a = useAction()
  const [form, setForm] = useState({ name: project.name, description: project.description ?? '' })
  const set = (k: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => setForm({ ...form, [k]: e.target.value })
  return (
    <Dialog title={`Projet ${project.slug}`} busy={a.busy} error={a.error} onClose={onClose}
      onSubmit={() => void a.run(() => update({ variables: { ns, slug: project.slug, input: form } }), onClose)}>
      <Field label="Nom">{(id) => <input id={id} className="lc-input" required value={form.name} onChange={set('name')} />}</Field>
      <Field label="Description">{(id) => <textarea id={id} className="lc-input" rows={3} value={form.description} onChange={set('description')} />}</Field>
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
  return (
    <div className="flex max-w-[720px] flex-col gap-3">
      <p className="lc-muted m-0 text-[13px]">L'ordre des environnements est celui des filtres, des colonnes et du graphe. Les slugs viennent du label <code className="lc-mono">env</code> des agents.</p>
      {a.error ? <div className="lc-alert" role="alert">{a.error}</div> : null}
      <ol className="lc-order" aria-label="Environnements">
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
              <button type="button" className="lc-btn lc-btn--small" onClick={() => setRenaming(e)}>Renommer</button>
              <button type="button" className="lc-btn lc-btn--icon lc-btn--small" aria-label={`Monter ${e.slug}`} disabled={i === 0}
                onClick={() => setOrder(moved(order, i, -1))}>↑</button>
              <button type="button" className="lc-btn lc-btn--icon lc-btn--small" aria-label={`Descendre ${e.slug}`} disabled={i === order.length - 1}
                onClick={() => setOrder(moved(order, i, 1))}>↓</button>
            </li>
          )
        })}
      </ol>
      {order.length === 0 ? <span className="lc-muted">Aucun environnement : ils apparaissent avec les snapshots des agents.</span> : null}
      <div className="flex gap-3">
        <button type="button" className="lc-btn lc-btn--primary" disabled={!changed || a.busy}
          onClick={() => void a.run(() => reorder({ variables: { ns, slugs: order } }))}>{a.busy ? 'Enregistrement…' : 'Enregistrer l\'ordre'}</button>
        {changed ? <button type="button" className="lc-btn lc-btn--ghost" onClick={() => setOrder(sorted.map((e) => e.slug))}>Annuler</button> : null}
      </div>
      {renaming ? <RenameEnvironment ns={ns} env={renaming} onClose={() => setRenaming(null)} /> : null}
    </div>
  )
}

function RenameEnvironment({ ns, env, onClose }: { ns: string; env: Environment; onClose: () => void }) {
  const [update] = useMutation(UpdateEnvironmentDoc)
  const a = useAction()
  const [name, setName] = useState(env.name)
  return (
    <Dialog title={`Environnement ${env.slug}`} busy={a.busy} error={a.error} onClose={onClose}
      onSubmit={() => void a.run(() => update({ variables: { ns, slug: env.slug, input: { name } } }), onClose)}>
      <Field label="Nom">{(id) => <input id={id} className="lc-input" required value={name} onChange={(e) => setName(e.target.value)} />}</Field>
    </Dialog>
  )
}

const kindFr: Record<string, string> = { private: 'privé', public: 'public' }

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
  const t = useTable(networks, (x) => [x.slug, x.name, x.description ?? '', ...x.cidrs], (x, by) => by === 'name' ? x.name : by === 'kind' ? x.kind : x.slug, 'slug')
  const head = (label: string, by: string) => <SortHeader label={label} by={by} sort={t.sort.by} desc={t.sort.desc} onSort={t.onSort} />
  return (
    <>
      <FilterBar search={<SearchInput value={t.q} onChange={t.setQ} placeholder="Chercher un réseau, un CIDR" width={280} />} fields={[]} count={plural(t.list.length, 'réseau', 'réseaux')} />
      <div className="overflow-x-auto rounded-lg border border-border">
        <table className="lc-table">
          <thead><tr>{head('Slug', 'slug')}{head('Nom', 'name')}{head('Type', 'kind')}<th>CIDR</th><th className="lc-num">Hosts</th><th>Description</th><th><span className="sr-only">Actions</span></th></tr></thead>
          <tbody>
            {t.shown.map((x) => (
              <tr key={x.id}>
                <td><span className="lc-mono font-semibold">{x.slug}</span></td>
                <td>
                  <span className="flex flex-col leading-[18px]">
                    <span>{x.name}</span>
                    {x.splitFrom ? <span className="text-[12.5px] text-ink-muted">scindé de {x.splitFrom.name}</span> : null}
                  </span>
                </td>
                <td>{kindFr[x.kind] ?? x.kind}</td>
                <td><span className="lc-ips">{x.cidrs.map((c) => <span key={c} className="lc-copyable"><span className="lc-mono">{c}</span><CopyButton value={c} /></span>)}</span></td>
                <td className="lc-num">{x.hosts.length}</td>
                <td className="text-ink-muted" style={{ whiteSpace: 'normal', minWidth: 200 }}>{x.description}</td>
                <td className="lc-num">
                  <span className="inline-flex gap-1.5">
                    <button type="button" className="lc-btn lc-btn--small" onClick={open('edit', x)}>Modifier</button>
                    {x.kind === 'private' ? (
                      <button type="button" className="lc-btn lc-btn--small" disabled={x.hosts.length === 0}
                        title="Deux réseaux privés sur le même range : déplacer des hosts dans un nouveau réseau" onClick={open('split', x)}>Scinder</button>
                    ) : null}
                    {family(networks, x).length > 1 ? <button type="button" className="lc-btn lc-btn--small" onClick={open('move', x)}>Rattacher des hosts</button> : null}
                    {x.splitFrom ? <button type="button" className="lc-btn lc-btn--small lc-btn--danger" onClick={open('delete', x)}>Supprimer</button> : null}
                  </span>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Pager page={t.page} size={t.size} total={t.list.length} noun="réseaux" onPage={t.setPage} onSize={t.setSize} />
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
  return (
    <Dialog title={`Réseau ${network.slug}`} busy={a.busy} error={a.error} onClose={onClose}
      onSubmit={() => void a.run(() => update({ variables: { ns, slug: network.slug, input: form } }), onClose)}>
      <Field label="Nom">{(id) => <input id={id} className="lc-input" required value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />}</Field>
      <Field label="Type">
        {(id) => (
          <select id={id} className="lc-select" value={form.kind} onChange={(e) => setForm({ ...form, kind: e.target.value })}>
            <option value="private">privé</option>
            <option value="public">public</option>
          </select>
        )}
      </Field>
      <Field label="Description">{(id) => <textarea id={id} className="lc-input" rows={3} value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} />}</Field>
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
  return (
    <Dialog title={`Scinder ${network.name}`} submit="Scinder" busy={a.busy} error={a.error} onClose={onClose} wide
      onSubmit={() => void a.run(() => split({ variables: { ns, slug: network.slug, name, hosts } }), onClose)}>
      <p className="m-0 text-[13px] text-ink-muted">Deux réseaux privés sur le même range ({network.cidrs.join(', ')}) ne se distinguent pas par leurs IPs.
        Les hosts choisis passent dans un nouveau réseau et y restent à leurs prochains snapshots ; les liens sont recalculés.</p>
      <Field label="Nom du nouveau réseau">{(id) => <input id={id} className="lc-input" required value={name} onChange={(e) => setName(e.target.value)} />}</Field>
      <AddPicker label="Hosts à déplacer" placeholder="Chercher un host du réseau" empty="Aucun host : choisissez ceux du second réseau."
        items={network.hosts.map((h) => ({ id: h.id, label: h.hostname }))} selected={hosts} onChange={setHosts} />
    </Dialog>
  )
}

// MoveHosts brings hosts of the other networks of the range into this one.
function MoveHosts({ ns, network, networks, onClose }: { ns: string; network: Network; networks: Network[]; onClose: () => void }) {
  const [move] = useMutation(MoveHostsToNetworkDoc, refetchNetworks)
  const a = useAction()
  const [hosts, setHosts] = useState<string[]>([])
  const items = family(networks, network).filter((x) => x.id !== network.id)
    .flatMap((x) => x.hosts.map((h) => ({ id: h.id, label: h.hostname, detail: x.name })))
    .sort((p, q) => p.label.localeCompare(q.label, 'fr', { numeric: true }))
  return (
    <Dialog title={`Rattacher des hosts à ${network.name}`} submit="Rattacher" busy={a.busy} error={a.error} onClose={onClose} wide
      onSubmit={() => void a.run(() => move({ variables: { ns, slug: network.slug, hosts } }), onClose)}>
      <p className="m-0 text-[13px] text-ink-muted">Les hosts viennent des autres réseaux du même range ; ils y restent à leurs prochains snapshots, et les liens sont recalculés.</p>
      <AddPicker label="Hosts à rattacher" placeholder="Chercher un host du range" empty="Aucun host choisi."
        items={items} selected={hosts} onChange={setHosts} />
    </Dialog>
  )
}

function DeleteNetwork({ ns, network, onClose }: { ns: string; network: Network; onClose: () => void }) {
  const [remove] = useMutation(DeleteNetworkDoc, refetchNetworks)
  const a = useAction()
  return (
    <Dialog title={`Supprimer ${network.name} ?`} submit="Supprimer" danger busy={a.busy} error={a.error} onClose={onClose}
      onSubmit={() => void a.run(() => remove({ variables: { ns, slug: network.slug } }), onClose)}>
      <p className="m-0">{network.hosts.length
        ? `${plural(network.hosts.length, 'host', 'hosts')} ${network.hosts.length > 1 ? 'reviennent' : 'revient'} dans ${network.splitFrom?.name ?? 'le réseau d\'origine'}, et les liens sont recalculés.`
        : `Il ne contient aucun host.`}</p>
    </Dialog>
  )
}
