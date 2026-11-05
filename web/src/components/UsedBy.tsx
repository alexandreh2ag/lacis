import type { ReactNode } from 'react'
import { Link } from 'react-router'
import { Copyable } from './Copy'
import { TechLogo } from './Icon'
import { FilterBar, type FilterField } from './FilterBar'
import { Pager, SearchInput, SortHeader, useTable } from './Listing'
import { graphql } from '@/graphql'
import { useT } from '@/i18n'
import { confidenceText } from '@/lib/format'

// UsedByLink is what UsedBy needs of a link to a component.
export const UsedByLink = graphql(`
  fragment UsedByHost on Host { id hostname project { slug } environment { slug } }
  fragment UsedByLink on Link {
    id key confidence
    from {
      __typename
      ... on App { id name framework host { ...UsedByHost } }
      ... on Service { id type instance host { ...UsedByHost } }
      ... on Webserver { id type host { ...UsedByHost } }
      ... on Host { ...UsedByHost }
      ... on External { target reason }
    }
  }
`)

interface HostRef {
  id: string
  hostname: string
  project?: { slug: string } | null
  environment?: { slug: string } | null
}

// End is the end of a link, as the pages query it.
export type End =
  | { __typename: 'App'; id: string; name: string; framework?: string | null; host: HostRef }
  | { __typename: 'Service'; id: string; type: string; instance: string; host: HostRef }
  | { __typename: 'Webserver'; id: string; type: string; host: HostRef }
  | ({ __typename: 'Host' } & HostRef)
  | { __typename: 'External'; target: string; reason?: string | null }

// UsedByRow is a link to a component: who uses it, and which one.
export interface UsedByRow {
  id: string
  key?: string | null
  confidence: string
  from: End
  // target names the component used, on the host page or the project page.
  target: { name: string; tech: string; host?: { id: string; hostname: string } }
}

// UsedByGroup is one consumer of the components, whatever the hosts it runs
// on and the components it uses: an app, a service or a webserver of a
// project and an env.
export interface UsedByGroup {
  id: string
  from: End
  project?: string
  env?: string
  targets: { name: string; tech: string }[]
  keys: string[]
  confidence: string
}

// mergeUsedBy groups the links by consumer, project and env, whatever the
// hosts and the components: the listing says who uses the entity, once.
export function mergeUsedBy(rows: UsedByRow[]): UsedByGroup[] {
  const out = new Map<string, UsedByGroup>()
  for (const r of rows) {
    const host = hostOf(r.from)
    const project = host?.project?.slug
    const env = host?.environment?.slug
    const id = [r.from.__typename, techOf(r.from), nameOf(r.from), project, env].join('\u0000')
    const g = out.get(id) ?? { id, from: r.from, project, env, targets: [], keys: [], confidence: r.confidence }
    if (!g.targets.some((x) => x.name === r.target.name && x.tech === r.target.tech)) g.targets.push({ name: r.target.name, tech: r.target.tech })
    if (r.key && !g.keys.includes(r.key)) g.keys.push(r.key)
    if (r.confidence !== 'EXACT') g.confidence = r.confidence
    out.set(id, g)
  }
  for (const g of out.values()) g.targets.sort((a, b) => a.name.localeCompare(b.name))
  return [...out.values()]
}

// splitUsedBy sets apart the links the entity makes to itself, then merges
// both by consumer.
export function splitUsedBy(rows: UsedByRow[], isSelf: (r: UsedByRow) => boolean): { others: UsedByGroup[]; self: UsedByGroup[] } {
  const others: UsedByRow[] = []
  const self: UsedByRow[] = []
  for (const r of rows) (isSelf(r) ? self : others).push(r)
  return { others: mergeUsedBy(others), self: mergeUsedBy(self) }
}

export function hostOf(e: End): HostRef | null {
  if (e.__typename === 'External') return null
  return e.__typename === 'Host' ? e : e.host
}

export function nameOf(e: End): string {
  switch (e.__typename) {
    case 'App': return e.name
    case 'Service': return e.instance && e.instance !== 'default' ? `${e.type} ${e.instance}` : e.type
    case 'Webserver': return e.type
    case 'Host': return e.hostname
    default: return e.target
  }
}

export function techOf(e: End): string {
  switch (e.__typename) {
    case 'App': return e.framework ?? 'app'
    case 'Service': case 'Webserver': return e.type
    case 'Host': return 'host'
    default: return 'ext'
  }
}

// UsedBy lists who uses the components of a host, a project or an app: one
// row per consumer, project and env, without the hosts.
export function UsedBy({ ns, rows, fields = [], emptyText }: { ns: string; rows: UsedByGroup[]; fields?: FilterField[]; emptyText?: string }) {
  const m = useT().components.usedBy
  const t = useTable(rows,
    (r) => [nameOf(r.from), r.project ?? '', r.env ?? '', ...r.targets.map((x) => x.name), ...r.keys],
    (r, by) => by === 'project' ? r.project ?? '' : by === 'env' ? r.env ?? ''
      : by === 'target' ? r.targets.map((x) => x.name).join() : by === 'confidence' ? r.confidence : nameOf(r.from) + (r.env ?? ''),
    'from')
  const empty = <p className="lc-muted m-0">{emptyText ?? m.empty}</p>
  // Without filter to clear, the empty listing says so alone.
  if (rows.length === 0 && !fields.some((f) => f.value !== null)) return empty
  const head = (label: string, by: string) => <SortHeader label={label} by={by} sort={t.sort.by} desc={t.sort.desc} onSort={t.onSort} />
  return (
    <>
      <FilterBar search={<SearchInput value={t.q} onChange={t.setQ} placeholder={m.search} width={320} />}
        fields={fields} count={m.count(rows.length)} />
      {rows.length === 0 ? empty : null}
      <div className="overflow-x-auto rounded-lg border border-border" hidden={rows.length === 0}><table className="lc-table">
        <thead><tr>
          {head(m.from, 'from')}{head(m.project, 'project')}{head(m.env, 'env')}{head(m.target, 'target')}<th>{m.key}</th>{head(m.confidence, 'confidence')}
        </tr></thead>
        <tbody>{t.shown.map((r) => (
          <tr key={r.id}>
            <td>
              <span className="inline-flex items-center gap-2">
                <span className="lc-node__techs"><TechLogo tech={techOf(r.from)} /></span>
                {r.from.__typename === 'External' ? <Copyable value={r.from.target} />
                  : r.from.__typename === 'App' ? <Link className="font-semibold" to={`/${ns}/apps/${r.from.id}`}>{nameOf(r.from)}</Link>
                  : <span className="font-semibold">{nameOf(r.from)}</span>}
              </span>
            </td>
            <td>{r.project ? <Link to={`/${ns}/projects/${r.project}`}>{r.project}</Link> : <span className="lc-muted">—</span>}</td>
            <td>{r.env}</td>
            <td>
              <span className="flex flex-wrap items-center gap-x-3 gap-y-1">
                {r.targets.map((x) => (
                  <span key={x.tech + x.name} className="inline-flex items-center gap-2">
                    <span className="lc-node__techs"><TechLogo tech={x.tech} /></span>
                    <span>{x.name}</span>
                  </span>
                ))}
              </span>
            </td>
            <td style={{ whiteSpace: 'normal', maxWidth: 280, wordBreak: 'break-all' }}>
              <div className="flex flex-wrap gap-x-3">{r.keys.map((k) => <Copyable key={k} value={k} />)}</div>
            </td>
            <td>{confidenceText(r.confidence)}</td>
          </tr>
        ))}</tbody>
      </table></div>
      <Pager page={t.page} size={t.size} total={t.list.length} noun={m.noun} onPage={t.setPage} onSize={t.setSize} />
    </>
  )
}

// UsedBySection titles one listing of the tab.
export function UsedBySection({ title, help, children }: { title: string; help?: string; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-3">
      <h2 className="title m-0">{title}</h2>
      {help ? <p className="lc-muted m-0 text-[13px]">{help}</p> : null}
      {children}
    </section>
  )
}

// Consumers names who uses a component, in a cell of a listing: two of
// them, then how many more.
export function Consumers({ ns, links }: { ns: string; links: { from: End }[] }) {
  const seen = new Map<string, End>()
  for (const l of links) {
    const host = hostOf(l.from)
    seen.set(nameOf(l.from) + '@' + (host?.id ?? ''), l.from)
  }
  const all = [...seen.values()]
  if (all.length === 0) return <span className="text-ink-muted">—</span>
  return (
    <span className="flex flex-wrap items-center gap-x-3 gap-y-0.5">
      {all.slice(0, 2).map((e) => {
        const host = hostOf(e)
        const label = e.__typename === 'Host' ? e.hostname : `${nameOf(e)}${host ? ' · ' + host.hostname : ''}`
        return host ? <Link key={label} className="lc-mono text-[12.5px]" to={`/${ns}/hosts/${host.id}`}>{label}</Link> : <span key={label} className="lc-mono text-[12.5px]">{label}</span>
      })}
      {all.length > 2 ? <span className="lc-more" title={all.slice(2).map(nameOf).join(', ')}>+{all.length - 2}</span> : null}
    </span>
  )
}
