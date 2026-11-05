import { Fragment, useState, type ReactNode } from 'react'
import { Chevron, TechLogo } from './Icon'
import { graphql } from '@/graphql'
import { useT } from '@/i18n'
import { confidenceText } from '@/lib/format'
import { field, str, strings } from '@/lib/json'

// EffectiveLink is what the listings need of an effective link: who uses
// what, through the proxies of its path.
export const EffectiveLinkFragment = graphql(`
  fragment UsedByHost on Host { id hostname project { slug } environment { slug } }
  fragment LinkEndRef on LinkEnd {
    __typename
    ... on App { id name framework host { ...UsedByHost } }
    ... on Service { id type instance version host { ...UsedByHost } }
    ... on Webserver { id type host { ...UsedByHost } }
    ... on Host { ...UsedByHost }
    ... on External { target reason }
  }
  fragment EffectiveLink on EffectiveLink {
    key confidence deadEnd
    from { ...LinkEndRef }
    to { ...LinkEndRef }
    path { id rule reason confidence detail to { ...LinkEndRef } }
  }
`)

export interface HostRef {
  id: string
  hostname: string
  project?: { slug: string } | null
  environment?: { slug: string } | null
}

// End is the end of a link, as the pages query it.
export type End =
  | { __typename: 'App'; id: string; name: string; framework?: string | null; host: HostRef }
  | { __typename: 'Service'; id: string; type: string; instance: string; version?: string | null; host: HostRef }
  | { __typename: 'Webserver'; id: string; type: string; host: HostRef }
  | ({ __typename: 'Host' } & HostRef)
  | { __typename: 'External'; target: string; reason?: string | null }

// PathLink is an effective link as the EffectiveLink fragment reads it.
export interface PathLink {
  key?: string | null
  confidence: string
  deadEnd: boolean
  from: End
  to: End
  path: { id: string; rule: string; reason?: string | null; confidence: string; detail: unknown; to: End }[]
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

// cdnOf is the name of the CDN a link goes through, as linking.cdns names
// it; none for the unnamed ranges, which the cdn reason already tells.
export function cdnOf(detail: unknown): string | undefined {
  const name = str(field(detail, 'cdn'))
  return name && name !== 'cdn' ? name : undefined
}

// ruleOf tells how a link was resolved: its rule, its reason, its CDN.
export function ruleOf(l: { rule: string; reason?: string | null; detail: unknown }): string {
  return [l.rule, l.reason, cdnOf(l.detail)].filter(Boolean).join(' · ')
}

// Step is a node of a route: the consumer, a proxy crossed, or what is
// used, with how the link before it was resolved. Merged, it holds the
// hosts and the details of every deployment.
export interface Step {
  end: End
  hosts: string[]
  rule?: string
  // details are the dependency for the first link, the routes after;
  // names the names a proxy routes for the call.
  details: string[]
  names: string[]
  confidence?: string
}

export type Route = Step[]

// calledName is the host name an address calls; none for an IP or a
// socket.
export function calledName(target: string): string | undefined {
  let host = target.includes('://') ? target.slice(target.indexOf('://') + 3) : target
  host = host.split('/')[0]
  host = host.startsWith('[') ? host.slice(1, host.indexOf(']')) : host.split(':')[0]
  if (!host || /^[\d.]+$/.test(host) || host.includes(':')) return undefined
  return host.toLowerCase()
}

// routes tells whether a domain pattern of a webserver serves a name, as
// the server matches them.
export function routes(pattern: string, name: string): boolean {
  if (pattern.startsWith('~')) {
    try { return new RegExp(pattern.slice(1)).test(name) } catch { return false }
  }
  const p = pattern.toLowerCase().replace(/\.$/, '')
  if (p.startsWith('*.')) return name.endsWith(p.slice(1))
  if (p.startsWith('.')) return name === p.slice(1) || name.endsWith(p)
  return p === name
}

// routeOf is the route of an effective link, step by step.
export function routeOf(l: PathLink): Route {
  const first = l.path[0]?.detail
  const key = l.key ?? str(field(first, 'key'))
  const target = str(field(first, 'target'))
  const name = target ? calledName(target) : undefined
  const host = (e: End) => { const h = hostOf(e); return h ? [h.hostname] : [] }
  const route: Route = [{ end: l.from, hosts: host(l.from), details: [], names: [] }]
  l.path.forEach((p, i) => {
    const details = i === 0
      ? [key && target ? `${key} = ${target}` : key ?? target ?? ''].filter(Boolean)
      : [strings(field(p.detail, 'routes')).join(', ')].filter(Boolean)
    const names = i > 0 && name ? strings(field(p.detail, 'domains')).filter((d) => routes(d, name)) : []
    route.push({ end: p.to, hosts: host(p.to), rule: ruleOf(p), details, names, confidence: p.confidence })
  })
  return route
}

function signature(r: Route): string {
  return r.map((s) => {
    const h = hostOf(s.end)
    return [s.end.__typename, nameOf(s.end), h?.project?.slug ?? '', h?.environment?.slug ?? '', s.rule ?? ''].join('\u0001')
  }).join('\u0000')
}

function addNew(list: string[], values: string[]) {
  for (const v of values) if (!list.includes(v)) list.push(v)
}

// mergeRoutes merges the routes going through the same nodes, whatever
// their hosts: the hosts and the details of each step add up.
export function mergeRoutes(all: Route[]): Route[] {
  const out = new Map<string, Route>()
  for (const r of all) {
    const k = signature(r)
    const cur = out.get(k)
    if (!cur) {
      out.set(k, r.map((s) => ({ ...s, hosts: [...s.hosts], details: [...s.details], names: [...s.names] })))
      continue
    }
    r.forEach((s, i) => {
      addNew(cur[i].hosts, s.hosts)
      addNew(cur[i].details, s.details)
      addNew(cur[i].names, s.names)
      if (s.confidence && s.confidence !== 'EXACT') cur[i].confidence = s.confidence
    })
  }
  for (const r of out.values()) for (const s of r) s.hosts.sort()
  return [...out.values()]
}

// crosses tells routes going through a proxy: those the chevron unfolds.
export function crosses(rs: Route[]): boolean {
  return rs.some((r) => r.length > 2)
}

// useExpanded keeps the rows unfolded, and folds or unfolds them all.
export function useExpanded(ids: string[]) {
  const [open, setOpen] = useState<Set<string>>(() => new Set())
  const anyOpen = ids.some((id) => open.has(id))
  return {
    isOpen: (id: string) => open.has(id),
    toggle: (id: string) => setOpen((cur) => {
      const next = new Set(cur)
      if (!next.delete(id)) next.add(id)
      return next
    }),
    anyOpen,
    toggleAll: () => setOpen(anyOpen ? new Set() : new Set(ids)),
    some: ids.length > 0,
  }
}

// ExpandAll folds every route of a listing, or unfolds them all.
export function ExpandAll({ anyOpen, onClick }: { anyOpen: boolean; onClick: () => void }) {
  const m = useT().components.linkPath
  return (
    <button type="button" className="lc-btn lc-path__all" onClick={onClick}>
      <Chevron dir={anyOpen ? 'up' : 'down'} />{anyOpen ? m.collapseAll : m.expandAll}
    </button>
  )
}

// PathToggle is the chevron starting a row whose route crosses proxies;
// an empty cell holds its place on the other rows.
export function PathToggle({ shown, open, onToggle }: { shown: boolean; open: boolean; onToggle: () => void }) {
  const m = useT().components.linkPath
  return (
    <td className="lc-path__cell">
      {shown ? (
        <button type="button" className="lc-path__toggle" aria-expanded={open} aria-label={open ? m.collapse : m.expand} onClick={onToggle}>
          <Chevron dir={open ? 'down' : 'right'} />
        </button>
      ) : null}
    </td>
  )
}

// Via names the first proxy a route crosses, and how many more.
export function Via({ routes: rs }: { routes: Route[] }) {
  const m = useT().components.linkPath
  const crossing = rs.find((r) => r.length > 2)
  if (!crossing) return null
  const proxies = crossing.slice(1, -1)
  const label = nameOf(proxies[0].end) + (proxies.length > 1 ? ` +${proxies.length - 1}` : '')
  return (
    <span className="lc-via">
      <svg viewBox="0 0 12 12" fill="none" stroke="currentColor" strokeWidth={1.5} aria-hidden="true"><circle cx="2.5" cy="6" r="1.5" /><circle cx="9.5" cy="6" r="1.5" /><path d="M4 6h4" /></svg>
      {m.via(label)}
    </span>
  )
}

// PathDetail is the row under a listing row: its routes, read top down,
// one step a line.
export function PathDetail({ routes: rs, colSpan }: { routes: Route[]; colSpan: number }) {
  const m = useT().components.linkPath
  return (
    <tr className="lc-path">
      <td colSpan={colSpan}>
        <div className="overflow-x-auto">
          <table className="lc-path__table">
            <thead><tr><th /><th>{m.step}</th><th>{m.host}</th><th>{m.rule}</th><th>{m.detail}</th><th>{m.confidence}</th></tr></thead>
            {rs.map((r, k) => (
              <tbody key={k}>{r.map((s, i) => (
                <tr key={i}>
                  <td className="lc-path__n">{i + 1}</td>
                  <td>
                    <span className="inline-flex items-center gap-2">
                      <span className="lc-node__techs"><TechLogo tech={techOf(s.end)} /></span>
                      <span className="font-semibold">{nameOf(s.end)}</span>
                    </span>
                  </td>
                  <td><span className="lc-path__hosts">{s.hosts.map((h) => <span key={h}>{h}</span>)}</span></td>
                  <td className="text-ink-muted">{i === 0 ? '—' : s.rule}</td>
                  <td className="lc-path__detail">
                    {i === 0 ? <span className="text-ink-muted">{m.source}</span> : <Lines values={s.details} />}
                    {s.names.length ? <small>{s.names.join(', ')}</small> : null}
                  </td>
                  <td>{s.confidence ? confidenceText(s.confidence) : null}</td>
                </tr>
              ))}</tbody>
            ))}
          </table>
        </div>
      </td>
    </tr>
  )
}

function Lines({ values }: { values: string[] }) {
  return <>{values.map((v, i) => <Fragment key={v}>{i ? <br /> : null}<span className="lc-mono">{v}</span></Fragment>)}</>
}

// PathBadges tell a route ending at a proxy, or the entity using itself.
export function PathBadges({ deadEnd, self }: { deadEnd?: boolean; self?: boolean }): ReactNode {
  const m = useT().components.linkPath
  return (
    <>
      {self ? <span className="lc-badge lc-badge--plain lc-badge--self h-[18px] text-[11px]">{m.self}</span> : null}
      {deadEnd ? <span className="lc-badge lc-badge--plain lc-badge--stale h-[18px] text-[11px]" title={m.deadEndHelp}>{m.deadEnd}</span> : null}
    </>
  )
}
