import { useQuery } from '@apollo/client'
import { Link, useParams } from 'react-router'
import { CopyButton, Ips } from '@/components/Copy'
import { Page } from '@/components/Layout'
import { chipsField, FilterBar, pickerField } from '@/components/FilterBar'
import { envOptions, Pager, SearchInput, SortHeader, StatusBadge, statusOptions, useListParams } from '@/components/Listing'
import { graphql } from '@/graphql'
import type { HostSortField, HostStatus } from '@/graphql/graphql'
import { ago } from '@/lib/format'

const HostsDoc = graphql(`
  query Hosts($ns: String!, $filter: HostFilter, $sort: HostSort, $page: PageInput) {
    namespace(slug: $ns) {
      id
      environments { id slug sortOrder }
      projects { id slug }
      hosts(filter: $filter, sort: $sort, page: $page) {
        total
        items {
          id hostname status lastSeenAt
          project { slug } environment { slug }
          os { pretty id }
          ips { address interface defaultRoute }
          services { id type version }
          webservers { id type version }
          apps { id }
        }
      }
    }
  }
`)

const columns: [HostSortField, string, boolean?][] = [
  ['HOSTNAME', 'Host'], ['PROJECT', 'Projet'], ['ENV', 'Env'], ['IP', 'IP'], ['OS', 'OS'],
]

export function Hosts() {
  const { ns = '' } = useParams()
  const l = useListParams({ sort: 'HOSTNAME' })
  const envs = l.list('env')
  const statuses = l.list('status') as HostStatus[]
  const projects = l.list('project')
  const { data, loading, error } = useQuery(HostsDoc, {
    variables: { ns, filter: { search: l.q, envs, projects, status: statuses }, sort: { by: l.sort as HostSortField, desc: l.desc }, page: { offset: (l.page - 1) * l.size, limit: l.size } },
  })
  const list = data?.namespace?.hosts
  const onSort = (by: string) => l.toggleSort(by, l.sort, l.desc)
  return (
    <Page title="Hosts">
      <FilterBar
        search={<SearchInput value={l.q} onChange={(q) => l.set({ q })} placeholder="Chercher un hostname, une IP, une version" />}
        fields={[
          chipsField({ label: 'Environnement', short: 'Env', options: envOptions(data?.namespace?.environments), selected: envs, onChange: (v) => l.set({ env: v }) }),
          pickerField({ label: 'Projet', choose: 'Choisir un projet', options: (data?.namespace?.projects ?? []).map((p) => p.slug), selected: projects, onChange: (v) => l.set({ project: v }) }),
          chipsField({ label: 'Statut', options: statusOptions, selected: statuses, onChange: (v) => l.set({ status: v }) }),
        ]}
        onClearAll={() => l.set({ env: null, project: null, status: null })}
        count={list ? `${list.total} hosts` : ''}
      />
      {error ? <p role="alert">Le listing n'a pas pu être chargé : {error.message}</p> : null}
      <div className="overflow-x-auto rounded-lg border border-border">
        <table className="lc-table">
          <thead>
            <tr>
              {columns.map(([by, label]) => <SortHeader key={by} label={label} by={by} sort={l.sort} desc={l.desc} onSort={onSort} />)}
              <th>Webserver et services</th>
              <SortHeader label="Apps" by="APPS" sort={l.sort} desc={l.desc} onSort={onSort} num />
              <SortHeader label="Statut" by="STATUS" sort={l.sort} desc={l.desc} onSort={onSort} />
            </tr>
          </thead>
          <tbody>
            {(list?.items ?? []).map((h) => (
              <tr key={h.id}>
                <td><span className="lc-copyable"><Link className="lc-mono" to={`/${ns}/hosts/${h.id}`}>{h.hostname}</Link><CopyButton value={h.hostname} /></span></td>
                <td>{h.project ? <Link to={`/${ns}/projects/${h.project.slug}`}>{h.project.slug}</Link> : <span className="lc-muted">—</span>}</td>
                <td>{h.environment?.slug ?? '—'}</td>
                <td><Ips ips={[...h.ips].sort((a, b) => Number(b.defaultRoute) - Number(a.defaultRoute))} /></td>
                <td className="text-ink-muted">{h.os?.pretty?.replace('Debian GNU/Linux', 'Debian').replace(/ \(.*\)/, '') ?? h.os?.id ?? ''}</td>
                <td className="lc-mono text-[12px] text-ink-muted">
                  {[...h.webservers.map((w) => `${w.type} ${w.version ?? ''}`), ...h.services.map((s) => `${s.type} ${s.version ?? ''}`)].map((s) => s.trim()).join(', ')}
                </td>
                <td className="lc-num">{h.apps.length}</td>
                <td><StatusBadge status={h.status} lastSeen={`Dernier snapshot ${ago(h.lastSeenAt)}`} /></td>
              </tr>
            ))}
            {!loading && list?.items.length === 0 ? <tr><td colSpan={8} className="text-ink-muted">Aucun host ne correspond.</td></tr> : null}
          </tbody>
        </table>
      </div>
      {list ? <Pager page={l.page} size={l.size} total={list.total} noun="hosts" onPage={l.setPage} onSize={(s) => l.set({ size: String(s) })} /> : null}
    </Page>
  )
}
