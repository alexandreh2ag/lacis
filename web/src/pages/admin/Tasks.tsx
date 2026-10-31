import { useMutation, useQuery } from '@apollo/client'
import { Fragment, useEffect, useState } from 'react'
import { errorMessage } from '@/components/Dialog'
import { graphql } from '@/graphql'
import type { AdminTasksQuery } from '@/graphql/graphql'
import { ago, formatDateTime } from '@/lib/format'

const TasksDoc = graphql(`
  query AdminTasks {
    tasks {
      kind schedule nextRunAt
      runs { id kind state attempt maxAttempts createdAt attemptedAt finalizedAt errors output }
    }
    taskFailures(limit: 20) { id kind state attempt maxAttempts createdAt attemptedAt finalizedAt errors output }
  }
`)

const RunTaskDoc = graphql(`
  mutation RunTask($kind: String!) {
    runTask(kind: $kind) { id state }
  }
`)

const RetryTaskRunDoc = graphql(`
  mutation RetryTaskRun($id: String!) {
    retryTaskRun(id: $id) { id state }
  }
`)

type Run = AdminTasksQuery['tasks'][number]['runs'][number]

// tasksFr names the jobs and says what they do.
export const tasksFr: Record<string, { label: string; detail: string }> = {
  purge_snapshots: { label: 'Purge des snapshots', detail: 'Supprime les snapshots hors de la rétention de leur namespace, en gardant toujours le dernier de chaque host.' },
  purge_removed_entities: { label: 'Purge des entités retirées', detail: 'Supprime définitivement les apps, services, serveurs web et runtimes retirés depuis plus de la durée configurée (90 jours par défaut).' },
  purge_expired_tokens: { label: 'Purge des tokens et sessions expirés', detail: 'Supprime les tokens d\'enrôlement expirés qui n\'ont enrôlé aucun agent, et les sessions expirées.' },
  detect_token_conflicts: { label: 'Détection des hosts clonés', detail: 'Repère les agents dont deux machines partagent le token : leurs snapshots alternent entre deux noms d\'hôte sur 24 h. Ils sont signalés dans l\'onglet Agents.' },
  purge_inactive_hosts: { label: 'Purge des hosts inactifs', detail: 'Supprime les hosts sans snapshot depuis plus de la durée configurée (180 jours par défaut), comme une suppression à la main. Désactivée par défaut.' },
  normalize_snapshot: { label: 'Normalisation d\'un snapshot', detail: '' },
  recompute_links: { label: 'Calcul des liens', detail: '' },
}

const statesFr: Record<string, { label: string; tone: string }> = {
  available: { label: 'En attente', tone: 'lc-badge--plain' },
  pending: { label: 'En attente', tone: 'lc-badge--plain' },
  scheduled: { label: 'Programmée', tone: 'lc-badge--plain' },
  running: { label: 'En cours', tone: 'lc-badge--stale' },
  retryable: { label: 'Nouvel essai prévu', tone: 'lc-badge--stale' },
  completed: { label: 'Terminée', tone: '' },
  discarded: { label: 'Échec', tone: 'lc-badge--error' },
  cancelled: { label: 'Annulée', tone: 'lc-badge--inactive' },
}

// outputFr says what a run changed: « 12 snapshots supprimés ».
const outputFr: Record<string, [string, string]> = {
  snapshots: ['snapshot supprimé', 'snapshots supprimés'],
  entities: ['entité supprimée', 'entités supprimées'],
  tokens: ['token supprimé', 'tokens supprimés'],
  sessions: ['session supprimée', 'sessions supprimées'],
  conflicts: ['token partagé', 'tokens partagés'],
  hosts: ['host supprimé', 'hosts supprimés'],
}

export function outputText(output: unknown): string {
  if (!output || typeof output !== 'object') return ''
  return Object.entries(output as Record<string, unknown>)
    .filter(([, v]) => typeof v === 'number')
    .map(([k, v]) => {
      const n = v as number
      const words = outputFr[k]
      return words ? `${n} ${n > 1 ? words[1] : words[0]}` : `${k} : ${n}`
    })
    .join(', ')
}

// pending tells a run not over yet: the page follows it.
export function pending(state: string): boolean {
  return ['available', 'pending', 'scheduled', 'running', 'retryable'].includes(state)
}

function StateBadge({ run }: { run: Run }) {
  const s = statesFr[run.state] ?? { label: run.state, tone: 'lc-badge--plain' }
  return <span className={`lc-badge ${s.tone}`}>{s.label}{run.attempt > 1 ? ` · essai ${run.attempt}/${run.maxAttempts}` : ''}</span>
}

function RunLine({ run }: { run: Run }) {
  const at = run.finalizedAt ?? run.attemptedAt ?? run.createdAt
  const last = run.errors[run.errors.length - 1]
  return (
    <span className="flex flex-col gap-0.5 leading-[18px]">
      <span className="inline-flex flex-wrap items-center gap-2">
        <StateBadge run={run} />
        <span className="text-[12.5px] text-ink-muted" title={formatDateTime(at)}>{ago(at)}</span>
      </span>
      {run.state === 'completed' && outputText(run.output) ? <span className="text-[12.5px]">{outputText(run.output)}</span> : null}
      {last && run.state !== 'completed' ? <span className="lc-mono text-[12px] text-danger" style={{ whiteSpace: 'normal', wordBreak: 'break-word' }}>{last}</span> : null}
    </span>
  )
}

// Tasks lists the scheduled tasks, their last runs and the jobs failed;
// a user who may write them runs a task now or a failed job again.
export function Tasks({ write }: { write: boolean }) {
  const { data, error, refetch, startPolling, stopPolling } = useQuery(TasksDoc)
  const [runTask] = useMutation(RunTaskDoc)
  const [retry] = useMutation(RetryTaskRunDoc)
  const [open, setOpen] = useState<string | null>(null)
  const [message, setMessage] = useState<string | null>(null)

  // A run not over yet is followed until it is.
  const inFlight = !!data?.tasks.some((t) => t.runs.some((r) => pending(r.state)))
  useEffect(() => {
    if (inFlight) startPolling(2000)
    else stopPolling()
    return stopPolling
  }, [inFlight, startPolling, stopPolling])

  const act = async (fn: () => Promise<unknown>) => {
    setMessage(null)
    try {
      await fn()
      await refetch()
    } catch (err) {
      setMessage(errorMessage(err))
    }
  }

  if (error) return <p role="alert">Les tâches n'ont pas pu être chargées : {error.message}</p>
  if (!data) return <span className="lc-muted">Chargement…</span>
  const cols = write ? 4 : 3
  return (
    <>
      <p className="lc-muted m-0 text-[13px]">Les horaires viennent de <code className="lc-mono">workers.schedules</code> dans <code className="lc-mono">server.yaml</code> (cron) ;
        sans horaire, une tâche ne tourne qu'à la main. Une tâche en échec est relancée jusqu'à 5 fois, avec un délai croissant.</p>
      {message ? <div className="lc-alert" role="alert">{message}</div> : null}
      <div className="overflow-x-auto rounded-lg border border-border">
        <table className="lc-table">
          <thead><tr><th>Tâche</th><th>Planification</th><th>Dernière exécution</th>{write ? <th><span className="sr-only">Actions</span></th> : null}</tr></thead>
          <tbody>
            {data.tasks.map((t) => {
              const fr = tasksFr[t.kind]
              const last = t.runs[0]
              const busy = !!last && pending(last.state)
              return (
                <Fragment key={t.kind}>
                  <tr className="align-top">
                    <td style={{ whiteSpace: 'normal', minWidth: 280, maxWidth: 460 }}>
                      <div className="font-semibold">{fr?.label ?? t.kind}</div>
                      <div className="lc-mono text-[12px] text-ink-muted">{t.kind}</div>
                      {fr?.detail ? <div className="mt-1 text-[12.5px] leading-[18px] text-ink-muted">{fr.detail}</div> : null}
                    </td>
                    <td>
                      {t.schedule ? (
                        <span className="flex flex-col leading-[18px]">
                          <code className="lc-mono">{t.schedule}</code>
                          {t.nextRunAt ? <span className="text-[12.5px] text-ink-muted">prochaine : {formatDateTime(t.nextRunAt)}</span> : null}
                        </span>
                      ) : <span className="lc-muted">à la main</span>}
                    </td>
                    <td style={{ whiteSpace: 'normal', minWidth: 220 }}>
                      {last ? <RunLine run={last} /> : <span className="lc-muted">jamais</span>}
                      {t.runs.length > 1 ? (
                        <button type="button" className="lc-more mt-1" aria-expanded={open === t.kind} onClick={() => setOpen(open === t.kind ? null : t.kind)}>
                          {open === t.kind ? 'Masquer l\'historique' : `Historique (${t.runs.length})`}
                        </button>
                      ) : null}
                    </td>
                    {write ? (
                      <td className="lc-num">
                        <button type="button" className="lc-btn lc-btn--small" disabled={busy}
                          onClick={() => void act(() => runTask({ variables: { kind: t.kind } }))}>{busy ? 'En cours…' : 'Lancer maintenant'}</button>
                      </td>
                    ) : null}
                  </tr>
                  {open === t.kind ? (
                    <tr>
                      <td colSpan={cols} className="bg-surface">
                        <ul className="m-0 flex list-none flex-col gap-2 p-0">
                          {t.runs.map((r) => <li key={r.id}><RunLine run={r} /></li>)}
                        </ul>
                      </td>
                    </tr>
                  ) : null}
                </Fragment>
              )
            })}
          </tbody>
        </table>
      </div>

      <h2 className="m-0 mt-2 text-[17px] font-semibold">Échecs</h2>
      {data.taskFailures.length === 0 ? <p className="lc-muted m-0">Aucun échec : toutes les tâches, planifiées ou non, ont abouti.</p> : (
        <div className="overflow-x-auto rounded-lg border border-border">
          <table className="lc-table">
            <thead><tr><th>Tâche</th><th>État</th><th>Dernière erreur</th>{write ? <th><span className="sr-only">Actions</span></th> : null}</tr></thead>
            <tbody>
              {data.taskFailures.map((r) => (
                <tr key={r.id} className="align-top">
                  <td><div className="font-semibold">{tasksFr[r.kind]?.label ?? r.kind}</div><div className="lc-mono text-[12px] text-ink-muted">{r.kind} · #{r.id}</div></td>
                  <td><span className="flex flex-col gap-0.5"><StateBadge run={r} />
                    <span className="text-[12.5px] text-ink-muted">{formatDateTime(r.finalizedAt ?? r.attemptedAt ?? r.createdAt)}</span></span></td>
                  <td className="lc-mono text-[12px]" style={{ whiteSpace: 'normal', wordBreak: 'break-word', maxWidth: 520 }}>{r.errors[r.errors.length - 1] ?? '—'}</td>
                  {write ? (
                    <td className="lc-num">
                      <button type="button" className="lc-btn lc-btn--small" onClick={() => void act(() => retry({ variables: { id: r.id } }))}>Relancer</button>
                    </td>
                  ) : null}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  )
}
