import { useEffect, useId, useRef, useState } from 'react'
import { Chevron } from './Icon'
import { Chips, type ChipOption } from './Listing'
import { MultiPicker } from './MultiPicker'

// FilterField is a filter of a listing: its control in the panel, and the
// pill telling its value once set.
export interface FilterField {
  label: string
  // short names the field on its pill: Env, Projet…
  short?: string
  // value is what the pill says; null when the filter is not set.
  value: string | null
  control: React.ReactNode
  clear: () => void
}

function Funnel() {
  return (
    <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth={1.5} strokeLinejoin="round" aria-hidden="true">
      <path d="M2 3h12l-4.5 5.5V13l-3-1.5v-3Z" />
    </svg>
  )
}

// FilterBar is the head of a listing: its search, a "Filtres" button
// opening a small panel of the filters, closed until asked, the filters
// set as pills, and how many rows match.
export function FilterBar({ search, fields, count, onClearAll }: {
  search?: React.ReactNode; fields: FilterField[]; count?: React.ReactNode
  // onClearAll clears every filter at once, where clearing them one by one
  // would not compose.
  onClearAll?: () => void
}) {
  const id = useId()
  const root = useRef<HTMLDivElement>(null)
  const [open, setOpen] = useState(false)
  const set = fields.filter((f) => f.value !== null)

  useEffect(() => {
    if (!open) return
    const close = (e: MouseEvent) => { if (!root.current?.contains(e.target as Node)) setOpen(false) }
    const escape = (e: KeyboardEvent) => { if (e.key === 'Escape') setOpen(false) }
    document.addEventListener('mousedown', close)
    document.addEventListener('keydown', escape)
    return () => {
      document.removeEventListener('mousedown', close)
      document.removeEventListener('keydown', escape)
    }
  }, [open])

  const clearAll = onClearAll ?? (() => set.forEach((f) => f.clear()))
  return (
    <div className="lc-fbar">
      {search}
      {fields.length ? (
        <div ref={root} className="relative">
          <button type="button" className="lc-btn lc-fbar__btn" aria-expanded={open} aria-controls={id} onClick={() => setOpen(!open)}>
            <Funnel />Filtres
            {set.length ? <span className="lc-fbar__count">{set.length}</span> : null}
            <Chevron dir={open ? 'up' : 'down'} />
          </button>
          {open ? (
            <div id={id} role="dialog" aria-label="Filtres" className="lc-panel lc-fbar__panel">
              {fields.map((f) => (
                <div key={f.label} className="lc-panel__section">
                  <span className="lc-label">{f.label}</span>
                  {f.control}
                </div>
              ))}
              <div className="lc-panel__section" style={{ flexDirection: 'row', justifyContent: 'flex-end' }}>
                <button type="button" className="lc-btn lc-btn--ghost" disabled={!set.length} onClick={clearAll}>Tout effacer</button>
              </div>
            </div>
          ) : null}
        </div>
      ) : null}
      {set.map((f) => (
        <span key={f.label} className="lc-pill">
          <span className="lc-pill__key">{f.short ?? f.label} :</span> {f.value}
          <button type="button" aria-label={`Retirer le filtre ${f.label}`} onClick={f.clear}>×</button>
        </span>
      ))}
      {set.length > 1 ? <button type="button" className="lc-btn lc-btn--ghost" onClick={clearAll}>Tout effacer</button> : null}
      <span className="flex-1" />
      {count ? <span className="text-[13px] text-ink-muted">{count}</span> : null}
    </div>
  )
}

// chipsField is a filter among few values, each a chip: none pressed
// takes all.
export function chipsField({ label, short, options, selected, onChange }: {
  label: string; short?: string; options: ChipOption[]; selected: string[]; onChange: (v: string[]) => void
}): FilterField {
  const names = selected.map((v) => options.find((o) => o.value === v)?.label ?? v)
  return {
    label, short, value: names.length ? names.join(', ') : null, clear: () => onChange([]),
    control: <Chips label={label} options={options} selected={selected} onChange={onChange} />,
  }
}

// pickerField is a filter among many values, picked from a searchable
// list.
export function pickerField({ label, short, choose, options, selected, onChange }: {
  label: string; short?: string; choose: string; options: string[]; selected: string[]; onChange: (v: string[]) => void
}): FilterField {
  return {
    label, short, value: selected.length ? selected.join(', ') : null, clear: () => onChange([]),
    control: <MultiPicker label={choose} options={options} selected={selected} onChange={onChange} />,
  }
}
