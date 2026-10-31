import { useMutation, useQuery } from '@apollo/client'
import { useState } from 'react'
import { Link } from 'react-router'
import { CopyButton } from '@/components/Copy'
import { Dialog, Field, useAction } from '@/components/Dialog'
import { chipsField, FilterBar } from '@/components/FilterBar'
import { Pager, SearchInput, SortHeader, useTable } from '@/components/Listing'
import { graphql } from '@/graphql'
import type { AdminAgentsQuery, AdminTokensQuery } from '@/graphql/graphql'
import { ago, formatDate, formatDateTime, plural } from '@/lib/format'

const TokensDoc = graphql(`
  query AdminTokens {
    enrollmentTokens { id label createdAt expiresAt revokedAt agents }
  }
`)

const CreateTokenDoc = graphql(`
  mutation CreateEnrollmentToken($label: String, $ttl: Int!) {
    createEnrollmentToken(label: $label, ttl: $ttl) { secret token { id } }
  }
`)

const RevokeTokenDoc = graphql(`
  mutation RevokeEnrollmentToken($id: ID!) {
    revokeEnrollmentToken(id: $id) { id revokedAt }
  }
`)

const AgentsDoc = graphql(`
  query AdminAgents {
    agents { id createdAt lastSeenAt revokedAt tokenLabel conflictAt conflictHostnames host { id hostname namespace } }
  }
`)

const RevokeAgentDoc = graphql(`
  mutation RevokeAgent($id: ID!) {
    revokeAgent(id: $id) { id revokedAt }
  }
`)

type Token = AdminTokensQuery['enrollmentTokens'][number]
type Agent = AdminAgentsQuery['agents'][number]

type TokenState = 'active' | 'expired' | 'revoked'

// tokenState tells whether a token still enrolls agents at now.
export function tokenState(t: { expiresAt?: string | null; revokedAt?: string | null }, now = Date.now()): TokenState {
  if (t.revokedAt) return 'revoked'
  if (t.expiresAt && Date.parse(t.expiresAt) <= now) return 'expired'
  return 'active'
}

const stateOptions = [{ value: 'active', label: 'actif' }, { value: 'expired', label: 'expiré' }, { value: 'revoked', label: 'révoqué' }]

function StateBadge({ state }: { state: TokenState }) {
  if (state === 'revoked') return <span className="lc-badge lc-badge--error">Révoqué</span>
  if (state === 'expired') return <span className="lc-badge lc-badge--inactive">Expiré</span>
  return <span className="lc-badge">Actif</span>
}

const lifetimes = [
  { value: 86400, label: '1 jour' },
  { value: 7 * 86400, label: '7 jours' },
  { value: 30 * 86400, label: '30 jours' },
  { value: 90 * 86400, label: '90 jours' },
  { value: 0, label: 'Jamais' },
]

// Tokens lists the enrollment tokens; they enroll agents into any
// namespace, the one of their label.
export function Tokens({ write }: { write: boolean }) {
  const { data, error } = useQuery(TokensDoc)
  const [states, setStates] = useState<string[]>([])
  const [dialog, setDialog] = useState<{ kind: 'create' } | { kind: 'revoke'; token: Token } | null>(null)
  const rows = (data?.enrollmentTokens ?? []).filter((t) => !states.length || states.includes(tokenState(t)))
  const t = useTable(rows, (x) => [x.label ?? ''],
    (x, by) => by === 'label' ? x.label ?? '' : by === 'agents' ? x.agents : by === 'expires' ? x.expiresAt ?? '9999' : x.createdAt, 'created', true)
  const head = (label: string, by: string, num?: boolean) => <SortHeader label={label} by={by} sort={t.sort.by} desc={t.sort.desc} onSort={t.onSort} num={num} />

  if (error) return <p role="alert">Les tokens n'ont pas pu être chargés : {error.message}</p>
  if (!data) return <span className="lc-muted">Chargement…</span>
  return (
    <>
      <p className="lc-muted m-0 text-[13px]">Un token d'enrôlement sert à la première connexion d'un agent, dans n'importe quel namespace : celui du label de l'agent.
        Révoquer un token n'arrête pas les agents qu'il a enrôlés.</p>
      <div className="flex flex-wrap items-center gap-2">
        <div className="min-w-0 flex-1">
          <FilterBar search={<SearchInput value={t.q} onChange={t.setQ} placeholder="Chercher un libellé" width={260} />}
            fields={[chipsField({ label: 'État', options: stateOptions, selected: states, onChange: setStates })]}
            count={plural(t.list.length, 'token')} />
        </div>
        {write ? <button type="button" className="lc-btn lc-btn--primary" onClick={() => setDialog({ kind: 'create' })}>Nouveau token</button> : null}
      </div>
      <div className="overflow-x-auto rounded-lg border border-border">
        <table className="lc-table">
          <thead><tr>
            {head('Libellé', 'label')}{head('Créé le', 'created')}{head('Expire', 'expires')}<th>État</th>{head('Agents', 'agents', true)}
            {write ? <th><span className="sr-only">Actions</span></th> : null}
          </tr></thead>
          <tbody>
            {t.shown.map((x) => {
              const state = tokenState(x)
              return (
                <tr key={x.id}>
                  <td>{x.label ? <span className="font-semibold">{x.label}</span> : <span className="lc-muted">sans libellé</span>}</td>
                  <td>{formatDate(x.createdAt)}</td>
                  <td>{x.expiresAt ? <span title={formatDateTime(x.expiresAt)}>{formatDate(x.expiresAt)}</span> : <span className="lc-muted">jamais</span>}</td>
                  <td><StateBadge state={state} /></td>
                  <td className="lc-num">{x.agents}</td>
                  {write ? (
                    <td className="lc-num">
                      <button type="button" className="lc-btn lc-btn--small lc-btn--danger" disabled={state === 'revoked'}
                        onClick={() => setDialog({ kind: 'revoke', token: x })}>Révoquer</button>
                    </td>
                  ) : null}
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
      <Pager page={t.page} size={t.size} total={t.list.length} noun="tokens" onPage={t.setPage} onSize={t.setSize} />
      {dialog?.kind === 'create' ? <CreateToken onClose={() => setDialog(null)} /> : null}
      {dialog?.kind === 'revoke' ? <RevokeToken token={dialog.token} onClose={() => setDialog(null)} /> : null}
    </>
  )
}

function CreateToken({ onClose }: { onClose: () => void }) {
  const [create] = useMutation(CreateTokenDoc, { refetchQueries: ['AdminTokens'] })
  const a = useAction()
  const [label, setLabel] = useState('')
  const [ttl, setTtl] = useState(7 * 86400)
  const [secret, setSecret] = useState<string | null>(null)
  if (secret) {
    return (
      <Dialog title="Token créé" onClose={onClose}>
        <div className="lc-alert lc-alert--warn">Copiez-le maintenant : il ne sera plus affiché. Lacis n'en garde que l'empreinte.</div>
        <div className="lc-secret"><code>{secret}</code><CopyButton value={secret} /></div>
        <p className="lc-muted m-0 text-[13px]">Mettez-le dans <code className="lc-mono">auth.enrollment_token</code> de <code className="lc-mono">agent.yaml</code>,
          ou dans la variable <code className="lc-mono">LACIS_AUTH_ENROLLMENT_TOKEN</code>, puis lancez <code className="lc-mono">lacis-agent start</code>.</p>
      </Dialog>
    )
  }
  return (
    <Dialog title="Nouveau token d'enrôlement" submit="Créer" busy={a.busy} error={a.error} onClose={onClose}
      onSubmit={() => void a.run(async () => {
        const res = await create({ variables: { label: label.trim() || null, ttl } })
        setSecret(res.data?.createEnrollmentToken.secret ?? null)
      })}>
      <Field label="Libellé" hint="Pour le reconnaître : où il sert, par ex. « serveurs web acme ».">
        {(id) => <input id={id} className="lc-input" value={label} onChange={(e) => setLabel(e.target.value)} />}
      </Field>
      <Field label="Durée de validité">
        {(id) => (
          <select id={id} className="lc-select" value={ttl} onChange={(e) => setTtl(Number(e.target.value))}>
            {lifetimes.map((l) => <option key={l.value} value={l.value}>{l.label}</option>)}
          </select>
        )}
      </Field>
    </Dialog>
  )
}

function RevokeToken({ token, onClose }: { token: Token; onClose: () => void }) {
  const [revoke] = useMutation(RevokeTokenDoc, { refetchQueries: ['AdminTokens'] })
  const a = useAction()
  return (
    <Dialog title={`Révoquer ${token.label ? `« ${token.label} »` : 'ce token'} ?`} submit="Révoquer" danger busy={a.busy} error={a.error} onClose={onClose}
      onSubmit={() => void a.run(() => revoke({ variables: { id: token.id } }), onClose)}>
      <p className="m-0">Il n'enrôlera plus d'agent. Les {token.agents} agents déjà enrôlés continuent d'envoyer leurs snapshots :
        révoquez-les dans l'onglet Agents pour les arrêter.</p>
    </Dialog>
  )
}

// Agents lists the enrolled agents and the host of their snapshots.
export function Agents({ write }: { write: boolean }) {
  const { data, error } = useQuery(AgentsDoc)
  const [states, setStates] = useState<string[]>([])
  const [revoking, setRevoking] = useState<Agent | null>(null)
  const rows = (data?.agents ?? []).filter((x) => !states.length || states.includes(x.revokedAt ? 'revoked' : 'active')
    || (states.includes('conflict') && !!x.conflictAt))
  const t = useTable(rows, (x) => [x.host?.hostname ?? '', x.host?.namespace ?? '', x.tokenLabel ?? ''],
    (x, by) => by === 'host' ? x.host?.hostname ?? '' : by === 'namespace' ? x.host?.namespace ?? '' : by === 'seen' ? x.lastSeenAt ?? '' : x.createdAt, 'created', true)
  const head = (label: string, by: string) => <SortHeader label={label} by={by} sort={t.sort.by} desc={t.sort.desc} onSort={t.onSort} />

  if (error) return <p role="alert">Les agents n'ont pas pu être chargés : {error.message}</p>
  if (!data) return <span className="lc-muted">Chargement…</span>
  return (
    <>
      <FilterBar search={<SearchInput value={t.q} onChange={t.setQ} placeholder="Chercher un host, un namespace, un token" width={300} />}
        fields={[chipsField({ label: 'État', options: [{ value: 'active', label: 'actif' }, { value: 'revoked', label: 'révoqué' }, { value: 'conflict', label: 'token partagé' }],
          selected: states, onChange: setStates })]}
        count={plural(t.list.length, 'agent')} />
      <div className="overflow-x-auto rounded-lg border border-border">
        <table className="lc-table">
          <thead><tr>
            {head('Host', 'host')}{head('Namespace', 'namespace')}<th>Token</th>{head('Enrôlé le', 'created')}{head('Dernier contact', 'seen')}<th>État</th>
            {write ? <th><span className="sr-only">Actions</span></th> : null}
          </tr></thead>
          <tbody>
            {t.shown.map((x) => (
              <tr key={x.id}>
                <td>{x.host ? (
                  <span className="lc-copyable"><Link className="lc-mono font-semibold" to={`/${x.host.namespace}/hosts/${x.host.id}`}>{x.host.hostname}</Link><CopyButton value={x.host.hostname} /></span>
                ) : <span className="lc-muted">en attente du premier snapshot</span>}</td>
                <td>{x.host ? <Link to={`/${x.host.namespace}`}>{x.host.namespace}</Link> : <span className="lc-muted">—</span>}</td>
                <td>{x.tokenLabel ?? <span className="lc-muted">sans libellé</span>}</td>
                <td>{formatDate(x.createdAt)}</td>
                <td>{x.lastSeenAt ? <span title={formatDateTime(x.lastSeenAt)}>{ago(x.lastSeenAt)}</span> : <span className="lc-muted">jamais</span>}</td>
                <td><span className="inline-flex flex-wrap gap-1.5">
                  {x.revokedAt ? <span className="lc-badge lc-badge--error" title={formatDateTime(x.revokedAt)}>Révoqué</span> : <span className="lc-badge">Actif</span>}
                  {x.conflictAt ? (
                    <span className="lc-badge lc-badge--stale" title={`Depuis le ${formatDateTime(x.conflictAt)}, ses snapshots alternent entre ${(x.conflictHostnames ?? []).join(', ')} : `
                      + 'deux machines partagent son token, un host cloné. Révoquez-le, puis réenrôlez chaque machine avec sa propre identité.'}>Token partagé</span>
                  ) : null}
                </span></td>
                {write ? (
                  <td className="lc-num">
                    <button type="button" className="lc-btn lc-btn--small lc-btn--danger" disabled={!!x.revokedAt} onClick={() => setRevoking(x)}>Révoquer</button>
                  </td>
                ) : null}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Pager page={t.page} size={t.size} total={t.list.length} noun="agents" onPage={t.setPage} onSize={t.setSize} />
      {revoking ? <RevokeAgent agent={revoking} onClose={() => setRevoking(null)} /> : null}
    </>
  )
}

function RevokeAgent({ agent, onClose }: { agent: Agent; onClose: () => void }) {
  const [revoke] = useMutation(RevokeAgentDoc, { refetchQueries: ['AdminAgents'] })
  const a = useAction()
  const name = agent.host?.hostname ?? 'cet agent'
  return (
    <Dialog title={`Révoquer ${name} ?`} submit="Révoquer" danger busy={a.busy} error={a.error} onClose={onClose}
      onSubmit={() => void a.run(() => revoke({ variables: { id: agent.id } }), onClose)}>
      <p className="m-0">Ses snapshots seront refusés et l'agent s'arrêtera. {agent.host
        ? 'Le host reste dans l\'inventaire : il deviendra stale puis inactif, et pourra être supprimé depuis sa fiche.' : ''}</p>
    </Dialog>
  )
}
