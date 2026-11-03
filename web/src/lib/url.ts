// safeHttpUrl is the address as a link may carry it: an absolute http or
// https URL, without credentials; null for anything else, such as
// javascript:, data: or file:, which the page shows as text. The agent
// reads these addresses from the hosts: they are not trusted.
export function safeHttpUrl(raw: string | null | undefined): string | null {
  if (!raw) return null
  let url: URL
  try {
    url = new URL(raw.trim())
  } catch {
    return null
  }
  if ((url.protocol !== 'http:' && url.protocol !== 'https:') || !url.hostname) return null
  url.username = ''
  url.password = ''
  return url.href
}
