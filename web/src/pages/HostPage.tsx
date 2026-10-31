import { useMutation, useQuery } from '@apollo/client'
import { useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { Copyable, CopyButton, Ips } from '@/components/Copy'
import { HistoryEvent } from '@/components/HistoryEvents'
import { TechLogo } from '@/components/Icon'
import { Page } from '@/components/Layout'
import { Tabs, useTab } from '@/components/Tabs'
import { UsedBy, type UsedByRow } from '@/components/UsedBy'
import { chipsField, FilterBar } from '@/components/FilterBar'
import { Pager, SearchInput, SortHeader, StatusBadge } from '@/components/Listing'
import { graphql } from '@/graphql'
import type { HostPageQuery } from '@/graphql/graphql'
import { useT } from '@/i18n'
import { ago } from '@/lib/format'

const HostPageDoc = graphql(`
  query HostPage($ns: String!, $id: ID!) {
    namespace(slug: $ns) {
      id slug canEdit
      host(id: $id) {
        id hostname status lastSeenAt firstSeenAt agentVersion kernel arch virtualization resources
        project { slug name } environment { slug name }
        os { id version pretty }
        ips { address prefixLength interface defaultRoute network { slug name } }
        runtimes { id type version path manager firstSeenAt }
        services { id type instance version details firstSeenAt dependents { ...UsedByLink } }
        webservers { id type version domains firstSeenAt dependents { ...UsedByLink } }
        apps {
          id name path role framework frameworkVersion vhosts firstSeenAt
          code { version commit branch deployedAt }
          runtime { type version source }
          dependencies { key }
          links { id }
        }
        history(page: { limit: 8 }) { items { id occurredAt hostname entityType kind before after } hasMore }
      }
    }
  }
`)

const DeleteHostDoc = graphql(`
  mutation DeleteHost($ns: String!, $id: ID!) { deleteHost(namespace: $ns, id: $id) }
`)

type Host = NonNullable<NonNullable<HostPageQuery['namespace']>['host']>
type App = Host['apps'][number]

export function HostPage() {
  const { ns = '', id = '' } = useParams()
  const { data, loading, error } = useQuery(HostPageDoc, { variables: { ns, id } })
  const t = useT()
  const th = t.pages.host
  const ts = t.pages.shared
  const h = data?.namespace?.host
  if (loading && !h) return <Page title="…"><span className="lc-muted">{ts.loading}</span></Page>
  if (error) return <Page title={th.failedTitle}><p role="alert">{ts.sheetFailed(error.message)}</p></Page>
  if (!h) return <Page title={th.notFound}><Link to={`/${ns}/hosts`}>{th.back}</Link></Page>
  const main = h.ips.find((i) => i.defaultRoute) ?? h.ips[0]
  return (
    <Page
      title={<span className="lc-copyable gap-1.5"><span className="font-mono text-[26px]">{h.hostname}</span><CopyButton value={h.hostname} /></span>}
      meta={<>
        <StatusBadge status={h.status} />
        <span>
          {h.project ? <Link to={`/${ns}/projects/${h.project.slug}`}>{h.project.slug}</Link> : ts.noProject}
          {h.environment ? ` · ${h.environment.slug}` : ''}{th.lastSnapshot(ago(h.lastSeenAt))}{h.agentVersion ? th.agent(h.agentVersion) : ''}
        </span>
        {main ? <Copyable value={main.address} /> : null}
        <span className="flex-1" />
        <Link className="lc-btn no-underline" to={`/${ns}/graph?${new URLSearchParams({ lens: 'infra', host: h.hostname, open: `host:${h.id}` })}`}>{ts.seeInGraph}</Link>
        <CopyLink />
        {data?.namespace?.canEdit && h.status !== 'ACTIVE' ? <DeleteHost ns={ns} host={h} /> : null}
      </>}
    >
      <HostTabs ns={ns} host={h} />
    </Page>
  )
}

function HostTabs({ ns, host: h }: { ns: string; host: Host }) {
  const usedBy: UsedByRow[] = useMemo(() => [
    ...h.services.flatMap((s) => s.dependents.map((l) => ({ ...l, target: { name: s.instance && s.instance !== 'default' ? `${s.type} ${s.instance}` : s.type, tech: s.type } }))),
    ...h.webservers.flatMap((w) => w.dependents.map((l) => ({ ...l, target: { name: w.type, tech: w.type } }))),
  ], [h])
  const t = useT()
  const tabs = [
    { id: 'general', label: t.pages.shared.general },
    { id: 'apps', label: t.pages.host.tabs.apps, count: h.apps.length },
    { id: 'components', label: t.pages.host.tabs.components, count: h.services.length + h.webservers.length + h.runtimes.length },
    { id: 'used-by', label: t.pages.shared.usedBy, count: usedBy.length },
  ]
  const [tab, setTab] = useTab(tabs, 'general')
  return (
    <Tabs label={t.pages.host.content} tabs={tabs} current={tab} onChange={setTab}>
      {tab === 'general' ? <General host={h} /> : null}
      {tab === 'apps' ? <Apps apps={h.apps} /> : null}
      {tab === 'components' ? <Components host={h} /> : null}
      {tab === 'used-by' ? <UsedBy ns={ns} rows={usedBy} /> : null}
    </Tabs>
  )
}

function CopyLink() {
  const [done, setDone] = useState(false)
  const t = useT()
  return (
    <button className="lc-btn" type="button" onClick={() => void navigator.clipboard?.writeText(window.location.href).then(() => { setDone(true); setTimeout(() => setDone(false), 1500) })}>
      {done ? t.pages.host.linkCopied : t.pages.host.copyLink}
    </button>
  )
}

function DeleteHost({ ns, host }: { ns: string; host: Host }) {
  const navigate = useNavigate()
  const [confirm, setConfirm] = useState(false)
  const [remove, { loading, error }] = useMutation(DeleteHostDoc)
  const th = useT().pages.host
  if (!confirm) return <button className="lc-btn" type="button" onClick={() => setConfirm(true)}>{th.delete}</button>
  return (
    <span className="flex items-center gap-2" role="group" aria-label={th.confirmDelete}>
      <span className="text-[13px] text-ink">{th.deleteQuestion(host.hostname)}</span>
      <button className="lc-btn lc-btn--primary" type="button" disabled={loading}
        onClick={() => void remove({ variables: { ns, id: host.id } }).then(() => navigate(`/${ns}/hosts`))}>{th.remove}</button>
      <button className="lc-btn" type="button" onClick={() => setConfirm(false)}>{th.cancel}</button>
      {error ? <span role="alert" className="text-danger">{error.message}</span> : null}
    </span>
  )
}

function AppCard({ app }: { app: App }) {
  const { ns = '' } = useParams()
  const ts = useT().pages.shared
  return (
    <div className="flex flex-col gap-2.5 rounded-lg border border-border bg-surface-raised p-3.5">
      <div className="flex items-center gap-2.5">
        <span className="lc-node__icon" style={{ ['--node-ink' as string]: 'var(--kind-app)', ['--node-tint' as string]: 'var(--kind-app-tint)' }}>
          <TechLogo tech={app.framework ?? app.runtime?.type ?? 'app'} />
        </span>
        <div className="flex min-w-0 flex-1 flex-col">
          <Link className="heading" to={`/${ns}/apps/${app.id}`}>{app.name}</Link>
          <Copyable value={app.path} className="text-[11.5px] text-ink-muted" />
        </div>
        {app.code?.version ? <span className="lc-badge lc-badge--plain lc-mono">{app.code.version}</span> : null}
      </div>
      <dl className="lc-dl" style={{ gridTemplateColumns: 'max-content minmax(0, 1fr)' }}>
        {app.code?.commit ? <><dt>{ts.commit}</dt><dd><Copyable value={app.code.commit} shown={app.code.commit.slice(0, 7) + (app.code.branch ? ` · ${app.code.branch}` : '')} /></dd></> : null}
        {app.code?.deployedAt ? <><dt>{ts.deployed}</dt><dd>{ago(app.code.deployedAt)}</dd></> : null}
        {app.framework ? <><dt>{ts.framework}</dt><dd>{app.framework} {app.frameworkVersion}</dd></> : null}
        {app.runtime ? <><dt>{ts.runtime}</dt><dd>{app.runtime.type} {app.runtime.version}{app.runtime.source ? ` · ${app.runtime.source}` : ''}</dd></> : null}
        {app.vhosts.map((v) => <div key={v} className="contents"><dt>{ts.domain}</dt><dd><Copyable value={v} /></dd></div>)}
        <dt>{ts.dependencies}</dt><dd>{app.dependencies.length} · {ts.links(app.links.length)}</dd>
      </dl>
    </div>
  )
}

// Apps shows up to 4 apps as cards, more as a table.
function Apps({ apps }: { apps: App[] }) {
  const { ns = '' } = useParams()
  const t = useT()
  const ts = t.pages.shared
  const [q, setQ] = useState('')
  const [page, setPage] = useState(1)
  const [size, setSize] = useState(10)
  const [sort, setSort] = useState<{ by: string; desc: boolean }>({ by: 'name', desc: false })
  const sorted = useMemo(() => {
    const value = (a: App) => sort.by === 'version' ? a.code?.version ?? '' : sort.by === 'framework' ? a.framework ?? '' : a.name
    return apps.filter((a) => !q || [a.name, a.path, a.code?.version, a.framework, ...a.vhosts].some((x) => x?.toLowerCase().includes(q.toLowerCase())))
      .sort((a, b) => value(a).localeCompare(value(b), 'fr', { numeric: true }) * (sort.desc ? -1 : 1))
  }, [apps, q, sort])
  if (apps.length === 0) return <p className="lc-muted m-0">{t.pages.host.noApp}</p>
  if (apps.length <= 4) return <div className="grid gap-4 sm:grid-cols-2">{apps.map((a) => <AppCard key={a.id} app={a} />)}</div>
  const onSort = (by: string) => setSort((s) => ({ by, desc: s.by === by && !s.desc }))
  return (
    <section className="flex flex-col gap-3">
      <SearchInput value={q} onChange={(v) => { setQ(v); setPage(1) }} placeholder={t.pages.host.searchApps} />
      <div className="overflow-x-auto rounded-lg border border-border">
        <table className="lc-table">
          <thead><tr>
            <SortHeader label={ts.app} by="name" sort={sort.by} desc={sort.desc} onSort={onSort} />
            <th>{ts.path}</th>
            <SortHeader label={ts.version} by="version" sort={sort.by} desc={sort.desc} onSort={onSort} />
            <SortHeader label={ts.framework} by="framework" sort={sort.by} desc={sort.desc} onSort={onSort} />
            <th>{ts.domain}</th>
          </tr></thead>
          <tbody>
            {sorted.slice((page - 1) * size, page * size).map((a) => (
              <tr key={a.id}>
                <td><span className="inline-flex items-center gap-2 font-semibold"><span className="lc-node__techs"><TechLogo tech={a.framework ?? 'app'} /></span><Link to={`/${ns}/apps/${a.id}`}>{a.name}</Link></span></td>
                <td className="max-w-[240px]"><Copyable value={a.path} className="text-[12px] text-ink-muted" /></td>
                <td className="lc-mono">{a.code?.version}</td>
                <td className="text-ink-muted">{[a.framework, a.frameworkVersion].filter(Boolean).join(' ')}</td>
                <td>{a.vhosts[0] ? <Copyable value={a.vhosts[0]} /> : null}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Pager page={page} size={size} total={sorted.length} noun={ts.nouns.apps} sizes={[10, 20, 50]} onPage={setPage} onSize={(s) => { setSize(s); setPage(1) }} />
    </section>
  )
}

interface Component {
  key: string
  type: 'service' | 'webserver' | 'runtime'
  name: string
  tech: string
  version: string
  where: string[]
  since: string
}

function listens(details: unknown): string[] {
  const d = (details ?? {}) as { listen?: unknown; listens?: unknown; socket?: unknown }
  const raw = d.listen ?? d.listens ?? d.socket
  if (Array.isArray(raw)) return raw.map((x) => (typeof x === 'string' ? x : (x as { address?: string }).address ?? '')).filter(Boolean)
  return typeof raw === 'string' ? [raw] : []
}

function Components({ host }: { host: Host }) {
  const all: Component[] = useMemo(() => [
    ...host.webservers.map((w) => ({ key: w.id, type: 'webserver' as const, name: w.type, tech: w.type, version: w.version ?? '', where: w.domains.slice(0, 3), since: w.firstSeenAt })),
    ...host.services.map((s) => ({ key: s.id, type: 'service' as const, name: s.instance && s.instance !== 'default' ? `${s.type} ${s.instance}` : s.type, tech: s.type, version: s.version ?? '', where: listens(s.details), since: s.firstSeenAt })),
    ...host.runtimes.map((r) => ({ key: r.id, type: 'runtime' as const, name: r.type, tech: r.type, version: r.version, where: [r.path], since: r.firstSeenAt })),
  ], [host])
  const t = useT()
  const ts = t.pages.shared
  const [q, setQ] = useState('')
  const [types, setTypes] = useState<string[]>([])
  const [sort, setSort] = useState<{ by: keyof Component; desc: boolean }>({ by: 'name', desc: false })
  const [page, setPage] = useState(1)
  const [size, setSize] = useState(20)
  const list = all.filter((c) => (!types.length || types.includes(c.type)) && (!q || [c.name, c.version, ...c.where].some((x) => x.toLowerCase().includes(q.toLowerCase()))))
    .sort((a, b) => String(a[sort.by]).localeCompare(String(b[sort.by]), 'fr', { numeric: true }) * (sort.desc ? -1 : 1))
  const onSort = (by: string) => setSort((s) => ({ by: by as keyof Component, desc: s.by === by && !s.desc }))
  const count = (type: string) => all.filter((c) => c.type === type).length
  return (
    <section className="flex flex-col gap-3">
      <FilterBar
        search={<SearchInput value={q} onChange={(v) => { setQ(v); setPage(1) }} placeholder={t.pages.host.searchComponents} width={300} />}
        fields={[chipsField({ label: ts.type, options: [{ value: 'service', label: ts.kinds.services, count: count('service') }, { value: 'webserver', label: ts.kinds.webservers, count: count('webserver') }, { value: 'runtime', label: ts.kinds.runtimes, count: count('runtime') }],
          selected: types, onChange: (v) => { setTypes(v); setPage(1) } })]}
        count={t.pages.host.components(list.length, all.length)}
      />
      <div className="overflow-x-auto rounded-lg border border-border">
        <table className="lc-table">
          <thead><tr>
            <SortHeader label={ts.type} by="type" sort={sort.by} desc={sort.desc} onSort={onSort} />
            <SortHeader label={ts.name} by="name" sort={sort.by} desc={sort.desc} onSort={onSort} />
            <SortHeader label={ts.version} by="version" sort={sort.by} desc={sort.desc} onSort={onSort} />
            <th>{t.pages.host.where}</th>
            <SortHeader label={ts.seenSince} by="since" sort={sort.by} desc={sort.desc} onSort={onSort} />
          </tr></thead>
          <tbody>
            {list.slice((page - 1) * size, page * size).map((c) => (
              <tr key={c.key}>
                <td>{c.type}</td>
                <td><span className="inline-flex items-center gap-2"><span className="lc-node__techs"><TechLogo tech={c.tech} /></span>{c.name}</span></td>
                <td className="lc-mono">{c.version}</td>
                <td><div className="flex flex-wrap gap-x-3.5 gap-y-0.5">{c.where.map((w) => <Copyable key={w} value={w} />)}</div></td>
                <td className="text-ink-muted">{ago(c.since)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Pager page={page} size={size} total={list.length} noun={ts.nouns.components} onPage={setPage} onSize={(s) => { setSize(s); setPage(1) }} />
    </section>
  )
}

// General tells what the host is: its system, its addresses, its history.
function General({ host: h }: { host: Host }) {
  const res = (h.resources ?? {}) as { cpu?: { effective_cores?: number; model?: string }; memory_mb?: { effective?: number }; disks?: { mount: string; size_gb: number }[] }
  const t = useT()
  const th = t.pages.host
  const ts = t.pages.shared
  const networks = [...new Map(h.ips.filter((i) => i.network).map((i) => [i.network!.slug, i.network!])).values()]
  return (
    <div className="grid items-start gap-6 lg:grid-cols-2">
      <section className="lc-panel lc-panel--card">
        <div className="lc-panel__section">
          <span className="lc-label">{th.system}</span>
          <dl className="lc-dl" style={{ gridTemplateColumns: 'max-content minmax(0, 1fr)' }}>
            <dt>{th.os}</dt><dd>{h.os?.pretty ?? h.os?.id}</dd>
            {h.kernel ? <><dt>{th.kernel}</dt><dd>{h.kernel}</dd></> : null}
            <dt>{th.arch}</dt><dd>{[h.arch, h.virtualization].filter(Boolean).join(' · ')}</dd>
            {res.cpu?.effective_cores ? <><dt>{th.cpu}</dt><dd>{res.cpu.effective_cores} vCPU</dd></> : null}
            {res.memory_mb?.effective ? <><dt>{th.memory}</dt><dd>{th.gb(Math.round(res.memory_mb.effective / 1024))}</dd></> : null}
            {res.disks?.map((d) => <div key={d.mount} className="contents"><dt>{th.disk}</dt><dd>{d.mount} · {th.gb(d.size_gb)}</dd></div>)}
            <dt>{th.ip}</dt><dd><Ips ips={h.ips.map((i) => ({ address: i.address, interface: i.interface }))} shown={8} interfaces /></dd>
            {networks.map((n) => <div key={n.slug} className="contents"><dt>{th.network}</dt><dd><Copyable value={n.name} /></dd></div>)}
            <dt>{ts.seenSince}</dt><dd>{ago(h.firstSeenAt)}</dd>
            <dt>{th.lastSnapshotLabel}</dt><dd>{ago(h.lastSeenAt)}</dd>
            {h.agentVersion ? <><dt>{th.agentLabel}</dt><dd>{h.agentVersion}</dd></> : null}
          </dl>
        </div>
      </section>
      <section className="lc-panel lc-panel--card">
        <div className="lc-panel__section">
          <span className="lc-label">{ts.history}</span>
          {h.history.items.length === 0 ? <span className="lc-muted">{ts.noChange}</span> : null}
          {h.history.items.map((e) => <HistoryEvent key={e.id} event={e} />)}
        </div>
      </section>
    </div>
  )
}
