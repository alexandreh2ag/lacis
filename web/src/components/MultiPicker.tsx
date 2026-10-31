import { useEffect, useId, useRef, useState } from 'react'
import { useT } from '@/i18n'
import { Chevron } from './Icon'

// PickerShown is the most options the list shows: past it, a search
// narrows them.
export const PickerShown = 10

// MultiPicker picks values among many: a button opens a searchable list,
// a value picked joins the chips beside it, its × takes it out.
export function MultiPicker({ label, options, selected, onChange }: {
  label: string
  options: string[]
  selected: string[]
  onChange: (values: string[]) => void
}) {
  const t = useT().components
  const id = useId()
  const root = useRef<HTMLDivElement>(null)
  const [open, setOpen] = useState(false)
  const [q, setQ] = useState('')
  const [active, setActive] = useState(0)
  const free = options.filter((o) => !selected.includes(o))
  const found = q.trim() ? free.filter((o) => o.toLowerCase().includes(q.trim().toLowerCase())) : free
  const shown = found.slice(0, PickerShown)

  useEffect(() => {
    if (!open) return
    const close = (e: MouseEvent) => { if (!root.current?.contains(e.target as Node)) setOpen(false) }
    document.addEventListener('mousedown', close)
    return () => document.removeEventListener('mousedown', close)
  }, [open])

  const pick = (value: string) => {
    onChange([...selected, value])
    setQ('')
    setActive(0)
  }
  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault()
      const step = e.key === 'ArrowDown' ? 1 : -1
      setActive((a) => (a + step + shown.length) % Math.max(shown.length, 1))
    } else if (e.key === 'Enter' && shown[active]) {
      e.preventDefault()
      pick(shown[active])
    } else if (e.key === 'Escape') {
      setOpen(false)
    }
  }

  return (
    <div ref={root} className="relative flex flex-wrap items-center gap-1.5">
      <button type="button" className="lc-chip [&_svg]:size-3 [&_svg]:text-ink-muted" aria-haspopup="dialog" aria-expanded={open}
        onClick={() => { setOpen(!open); setActive(0) }}>
        {label}<Chevron dir="down" />
      </button>
      {selected.map((v) => (
        <button key={v} type="button" className="lc-chip" aria-pressed="true" aria-label={t.remove(v)}
          onClick={() => onChange(selected.filter((x) => x !== v))}>
          {v} <span aria-hidden="true">×</span>
        </button>
      ))}
      {open ? (
        <div role="dialog" aria-label={t.multiPicker.choose(label)} className="lc-panel absolute left-0 top-8 z-50 w-72">
          <div className="lc-panel__section">
            <label htmlFor={id} className="sr-only">{t.multiPicker.search}</label>
            <input id={id} className="lc-input w-full" type="search" placeholder={t.multiPicker.search} autoFocus autoComplete="off"
              role="combobox" aria-expanded aria-controls={`${id}-list`} aria-activedescendant={shown[active] ? `${id}-${active}` : undefined}
              value={q} onChange={(e) => { setQ(e.target.value); setActive(0) }} onKeyDown={onKey} />
            <ul id={`${id}-list`} role="listbox" aria-label={label} className="m-0 flex list-none flex-col p-0">
              {shown.map((o, i) => (
                <li key={o} id={`${id}-${i}`} role="option" aria-selected={i === active}
                  className={'cursor-pointer rounded-sm px-2 py-1.5 font-mono text-[13px] ' + (i === active ? 'bg-surface-hover' : '')}
                  onMouseEnter={() => setActive(i)} onMouseDown={(e) => { e.preventDefault(); pick(o) }}>
                  {o}
                </li>
              ))}
            </ul>
            {found.length === 0 ? <span className="lc-muted text-[13px]">{free.length ? t.noneMatch : t.multiPicker.allChosen}</span> : null}
            {found.length > PickerShown ? (
              <span className="lc-muted text-[12.5px]" role="status">{t.shown(PickerShown, found.length)}</span>
            ) : null}
          </div>
        </div>
      ) : null}
    </div>
  )
}
