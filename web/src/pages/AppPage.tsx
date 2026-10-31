import { useQuery } from '@apollo/client'
import { useMemo } from 'react'
import { Link, useParams } from 'react-router'
import { Copyable, CopyButton } from '@/components/Copy'
import { TechLogo } from '@/components/Icon'
import { Page } from '@/components/Layout'
import { Pager, SearchInput, SortHeader, StatusBadge, useTable } from '@/components/Listing'
import { Tabs, useTab } from '@/components/Tabs'
import { hostOf, nameOf, techOf, UsedBy, type End, type UsedByRow } from '@/components/UsedBy'
import { graphql } from '@/graphql'
import type { AppPageQuery } from '@/graphql/graphql'
import { ago, confidenceFr, formatDateTime } from '@/lib/format'

const AppPageDoc = graphql(`
  query AppPage($ns: String!, $id: ID!) {
    namespace(slug: $ns) {
      id
      app(id: $id) {
        id name path resolvedPath role framework frameworkVersion vhosts firstSeenAt
        runtime { type version path source }
        code { source version commit branch tag repository pipelineId pipelineUrl deployedAt deployedBy }
        project { slug name }
        environment { slug }
        host { id hostname status lastSeenAt }
        dependencies { key scheme port path target unparsed }
        links {
          id key confidence rule reason
          to {
            __typename
            ... on App { id name framework host { ...UsedByHost } }
            ... on Service { id type instance host { ...UsedByHost } }
            ... on Webserver { id type host { ...UsedByHost } }
            ... on Host { ...UsedByHost }
            ... on External { target reason }
          }
        }
        dependents { ...UsedByLink }
      }
    }
  }
`)

const SiblingsDoc = graphql(`
  query AppSiblings($ns: String!, $project: String!, $env: String!, $name: String!) {
    namespace(slug: $ns) {
      id
      apps(filter: { projects: [$project], envs: [$env], search: $name }, page: { limit: 100 }) {
        items { id name host { id hostname status } code { version commit branch deployedAt } }
      }
    }
  }
`)

// The impact of the app, both ways, at the level of the apps: what it
// reaches through proxies, and who reaches it through them.
const ChainDoc = graphql(`
  query AppChain($ns: String!, $node: String!) {
    namespace(slug: $ns) {
      id
      graph(level: APP, filter: { status: [ACTIVE, STALE, INACTIVE] }) {
        nodes { id kind label hostname project env entity { __typename ... on App { id } ... on Host { id } } hosts { id } }
      }
      needs: impact(level: APP, filter: { status: [ACTIVE, STALE, INACTIVE] }, node: $node, direction: NEEDS) { nodes { id level } }
      dependents: impact(level: APP, filter: { status: [ACTIVE, STALE, INACTIVE] }, node: $node, direction: DEPENDENTS) { nodes { id level } }
    }
  }
`)

type App = NonNullable<NonNullable<AppPageQuery['namespace']>['app']>
type ChainNode = { id: string; kind: string; label: string; hostname?: string | null; project?: string | null; env?: string | null
  entity?: { __typename: string; id?: string } | null; hosts: { id: string }[]; level: number }

const kindFr: Record<string, string> = { APP: 'app', SERVICE: 'service', WEBSERVER: 'proxy', HOST: 'host', EXTERNAL: 'externe' }

// EndCell is the other end of a link: what it is, on which host, of which
// project.
function EndCell({ ns, end }: { ns: string; end: End }) {
  const host = hostOf(end)
  return (
    <span className="inline-flex items-center gap-2">
      <span className="lc-node__techs"><TechLogo tech={techOf(end)} /></span>
      {end.__typename === 'External' ? <Copyable value={end.target} /> : (
        <span className="flex flex-col leading-[18px]">
          {end.__typename === 'App' ? <Link className="font-semibold" to={`/${ns}/apps/${end.id}`}>{nameOf(end)}</Link>
            : end.__typename !== 'Host' ? <span className="font-semibold">{nameOf(end)}</span> : null}
          {host ? <span className="lc-copyable"><Link className="lc-mono text-[12.5px]" to={`/${ns}/hosts/${host.id}`}>{host.hostname}</Link><CopyButton value={host.hostname} /></span> : null}
        </span>
      )}
    </span>
  )
}

export function AppPage() {
  const { ns = '', id = '' } = useParams()
  const { data, loading, error } = useQuery(AppPageDoc, { variables: { ns, id } })
  const a = data?.namespace?.app
  const tabs = [
    { id: 'general', label: 'Général' },
    { id: 'uses', label: 'Utilise', count: a?.links.length },
    { id: 'used-by', label: 'Utilisé par', count: a?.dependents.length },
    { id: 'config', label: 'Configuration lue', count: a?.dependencies.length },
  ]
  const [tab, setTab] = useTab(tabs, 'general')
  if (loading && !a) return <Page title="…"><span className="lc-muted">Chargement…</span></Page>
  if (error) return <Page title="App"><p role="alert">La fiche n'a pas pu être chargée : {error.message}</p></Page>
  if (!a) return <Page title="App introuvable"><Link to={`/${ns}/apps`}>Retour aux apps</Link></Page>
  const graph = a.project && a.environment
    ? `/${ns}/graph?${new URLSearchParams({ lens: 'projects', env: a.environment.slug, open: `project:${a.project.slug}@${a.environment.slug}` })}`
    : `/${ns}/graph?${new URLSearchParams({ lens: 'infra', host: a.host.hostname, open: `host:${a.host.id}` })}`
  return (
    <Page
      title={<span className="lc-copyable gap-1.5"><span>{a.name}</span><CopyButton value={a.name} /></span>}
      meta={<>
        <StatusBadge status={a.host.status} />
        <span>
          app{a.project ? <> du projet <Link to={`/${ns}/projects/${a.project.slug}`}>{a.project.slug}</Link></> : ''}
          {a.environment ? ` · ${a.environment.slug}` : ''} · sur <Link className="lc-mono" to={`/${ns}/hosts/${a.host.id}`}>{a.host.hostname}</Link>
        </span>
        {a.code?.version ? <span className="lc-badge lc-badge--plain lc-mono">{a.code.version}</span> : null}
        <span className="flex-1" />
        <Link className="lc-btn no-underline" to={graph}>Voir dans le graphe</Link>
      </>}
    >
      <Tabs label="Contenu de l'app" tabs={tabs} current={tab} onChange={setTab}>
        {tab === 'general' ? <General ns={ns} app={a} /> : null}
        {tab === 'uses' ? <Uses ns={ns} app={a} /> : null}
        {tab === 'used-by' ? <UsedByTab ns={ns} app={a} /> : null}
        {tab === 'config' ? <Config app={a} /> : null}
      </Tabs>
    </Page>
  )
}

// General tells what is deployed, and where else the same app runs.
function General({ ns, app: a }: { ns: string; app: App }) {
  const c = a.code
  return (
    <div className="flex flex-col gap-6">
      <div className="grid items-start gap-6 lg:grid-cols-2">
        <section className="lc-panel lc-panel--card">
          <div className="lc-panel__section">
            <span className="lc-label">Code déployé</span>
            {c ? (
              <dl className="lc-dl" style={{ gridTemplateColumns: 'max-content minmax(0, 1fr)' }}>
                {c.version ? <><dt>Version</dt><dd><Copyable value={c.version} /></dd></> : null}
                {c.commit ? <><dt>Commit</dt><dd><Copyable value={c.commit} shown={c.commit.slice(0, 7)} /></dd></> : null}
                {c.branch ? <><dt>Branche</dt><dd><Copyable value={c.branch} /></dd></> : null}
                {c.tag ? <><dt>Tag</dt><dd><Copyable value={c.tag} /></dd></> : null}
                {c.repository ? <><dt>Dépôt</dt><dd><Copyable value={c.repository} /></dd></> : null}
                {c.deployedAt ? <><dt>Déployé</dt><dd>{formatDateTime(c.deployedAt)} · {ago(c.deployedAt)}{c.deployedBy ? ` · ${c.deployedBy}` : ''}</dd></> : null}
                {c.pipelineUrl ? <><dt>Pipeline</dt><dd><a href={c.pipelineUrl} target="_blank" rel="noreferrer">{c.pipelineId ?? c.pipelineUrl}</a></dd></> : null}
                <dt>Source</dt><dd>{c.source}</dd>
              </dl>
            ) : <span className="lc-muted">Aucune information de code collectée.</span>}
          </div>
        </section>
        <section className="lc-panel lc-panel--card">
          <div className="lc-panel__section">
            <span className="lc-label">App</span>
            <dl className="lc-dl" style={{ gridTemplateColumns: 'max-content minmax(0, 1fr)' }}>
              <dt>Rôle</dt><dd>{a.role}</dd>
              {a.framework ? <><dt>Framework</dt><dd>{a.framework} {a.frameworkVersion}</dd></> : null}
              {a.runtime ? <><dt>Runtime</dt><dd>{a.runtime.type} {a.runtime.version}{a.runtime.source ? ` · ${a.runtime.source}` : ''}</dd></> : null}
              <dt>Chemin</dt><dd><Copyable value={a.path} /></dd>
              {a.resolvedPath && a.resolvedPath !== a.path ? <><dt>Résolu</dt><dd><Copyable value={a.resolvedPath} /></dd></> : null}
              {a.vhosts.map((v) => <div key={v} className="contents"><dt>Domaine</dt><dd><Copyable value={v} /></dd></div>)}
              <dt>Vu depuis</dt><dd>{ago(a.firstSeenAt)}</dd>
            </dl>
          </div>
        </section>
      </div>
      {a.project && a.environment ? <Siblings ns={ns} app={a} /> : null}
    </div>
  )
}

// Siblings are the deployments of the app in its project and env.
function Siblings({ ns, app: a }: { ns: string; app: App }) {
  const { data } = useQuery(SiblingsDoc, { variables: { ns, project: a.project!.slug, env: a.environment!.slug, name: a.name } })
  const all = (data?.namespace?.apps.items ?? []).filter((x) => x.name === a.name)
  if (all.length < 2) return null
  const versions = new Set(all.map((x) => `${x.code?.version ?? ''}/${x.code?.commit ?? ''}`))
  return (
    <section className="flex flex-col gap-3">
      <div className="flex items-center gap-3">
        <h2 className="title m-0">Déploiements en {a.environment!.slug}</h2>
        {versions.size > 1 ? <span className="lc-badge lc-badge--stale">versions différentes</span> : null}
      </div>
      <div className="overflow-x-auto rounded-lg border border-border"><table className="lc-table">
        <thead><tr><th>Host</th><th>Version</th><th>Commit</th><th>Déployé</th><th>Statut</th></tr></thead>
        <tbody>{all.map((x) => (
          <tr key={x.id} style={x.id === a.id ? { background: 'var(--brand-tint)' } : undefined}>
            <td>{x.id === a.id ? <span className="lc-mono">{x.host.hostname}</span> : <Link className="lc-mono" to={`/${ns}/apps/${x.id}`}>{x.host.hostname}</Link>}</td>
            <td className="lc-mono">{x.code?.version ?? x.code?.branch}</td>
            <td>{x.code?.commit ? <Copyable value={x.code.commit} shown={x.code.commit.slice(0, 7)} /> : null}</td>
            <td className="text-ink-muted">{ago(x.code?.deployedAt)}</td>
            <td><StatusBadge status={x.host.status} /></td>
          </tr>
        ))}</tbody>
      </table></div>
    </section>
  )
}

// useChain is the impact of the app one way, the app itself left out.
function useChain(ns: string, a: App, way: 'needs' | 'dependents'): ChainNode[] | null {
  const { data } = useQuery(ChainDoc, { variables: { ns, node: `app:${a.id}` } })
  return useMemo(() => {
    const n = data?.namespace
    if (!n) return null
    const byId = new Map(n.graph.nodes.map((x) => [x.id, x]))
    return n[way].nodes.flatMap((x) => {
      const node = byId.get(x.id)
      return node ? [{ ...node, level: x.level } as ChainNode] : []
    })
  }, [data, way])
}

// Chain lists what the app reaches, or what reaches it, through proxies
// and other apps, level by level.
function Chain({ ns, nodes, empty }: { ns: string; nodes: ChainNode[] | null; empty: string }) {
  if (!nodes) return <span className="lc-muted">Chargement…</span>
  if (!nodes.length) return <p className="lc-muted m-0">{empty}</p>
  // A webserver drawn inside the app takes a level: the levels shown
  // follow one another from 1.
  const steps = [...new Set(nodes.map((n) => n.level))].sort((x, y) => x - y)
  return (
    <div className="overflow-x-auto rounded-lg border border-border"><table className="lc-table">
      <thead><tr><th className="lc-num">Niveau</th><th>Nœud</th><th>Type</th><th>Host</th><th>Projet</th><th>Env</th></tr></thead>
      <tbody>{nodes.map((n) => {
        const appId = n.entity?.__typename === 'App' ? n.entity.id : null
        const hostId = n.hosts.length === 1 ? n.hosts[0].id : null
        return (
          <tr key={n.id}>
            <td className="lc-num">{steps.indexOf(n.level) + 1}</td>
            <td>{appId ? <Link className="font-semibold" to={`/${ns}/apps/${appId}`}>{n.label}</Link> : <span className="font-semibold">{n.label}</span>}</td>
            <td className="text-ink-muted">{kindFr[n.kind] ?? n.kind.toLowerCase()}</td>
            <td>{n.hostname && hostId ? <Link className="lc-mono" to={`/${ns}/hosts/${hostId}`}>{n.hostname}</Link> : null}</td>
            <td>{n.project ? <Link to={`/${ns}/projects/${n.project}`}>{n.project}</Link> : null}</td>
            <td>{n.env}</td>
          </tr>
        )
      })}</tbody>
    </table></div>
  )
}

// Uses lists the links of the app, then all it needs through them.
function Uses({ ns, app: a }: { ns: string; app: App }) {
  const t = useTable(a.links,
    (l) => [nameOf(l.to), hostOf(l.to)?.hostname ?? '', l.key ?? '', l.rule],
    (l, by) => by === 'key' ? l.key ?? '' : by === 'confidence' ? l.confidence : nameOf(l.to), 'target')
  const chain = useChain(ns, a, 'needs')
  const head = (label: string, by: string) => <SortHeader label={label} by={by} sort={t.sort.by} desc={t.sort.desc} onSort={t.onSort} />
  return (
    <div className="flex flex-col gap-6">
      <section className="flex flex-col gap-3">
        <h2 className="title m-0">Liens directs</h2>
        {a.links.length === 0 ? <p className="lc-muted m-0">Aucune dépendance résolue.</p> : (
          <>
            <SearchInput value={t.q} onChange={t.setQ} placeholder="Chercher une cible, une clé" width={300} />
            <div className="overflow-x-auto rounded-lg border border-border"><table className="lc-table">
              <thead><tr>{head('Cible', 'target')}<th>Projet</th>{head('Clé', 'key')}<th>Résolu par</th>{head('Confiance', 'confidence')}</tr></thead>
              <tbody>{t.shown.map((l) => {
                const host = hostOf(l.to)
                return (
                  <tr key={l.id}>
                    <td><EndCell ns={ns} end={l.to} /></td>
                    <td>{host?.project ? <Link to={`/${ns}/projects/${host.project.slug}`}>{host.project.slug}</Link> : <span className="lc-muted">—</span>}</td>
                    <td style={{ whiteSpace: 'normal', maxWidth: 280, wordBreak: 'break-all' }}>{l.key ? <Copyable value={l.key} /> : null}</td>
                    <td className="text-ink-muted">{l.rule}{l.reason ? ` · ${l.reason}` : ''}</td>
                    <td>{confidenceFr[l.confidence]}</td>
                  </tr>
                )
              })}</tbody>
            </table></div>
            <Pager page={t.page} size={t.size} total={t.list.length} noun="liens" onPage={t.setPage} onSize={t.setSize} />
          </>
        )}
      </section>
      <section className="flex flex-col gap-3">
        <h2 className="title m-0">Dépendances indirectes</h2>
        <p className="lc-muted m-0 text-[13px]">
          Ce dont l'app a besoin sans l'appeler elle-même : niveau 1, ce qu'elle appelle ; niveau 2, ce dont ceux-là ont besoin, et ainsi de suite.
          Un proxy n'est suivi que pour les domaines qu'on lui demande.
        </p>
        <Chain ns={ns} nodes={chain} empty="L'app n'a besoin de rien d'autre." />
      </section>
    </div>
  )
}

// UsedByTab lists the links to the app, then all that reaches it through
// them.
function UsedByTab({ ns, app: a }: { ns: string; app: App }) {
  const rows: UsedByRow[] = a.dependents.map((l) => ({ ...l, target: { name: a.name, tech: a.framework ?? 'app' } }))
  const chain = useChain(ns, a, 'dependents')
  return (
    <div className="flex flex-col gap-6">
      <section className="flex flex-col gap-3">
        <h2 className="title m-0">Liens directs</h2>
        <UsedBy ns={ns} rows={rows} />
      </section>
      <section className="flex flex-col gap-3">
        <h2 className="title m-0">Utilisateurs indirects</h2>
        <p className="lc-muted m-0 text-[13px]">
          Ce qui cesse de marcher sans elle, même sans l'appeler directement : niveau 1, ce qui la sert ou l'appelle (un proxy) ;
          niveau 2, les apps qui passent par ce proxy pour ses domaines, et ainsi de suite.
        </p>
        <Chain ns={ns} nodes={chain} empty="Rien d'autre ne dépend de l'app." />
      </section>
    </div>
  )
}

// targetText writes the target of a dependency: its value, then the
// addresses a name resolved to.
function targetText(target: unknown): string {
  if (typeof target === 'string') return target
  if (Array.isArray(target)) return target.map(targetText).join(', ')
  const t = (target ?? {}) as { value?: string; resolved?: string[] }
  if (!t.value) return target ? JSON.stringify(target) : ''
  return t.resolved?.length ? `${t.value} → ${t.resolved.join(', ')}` : t.value
}

// Config lists the dependencies read in the configuration of the app; a
// key no link resolves is told.
function Config({ app: a }: { app: App }) {
  const resolved = new Set(a.links.map((l) => l.key).filter(Boolean))
  if (!a.dependencies.length) return <p className="lc-muted m-0">Aucune dépendance lue dans la configuration.</p>
  return (
    <div className="overflow-x-auto rounded-lg border border-border"><table className="lc-table">
      <thead><tr><th>Clé</th><th>Cible</th><th>Schéma</th><th className="lc-num">Port</th><th>État</th></tr></thead>
      <tbody>{a.dependencies.map((d) => {
        const target = targetText(d.target)
        return (
          <tr key={d.key}>
            <td style={{ whiteSpace: 'normal', maxWidth: 320, wordBreak: 'break-all' }}><Copyable value={d.key} /></td>
            <td className="lc-mono" style={{ whiteSpace: 'normal', wordBreak: 'break-all' }}>{target}{d.path ? ` ${d.path}` : ''}</td>
            <td className="lc-mono">{d.scheme}</td>
            <td className="lc-num">{d.port}</td>
            <td>{d.unparsed ? <span className="lc-badge lc-badge--stale">non lue</span> : resolved.has(d.key) ? <span className="lc-badge">résolue</span>
              : <span className="lc-badge lc-badge--inactive">non résolue</span>}</td>
          </tr>
        )
      })}</tbody>
    </table></div>
  )
}
