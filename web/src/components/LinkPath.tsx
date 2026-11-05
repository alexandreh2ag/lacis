import { useQuery } from '@apollo/client'
import { Fragment, useState, type ReactNode } from 'react'
import { Chevron, TechLogo } from './Icon'
import { graphql } from '@/graphql'
import { useT } from '@/i18n'
import { confidenceText } from '@/lib/format'

// AppLink is what the listings need of an app link: who uses what through
// which proxies, whatever the hosts. Its route is read on unfolding only.
export const AppLinkFragment = graphql(`
  fragment UsedByHost on Host { id hostname project { slug } environment { slug } }
  fragment LinkEndRef on LinkEnd {
    __typename
    ... on App { id name framework host { ...UsedByHost } }
    ... on Service { id type instance version host { ...UsedByHost } }
    ... on Webserver { id type host { ...UsedByHost } }
    ... on Host { ...UsedByHost }
    ... on External { target reason }
  }
  fragment AppLink on AppLink {
    id key confidence deadEnd resolvedBy via
    from { ...LinkEndRef }
    to { ...LinkEndRef }
  }
`)

// The routes of app links, read when a row unfolds.
const LinkRoutesDoc = graphql(`
  query LinkRoutes($ns: String!, $ids: [String!]!) {
    namespace(slug: $ns) {
      id
      linkRoutes(ids: $ids) { steps { node { ...LinkEndRef } hosts rule confidence details names } }
    }
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

// PathLink is an app link as the AppLink fragment reads it.
export interface PathLink {
  id: string
  key?: string | null
  confidence: string
  deadEnd: boolean
  resolvedBy: string[]
  // via are the webservers a route crosses, by type: none for a direct
  // link.
  via: string[]
  from: End
  to: End
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

// crossing are the links of a row going through a proxy: those its
// chevron unfolds.
export function crossing(links: PathLink[]): PathLink[] {
  return links.filter((l) => l.via.length > 0)
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

// Via names the first proxy the routes of a row cross, and how many more.
export function Via({ links }: { links: PathLink[] }) {
  const m = useT().components.linkPath
  const via = links.find((l) => l.via.length > 0)?.via
  if (!via) return null
  const label = via[0] + (via.length > 1 ? ` +${via.length - 1}` : '')
  return (
    <span className="lc-via">
      <svg viewBox="0 0 12 12" fill="none" stroke="currentColor" strokeWidth={1.5} aria-hidden="true"><circle cx="2.5" cy="6" r="1.5" /><circle cx="9.5" cy="6" r="1.5" /><path d="M4 6h4" /></svg>
      {m.via(label)}
    </span>
  )
}

// PathDetail is the row under a listing row: the routes of its links
// crossing a proxy, read top down, one step a line, asked for on
// unfolding.
export function PathDetail({ ns, links, colSpan }: { ns: string; links: PathLink[]; colSpan: number }) {
  const t = useT()
  const m = t.components.linkPath
  const { data, error } = useQuery(LinkRoutesDoc, { variables: { ns, ids: crossing(links).map((l) => l.id) } })
  const routes = (data?.namespace?.linkRoutes ?? []).filter((r) => r.steps.length > 2)
  return (
    <tr className="lc-path">
      <td colSpan={colSpan}>
        {error ? <p role="alert" className="lc-path__note">{error.message}</p>
          : !data ? <p className="lc-path__note lc-muted">{t.pages.shared.loading}</p> : (
            <div className="overflow-x-auto">
              <table className="lc-path__table">
                <thead><tr><th /><th>{m.step}</th><th>{m.host}</th><th>{m.rule}</th><th>{m.detail}</th><th>{m.confidence}</th></tr></thead>
                {routes.map((r, k) => (
                  <tbody key={k}>{r.steps.map((s, i) => {
                    const end = s.node as End
                    return (
                      <tr key={i}>
                        <td className="lc-path__n">{i + 1}</td>
                        <td>
                          <span className="inline-flex items-center gap-2">
                            <span className="lc-node__techs"><TechLogo tech={techOf(end)} /></span>
                            <span className="font-semibold">{nameOf(end)}</span>
                          </span>
                        </td>
                        <td><span className="lc-path__hosts">{s.hosts.map((h) => <span key={h}>{h}</span>)}</span></td>
                        <td className="text-ink-muted">{s.rule ?? '—'}</td>
                        <td className="lc-path__detail">
                          {i === 0 ? <span className="text-ink-muted">{m.source}</span> : <Lines values={s.details} />}
                          {s.names.length ? <small>{s.names.join(', ')}</small> : null}
                        </td>
                        <td>{s.confidence ? confidenceText(s.confidence) : null}</td>
                      </tr>
                    )
                  })}</tbody>
                ))}
              </table>
            </div>
          )}
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
