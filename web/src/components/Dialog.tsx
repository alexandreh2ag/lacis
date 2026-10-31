import { useEffect, useId, useRef, useState } from 'react'
import type { ApolloError } from '@apollo/client'
import { t as text, useT } from '@/i18n'

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

// messages say the refusals of the server by their code.
function messages(): Record<string, string> {
  const e = text().components.errors
  return { FORBIDDEN: e.forbidden, NOT_FOUND: e.notFound }
}

// translations say in the language spoken the refusals of the server the
// forms meet; another refusal shows as the server words it, naming the
// field.
function translations(): [RegExp, string | ((m: RegExpMatchArray) => string)][] {
  const e = text().components.errors
  return [
    [/password needs at least (\d+)/, (m) => e.password(m[1])],
    [/^username /, e.username],
    [/^code /, e.code],
    [/already exists: user /, e.userExists],
    [/already exists: role /, e.roleExists],
    [/last user holding the admin role/, e.lastAdmin],
    [/cannot delete your own account/, e.ownAccount],
    [/signs in with SSO/, e.sso],
    [/a name holds (\d+)/, (m) => e.nameMax(m[1])],
    [/an OIDC group holds/, e.oidcGroup],
    [/inactiveAfter must be longer than staleAfter/, e.inactiveAfter],
    [/must be a minute or more/, e.minute],
    [/already exists: network /, e.networkExists],
    [/a public network is not split/, e.publicSplit],
    [/the name must hold a letter or a digit/, e.nameLetter],
    [/no host to move/, e.noHost],
    [/^(\S+) has no IP in the range of (\S+)/, (m) => e.noIP(m[1], m[2])],
    [/^(\S+) is already in (\S+)/, (m) => e.alreadyIn(m[1], m[2])],
    [/^(\S+) is not in (\S+)/, (m) => e.notIn(m[1], m[2])],
    [/only a network split by hand is deleted/, e.splitOnly],
  ]
}

// errorMessage says why a mutation failed.
export function errorMessage(err: unknown): string {
  const apollo = err as ApolloError
  const first = apollo?.graphQLErrors?.[0]
  if (first) {
    const code = first.extensions?.code as string | undefined
    const byCode = messages()
    if (code && byCode[code]) return byCode[code]
    const message = first.message.replace(/^(invalid input|not found): /, '')
    for (const [pattern, said] of translations()) {
      const m = message.match(pattern)
      if (m) return typeof said === 'string' ? said : said(m)
    }
    return message
  }
  return apollo?.networkError ? text().components.errors.noServer : String((err as Error)?.message ?? err)
}

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
