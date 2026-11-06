import { useQuery } from '@apollo/client'
import { Fragment, useMemo } from 'react'
import { Link, useParams } from 'react-router'
import { PackageVersion } from '@/components/PackageVersion'
import { Copyable, CopyButton } from '@/components/Copy'
import { FilterBar } from '@/components/FilterBar'
import { TechLogo } from '@/components/Icon'
import { Page } from '@/components/Layout'
import { Pager, SearchInput, SortHeader, StatusBadge, useTable } from '@/components/Listing'
import { Tabs, useTab } from '@/components/Tabs'
import { crossing, envOf, ExpandAll, nameOf, projectOf, PathBadges, PathDetail, PathToggle, techOf, useExpanded, Via,
  type End, type PathLink } from '@/components/LinkPath'
import { EndName, mergeUsedBy, UsedBy, usedByRows, UsedBySection, type UsedByGroup } from '@/components/UsedBy'
import { graphql } from '@/graphql'
import type { AppPageQuery } from '@/graphql/graphql'
import { useT } from '@/i18n'
import { ago, confidenceText, formatDateTime } from '@/lib/format'
import { repositoryUrl } from '@/lib/git'
import { field, launchersOf, str, strings, type Launcher } from '@/lib/json'
import { safeHttpUrl } from '@/lib/url'
import { refresh } from '@/config'

const AppPageDoc = graphql(`
  query AppPage($ns: String!, $id: ID!) {
    namespace(slug: $ns) {
      id
      app(id: $id) {
        id name path resolvedPath role framework frameworkVersion vhosts launchers firstSeenAt
        runtime { type version packageVersion path source }
        code { source version commit branch tag repository pipelineId pipelineUrl deployedAt deployedBy }
        project { slug name }
        environment { slug }
        host { id hostname status lastSeenAt }
        deployments { id path host { id hostname status } code { version commit branch deployedAt } }
        dependencies { key scheme port path target unparsed }
        uses { ...AppLink }
        usedBy { ...AppLink }
      }
    }
  }
`)

// The impact of the app, both ways, at the level of the apps: what it
// reaches through proxies, and who reaches it through them, the proxies
// crossed without taking a level.
const ChainDoc = graphql(`
  query AppChain($ns: String!, $node: String!) {
    namespace(slug: $ns) {
      id
      graph(level: APP, filter: { status: [ACTIVE, STALE, INACTIVE] }) {
        nodes { id kind label project env entity { __typename ... on App { id } ... on Host { id } } }
      }
      needs: impact(level: APP, filter: { status: [ACTIVE, STALE, INACTIVE] }, node: $node, direction: NEEDS, throughProxies: true) { nodes { id level } }
      dependents: impact(level: APP, filter: { status: [ACTIVE, STALE, INACTIVE] }, node: $node, direction: DEPENDENTS, throughProxies: true) { nodes { id level } }
    }
  }
`)

type App = NonNullable<NonNullable<AppPageQuery['namespace']>['app']>
type ChainNode = { id: string; kind: string; label: string; project?: string | null; env?: string | null
  entity?: { __typename: string; id?: string } | null; level: number }

// Use is what the app uses, whatever the hosts: the app links to one
// component of a project and an env, merged.
interface Use {
  id: string
  to: End
  project?: string
  env?: string
  keys: string[]
  resolved: string[]
  confidence: string
  deadEnd: boolean
  links: PathLink[]
}

export function usesOf(links: PathLink[]): Use[] {
  const out = new Map<string, Use>()
  for (const l of links) {
    const project = projectOf(l.to)
    const env = envOf(l.to)
    const id = [l.to.__typename, techOf(l.to), nameOf(l.to), project, env].join('\u0000')
    const u = out.get(id) ?? { id, to: l.to, project, env, keys: [], resolved: [], confidence: l.confidence, deadEnd: false, links: [] }
    if (l.key && !u.keys.includes(l.key)) u.keys.push(l.key)
    for (const how of l.resolvedBy) if (!u.resolved.includes(how)) u.resolved.push(how)
    if (l.confidence !== 'EXACT') u.confidence = l.confidence
    u.deadEnd ||= l.deadEnd
    u.links.push(l)
    out.set(id, u)
  }
  return [...out.values()]
}

export function AppPage() {
  const { ns = '', id = '' } = useParams()
  const { data, loading, error } = useQuery(AppPageDoc, { variables: { ns, id }, ...refresh })
  const a = data?.namespace?.app
  const t = useT()
  const ta = t.pages.app
  const ts = t.pages.shared
  const launchers = useMemo(() => launchersOf(a?.launchers), [a?.launchers])
  const tabs = [
    { id: 'general', label: ts.general },
    { id: 'hosts', label: ta.tabs.hosts, count: a?.deployments.length },
    { id: 'uses', label: ta.tabs.uses, count: a ? usesOf(a.uses).length : undefined },
    { id: 'used-by', label: ts.usedBy, count: a ? usedByOf(a).length : undefined },
    { id: 'config', label: ta.tabs.config, count: a?.dependencies.length },
    { id: 'workers', label: ta.tabs.workers, count: a ? launchers.length : undefined },
    { id: 'scheduled', label: ta.tabs.scheduled, count: a ? launchers.reduce((n, l) => n + l.tasks.length, 0) : undefined },
  ]
  const [tab, setTab] = useTab(tabs, 'general')
  if (loading && !a) return <Page title="…"><span className="lc-muted">{ts.loading}</span></Page>
  if (error) return <Page title={ta.failedTitle}><p role="alert">{ts.sheetFailed(error.message)}</p></Page>
  if (!a) return <Page title={ta.notFound}><Link to={`/${ns}/apps`}>{ta.back}</Link></Page>
  const graph = a.project && a.environment
    ? `/${ns}/graph?${new URLSearchParams({ lens: 'projects', env: a.environment.slug, open: `project:${a.project.slug}@${a.environment.slug}` })}`
    : `/${ns}/graph?${new URLSearchParams({ lens: 'infra', host: a.host.hostname, open: `host:${a.host.id}` })}`
  return (
    <Page
      title={<span className="lc-copyable gap-1.5"><span>{a.name}</span><CopyButton value={a.name} /></span>}
      meta={<>
        <span>
          {ta.meta}{a.project ? <>{ta.ofProject}<Link to={`/${ns}/projects/${a.project.slug}`}>{a.project.slug}</Link></> : ''}
          {a.environment ? ` · ${a.environment.slug}` : ''}
        </span>
        {diverges(a) ? <span className="lc-badge lc-badge--stale">{ta.differentVersions}</span>
          : a.code?.version ? <span className="lc-badge lc-badge--plain lc-mono">{a.code.version}</span> : null}
        <span className="flex-1" />
        <Link className="lc-btn no-underline" to={graph}>{ts.seeInGraph}</Link>
      </>}
    >
      <Tabs label={ta.content} tabs={tabs} current={tab} onChange={setTab}>
        {tab === 'general' ? <General app={a} /> : null}
        {tab === 'hosts' ? <Hosts ns={ns} app={a} /> : null}
        {tab === 'uses' ? <Uses ns={ns} app={a} /> : null}
        {tab === 'used-by' ? <UsedByTab ns={ns} app={a} /> : null}
        {tab === 'config' ? <Config app={a} /> : null}
        {tab === 'workers' ? <Workers launchers={launchers} /> : null}
        {tab === 'scheduled' ? <Scheduled launchers={launchers} /> : null}
      </Tabs>
    </Page>
  )
}

// Repository is the address of a repository, a link to its page when it
// has one.
function Repository({ repository }: { repository: string }) {
  const url = safeHttpUrl(repositoryUrl(repository))
  return url ? <a href={url} target="_blank" rel="noreferrer">{repository}</a> : <>{repository}</>
}

// Pipeline is the pipeline that deployed the code, a link when its
// address is a web page.
function Pipeline({ url, id }: { url: string; id?: string | null }) {
  const href = safeHttpUrl(url)
  return href ? <a href={href} target="_blank" rel="noreferrer">{id ?? url}</a> : <>{id ? `${id} · ${url}` : url}</>
}

// General tells the code and the app of the deployment shown.
function General({ app: a }: { app: App }) {
  const c = a.code
  const t = useT()
  const ta = t.pages.app
  const ts = t.pages.shared
  return (
    <div className="flex flex-col gap-6">
      <div className="grid items-start gap-6 lg:grid-cols-2">
        <section className="lc-panel lc-panel--card">
          <div className="lc-panel__section">
            <span className="lc-label">{a.deployments.length > 1 ? ta.deployedOn(a.host.hostname) : ta.deployedCode}</span>
            {c ? (
              <dl className="lc-dl" style={{ gridTemplateColumns: 'max-content minmax(0, 1fr)' }}>
                {c.version ? <><dt>{ts.version}</dt><dd><Copyable value={c.version} /></dd></> : null}
                {c.commit ? <><dt>{ts.commit}</dt><dd><Copyable value={c.commit} shown={c.commit.slice(0, 7)} /></dd></> : null}
                {c.branch ? <><dt>{ta.branch}</dt><dd><Copyable value={c.branch} /></dd></> : null}
                {c.tag ? <><dt>{ta.tag}</dt><dd><Copyable value={c.tag} /></dd></> : null}
                {c.repository ? <><dt>{ta.repository}</dt><dd><Copyable value={c.repository} shown={<Repository repository={c.repository} />} /></dd></> : null}
                {c.deployedAt ? <><dt>{ts.deployed}</dt><dd>{formatDateTime(c.deployedAt)} · {ago(c.deployedAt)}{c.deployedBy ? ` · ${c.deployedBy}` : ''}</dd></> : null}
                {c.pipelineUrl ? <><dt>{ta.pipeline}</dt><dd><Pipeline url={c.pipelineUrl} id={c.pipelineId} /></dd></> : null}
                <dt>{ta.source}</dt><dd>{c.source}</dd>
              </dl>
            ) : <span className="lc-muted">{ta.noCode}</span>}
          </div>
        </section>
        <section className="lc-panel lc-panel--card">
          <div className="lc-panel__section">
            <span className="lc-label">{ts.app}</span>
            <dl className="lc-dl" style={{ gridTemplateColumns: 'max-content minmax(0, 1fr)' }}>
              <dt>{ta.role}</dt><dd>{a.role}</dd>
              {a.framework ? <><dt>{ts.framework}</dt><dd>{a.framework} {a.frameworkVersion}</dd></> : null}
              {a.runtime ? <><dt>{ts.runtime}</dt><dd><span className="inline-flex flex-wrap items-center gap-x-2">{a.runtime.type} {a.runtime.version}{a.runtime.source ? ` · ${a.runtime.source}` : ''}<PackageVersion version={a.runtime.version} packageVersion={a.runtime.packageVersion} /></span></dd></> : null}
              <dt>{ts.path}</dt><dd><Copyable value={a.path} /></dd>
              {a.resolvedPath && a.resolvedPath !== a.path ? <><dt>{ta.resolved}</dt><dd><Copyable value={a.resolvedPath} /></dd></> : null}
              {a.vhosts.map((v) => <div key={v} className="contents"><dt>{ts.domain}</dt><dd><Copyable value={v} /></dd></div>)}
              <dt>{ts.seenSince}</dt><dd>{ago(a.firstSeenAt)}</dd>
            </dl>
          </div>
        </section>
      </div>
    </div>
  )
}

// diverges tells the deployments of an app running different code.
function diverges(a: App): boolean {
  return new Set(a.deployments.map((x) => `${x.code?.version ?? ''}/${x.code?.commit ?? ''}`)).size > 1
}

// Hosts are the hosts the app is deployed on in its project and env: one
// row per deployment, the one shown highlighted.
function Hosts({ ns, app: a }: { ns: string; app: App }) {
  const t = useT()
  const ta = t.pages.app
  const ts = t.pages.shared
  if (a.deployments.length === 0) return <p className="lc-muted">{ta.noHost}</p>
  return (
    <div className="overflow-x-auto rounded-lg border border-border"><table className="lc-table">
      <thead><tr><th>{ts.host}</th><th>{ts.path}</th><th>{ts.version}</th><th>{ts.commit}</th><th>{ts.deployed}</th><th>{ts.status}</th></tr></thead>
      <tbody>{a.deployments.map((x) => (
        <tr key={x.id} style={x.id === a.id ? { background: 'var(--brand-tint)' } : undefined}>
          <td><span className="lc-copyable"><Link className="lc-mono" to={`/${ns}/hosts/${x.host.id}`}>{x.host.hostname}</Link><CopyButton value={x.host.hostname} /></span></td>
          <td>{x.id === a.id
            ? <span className="lc-mono">{x.path} <span className="lc-badge lc-badge--plain">{ta.viewing}</span></span>
            : <Link className="lc-mono" to={`/${ns}/apps/${x.id}?tab=general`}>{x.path}</Link>}</td>
          <td className="lc-mono">{x.code?.version ?? x.code?.branch}</td>
          <td>{x.code?.commit ? <Copyable value={x.code.commit} shown={x.code.commit.slice(0, 7)} /> : null}</td>
          <td className="text-ink-muted">{ago(x.code?.deployedAt)}</td>
          <td><StatusBadge status={x.host.status} /></td>
        </tr>
      ))}</tbody>
    </table></div>
  )
}

// useChain is the impact of the app one way from level 2, the app itself
// left out: level 1 is what the direct links already list.
function useChain(ns: string, a: App, way: 'needs' | 'dependents'): ChainNode[] | null {
  const { data } = useQuery(ChainDoc, { variables: { ns, node: `app:${a.id}` } })
  return useMemo(() => {
    const n = data?.namespace
    if (!n) return null
    const byId = new Map(n.graph.nodes.map((x) => [x.id, x]))
    return n[way].nodes.flatMap((x) => {
      const node = x.level >= 2 ? byId.get(x.id) : undefined
      return node ? [{ ...node, level: x.level } as ChainNode] : []
    })
  }, [data, way])
}

// byConsumer merges the nodes of one app, project and env deployed on
// several hosts, at the nearest level.
function byConsumer(nodes: ChainNode[]): ChainNode[] {
  const out = new Map<string, ChainNode>()
  for (const n of nodes) {
    const k = [n.kind, n.label, n.project ?? '', n.env ?? ''].join('\u0000')
    const cur = out.get(k)
    if (!cur || n.level < cur.level) out.set(k, n)
  }
  return [...out.values()].sort((x, y) => x.level - y.level)
}

// Chain lists what the app reaches, or what reaches it, through proxies
// and other apps, level by level, once per project and env whatever the
// hosts.
function Chain({ ns, nodes: all, empty }: { ns: string; nodes: ChainNode[] | null; empty: string }) {
  const t = useT()
  const ts = t.pages.shared
  const nodes = useMemo(() => all && byConsumer(all), [all])
  if (!nodes) return <span className="lc-muted">{ts.loading}</span>
  if (!nodes.length) return <p className="lc-muted m-0">{empty}</p>
  return (
    <div className="overflow-x-auto rounded-lg border border-border"><table className="lc-table">
      <thead><tr><th className="lc-num">{t.pages.app.level}</th><th>{t.pages.app.node}</th><th>{ts.type}</th><th>{ts.project}</th><th>{ts.env}</th></tr></thead>
      <tbody>{nodes.map((n) => {
        const appId = n.entity?.__typename === 'App' ? n.entity.id : null
        return (
          <tr key={n.id}>
            <td className="lc-num">{n.level}</td>
            <td>{appId ? <Link className="font-semibold" to={`/${ns}/apps/${appId}`}>{n.label}</Link> : <span className="font-semibold">{n.label}</span>}</td>
            <td className="text-ink-muted">{t.pages.app.kinds[n.kind] ?? n.kind.toLowerCase()}</td>
            <td>{n.project ? <Link to={`/${ns}/projects/${n.project}`}>{n.project}</Link> : null}</td>
            <td>{n.env}</td>
          </tr>
        )
      })}</tbody>
    </table></div>
  )
}

// Uses lists what the app uses, through the proxies, then all it needs
// through them.
function Uses({ ns, app: a }: { ns: string; app: App }) {
  const uses = useMemo(() => usesOf(a.uses), [a])
  const tbl = useTable(uses,
    (u) => [nameOf(u.to), u.project ?? '', u.env ?? '', ...u.keys, ...u.resolved],
    (u, by) => by === 'key' ? u.keys.join() : by === 'confidence' ? u.confidence : by === 'project' ? u.project ?? '' : by === 'env' ? u.env ?? '' : nameOf(u.to), 'target')
  const x = useExpanded(uses.filter((u) => crossing(u.links).length).map((u) => u.id))
  const chain = useChain(ns, a, 'needs')
  const t = useT()
  const ta = t.pages.app
  const ts = t.pages.shared
  const head = (label: string, by: string) => <SortHeader label={label} by={by} sort={tbl.sort.by} desc={tbl.sort.desc} onSort={tbl.onSort} />
  return (
    <div className="flex flex-col gap-6">
      <section className="flex flex-col gap-3">
        <h2 className="title m-0">{ta.directLinks}</h2>
        {uses.length === 0 ? <p className="lc-muted m-0">{ta.noDependency}</p> : (
          <>
            <FilterBar search={<SearchInput value={tbl.q} onChange={tbl.setQ} placeholder={ta.searchLinks} width={300} />} fields={[]}
              actions={x.some ? <ExpandAll anyOpen={x.anyOpen} onClick={x.toggleAll} /> : null} />
            <div className="overflow-x-auto rounded-lg border border-border"><table className="lc-table">
              <thead><tr><th className="lc-path__cell" />{head(ts.target, 'target')}{head(ts.project, 'project')}{head(ts.env, 'env')}{head(ts.key, 'key')}<th>{ta.resolvedBy}</th>{head(ts.confidence, 'confidence')}</tr></thead>
              <tbody>{tbl.shown.map((u) => (
                <Fragment key={u.id}>
                  <tr>
                    <PathToggle shown={crossing(u.links).length > 0} open={x.isOpen(u.id)} onToggle={() => x.toggle(u.id)} />
                    <td>
                      <span className="inline-flex items-center gap-2">
                        <span className="lc-node__techs"><TechLogo tech={techOf(u.to)} /></span>
                        <EndName ns={ns} end={u.to} />
                        <PathBadges deadEnd={u.deadEnd} />
                        <Via links={u.links} />
                      </span>
                    </td>
                    <td>{u.project ? <Link to={`/${ns}/projects/${u.project}`}>{u.project}</Link> : <span className="lc-muted">—</span>}</td>
                    <td>{u.env}</td>
                    <td style={{ whiteSpace: 'normal', maxWidth: 280, wordBreak: 'break-all' }}><div className="flex flex-wrap gap-x-3">{u.keys.map((k) => <Copyable key={k} value={k} />)}</div></td>
                    <td className="text-ink-muted">{u.resolved.join(', ')}</td>
                    <td>{confidenceText(u.confidence)}</td>
                  </tr>
                  {x.isOpen(u.id) ? <PathDetail ns={ns} links={u.links} colSpan={7} /> : null}
                </Fragment>
              ))}</tbody>
            </table></div>
            <Pager page={tbl.page} size={tbl.size} total={tbl.list.length} noun={ts.nouns.links} onPage={tbl.setPage} onSize={tbl.setSize} />
          </>
        )}
      </section>
      <section className="flex flex-col gap-3">
        <h2 className="title m-0">{ta.indirectDeps}</h2>
        <p className="lc-muted m-0 text-[13px]">{ta.indirectDepsHelp}</p>
        <Chain ns={ns} nodes={chain} empty={ta.needsNothing} />
      </section>
    </div>
  )
}

// usedByOf merges the effective links to the app by who makes them.
function usedByOf(a: App) {
  return mergeUsedBy(usedByRows(a.usedBy, { name: a.name, tech: a.framework ?? 'app' }))
}

// UsedByTab lists who uses the app, the app itself included, then all
// that reaches it through them.
function UsedByTab({ ns, app: a }: { ns: string; app: App }) {
  const rows = useMemo(() => usedByOf(a), [a])
  const chain = useChain(ns, a, 'dependents')
  const t = useT()
  const ta = t.pages.app
  // The app calling itself, from any of its hosts, is badged.
  const isSelf = (g: UsedByGroup) => g.from.__typename === 'App' && g.from.name === a.name
    && g.project === (a.project?.slug ?? projectOf(g.from)) && g.env === (a.environment?.slug ?? envOf(g.from))
  return (
    <div className="flex flex-col gap-6">
      <UsedBySection title={ta.directLinks}><UsedBy ns={ns} rows={rows} isSelf={isSelf} /></UsedBySection>
      <UsedBySection title={ta.indirectUsers} help={ta.indirectUsersHelp}>
        <Chain ns={ns} nodes={chain} empty={ta.nobodyDepends} />
      </UsedBySection>
    </div>
  )
}

// targetText writes the target of a dependency: its value, then the
// addresses a name resolved to.
function targetText(target: unknown): string {
  if (typeof target === 'string') return target
  if (Array.isArray(target)) return target.map(targetText).join(', ')
  const value = str(field(target, 'value'))
  if (!value) return target ? JSON.stringify(target) : ''
  const resolved = strings(field(target, 'resolved'))
  return resolved.length ? `${value} → ${resolved.join(', ')}` : value
}

// Config lists the dependencies read in the configuration of the app; a
// key no link resolves is told.
function Config({ app: a }: { app: App }) {
  const resolved = new Set(a.uses.map((l) => l.key).filter(Boolean))
  const t = useT()
  const ta = t.pages.app
  if (!a.dependencies.length) return <p className="lc-muted m-0">{ta.noConfig}</p>
  return (
    <div className="overflow-x-auto rounded-lg border border-border"><table className="lc-table">
      <thead><tr><th>{t.pages.shared.key}</th><th>{t.pages.shared.target}</th><th>{ta.scheme}</th><th className="lc-num">{ta.port}</th><th>{ta.state}</th></tr></thead>
      <tbody>{a.dependencies.map((d) => {
        const target = targetText(d.target)
        return (
          <tr key={d.key}>
            <td style={{ whiteSpace: 'normal', maxWidth: 320, wordBreak: 'break-all' }}><Copyable value={d.key} /></td>
            <td className="lc-mono" style={{ whiteSpace: 'normal', wordBreak: 'break-all' }}>{target}{d.path ? ` ${d.path}` : ''}</td>
            <td className="lc-mono">{d.scheme}</td>
            <td className="lc-num">{d.port}</td>
            <td>{d.unparsed ? <span className="lc-badge lc-badge--stale">{ta.unparsed}</span> : resolved.has(d.key) ? <span className="lc-badge">{ta.resolvedState}</span>
              : <span className="lc-badge lc-badge--inactive">{ta.unresolved}</span>}</td>
          </tr>
        )
      })}</tbody>
    </table></div>
  )
}

// Workers lists the programs starting the app, such as supervisor ones:
// those listening on a port serve it (web), the others work for it.
function Workers({ launchers }: { launchers: Launcher[] }) {
  const t = useT()
  const ta = t.pages.app
  if (!launchers.length) return <p className="lc-muted m-0">{ta.noWorker}</p>
  return (
    <div className="overflow-x-auto rounded-lg border border-border"><table className="lc-table">
      <thead><tr><th>{ta.program}</th><th>{ta.role}</th><th>{ta.command}</th><th>{ta.directory}</th><th>{ta.user}</th><th>{ta.launcher}</th></tr></thead>
      <tbody>{launchers.map((l) => (
        <tr key={`${l.type}/${l.name}`}>
          <td><Copyable value={l.name} /></td>
          <td><span className={l.role === 'web' ? 'lc-badge' : 'lc-badge lc-badge--plain'}>{l.role}</span></td>
          <td className="lc-mono" style={{ whiteSpace: 'normal', wordBreak: 'break-all' }}>{l.command}</td>
          <td style={{ whiteSpace: 'normal', wordBreak: 'break-all' }}>{l.directory ? <Copyable value={l.directory} /> : null}</td>
          <td className="lc-mono">{l.user}</td>
          <td>{l.type}</td>
        </tr>
      ))}</tbody>
    </table></div>
  )
}

// Scheduled lists the tasks the launchers of the app schedule, such as
// those of a gtask configuration, with their cron expressions, and their
// commands when the agent sends them (collectors.apps.commands: full).
function Scheduled({ launchers }: { launchers: Launcher[] }) {
  const t = useT()
  const ta = t.pages.app
  const rows = launchers.flatMap((l) => l.tasks.map((task) => ({ launcher: l.name, ...task })))
  if (!rows.length) return <p className="lc-muted m-0">{ta.noScheduled}</p>
  const commands = rows.some((r) => r.command)
  return (
    <div className="overflow-x-auto rounded-lg border border-border"><table className="lc-table">
      <thead><tr><th>{ta.task}</th><th>{ta.schedule}</th>{commands ? <th>{ta.command}</th> : null}<th>{ta.program}</th></tr></thead>
      <tbody>{rows.map((r) => (
        <tr key={`${r.launcher}/${r.id}`}>
          <td><Copyable value={r.id} /></td>
          <td className="lc-mono">{r.schedule ?? <span className="lc-muted">{ta.manual}</span>}</td>
          {commands ? <td className="lc-mono" style={{ whiteSpace: 'normal', wordBreak: 'break-all' }}>{r.command}</td> : null}
          <td className="lc-mono">{r.launcher}</td>
        </tr>
      ))}</tbody>
    </table></div>
  )
}
