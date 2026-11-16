import type { ReactNode } from 'react'
import { ago, formatFull } from '@/lib/format'

// Ago shows how long ago an instant was (« il y a 15 heures »), the full
// date and time in a tooltip on hover. children replace the relative text,
// e.g. a sentence holding it.
export function Ago({ at, children }: { at: string | null | undefined; children?: ReactNode }) {
  if (!at) return null
  return <time dateTime={at} title={formatFull(at)} className="cursor-help">{children ?? ago(at)}</time>
}
