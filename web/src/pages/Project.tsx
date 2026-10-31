import { useQuery } from '@apollo/client'
import { useMemo, useState } from 'react'
import { Link, useParams } from 'react-router'
import { Copyable, CopyButton, Ips } from '@/components/Copy'
import { TechLogo } from '@/components/Icon'
import { Page } from '@/components/Layout'
import { chipsField, FilterBar } from '@/components/FilterBar'
import { Pager, SearchInput, SortHeader, StatusBadge, useTable } from '@/components/Listing'
import { Tabs, useTab } from '@/components/Tabs'
import { UsedBy, type UsedByRow } from '@/components/UsedBy'
import { graphql } from '@/graphql'
import type { ProjectPageQuery } from '@/graphql/graphql'
import { confidenceFr } from '@/lib/format'

const ProjectPageDoc = graphql(`
  query ProjectPage($ns: String!, $project: String!) {
    namespace(slug: $ns) {
      id slug
      projects { id slug name description }
      environments { id slug sortOrder }
      apps(filter: { projects: [$project] }, page: { limit: 500 }) {
        items {
          id name framework vhosts
          environment { slug }
          host { id hostname }
          code { version commit branch }
          links {
            id key confidence
            to {
              __typename
              ... on Service { id type instance version host { id hostname } }
              ... on Host { id hostname }
              ... on Webserver { id type host { id hostname } }
              ... on App { id name host { id hostname } }
              ... on External { target reason }
            }
          }
        }
      }
      hosts(filter: { projects: [$project] }, page: { limit: 500 }) {
        items {
          id hostname status environment { slug } ips { address defaultRoute } os { pretty } apps { id }
          services { id type instance version dependents { ...UsedByLink } }
          webservers { id type version dependents { ...UsedByLink } }
        }
      }
    }
  }
`)

type NS = NonNullable<ProjectPageQuery['namespace']>
type App = NS['apps']['items'][number]

interface Dep {
  key: string
  target: string
  hostId?: string
  kind: 'service' | 'external' | 'proxy' | 'host' | 'app'
  tech?: string
  detail: string
  env: string
  by: string[]
  keys: string[]
  confidence: string
}

function dependencies(apps: App[]): Dep[] {
  const out = new Map<string, Dep>()
  for (const a of apps) {
    for (const l of a.links) {
      const t = l.to
      let d: Omit<Dep, 'by' | 'keys' | 'env' | 'confidence'>
      switch (t.__typename) {
        case 'Service': d = { key: t.id, target: t.host.hostname, hostId: t.host.id, kind: 'service', tech: t.type, detail: [t.type, t.version].filter(Boolean).join(' ') }; break
        case 'Host': d = { key: t.id, target: t.hostname, hostId: t.id, kind: 'host', detail: 'host' }; break
        case 'Webserver': d = { key: t.id, target: t.host.hostname, hostId: t.host.id, kind: 'proxy', tech: t.type, detail: t.type }; break
        case 'App': d = { key: t.id, target: `${t.name} · ${t.host.hostname}`, hostId: t.host.id, kind: 'app', detail: 'app' }; break
        default: d = { key: 'ext:' + t.target, target: t.target, kind: 'external', detail: t.reason ?? 'externe' }
      }
      const env = a.environment?.slug ?? ''
      const id = d.key + '@' + env
      const cur = out.get(id) ?? { ...d, env, by: [], keys: [], confidence: l.confidence }
      if (!cur.by.includes(a.name)) cur.by.push(a.name)
      if (l.key && !cur.keys.includes(l.key)) cur.keys.push(l.key)
      if (l.confidence !== 'EXACT') cur.confidence = l.confidence
      out.set(id, cur)
    }
  }
  return [...out.values()]
}

// consumerProject is the project of the app or host using a component.
function consumerProject(row: UsedByRow): string | undefined {
  const from = row.from
  if (from.__typename === 'External') return undefined
  return (from.__typename === 'Host' ? from : from.host).project?.slug
}

export function ProjectPage() {
  const { ns = '', slug = '' } = useParams()
  const { data, error } = useQuery(ProjectPageDoc, { variables: { ns, project: slug } })
  const n = data?.namespace
  const [env, setEnv] = useState<string[]>([])
  const project = n?.projects.find((p) => p.slug === slug)
  const inEnv = (e?: string | null) => !env.length || env.includes(e ?? '')
  const apps = useMemo(() => (n?.apps.items ?? []).filter((a) => inEnv(a.environment?.slug)), [n, env])
  const hosts = useMemo(() => (n?.hosts.items ?? []).filter((h) => inEnv(h.environment?.slug)), [n, env])
  const deps = useMemo(() => dependencies(apps), [apps])
  const usedBy: UsedByRow[] = useMemo(() => hosts.flatMap((h) => [
    ...h.services.flatMap((s) => s.dependents.map((l) => ({ ...l, target: { name: s.instance && s.instance !== 'default' ? `${s.type} ${s.instance}` : s.type, tech: s.type, host: h } }))),
    ...h.webservers.flatMap((w) => w.dependents.map((l) => ({ ...l, target: { name: w.type, tech: w.type, host: h } }))),
  ]), [hosts])
  // An app deployed in several versions in one env diverges.
  const diverging = useMemo(() => {
    const versions = new Map<string, Set<string>>()
    for (const a of apps) {
      const k = a.name + '@' + (a.environment?.slug ?? '')
      versions.set(k, (versions.get(k) ?? new Set()).add(`${a.code?.version ?? ''}/${a.code?.commit ?? ''}`))
    }
    return new Set([...versions].filter(([, v]) => v.size > 1).map(([k]) => k))
  }, [apps])

  const appTable = useTable(apps, (a) => [a.name, a.host.hostname, a.code?.version ?? '', ...a.vhosts],
    (a, by) => by === 'env' ? a.environment?.slug ?? '' : by === 'host' ? a.host.hostname : by === 'version' ? a.code?.version ?? '' : a.name, 'name')
  const hostTable = useTable(hosts, (h) => [h.hostname, ...h.ips.map((i) => i.address)],
    (h, by) => by === 'env' ? h.environment?.slug ?? '' : by === 'apps' ? h.apps.length : by === 'status' ? h.status : h.hostname, 'hostname')
  const [kinds, setKinds] = useState<string[]>([])
  const depRows = useMemo(() => deps.filter((d) => !kinds.length || kinds.includes(d.kind)), [deps, kinds])
  const depTable = useTable(depRows, (d) => [d.target, d.detail, ...d.keys, ...d.by],
    (d, by) => by === 'env' ? d.env : by === 'by' ? d.by.join() : by === 'confidence' ? d.confidence : d.target, 'target')

  const domains = useMemo(() => [...new Set(apps.flatMap((a) => a.vhosts.map((v) => `${v}\u0000${a.name}\u0000${a.environment?.slug ?? ''}`)))]
    .sort().map((x) => x.split('\u0000')), [apps])
  const tabs = [
    { id: 'general', label: 'Général' },
    { id: 'apps', label: 'Apps', count: apps.length },
    { id: 'hosts', label: 'Hosts', count: hosts.length },
    { id: 'dependencies', label: 'Dépendances', count: deps.length },
    { id: 'used-by', label: 'Utilisé par', count: usedBy.length },
    { id: 'domains', label: 'Domaines servis', count: domains.length },
  ]
  const [tab, setTab] = useTab(tabs, 'general')

  if (error) return <Page title={slug}><p role="alert">Le projet n'a pas pu être chargé : {error.message}</p></Page>
  if (!n) return <Page title={slug}><span className="lc-muted">Chargement…</span></Page>
  if (!project) return <Page title="Projet introuvable"><Link to={`/${ns}`}>Retour au namespace</Link></Page>

  const envs = [...n.environments].sort((a, b) => a.sortOrder - b.sortOrder).filter((e) => n.hosts.items.some((h) => h.environment?.slug === e.slug))
  // The env filters every tab: each tab's bar carries it.
  const envField = chipsField({ label: 'Environnement', short: 'Env', options: envs.map((e) => ({ value: e.slug, label: e.slug })), selected: env, onChange: setEnv })
  const count = (k: string) => deps.filter((d) => d.kind === k).length
  return (
    <Page
      title={<span className="lc-copyable gap-1.5"><span>{project.name}</span><CopyButton value={project.slug} /></span>}
      meta={<>
        <span>projet <span className="lc-mono">{project.slug}</span>{project.description ? ` · ${project.description}` : ''}</span>
        <span className="flex-1" />
        <Link to={`/${ns}/history`} className="text-[13px]">Historique du namespace</Link>
        <Link className="lc-btn no-underline" to={`/${ns}/graph?${new URLSearchParams([['lens', 'projects'], ['project', slug], ...envs.slice(0, 1).map((e) => ['open', `project:${slug}@${e.slug}`])])}`}>Voir dans le graphe</Link>
      </>}
    >
      <Tabs label="Contenu du projet" tabs={tabs} current={tab} onChange={setTab}>
        {tab === 'general' ? <FilterBar fields={[envField]} /> : null}
        {tab === 'general' ? <General ns={ns} project={project} envs={envs} apps={apps} hosts={hosts} deps={deps} usedBy={usedBy} diverging={diverging} /> : null}
        {tab === 'apps' ? (
          <>
            <FilterBar search={<SearchInput value={appTable.q} onChange={appTable.setQ} placeholder="Chercher une app, un host, une version" width={300} />}
              fields={[envField]} count={`${apps.length} déploiements`} />
            <div className="overflow-x-auto rounded-lg border border-border"><table className="lc-table">
              <thead><tr>
                <SortHeader label="App" by="name" sort={appTable.sort.by} desc={appTable.sort.desc} onSort={appTable.onSort} />
                <SortHeader label="Env" by="env" sort={appTable.sort.by} desc={appTable.sort.desc} onSort={appTable.onSort} />
                <SortHeader label="Host" by="host" sort={appTable.sort.by} desc={appTable.sort.desc} onSort={appTable.onSort} />
                <SortHeader label="Version" by="version" sort={appTable.sort.by} desc={appTable.sort.desc} onSort={appTable.onSort} />
                <th>Commit</th><th>Domaine</th>
              </tr></thead>
              <tbody>{appTable.shown.map((a) => {
                const diverges = diverging.has(a.name + '@' + (a.environment?.slug ?? ''))
                return (
                  <tr key={a.id}>
                    <td><span className="inline-flex items-center gap-2 font-semibold"><span className="lc-node__techs"><TechLogo tech={a.framework ?? 'app'} /></span><Link to={`/${ns}/apps/${a.id}`}>{a.name}</Link></span></td>
                    <td>{a.environment?.slug}</td>
                    <td><span className="lc-copyable"><Link className="lc-mono" to={`/${ns}/hosts/${a.host.id}`}>{a.host.hostname}</Link><CopyButton value={a.host.hostname} /></span></td>
                    <td><span className={'lc-mono' + (diverges ? ' text-status-stale' : '')}>{a.code?.version}</span>{diverges ? <span className="lc-badge lc-badge--stale ml-1.5 h-[18px] text-[11px]">diffère</span> : null}</td>
                    <td>{a.code?.commit ? <Copyable value={a.code.commit} shown={a.code.commit.slice(0, 7)} /> : null}</td>
                    <td>{a.vhosts[0] ? <Copyable value={a.vhosts[0]} /> : null}</td>
                  </tr>
                )
              })}</tbody>
            </table></div>
            <Pager page={appTable.page} size={appTable.size} total={appTable.list.length} noun="déploiements" onPage={appTable.setPage} onSize={appTable.setSize} />
          </>
        ) : null}
        {tab === 'hosts' ? (
          <>
            <FilterBar search={<SearchInput value={hostTable.q} onChange={hostTable.setQ} placeholder="Chercher un host, une IP" width={300} />}
              fields={[envField]} count={`${hosts.length} hosts`} />
            <div className="overflow-x-auto rounded-lg border border-border"><table className="lc-table">
              <thead><tr>
                <SortHeader label="Host" by="hostname" sort={hostTable.sort.by} desc={hostTable.sort.desc} onSort={hostTable.onSort} />
                <SortHeader label="Env" by="env" sort={hostTable.sort.by} desc={hostTable.sort.desc} onSort={hostTable.onSort} />
                <th>IP</th><th>OS</th><th>Webserver et services</th>
                <SortHeader label="Apps" by="apps" sort={hostTable.sort.by} desc={hostTable.sort.desc} onSort={hostTable.onSort} num />
                <SortHeader label="Statut" by="status" sort={hostTable.sort.by} desc={hostTable.sort.desc} onSort={hostTable.onSort} />
              </tr></thead>
              <tbody>{hostTable.shown.map((h) => (
                <tr key={h.id}>
                  <td><span className="lc-copyable"><Link className="lc-mono" to={`/${ns}/hosts/${h.id}`}>{h.hostname}</Link><CopyButton value={h.hostname} /></span></td>
                  <td>{h.environment?.slug}</td>
                  <td><Ips ips={[...h.ips].sort((a, b) => Number(b.defaultRoute) - Number(a.defaultRoute))} /></td>
                  <td className="text-ink-muted">{h.os?.pretty?.replace(/ \(.*\)/, '')}</td>
                  <td className="lc-mono text-[12px] text-ink-muted">{[...h.webservers, ...h.services].map((s) => `${s.type} ${s.version ?? ''}`.trim()).join(', ')}</td>
                  <td className="lc-num">{h.apps.length}</td>
                  <td><StatusBadge status={h.status} /></td>
                </tr>
              ))}</tbody>
            </table></div>
            <Pager page={hostTable.page} size={hostTable.size} total={hostTable.list.length} noun="hosts" onPage={hostTable.setPage} onSize={hostTable.setSize} />
          </>
        ) : null}
        {tab === 'dependencies' ? (
          <>
            <FilterBar search={<SearchInput value={depTable.q} onChange={depTable.setQ} placeholder="Chercher une cible, une clé" width={300} />}
              fields={[envField, chipsField({ label: 'Type de cible', short: 'Type', options: [{ value: 'service', label: 'services', count: count('service') }, { value: 'external', label: 'externes', count: count('external') }, { value: 'proxy', label: 'proxies', count: count('proxy') }, { value: 'host', label: 'hosts', count: count('host') }],
                selected: kinds, onChange: setKinds })]}
              onClearAll={() => { setEnv([]); setKinds([]) }} count={`${depRows.length} cibles sur ${deps.length}`} />
            <div className="overflow-x-auto rounded-lg border border-border"><table className="lc-table">
              <thead><tr>
                <SortHeader label="Cible" by="target" sort={depTable.sort.by} desc={depTable.sort.desc} onSort={depTable.onSort} />
                <th>Détail</th>
                <SortHeader label="Env" by="env" sort={depTable.sort.by} desc={depTable.sort.desc} onSort={depTable.onSort} />
                <SortHeader label="Utilisé par" by="by" sort={depTable.sort.by} desc={depTable.sort.desc} onSort={depTable.onSort} />
                <th>Clés</th>
                <SortHeader label="Confiance" by="confidence" sort={depTable.sort.by} desc={depTable.sort.desc} onSort={depTable.onSort} />
              </tr></thead>
              <tbody>{depTable.shown.map((d) => (
                <tr key={d.key + d.env}>
                  <td><span className="inline-flex items-center gap-2">
                    <span className="lc-node__techs"><TechLogo tech={d.tech ?? (d.kind === 'external' ? 'ext' : d.kind)} /></span>
                    {d.hostId ? <span className="lc-copyable"><Link className="lc-mono" to={`/${ns}/hosts/${d.hostId}`}>{d.target}</Link><CopyButton value={d.target} /></span> : <Copyable value={d.target} />}
                  </span></td>
                  <td className="text-ink-muted">{d.detail}</td>
                  <td>{d.env}</td>
                  <td>{d.by.join(', ')}</td>
                  <td><div className="flex flex-wrap gap-x-3">{d.keys.map((k) => <Copyable key={k} value={k} />)}</div></td>
                  <td>{confidenceFr[d.confidence]}</td>
                </tr>
              ))}</tbody>
            </table></div>
            <Pager page={depTable.page} size={depTable.size} total={depTable.list.length} noun="cibles" onPage={depTable.setPage} onSize={depTable.setSize} />
          </>
        ) : null}
        {tab === 'used-by' ? <UsedBy ns={ns} rows={usedBy} showTargetHost fields={[envField]} /> : null}
        {tab === 'domains' ? <FilterBar fields={[envField]} count={`${domains.length} domaines`} /> : null}
        {tab === 'domains' ? (
          domains.length === 0 ? <p className="lc-muted m-0">Aucun domaine servi.</p> : (
            <div className="overflow-x-auto rounded-lg border border-border"><table className="lc-table">
              <thead><tr><th>Domaine</th><th>App</th><th>Env</th></tr></thead>
              <tbody>{domains.map(([d, app, env]) => (
                <tr key={d + app + env}><td><Copyable value={d} /></td><td className="font-semibold">{app}</td><td>{env}</td></tr>
              ))}</tbody>
            </table></div>
          )
        ) : null}
      </Tabs>
    </Page>
  )
}

// General tells what the project is and sums what the tabs list.
function General({ ns, project, envs, apps, hosts, deps, usedBy, diverging }: {
  ns: string
  project: NS['projects'][number]
  envs: { slug: string }[]
  apps: App[]
  hosts: NS['hosts']['items']
  deps: Dep[]
  usedBy: UsedByRow[]
  diverging: Set<string>
}) {
  const consumers = [...new Set(usedBy.map(consumerProject).filter((p): p is string => !!p && p !== project.slug))].sort()
  const kinds = (k: Dep['kind']) => deps.filter((d) => d.kind === k).length
  return (
    <div className="grid items-start gap-6 lg:grid-cols-2">
      <section className="lc-panel lc-panel--card">
        <div className="lc-panel__section">
          <span className="lc-label">Projet</span>
          <dl className="lc-dl" style={{ gridTemplateColumns: 'max-content minmax(0, 1fr)' }}>
            <dt>Nom</dt><dd className="font-sans">{project.name}</dd>
            <dt>Slug</dt><dd><Copyable value={project.slug} /></dd>
            {project.description ? <><dt>Description</dt><dd className="font-sans">{project.description}</dd></> : null}
            <dt>Envs</dt><dd>{envs.map((e) => e.slug).join(', ') || '—'}</dd>
          </dl>
        </div>
      </section>
      <section className="lc-panel lc-panel--card">
        <div className="lc-panel__section">
          <span className="lc-label">Résumé</span>
          <dl className="lc-dl" style={{ gridTemplateColumns: 'max-content minmax(0, 1fr)' }}>
            <dt>Hosts</dt>
            <dd>{hosts.length}{envs.length ? ` · ${envs.map((e) => `${hosts.filter((h) => h.environment?.slug === e.slug).length} en ${e.slug}`).join(' · ')}` : ''}</dd>
            <dt>Apps</dt><dd>{new Set(apps.map((a) => a.name)).size} · {apps.length} déploiements</dd>
            {diverging.size ? (
              <><dt>Versions divergentes</dt><dd className="text-status-stale">{[...diverging].map((k) => k.replace('@', ' en ')).join(', ')}</dd></>
            ) : null}
            <dt>Dépendances</dt><dd>{deps.length} · {kinds('service')} services · {kinds('external')} externes · {kinds('proxy')} proxies</dd>
            <dt>Utilisé par</dt>
            <dd>
              {usedBy.length} liens
              {consumers.length ? <> · {consumers.map((p, i) => <span key={p}>{i ? ', ' : ''}<Link to={`/${ns}/projects/${p}`}>{p}</Link></span>)}</> : null}
            </dd>
          </dl>
        </div>
      </section>
    </div>
  )
}
