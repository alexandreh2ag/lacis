import { useQuery } from '@apollo/client'
import { useMemo } from 'react'
import { Link, useParams } from 'react-router'
import { CopyButton, Copyable } from '@/components/Copy'
import { HistoryEvent } from '@/components/HistoryEvents'
import { Page } from '@/components/Layout'
import { chipsField, FilterBar } from '@/components/FilterBar'
import { Pager, SearchInput, SortHeader, useListParams } from '@/components/Listing'
import { projectRows, ProjectsTable } from './Projects'
import { graphql } from '@/graphql'
import type { NamespacesQuery } from '@/graphql/graphql'

const HomeDoc = graphql(`
  query Home($ns: String!) {
    namespace(slug: $ns) {
      id slug name description
      counts { hosts apps services links externalLinks }
      environments { id slug sortOrder }
      hosts(page: { limit: 500 }) { items { id status } }
      graph(level: PROJECT, filter: { status: [ACTIVE, STALE, INACTIVE] }) {
        nodes { id kind label project env count apps techs role }
        edges { from to }
      }
      history(page: { limit: 5 }) { items { id occurredAt hostname entityType kind before after } }
    }
  }
`)

const NamespacesDoc = graphql(`
  query Namespaces {
    namespaces {
      id slug name description canEdit
      counts { projects environments hosts staleHosts inactiveHosts apps webservers services links }
    }
  }
`)

type NamespaceRow = NamespacesQuery['namespaces'][number]

const namespaceSorts: Record<string, (n: NamespaceRow) => string | number> = {
  name: (n) => n.name,
  projects: (n) => n.counts.projects,
  environments: (n) => n.counts.environments,
  hosts: (n) => n.counts.hosts,
  stale: (n) => n.counts.staleHosts + n.counts.inactiveHosts,
  apps: (n) => n.counts.apps,
  webservers: (n) => n.counts.webservers,
  services: (n) => n.counts.services,
  links: (n) => n.counts.links,
}

// NamespaceIndex lists the namespaces the user may read.
export function NamespaceIndex() {
  const { data, error } = useQuery(NamespacesDoc)
  const l = useListParams({ sort: 'name' })
  const all = data?.namespaces ?? []
  const filters = l.list('only')
  const q = l.q.toLowerCase()
  const value = namespaceSorts[l.sort] ?? namespaceSorts.name
  const list = all.filter((n) => (!q || [n.slug, n.name, n.description ?? ''].some((x) => x.toLowerCase().includes(q)))
    && (!filters.includes('attention') || n.counts.staleHosts + n.counts.inactiveHosts > 0)
    && (!filters.includes('editable') || n.canEdit))
    .sort((a, b) => {
      const x = value(a)
      const y = value(b)
      const c = typeof x === 'number' && typeof y === 'number' ? x - y : String(x).localeCompare(String(y), 'fr', { numeric: true })
      return (l.desc ? -c : c) || a.slug.localeCompare(b.slug)
    })
  const onSort = (by: string) => l.toggleSort(by, l.sort, l.desc)
  const head = (label: string, by: string, num = true) => <SortHeader label={label} by={by} sort={l.sort} desc={l.desc} onSort={onSort} num={num} />
  const attention = all.filter((n) => n.counts.staleHosts + n.counts.inactiveHosts > 0).length
  return (
    <Page title="Namespaces">
      {error ? <p role="alert">Les namespaces n'ont pas pu être chargés : {error.message}</p> : null}
      {data && all.length === 0 ? <p className="lc-muted">Aucun namespace ne vous est ouvert. Demandez un rôle à un administrateur.</p> : null}
      {all.length > 0 ? (
        <>
          <FilterBar
            search={<SearchInput value={l.q} onChange={(v) => l.set({ q: v })} placeholder="Chercher un namespace, une description" />}
            fields={[
              chipsField({ label: 'État des hosts', short: 'Hosts', options: [{ value: 'attention', label: 'stale ou inactifs', count: attention }],
                selected: filters.filter((f) => f === 'attention'), onChange: (v) => l.set({ only: [...v, ...filters.filter((f) => f !== 'attention')] }) }),
              chipsField({ label: 'Mes droits', short: 'Droits', options: [{ value: 'editable', label: 'modifiables', count: all.filter((n) => n.canEdit).length }],
                selected: filters.filter((f) => f === 'editable'), onChange: (v) => l.set({ only: [...v, ...filters.filter((f) => f !== 'editable')] }) }),
            ]}
            onClearAll={() => l.set({ only: null })}
            count={`${list.length} namespaces`}
          />
          <div className="overflow-x-auto rounded-lg border border-border">
            <table className="lc-table">
              <thead><tr>
                {head('Namespace', 'name', false)}
                {head('Projets', 'projects')}
                {head('Envs', 'environments')}
                {head('Hosts', 'hosts')}
                {head('Stale / inactifs', 'stale')}
                {head('Apps', 'apps')}
                {head('Webservers', 'webservers')}
                {head('Services', 'services')}
                {head('Liens', 'links')}
              </tr></thead>
              <tbody>
                {list.slice((l.page - 1) * l.size, l.page * l.size).map((n) => (
                  <tr key={n.id}>
                    <td style={{ whiteSpace: 'normal', minWidth: 240 }}>
                      <div className="flex flex-col">
                        <span className="lc-copyable"><Link className="font-semibold" to={`/${n.slug}`}>{n.name}</Link><CopyButton value={n.slug} /></span>
                        <span className="text-[12.5px] text-ink-muted"><span className="lc-mono">{n.slug}</span>{n.description ? ` · ${n.description}` : ''}</span>
                      </div>
                    </td>
                    <td className="lc-num">{n.counts.projects}</td>
                    <td className="lc-num">{n.counts.environments}</td>
                    <td className="lc-num">{n.counts.hosts}</td>
                    <td className="lc-num">
                      {n.counts.staleHosts ? <span className="lc-badge lc-badge--stale">{n.counts.staleHosts} stale</span> : null}
                      {n.counts.inactiveHosts ? <span className="lc-badge lc-badge--inactive ml-1.5">{n.counts.inactiveHosts} inactifs</span> : null}
                      {!n.counts.staleHosts && !n.counts.inactiveHosts ? <span className="text-ink-muted">0</span> : null}
                    </td>
                    <td className="lc-num">{n.counts.apps}</td>
                    <td className="lc-num">{n.counts.webservers}</td>
                    <td className="lc-num">{n.counts.services}</td>
                    <td className="lc-num">{n.counts.links}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {list.length === 0 ? <p className="lc-muted m-0">Aucun namespace ne correspond.</p> : null}
          <Pager page={l.page} size={l.size} total={list.length} noun="namespaces" onPage={l.setPage} onSize={(v) => l.set({ size: String(v) })} />
        </>
      ) : null}
      {!data && !error ? <span className="lc-muted">Chargement…</span> : null}
    </Page>
  )
}

export function Home() {
  const { ns = '' } = useParams()
  const { data, error } = useQuery(HomeDoc, { variables: { ns } })
  const n = data?.namespace
  const rows = useMemo(() => (n ? projectRows(n.graph) : []), [n])
  if (error) return <Page title={ns}><p role="alert">Le namespace n'a pas pu être chargé : {error.message}</p></Page>
  if (data && !n) return <Page title="Namespace introuvable"><Link to="/">Voir les namespaces</Link></Page>
  if (!n) return <Page title={ns}><span className="lc-muted">Chargement…</span></Page>

  const stale = n.hosts.items.filter((h) => h.status === 'STALE').length
  const inactive = n.hosts.items.filter((h) => h.status === 'INACTIVE').length
  const tiles = [
    ['Hosts', n.counts.hosts, `${n.counts.hosts - stale - inactive} actifs${stale ? ` · ${stale} stale` : ''}${inactive ? ` · ${inactive} inactifs` : ''}`],
    ['Apps', n.counts.apps, `${rows.filter((r) => r.apps > 0).length} projets avec apps`],
    ['Services', n.counts.services, ''],
    ['Liens', n.counts.links, `${n.counts.externalLinks} vers l'extérieur`],
  ] as const
  return (
    <Page title={n.name} meta={<>Namespace <Copyable value={n.slug} />{n.description ? ` · ${n.description}` : ''}</>}>
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {tiles.map(([k, v, s]) => (
          <div key={k} className="flex flex-col gap-1 rounded-lg border border-border bg-surface-raised p-4">
            <span className="lc-label">{k}</span>
            <span className="text-[28px] font-[650] leading-[34px] tabular-nums">{v}</span>
            <span className="lc-muted text-[12.5px]">{s}</span>
          </div>
        ))}
      </div>
      <div className="flex flex-wrap items-start gap-6">
        <section className="flex min-w-0 flex-[2_1_560px] flex-col gap-3 rounded-lg border border-border bg-surface-raised p-4">
          <div className="flex items-center"><h2 className="title m-0 flex-1">Projets</h2><Link className="lc-btn lc-btn--primary no-underline" to={`/${ns}/graph`}>Ouvrir le graphe</Link></div>
          <ProjectsTable ns={ns} rows={rows} environments={n.environments} />
        </section>
        <section className="flex flex-[1_1_360px] flex-col gap-3.5 rounded-lg border border-border bg-surface-raised p-4">
          <div className="flex items-center"><h2 className="title m-0 flex-1">Derniers changements</h2><Link to={`/${ns}/history`} className="text-[13px]">Tout l'historique</Link></div>
          {n.history.items.length === 0 ? <span className="lc-muted">Aucun changement encore.</span> : null}
          {n.history.items.map((e) => <HistoryEvent key={e.id} event={e} />)}
        </section>
      </div>
    </Page>
  )
}
