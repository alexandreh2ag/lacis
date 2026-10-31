import { useMutation, useQuery } from '@apollo/client'
import { Fragment, useEffect, useState } from 'react'
import { errorMessage } from '@/components/Dialog'
import { graphql } from '@/graphql'
import type { AdminTasksQuery } from '@/graphql/graphql'
import { t as text, useT } from '@/i18n'
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

const tones: Record<string, string> = {
  available: 'lc-badge--plain', pending: 'lc-badge--plain', scheduled: 'lc-badge--plain', running: 'lc-badge--stale',
  retryable: 'lc-badge--stale', completed: '', discarded: 'lc-badge--error', cancelled: 'lc-badge--inactive',
}

export function outputText(output: unknown): string {
  if (!output || typeof output !== 'object') return ''
  return Object.entries(output as Record<string, unknown>)
    .filter(([, v]) => typeof v === 'number')
    .map(([k, v]) => {
      const said = text().admin.tasks.output[k]
      return said ? said(v as number) : `${k} : ${v as number}`
    })
    .join(', ')
}

// pending tells a run not over yet: the page follows it.
export function pending(state: string): boolean {
  return ['available', 'pending', 'scheduled', 'running', 'retryable'].includes(state)
}

function StateBadge({ run }: { run: Run }) {
  const k = useT().admin.tasks
  return <span className={`lc-badge ${tones[run.state] ?? 'lc-badge--plain'}`}>{k.states[run.state] ?? run.state}{run.attempt > 1 ? k.attempt(run.attempt, run.maxAttempts) : ''}</span>
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
  const t = useT()
  const k = t.admin.tasks

  // A run not over yet is followed until it is.
  const inFlight = !!data?.tasks.some((x) => x.runs.some((r) => pending(r.state)))
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

  if (error) return <p role="alert">{k.loadError}{error.message}</p>
  if (!data) return <span className="lc-muted">{t.admin.loading}</span>
  const cols = write ? 4 : 3
  return (
    <>
      <p className="lc-muted m-0 text-[13px]">{k.intro[0]}<code className="lc-mono">workers.schedules</code>{k.intro[1]}<code className="lc-mono">server.yaml</code>{k.intro[2]}</p>
      {message ? <div className="lc-alert" role="alert">{message}</div> : null}
      <div className="overflow-x-auto rounded-lg border border-border">
        <table className="lc-table">
          <thead><tr><th>{k.task}</th><th>{k.schedule}</th><th>{k.lastRun}</th>{write ? <th><span className="sr-only">{t.admin.actions}</span></th> : null}</tr></thead>
          <tbody>
            {data.tasks.map((task) => {
              const said = k.kinds[task.kind]
              const last = task.runs[0]
              const busy = !!last && pending(last.state)
              return (
                <Fragment key={task.kind}>
                  <tr className="align-top">
                    <td style={{ whiteSpace: 'normal', minWidth: 280, maxWidth: 460 }}>
                      <div className="font-semibold">{said?.label ?? task.kind}</div>
                      <div className="lc-mono text-[12px] text-ink-muted">{task.kind}</div>
                      {said?.detail ? <div className="mt-1 text-[12.5px] leading-[18px] text-ink-muted">{said.detail}</div> : null}
                    </td>
                    <td>
                      {task.schedule ? (
                        <span className="flex flex-col leading-[18px]">
                          <code className="lc-mono">{task.schedule}</code>
                          {task.nextRunAt ? <span className="text-[12.5px] text-ink-muted">{k.next(formatDateTime(task.nextRunAt))}</span> : null}
                        </span>
                      ) : <span className="lc-muted">{k.byHand}</span>}
                    </td>
                    <td style={{ whiteSpace: 'normal', minWidth: 220 }}>
                      {last ? <RunLine run={last} /> : <span className="lc-muted">{t.admin.never}</span>}
                      {task.runs.length > 1 ? (
                        <button type="button" className="lc-more mt-1" aria-expanded={open === task.kind} onClick={() => setOpen(open === task.kind ? null : task.kind)}>
                          {open === task.kind ? k.hideHistory : k.history(task.runs.length)}
                        </button>
                      ) : null}
                    </td>
                    {write ? (
                      <td className="lc-num">
                        <button type="button" className="lc-btn lc-btn--small" disabled={busy}
                          onClick={() => void act(() => runTask({ variables: { kind: task.kind } }))}>{busy ? k.running : k.runNow}</button>
                      </td>
                    ) : null}
                  </tr>
                  {open === task.kind ? (
                    <tr>
                      <td colSpan={cols} className="bg-surface">
                        <ul className="m-0 flex list-none flex-col gap-2 p-0">
                          {task.runs.map((r) => <li key={r.id}><RunLine run={r} /></li>)}
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

      <h2 className="m-0 mt-2 text-[17px] font-semibold">{k.failures}</h2>
      {data.taskFailures.length === 0 ? <p className="lc-muted m-0">{k.noFailure}</p> : (
        <div className="overflow-x-auto rounded-lg border border-border">
          <table className="lc-table">
            <thead><tr><th>{k.task}</th><th>{k.state}</th><th>{k.lastError}</th>{write ? <th><span className="sr-only">{t.admin.actions}</span></th> : null}</tr></thead>
            <tbody>
              {data.taskFailures.map((r) => (
                <tr key={r.id} className="align-top">
                  <td><div className="font-semibold">{k.kinds[r.kind]?.label ?? r.kind}</div><div className="lc-mono text-[12px] text-ink-muted">{r.kind} · #{r.id}</div></td>
                  <td><span className="flex flex-col gap-0.5"><StateBadge run={r} />
                    <span className="text-[12.5px] text-ink-muted">{formatDateTime(r.finalizedAt ?? r.attemptedAt ?? r.createdAt)}</span></span></td>
                  <td className="lc-mono text-[12px]" style={{ whiteSpace: 'normal', wordBreak: 'break-word', maxWidth: 520 }}>{r.errors[r.errors.length - 1] ?? '—'}</td>
                  {write ? (
                    <td className="lc-num">
                      <button type="button" className="lc-btn lc-btn--small" onClick={() => void act(() => retry({ variables: { id: r.id } }))}>{k.retry}</button>
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
