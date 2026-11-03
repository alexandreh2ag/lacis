// repositoryUrl is the web page of a git repository, from the address the
// agent read: https as is, ssh (git@host:group/repo.git or
// ssh://git@host:2222/group/repo.git) on https; null for any other, such
// as a local path. Credentials are left out.
export function repositoryUrl(repository: string): string | null {
  const scp = /^[\w.-]+@([\w.-]+):(?!\/)(.+)$/.exec(repository)
  let url: URL
  try {
    url = new URL(scp ? `https://${scp[1]}/${scp[2]}` : repository)
  } catch {
    return null
  }
  if (!['https:', 'http:', 'ssh:', 'git:'].includes(url.protocol) || !url.hostname) return null
  const path = url.pathname.replace(/\.git\/?$/, '').replace(/\/$/, '')
  if (!path) return null
  const http = url.protocol === 'http:' ? 'http:' : 'https:'
  return `${http}//${url.hostname}${http === url.protocol && url.port ? ':' + url.port : ''}${path}`
}
