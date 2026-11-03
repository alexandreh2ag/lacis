import { useState } from 'react'
import { config } from '@/config'
import { useT } from '@/i18n'
import { ago, formatDate } from '@/lib/format'
import { Copyable } from './Copy'
import { errorMessage } from './Dialog'

// Grant is an access an assistant was given through OAuth.
export interface Grant {
  id: string
  clientName: string
  createdAt: string
  lastUsedAt: string
  expiresAt: string
}

// McpSetup tells how to connect an AI assistant to Lacis through MCP.
export function McpSetup() {
  const t = useT().components.assistants
  const command = `claude mcp add --transport http lacis ${config.mcp.url}`
  return (
    <div className="flex flex-col gap-3">
      <p className="m-0">{t.intro}</p>
      <div className="flex flex-col gap-1">
        <span className="lc-label">{t.url}</span>
        <Copyable value={config.mcp.url} className="text-[13px]" />
      </div>
      <ul className="m-0 flex list-disc flex-col gap-1.5 pl-5 text-[13px]">
        <li>{t.claudeAi}</li>
        <li className="flex flex-col gap-1">
          <span>{t.claudeCode}</span>
          <Copyable value={command} className="text-[12.5px]" />
        </li>
      </ul>
      <p className="lc-muted m-0 text-[13px]">{t.login}</p>
    </div>
  )
}

// GrantList lists the assistants a user authorized; with onRevoke, each
// access can be removed.
export function GrantList({ grants, loading, empty, onRevoke }: {
  grants: Grant[]
  loading?: boolean
  empty: string
  onRevoke?: (id: string) => Promise<unknown>
}) {
  const t = useT().components.assistants
  const [revoking, setRevoking] = useState<string | null>(null)
  const [failure, setFailure] = useState<string | null>(null)

  const remove = async (id: string) => {
    if (!onRevoke) return
    setRevoking(id)
    setFailure(null)
    try {
      await onRevoke(id)
    } catch (err) {
      setFailure(errorMessage(err))
    } finally {
      setRevoking(null)
    }
  }

  if (loading) return <span className="lc-muted">{t.loading}</span>
  return (
    <div className="flex flex-col gap-2">
      {failure ? <div className="lc-alert" role="alert">{failure}</div> : null}
      {grants.length === 0 ? <p className="lc-muted m-0">{empty}</p> : (
        <ul className="m-0 flex list-none flex-col gap-2 p-0" aria-label={t.connected}>
          {grants.map((g) => (
            <li key={g.id} className="flex flex-wrap items-center gap-3 rounded-md border border-border px-3 py-2">
              <div className="flex min-w-0 flex-1 flex-col">
                <span className="font-semibold">{g.clientName}</span>
                <span className="lc-muted text-[12.5px]">
                  {[t.since(formatDate(g.createdAt)), t.used(ago(g.lastUsedAt)), t.until(formatDate(g.expiresAt))].join(' · ')}
                </span>
              </div>
              {onRevoke ? (
                <button type="button" className="lc-btn lc-btn--danger" disabled={revoking === g.id} onClick={() => void remove(g.id)}>
                  {revoking === g.id ? t.revoking : t.revoke}
                </button>
              ) : null}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
