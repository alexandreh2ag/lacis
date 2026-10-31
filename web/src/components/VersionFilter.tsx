import { useEffect, useId, useState } from 'react'
import type { VersionOp } from '@/graphql/graphql'
import type { FilterField } from './FilterBar'

// VersionFacet is a framework or a runtime, and the versions seen.
export interface VersionFacet {
  name: string
  versions: string[]
}

// VersionValue is a version constraint as the URL keeps it.
export interface VersionValue {
  name: string
  op: VersionOp
  version: string
}

export const ops: { value: VersionOp; label: string }[] = [
  { value: 'LT', label: '<' }, { value: 'LTE', label: '≤' }, { value: 'EQ', label: '=' }, { value: 'GTE', label: '≥' }, { value: 'GT', label: '>' },
]

// VersionControl picks a framework or a runtime and, if wanted, a version
// it compares to: php ≥ 7.1. The versions seen in the namespace fill the
// version in one click.
function VersionControl({ label, facets, value, onChange }: { label: string; facets: VersionFacet[]; value: VersionValue | null; onChange: (v: VersionValue | null) => void }) {
  const id = useId()
  const [version, setVersion] = useState(value?.version ?? '')
  useEffect(() => setVersion(value?.version ?? ''), [value?.version])
  // The version follows the input after a short pause.
  useEffect(() => {
    if (!value || version === value.version) return
    const t = setTimeout(() => onChange({ ...value, version }), 400)
    return () => clearTimeout(t)
  }, [version, value, onChange])
  const seen = facets.find((f) => f.name === value?.name)?.versions ?? []
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-1.5">
        <label htmlFor={`${id}-name`} className="sr-only">{label}</label>
        <select id={`${id}-name`} className="lc-input w-[110px]" value={value?.name ?? ''}
          onChange={(e) => onChange(e.target.value ? { name: e.target.value, op: value?.op ?? 'GTE', version: '' } : null)}>
          <option value="">Tous</option>
          {facets.map((f) => <option key={f.name} value={f.name}>{f.name}</option>)}
        </select>
        <label htmlFor={`${id}-op`} className="sr-only">Comparaison</label>
        <select id={`${id}-op`} className="lc-input w-[60px] font-mono" value={value?.op ?? 'GTE'} disabled={!value}
          onChange={(e) => value && onChange({ ...value, op: e.target.value as VersionOp })}>
          {ops.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
        </select>
        <label htmlFor={`${id}-version`} className="sr-only">Version</label>
        <input id={`${id}-version`} className="lc-input w-[96px] font-mono" placeholder="7.1" disabled={!value} value={version}
          onChange={(e) => setVersion(e.target.value)} />
      </div>
      {value && seen.length ? (
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="text-[12px] text-ink-muted">Vues :</span>
          {seen.map((v) => (
            <button key={v} type="button" className="lc-chip font-mono" aria-pressed={value.version === v} onClick={() => onChange({ ...value, version: v })}>{v}</button>
          ))}
        </div>
      ) : null}
    </div>
  )
}

// versionField filters the apps by a framework or a runtime and its
// version.
export function versionField({ label, facets, value, onChange }: { label: string; facets: VersionFacet[]; value: VersionValue | null; onChange: (v: VersionValue | null) => void }): FilterField {
  const op = ops.find((o) => o.value === value?.op)?.label ?? '≥'
  return {
    label, value: value ? `${value.name}${value.version ? ` ${op} ${value.version}` : ''}` : null, clear: () => onChange(null),
    control: <VersionControl label={label} facets={facets} value={value} onChange={onChange} />,
  }
}

// versionParam reads a version constraint in the URL under a prefix:
// rt, rtop, rtv for the runtime.
export function versionParam(get: (key: string) => string | null, prefix: string): VersionValue | null {
  const name = get(prefix)
  return name ? { name, op: (get(`${prefix}op`) as VersionOp) || 'GTE', version: get(`${prefix}v`) ?? '' } : null
}

export function versionParamChanges(prefix: string, v: VersionValue | null): Record<string, string | null> {
  return { [prefix]: v?.name ?? null, [`${prefix}op`]: v && v.op !== 'GTE' ? v.op : null, [`${prefix}v`]: v?.version || null }
}

// constraintOf is the constraint the API takes.
export function constraintOf(v: VersionValue | null) {
  return v ? { name: v.name, op: v.op, version: v.version || null } : null
}
