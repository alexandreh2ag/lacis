import { useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router'
import { useApolloClient } from '@apollo/client'
import { Logo } from '@/components/Layout'
import { config } from '@/config'

const oidcErrors: Record<string, string> = {
  oidc_unavailable: 'Le fournisseur d\'identité ne répond pas. Réessayez dans un instant.',
  oidc_expired: 'La connexion a expiré. Recommencez.',
  oidc_denied: 'Le fournisseur d\'identité a refusé la connexion.',
  oidc_failed: 'La connexion n\'a pas abouti. Recommencez ou contactez un administrateur.',
  oidc_account: 'Aucun compte ne correspond à cette identité : le nom d\'utilisateur manque ou appartient déjà à un autre compte.',
}

// safeRedirect keeps a page of the front.
function safeRedirect(target: string | null): string {
  return target && target.startsWith('/') && !target.startsWith('//') ? target : '/'
}

export function Login() {
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const client = useApolloClient()
  const redirect = safeRedirect(params.get('redirect'))
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(oidcErrors[params.get('error') ?? ''] ?? null)
  const [busy, setBusy] = useState(false)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      const res = await fetch('auth/login', {
        method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, password }),
      })
      if (res.ok) {
        // The session lives in a cookie: check the browser kept it before
        // leaving, or the next page would bring back here without a word.
        const check = await fetch('graphql', {
          method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ query: '{ me { id } }' }),
        })
        if (!check.ok) {
          setError(window.location.protocol === 'http:'
            ? 'Connexion acceptée, mais le navigateur a refusé le cookie de session : la page est en HTTP et le cookie est réservé au HTTPS. Ouvrez Lacis par son adresse HTTPS.'
            : 'Connexion acceptée, mais le navigateur n\'a pas gardé le cookie de session. Vérifiez que les cookies sont autorisés pour ce site.')
          return
        }
        await client.clearStore()
        navigate(redirect, { replace: true })
        return
      }
      setError(res.status === 401 ? 'Nom d\'utilisateur ou mot de passe incorrect.'
        : res.status === 429 ? 'Trop de tentatives. Réessayez dans 15 minutes.'
          : res.status === 403 ? 'La connexion par mot de passe est désactivée.'
            : 'La connexion n\'a pas abouti. Réessayez.')
    } catch {
      setError('Le serveur ne répond pas.')
    } finally {
      setBusy(false)
    }
  }

  const sso = `auth/oidc/login?redirect=${encodeURIComponent(config.basePath.replace(/\/$/, '') + redirect)}`
  return (
    <div className="grid min-h-full place-items-center bg-surface-sunken px-4"
      style={{ backgroundImage: 'radial-gradient(var(--border-strong) 0.8px, transparent 0.9px)', backgroundSize: '16px 16px' }}>
      <form onSubmit={(e) => void submit(e)} className="flex w-full max-w-[380px] flex-col gap-4 rounded-lg border border-border bg-surface-raised p-7 shadow-panel">
        <div className="flex items-center gap-2.5 text-xl font-bold"><Logo />Lacis</div>
        <p className="lc-muted m-0">Connectez-vous pour voir la carte de votre infrastructure.</p>
        {error ? <div className="lc-badge lc-badge--error h-auto py-2 leading-[18px]" role="alert">{error}</div> : null}
        {config.auth.local ? (
          <>
            <label htmlFor="username" className="lc-label">Nom d'utilisateur</label>
            <input className="lc-input -mt-2.5" id="username" type="text" autoComplete="username" autoCapitalize="none" spellCheck={false} required value={username} onChange={(e) => setUsername(e.target.value)} />
            <label htmlFor="password" className="lc-label">Mot de passe</label>
            <input className="lc-input -mt-2.5" id="password" type="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
            <button className="lc-btn lc-btn--primary h-10 justify-center" type="submit" disabled={busy}>{busy ? 'Connexion…' : 'Se connecter'}</button>
          </>
        ) : null}
        {config.auth.local && config.auth.oidc ? (
          <div className="flex items-center gap-2.5 text-xs text-ink-muted">
            <span className="h-px flex-1 bg-border" />ou<span className="h-px flex-1 bg-border" />
          </div>
        ) : null}
        {config.auth.oidc ? <a className="lc-btn h-10 justify-center no-underline" href={sso}>Se connecter avec SSO</a> : null}
        <p className="lc-muted m-0 text-xs">Lacis {config.version}</p>
      </form>
    </div>
  )
}
