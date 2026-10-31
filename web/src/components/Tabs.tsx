import { useId } from 'react'
import { useSearchParams } from 'react-router'

export interface Tab {
  id: string
  label: string
  count?: number
}

// useTab keeps the open tab in the URL: a shared link opens the same tab.
// fallback is the tab open when the URL names none, or one not offered.
export function useTab(tabs: Tab[], fallback: string): [string, (id: string) => void] {
  const [params, setParams] = useSearchParams()
  const asked = params.get('tab')
  const current = tabs.some((t) => t.id === asked) ? asked! : fallback
  const open = (id: string) => setParams((prev) => {
    const next = new URLSearchParams(prev)
    if (id === fallback) next.delete('tab')
    else next.set('tab', id)
    return next
  }, { replace: true })
  return [current, open]
}

// Tabs switch the panel of an entity page; the panel follows, labelled by
// its tab.
export function Tabs({ tabs, current, onChange, label, children }: {
  tabs: Tab[]; current: string; onChange: (id: string) => void; label: string; children: React.ReactNode
}) {
  const id = useId()
  const move = (e: React.KeyboardEvent, i: number) => {
    const to = e.key === 'ArrowRight' ? i + 1 : e.key === 'ArrowLeft' ? i - 1 : e.key === 'Home' ? 0 : e.key === 'End' ? tabs.length - 1 : null
    if (to === null) return
    e.preventDefault()
    const tab = tabs[(to + tabs.length) % tabs.length]
    onChange(tab.id)
    document.getElementById(`${id}-${tab.id}`)?.focus()
  }
  return (
    <div className="flex flex-col gap-4">
      <div role="tablist" aria-label={label} className="lc-tabs">
        {tabs.map((t, i) => (
          <button key={t.id} id={`${id}-${t.id}`} role="tab" type="button" className="lc-tab" aria-selected={t.id === current}
            aria-controls={`${id}-panel`} tabIndex={t.id === current ? 0 : -1} onClick={() => onChange(t.id)} onKeyDown={(e) => move(e, i)}>
            {t.label}
            {t.count !== undefined ? <span className="lc-tab__count">{t.count}</span> : null}
          </button>
        ))}
      </div>
      <div role="tabpanel" id={`${id}-panel`} aria-labelledby={`${id}-${current}`} className="flex flex-col gap-3">
        {children}
      </div>
    </div>
  )
}
