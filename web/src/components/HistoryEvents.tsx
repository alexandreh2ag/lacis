import { describe, type ChangeEvent } from '@/lib/history'
import { formatDateTime } from '@/lib/format'

// HistoryEvent is an event of the timeline: when, what, the diff.
export function HistoryEvent({ event, time = true }: { event: ChangeEvent; time?: boolean }) {
  const d = describe(event)
  return (
    <div className={`lc-event lc-event--${event.kind}`} style={time ? undefined : { gridTemplateColumns: '0 18px 1fr' }}>
      <span className="lc-event__time">{time ? formatDateTime(event.occurredAt) : ''}</span>
      <span className="lc-event__mark" />
      <div className="min-w-0">
        {d.title} <b className={event.entityType === 'host' ? 'lc-mono font-semibold' : undefined}>{d.subject}</b>
        {event.hostname && event.entityType !== 'host' ? <> sur <span className="lc-mono">{event.hostname}</span></> : null}
        {d.diffs.length ? (
          <div className="lc-event__diff">
            {d.diffs.map((x, i) => (
              <span key={x.field}>{i ? ' · ' : ''}{x.field} {x.before ? <del>{x.before}</del> : null} → <ins>{x.after || '∅'}</ins></span>
            ))}
          </div>
        ) : d.detail ? <div className="lc-event__diff">{d.detail}</div> : null}
      </div>
    </div>
  )
}
