import { useEffect, useId, useRef, useState } from 'react'
import { useT } from '@/i18n'
import { errorMessage } from '@/lib/errors'

// Dialog is a modal form: it opens when mounted, Escape or Annuler close
// it, the submit button runs onSubmit.
export function Dialog({ title, onClose, onSubmit, submit, busy, danger, wide, error, children }: {
  title: string
  onClose: () => void
  // onSubmit is left out by a dialog that only shows something.
  onSubmit?: () => void
  submit?: string
  busy?: boolean
  danger?: boolean
  wide?: boolean
  error?: string | null
  children: React.ReactNode
}) {
  const t = useT().components.dialog
  const id = useId()
  const ref = useRef<HTMLDialogElement>(null)
  useEffect(() => {
    const d = ref.current
    if (d && !d.open) d.showModal?.()
  }, [])
  return (
    <dialog ref={ref} className={'lc-dialog' + (wide ? ' lc-dialog--wide' : '')} aria-labelledby={id}
      onCancel={(e) => { e.preventDefault(); onClose() }}>
      <form onSubmit={(e) => { e.preventDefault(); if (!busy) onSubmit?.() }}>
        <div className="lc-dialog__head">
          <h2 id={id} className="lc-dialog__title">{title}</h2>
          <button type="button" className="lc-btn lc-btn--ghost lc-btn--icon" aria-label={t.close} onClick={onClose}>×</button>
        </div>
        <div className="lc-dialog__body">
          {error ? <div className="lc-alert" role="alert">{error}</div> : null}
          {children}
        </div>
        <div className="lc-dialog__foot">
          <button type="button" className="lc-btn" onClick={onClose}>{onSubmit ? t.cancel : t.close}</button>
          {onSubmit ? (
            <button type="submit" className={'lc-btn lc-btn--primary' + (danger ? ' lc-btn--danger' : '')} disabled={busy}>
              {busy ? t.saving : submit ?? t.save}
            </button>
          ) : null}
        </div>
      </form>
    </dialog>
  )
}

// Field labels a control, with a hint under it.
export function Field({ label, hint, children }: { label: string; hint?: React.ReactNode; children: (id: string) => React.ReactNode }) {
  const id = useId()
  return (
    <div className="lc-field">
      <label htmlFor={id} className="lc-label">{label}</label>
      {children(id)}
      {hint ? <span className="lc-field__hint">{hint}</span> : null}
    </div>
  )
}

// errorMessage says why a mutation failed, by the reason the server gives.
export { errorMessage }

// useAction runs a mutation from a dialog: busy while it runs, its error
// kept to show, done called on success.
export function useAction() {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const run = async (fn: () => Promise<unknown>, done?: () => void) => {
    setBusy(true)
    setError(null)
    try {
      await fn()
      done?.()
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }
  return { busy, error, setError, run }
}
