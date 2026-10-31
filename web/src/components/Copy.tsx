import { useEffect, useState } from 'react'
import { useT } from '@/i18n'

// CopyButton copies a value and says so for 1.5 s.
export function CopyButton({ value }: { value: string }) {
  const t = useT()
  const label = t.components.copy(value)
  const [done, setDone] = useState(false)
  useEffect(() => {
    if (!done) return
    const timer = setTimeout(() => setDone(false), 1500)
    return () => clearTimeout(timer)
  }, [done])
  return (
    <button
      className={'lc-copy' + (done ? ' lc-copy--done' : '')}
      data-done={t.components.copied}
      type="button"
      aria-label={label}
      title={label}
      onClick={(e) => {
        e.stopPropagation()
        e.preventDefault()
        void navigator.clipboard?.writeText(value).then(() => setDone(true))
      }}
    >
      <svg className="lc-copy__icon" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth={1.5} strokeLinejoin="round" aria-hidden="true">
        <rect x="5.5" y="5.5" width="8" height="8" rx="1.5" />
        <path d="M10.5 3.5v-.5A1.5 1.5 0 0 0 9 1.5H4A1.5 1.5 0 0 0 2.5 3v5A1.5 1.5 0 0 0 4 9.5h.5" />
      </svg>
      <svg className="lc-copy__done" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <path d="M3 8.5l3.2 3L13 4.5" />
      </svg>
    </button>
  )
}

// Copyable is an identifier followed by its copy button; shown, when set,
// is what the page writes instead of the value.
export function Copyable({ value, shown, mono = true, className }: { value: string; shown?: React.ReactNode; mono?: boolean; className?: string }) {
  return (
    <span className="lc-copyable">
      <span className={[mono ? 'lc-mono' : '', className ?? ''].join(' ')} title={value}>
        {shown ?? value}
      </span>
      <CopyButton value={value} />
    </span>
  )
}

export function Ips({ ips, shown = 2, interfaces = false }: { ips: { address: string; interface?: string | null }[]; shown?: number; interfaces?: boolean }) {
  const [open, setOpen] = useState(false)
  const list = open ? ips : ips.slice(0, shown)
  return (
    <div className="lc-ips">
      {list.map((ip) => (
        <span key={ip.address} className="lc-copyable">
          <span className="lc-mono">{ip.address}</span>
          <CopyButton value={ip.address} />
          {interfaces && ip.interface ? <span className="lc-ips__if">{ip.interface}</span> : null}
        </span>
      ))}
      {!open && ips.length > shown ? (
        <button className="lc-more" type="button" title={ips.slice(shown).map((i) => i.address).join(', ')} onClick={(e) => { e.stopPropagation(); setOpen(true) }}>
          +{ips.length - shown}
        </button>
      ) : null}
    </div>
  )
}
