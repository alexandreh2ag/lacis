import { Fragment, type ReactNode } from 'react'
import { Link } from 'react-router'
import { Copyable } from './Copy'
import { TechLogo } from './Icon'
import { FilterBar, type FilterField } from './FilterBar'
import { crossing, ExpandAll, hostOf, nameOf, PathBadges, PathDetail, PathToggle, techOf, useExpanded, Via,
  type End, type PathLink } from './LinkPath'
import { Pager, SearchInput, SortHeader, useTable } from './Listing'
import { useT } from '@/i18n'
import { confidenceText } from '@/lib/format'

export { hostOf, nameOf, techOf, type End } from './LinkPath'

// UsedByRow is an app link to a component: who uses it, and which one.
export interface UsedByRow {
  link: PathLink
  // target names the component used, on the host page or the project page.
  target: { name: string; tech: string; host?: { id: string; hostname: string } }
}

// usedByRows are the app links to a component.
export function usedByRows(links: PathLink[], target: UsedByRow['target']): UsedByRow[] {
  return links.map((link) => ({ link, target }))
}

// UsedByGroup is one consumer of one component, whatever the hosts it runs
// on: an app of a project and an env, so that its route tells one link.
export interface UsedByGroup {
  id: string
  from: End
  project?: string
  env?: string
  targets: { name: string; tech: string }[]
  keys: string[]
  confidence: string
  // links are its app links, whose routes unfold under the row.
  links: PathLink[]
}

// mergeUsedBy groups the links by consumer, project, env and component
// used, whatever the hosts: the listing says who uses what, once.
export function mergeUsedBy(rows: UsedByRow[]): UsedByGroup[] {
  const out = new Map<string, UsedByGroup>()
  for (const r of rows) {
    const from = r.link.from
    const host = hostOf(from)
    const project = host?.project?.slug
    const env = host?.environment?.slug
    const id = [from.__typename, techOf(from), nameOf(from), project, env, r.target.tech, r.target.name].join('\u0000')
    const g = out.get(id) ?? { id, from, project, env, targets: [], keys: [], confidence: r.link.confidence, links: [] }
    if (!g.targets.some((x) => x.name === r.target.name && x.tech === r.target.tech)) g.targets.push({ name: r.target.name, tech: r.target.tech })
    if (r.link.key && !g.keys.includes(r.link.key)) g.keys.push(r.link.key)
    if (r.link.confidence !== 'EXACT') g.confidence = r.link.confidence
    if (!g.links.some((l) => l.id === r.link.id)) g.links.push(r.link)
    out.set(id, g)
  }
  return [...out.values()].map((g) => ({ ...g, targets: g.targets.sort((a, b) => a.name.localeCompare(b.name)) }))
}

// splitUsedBy sets apart the links the entity makes to itself, then merges
// both by consumer.
export function splitUsedBy(rows: UsedByRow[], isSelf: (r: UsedByRow) => boolean): { others: UsedByGroup[]; self: UsedByGroup[] } {
  const others: UsedByRow[] = []
  const self: UsedByRow[] = []
  for (const r of rows) (isSelf(r) ? self : others).push(r)
  return { others: mergeUsedBy(others), self: mergeUsedBy(self) }
}

// UsedBy lists who uses the components of a host, a project or an app: one
// row per consumer, project, env and component, without the hosts. A row whose
// requests cross proxies unfolds their route.
export function UsedBy({ ns, rows, fields = [], emptyText, isSelf }: {
  ns: string; rows: UsedByGroup[]; fields?: FilterField[]; emptyText?: string
  // isSelf tells the entity using itself, badged.
  isSelf?: (g: UsedByGroup) => boolean
}) {
  const m = useT().components.usedBy
  const t = useTable(rows,
    (r) => [nameOf(r.from), r.project ?? '', r.env ?? '', ...r.targets.map((x) => x.name), ...r.keys],
    (r, by) => by === 'project' ? r.project ?? '' : by === 'env' ? r.env ?? ''
      : by === 'target' ? r.targets.map((x) => x.name).join() : by === 'confidence' ? r.confidence : nameOf(r.from) + (r.env ?? ''),
    'from')
  const x = useExpanded(rows.filter((r) => crossing(r.links).length).map((r) => r.id))
  const empty = <p className="lc-muted m-0">{emptyText ?? m.empty}</p>
  // Without filter to clear, the empty listing says so alone.
  if (rows.length === 0 && !fields.some((f) => f.value !== null)) return empty
  const head = (label: string, by: string) => <SortHeader label={label} by={by} sort={t.sort.by} desc={t.sort.desc} onSort={t.onSort} />
  return (
    <>
      <FilterBar search={<SearchInput value={t.q} onChange={t.setQ} placeholder={m.search} width={320} />}
        fields={fields} count={m.links(rows.length)} actions={x.some ? <ExpandAll anyOpen={x.anyOpen} onClick={x.toggleAll} /> : null} />
      {rows.length === 0 ? empty : null}
      <div className="overflow-x-auto rounded-lg border border-border" hidden={rows.length === 0}><table className="lc-table">
        <thead><tr>
          <th className="lc-path__cell" />
          {head(m.from, 'from')}{head(m.project, 'project')}{head(m.env, 'env')}{head(m.target, 'target')}<th>{m.key}</th>{head(m.confidence, 'confidence')}
        </tr></thead>
        <tbody>{t.shown.map((r) => (
          <Fragment key={r.id}>
            <tr>
              <PathToggle shown={crossing(r.links).length > 0} open={x.isOpen(r.id)} onToggle={() => x.toggle(r.id)} />
              <td>
                <span className="inline-flex items-center gap-2">
                  <span className="lc-node__techs"><TechLogo tech={techOf(r.from)} /></span>
                  <EndName ns={ns} end={r.from} />
                  <PathBadges self={isSelf?.(r)} />
                  <Via links={r.links} />
                </span>
              </td>
              <td>{r.project ? <Link to={`/${ns}/projects/${r.project}`}>{r.project}</Link> : <span className="lc-muted">—</span>}</td>
              <td>{r.env}</td>
              <td>
                <span className="flex flex-wrap items-center gap-x-3 gap-y-1">
                  {r.targets.map((y) => (
                    <span key={y.tech + y.name} className="inline-flex items-center gap-2">
                      <span className="lc-node__techs"><TechLogo tech={y.tech} /></span>
                      <span>{y.name}</span>
                    </span>
                  ))}
                </span>
              </td>
              <td style={{ whiteSpace: 'normal', maxWidth: 280, wordBreak: 'break-all' }}>
                <div className="flex flex-wrap gap-x-3">{r.keys.map((k) => <Copyable key={k} value={k} />)}</div>
              </td>
              <td>{confidenceText(r.confidence)}</td>
            </tr>
            {x.isOpen(r.id) ? <PathDetail ns={ns} links={r.links} colSpan={7} /> : null}
          </Fragment>
        ))}</tbody>
      </table></div>
      <Pager page={t.page} size={t.size} total={t.list.length} noun={m.noun} onPage={t.setPage} onSize={t.setSize} />
    </>
  )
}

// EndName names an end of a link, a link to its page where it has one.
export function EndName({ ns, end }: { ns: string; end: End }) {
  switch (end.__typename) {
    case 'External': return <Copyable value={end.target} />
    case 'App': return <Link className="font-semibold" to={`/${ns}/apps/${end.id}`}>{nameOf(end)}</Link>
    case 'Host': return <Link className="lc-mono" to={`/${ns}/hosts/${end.id}`}>{end.hostname}</Link>
    default: return <span className="font-semibold">{nameOf(end)}</span>
  }
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
