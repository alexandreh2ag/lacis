import { useEffect, useId, useRef, useState } from 'react'
import type { ApolloError } from '@apollo/client'

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
          <button type="button" className="lc-btn lc-btn--ghost lc-btn--icon" aria-label="Fermer" onClick={onClose}>×</button>
        </div>
        <div className="lc-dialog__body">
          {error ? <div className="lc-alert" role="alert">{error}</div> : null}
          {children}
        </div>
        <div className="lc-dialog__foot">
          <button type="button" className="lc-btn" onClick={onClose}>{onSubmit ? 'Annuler' : 'Fermer'}</button>
          {onSubmit ? (
            <button type="submit" className={'lc-btn lc-btn--primary' + (danger ? ' lc-btn--danger' : '')} disabled={busy}>
              {busy ? 'Enregistrement…' : submit ?? 'Enregistrer'}
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

const messages: Record<string, string> = {
  FORBIDDEN: 'Vous n\'avez pas le droit de faire cette modification.',
  NOT_FOUND: 'L\'élément n\'existe plus : rechargez la page.',
}

// translations say in French the refusals of the server the forms meet;
// another refusal shows as the server words it, naming the field.
const translations: [RegExp, string | ((m: RegExpMatchArray) => string)][] = [
  [/password needs at least (\d+)/, (m) => `Le mot de passe doit faire au moins ${m[1]} caractères.`],
  [/^username /, 'Nom d\'utilisateur invalide : 1 à 100 lettres, chiffres ou . _ @ + -, en commençant par une lettre ou un chiffre.'],
  [/^code /, 'Code invalide : 1 à 63 lettres minuscules, chiffres ou . _ -, en commençant par une lettre ou un chiffre.'],
  [/already exists: user /, 'Ce nom d\'utilisateur est déjà pris.'],
  [/already exists: role /, 'Un rôle a déjà ce code.'],
  [/last user holding the admin role/, 'C\'est le dernier utilisateur à détenir le rôle admin : il le garde.'],
  [/cannot delete your own account/, 'Vous ne pouvez pas supprimer votre propre compte.'],
  [/signs in with SSO/, 'Cet utilisateur se connecte par SSO : il n\'a pas de mot de passe.'],
  [/a name holds (\d+)/, (m) => `Un nom fait au plus ${m[1]} caractères.`],
  [/an OIDC group holds/, 'Un groupe OIDC fait de 1 à 200 caractères.'],
  [/inactiveAfter must be longer than staleAfter/, '« Inactif après » doit être plus long que « Stale après ».'],
  [/must be a minute or more/, 'Une durée fait au moins une minute.'],
  [/already exists: network /, 'Un réseau porte déjà ce nom.'],
  [/a public network is not split/, 'Un réseau public ne se scinde pas : ses IPs sont uniques.'],
  [/the name must hold a letter or a digit/, 'Le nom doit contenir une lettre ou un chiffre.'],
  [/no host to move/, 'Choisissez au moins un host.'],
  [/^(\S+) has no IP in the range of (\S+)/, (m) => `${m[1]} n'a pas d'IP dans le range de ${m[2]}.`],
  [/^(\S+) is already in (\S+)/, (m) => `${m[1]} est déjà dans ${m[2]}.`],
  [/^(\S+) is not in (\S+)/, (m) => `${m[1]} n'est pas dans ${m[2]}.`],
  [/only a network split by hand is deleted/, 'Seul un réseau scindé à la main se supprime : les autres viennent des IPs des hosts.'],
]

// errorMessage says why a mutation failed.
export function errorMessage(err: unknown): string {
  const apollo = err as ApolloError
  const first = apollo?.graphQLErrors?.[0]
  if (first) {
    const code = first.extensions?.code as string | undefined
    if (code && messages[code]) return messages[code]
    const message = first.message.replace(/^(invalid input|not found): /, '')
    for (const [pattern, text] of translations) {
      const m = message.match(pattern)
      if (m) return typeof text === 'string' ? text : text(m)
    }
    return message
  }
  return apollo?.networkError ? 'Le serveur ne répond pas.' : String((err as Error)?.message ?? err)
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
