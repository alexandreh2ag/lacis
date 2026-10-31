import { Link } from 'react-router'
import { CopyButton, Copyable } from './Copy'
import { TechLogo } from './Icon'
import { FilterBar, type FilterField } from './FilterBar'
import { Pager, SearchInput, SortHeader, useTable } from './Listing'
import { graphql } from '@/graphql'
import { confidenceFr } from '@/lib/format'

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

// UsedBy lists the links to the components of a host or a project, by who
// makes them.
export function UsedBy({ ns, rows, showTargetHost, fields = [] }: { ns: string; rows: UsedByRow[]; showTargetHost?: boolean; fields?: FilterField[] }) {
  const t = useTable(rows,
    (r) => [nameOf(r.from), hostOf(r.from)?.hostname ?? '', hostOf(r.from)?.project?.slug ?? '', r.target.name, r.target.host?.hostname ?? '', r.key ?? ''],
    (r, by) => by === 'project' ? hostOf(r.from)?.project?.slug ?? '' : by === 'env' ? hostOf(r.from)?.environment?.slug ?? ''
      : by === 'target' ? r.target.name + (r.target.host?.hostname ?? '') : by === 'confidence' ? r.confidence : nameOf(r.from) + (hostOf(r.from)?.hostname ?? ''),
    'from')
  const empty = <p className="lc-muted m-0">Rien ne dépend de ces composants : aucun lien n'a été résolu vers eux.</p>
  // Without filter to clear, the empty listing says so alone.
  if (rows.length === 0 && !fields.some((f) => f.value !== null)) return empty
  const head = (label: string, by: string) => <SortHeader label={label} by={by} sort={t.sort.by} desc={t.sort.desc} onSort={t.onSort} />
  return (
    <>
      <FilterBar search={<SearchInput value={t.q} onChange={t.setQ} placeholder="Chercher une app, un host, un projet, une clé" width={320} />}
        fields={fields} count={`${rows.length} liens`} />
      {rows.length === 0 ? empty : null}
      <div className="overflow-x-auto rounded-lg border border-border" hidden={rows.length === 0}><table className="lc-table">
        <thead><tr>
          {head('Utilisé par', 'from')}{head('Projet', 'project')}{head('Env', 'env')}{head('Composant utilisé', 'target')}<th>Clé</th>{head('Confiance', 'confidence')}
        </tr></thead>
        <tbody>{t.shown.map((r) => {
          const host = hostOf(r.from)
          return (
            <tr key={r.id}>
              <td>
                <span className="inline-flex items-center gap-2">
                  <span className="lc-node__techs"><TechLogo tech={techOf(r.from)} /></span>
                  {r.from.__typename === 'External' ? <Copyable value={r.from.target} /> : (
                    <span className="flex flex-col leading-[18px]">
                      {r.from.__typename === 'App' ? <Link className="font-semibold" to={`/${ns}/apps/${r.from.id}`}>{nameOf(r.from)}</Link>
                        : r.from.__typename !== 'Host' ? <span className="font-semibold">{nameOf(r.from)}</span> : null}
                      {host ? <span className="lc-copyable"><Link className="lc-mono text-[12.5px]" to={`/${ns}/hosts/${host.id}`}>{host.hostname}</Link><CopyButton value={host.hostname} /></span> : null}
                    </span>
                  )}
                </span>
              </td>
              <td>{host?.project ? <Link to={`/${ns}/projects/${host.project.slug}`}>{host.project.slug}</Link> : <span className="lc-muted">—</span>}</td>
              <td>{host?.environment?.slug}</td>
              <td>
                <span className="inline-flex items-center gap-2">
                  <span className="lc-node__techs"><TechLogo tech={r.target.tech} /></span>
                  <span className="flex flex-col leading-[18px]">
                    <span>{r.target.name}</span>
                    {showTargetHost && r.target.host ? <Link className="lc-mono text-[12.5px]" to={`/${ns}/hosts/${r.target.host.id}`}>{r.target.host.hostname}</Link> : null}
                  </span>
                </span>
              </td>
              <td style={{ whiteSpace: 'normal', maxWidth: 280, wordBreak: 'break-all' }}>{r.key ? <Copyable value={r.key} /> : null}</td>
              <td>{confidenceFr[r.confidence]}</td>
            </tr>
          )
        })}</tbody>
      </table></div>
      <Pager page={t.page} size={t.size} total={t.list.length} noun="liens" onPage={t.setPage} onSize={t.setSize} />
    </>
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
