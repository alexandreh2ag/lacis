import { useQuery } from '@apollo/client'
import { Link, useParams } from 'react-router'
import { CopyButton, Ips } from '@/components/Copy'
import { Page } from '@/components/Layout'
import { chipsField, FilterBar, pickerField } from '@/components/FilterBar'
import { envOptions, Pager, SearchInput, SortHeader, StatusBadge, statusOptions, useListParams } from '@/components/Listing'
import { graphql } from '@/graphql'
import type { HostSortField, HostStatus } from '@/graphql/graphql'
import { useT } from '@/i18n'
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


export function Hosts() {
  const { ns = '' } = useParams()
  const t = useT()
  const ts = t.pages.shared
  const columns: [HostSortField, string][] = [
    ['HOSTNAME', ts.host], ['PROJECT', ts.project], ['ENV', ts.env], ['IP', t.pages.host.ip], ['OS', t.pages.host.os],
  ]
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
    <Page title={t.common.nav.hosts}>
      <FilterBar
        search={<SearchInput value={l.q} onChange={(q) => l.set({ q })} placeholder={t.pages.hosts.search} />}
        fields={[
          chipsField({ label: ts.environment, short: ts.env, options: envOptions(data?.namespace?.environments), selected: envs, onChange: (v) => l.set({ env: v }) }),
          pickerField({ label: ts.project, choose: ts.chooseProject, options: (data?.namespace?.projects ?? []).map((p) => p.slug), selected: projects, onChange: (v) => l.set({ project: v }) }),
          chipsField({ label: ts.status, options: statusOptions, selected: statuses, onChange: (v) => l.set({ status: v }) }),
        ]}
        onClearAll={() => l.set({ env: null, project: null, status: null })}
        count={list ? ts.hosts(list.total) : ''}
      />
      {error ? <p role="alert">{t.pages.hosts.failed(error.message)}</p> : null}
      <div className="overflow-x-auto rounded-lg border border-border">
        <table className="lc-table">
          <thead>
            <tr>
              {columns.map(([by, label]) => <SortHeader key={by} label={label} by={by} sort={l.sort} desc={l.desc} onSort={onSort} />)}
              <th>{ts.webserversAndServices}</th>
              <SortHeader label={t.common.nav.apps} by="APPS" sort={l.sort} desc={l.desc} onSort={onSort} num />
              <SortHeader label={ts.status} by="STATUS" sort={l.sort} desc={l.desc} onSort={onSort} />
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
                <td><StatusBadge status={h.status} lastSeen={ts.lastSnapshot(ago(h.lastSeenAt))} /></td>
              </tr>
            ))}
            {!loading && list?.items.length === 0 ? <tr><td colSpan={8} className="text-ink-muted">{t.pages.hosts.none}</td></tr> : null}
          </tbody>
        </table>
      </div>
      {list ? <Pager page={l.page} size={l.size} total={list.total} noun={ts.nouns.hosts} onPage={l.setPage} onSize={(s) => l.set({ size: String(s) })} /> : null}
    </Page>
  )
}
