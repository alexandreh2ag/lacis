import { useMutation, useQuery } from '@apollo/client'
import { GrantList, McpSetup } from '@/components/Assistants'
import { config } from '@/config'
import { graphql } from '@/graphql'
import { useT } from '@/i18n'

const GrantsDoc = graphql(`
  query OAuthGrants {
    oauthGrants { id clientName createdAt lastUsedAt expiresAt }
  }
`)

const RevokeDoc = graphql(`
  mutation RevokeOAuthGrant($id: ID!) {
    revokeOAuthGrant(id: $id)
  }
`)

// Assistants is the tab of the profile on the AI assistants of the user:
// how to connect one through MCP, and those authorized, each revocable.
export function Assistants() {
  const t = useT().components.assistants
  const { data, loading, error } = useQuery(GrantsDoc, { fetchPolicy: 'network-only' })
  const [revoke] = useMutation(RevokeDoc, { refetchQueries: [GrantsDoc], awaitRefetchQueries: true })
  return (
    <div className="grid items-start gap-6 lg:grid-cols-2">
      <section className="lc-panel lc-panel--card">
        <div className="lc-panel__section">
          <span className="lc-label">{t.connect}</span>
          {config.mcp.enabled ? <McpSetup /> : <p className="lc-muted m-0">{t.disabled}</p>}
        </div>
      </section>
      <section className="lc-panel lc-panel--card">
        <div className="lc-panel__section">
          <span className="lc-label">{t.connected}</span>
          {error ? <p role="alert" className="m-0">{t.loadError}{error.message}</p> : (
            <GrantList grants={data?.oauthGrants ?? []} loading={loading && !data} empty={t.none}
              onRevoke={(id) => revoke({ variables: { id } })} />
          )}
        </div>
      </section>
    </div>
  )
}
