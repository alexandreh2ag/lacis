import type { ApolloError } from '@apollo/client'
import { t } from '@/i18n'
import { field, isObject, str } from './json'

// Refusal is what the server tells of an error in its extensions: its
// code (NOT_FOUND, FORBIDDEN, BAD_USER_INPUT, CONFLICT), the reason of a
// refusal a client can say in its own words, and the values it names.
export interface Refusal {
  message: string
  code?: string
  reason?: string
  args: Record<string, unknown>
}

// refusalOf is the first GraphQL error of err, null for another error.
export function refusalOf(err: unknown): Refusal | null {
  const first = (err as ApolloError | undefined)?.graphQLErrors?.[0]
  if (!first) return null
  const ext = first.extensions
  const args = field(ext, 'args')
  return { message: first.message, code: str(field(ext, 'code')), reason: str(field(ext, 'reason')), args: isObject(args) ? args : {} }
}

// reasonText says a refusal by its reason in the language spoken; null
// for a reason the front does not know.
export function reasonText(r: Refusal): string | null {
  const e = t().components.errors
  const arg = (k: string) => String(r.args[k] ?? '')
  switch (r.reason) {
    case 'PASSWORD_TOO_SHORT': return e.password(arg('min'))
    case 'PASSWORD_TOO_LONG': return e.passwordMax(arg('max'))
    case 'CURRENT_PASSWORD_REQUIRED': return e.currentRequired
    case 'CURRENT_PASSWORD_WRONG': return e.currentWrong
    case 'INVALID_USERNAME': return e.username
    case 'INVALID_ROLE_CODE': return e.code
    case 'USER_EXISTS': return e.userExists
    case 'ROLE_EXISTS': return e.roleExists
    case 'LAST_ADMIN': return e.lastAdmin
    case 'OWN_ACCOUNT': return e.ownAccount
    case 'SSO_USER': return e.sso
    case 'NAME_TOO_LONG': return e.nameMax(arg('max'))
    case 'INVALID_OIDC_GROUP': return e.oidcGroup
    case 'INACTIVE_BEFORE_STALE': return e.inactiveAfter
    case 'DURATION_TOO_SHORT': return e.minute
    case 'NETWORK_EXISTS': return e.networkExists
    case 'PUBLIC_NETWORK_SPLIT': return e.publicSplit
    case 'NAME_NEEDS_LETTER': return e.nameLetter
    case 'NO_HOST': return e.noHost
    case 'HOST_NOT_IN_RANGE': return e.noIP(arg('host'), arg('network'))
    case 'HOST_ALREADY_IN': return e.alreadyIn(arg('host'), arg('network'))
    case 'HOST_NOT_IN': return e.notIn(arg('host'), arg('network'))
    case 'NETWORK_NOT_SPLIT': return e.splitOnly
  }
  switch (r.code) {
    case 'FORBIDDEN': return e.forbidden
    case 'NOT_FOUND': return e.notFound
  }
  return null
}

// errorMessage says why a request failed: by the reason or the code the
// server gives, else as the server words it, naming the field.
export function errorMessage(err: unknown): string {
  const r = refusalOf(err)
  if (r) return reasonText(r) ?? r.message.replace(/^(invalid input|not found|already exists): /, '')
  const apollo = err as ApolloError | undefined
  return apollo?.networkError ? t().components.errors.noServer : String((err as Error)?.message ?? err)
}
