---
sidebar_position: 8
title: AI assistants (MCP)
---

# AI assistants (MCP)

An AI assistant such as Claude can read Lacis through the
[Model Context Protocol](https://modelcontextprotocol.io): "who runs PHP below
8.1?", "what breaks if `db-1` stops?", "what changed on `web-1` this week?".
The assistant reads only, with the rights of the user who authorized it.

Lacis is its own OAuth 2.1 authorization server: the user authorizes the
assistant on a Lacis page, after logging in as usual, with a local account
or through [OIDC](./oidc.md).

## Configuration

```yaml title="server.yaml"
listen:
  public_url: https://lacis.example.com
mcp:
  enabled: true
```

`listen.public_url` is required: the OAuth metadata give the assistants the
URLs to call. The MCP endpoint is `<public_url>/mcp`, for example
`https://lacis.example.com/mcp`. See [the `mcp` keys](./configuration/server.md#mcp)
for the lifetime of the tokens and of the authorizations.

### Reaching Lacis from the assistant

claude.ai, and the custom connectors of Claude Desktop, call the server from
the cloud of Anthropic: these paths must be reachable from the Internet, in
HTTPS.

| Path | Called by |
| ---- | --------- |
| `/mcp` | the assistant, with its access token |
| `/.well-known/oauth-protected-resource`, `/.well-known/oauth-authorization-server` (and, under a base path, the same at the root followed by the base path) | the assistant, to discover the server |
| `/oauth/register`, `/oauth/token`, `/oauth/revoke` | the assistant |
| `/oauth/authorize`, `/authorize`, `/login`, `/auth/*`, `/oauth/request`, the front | the browser of the user |

The last row is used by the browser of the user only: it may stay on your
internal network as long as the public URL resolves to Lacis from there too.
Claude Code runs on the computer of the user and needs no exposure beyond what
the browser reaches.

## Connecting an assistant

The tab **MCP access** of the profile (account menu › Profile) gives the
address of the MCP server and the command for Claude Code.

- **claude.ai**: Settings › Connectors › Add custom connector, then paste
  `https://lacis.example.com/mcp`.
- **Claude Code**:
  `claude mcp add --transport http lacis https://lacis.example.com/mcp`, then
  `/mcp` in Claude Code to log in.

On first use, the assistant opens Lacis in the browser: the user logs in if
needed, checks the name of the assistant and the site it goes back to, then
clicks **Authorize**.

## Removing an access

- The user: the tab **MCP access** of their profile lists the assistants they authorized;
  **Remove access** cuts one at once.
- An administrator: in the administration, tab **Users**, the button
  **MCP access** of a user lists theirs. Reading them takes the permission
  `users` `read`, removing one `users` `write`; the removal is logged with the
  administrator who made it.

Deleting a user removes their authorizations too.

## Tools

| Tool | What it answers |
| ---- | --------------- |
| `list_namespaces` | The namespaces the user may read, with their projects and environments. |
| `search` | Hosts, apps, services, webservers and containers by name, path, domain, vhost, image or IP. |
| `get_host` | A host: OS, kernel, resources, IPs, runtimes, services, webservers, apps, containers. |
| `get_app` | An app: project, env, framework, runtime, vhosts, dependencies, what it uses and who uses it. |
| `links` | Who uses a host (and everything on it), an app, a service or a webserver, or what it uses. |
| `find_by_version` | Who uses a technology, in a version compared with `lt`, `lte`, `eq`, `gte` or `gt`: PHP below 8.1, MySQL 5.7… |
| `list_versions` | The versions in use of every technology, or of one, as major.minor. |
| `graph` | The graph of a namespace as Mermaid, at the level of the projects, hosts, apps or services. |
| `history` | The changes of a namespace, of a host, since a date or a duration (`7d`). |

`find_by_version` and `list_versions` read the runtimes installed on the hosts
(`runtime`), the runtime of each app (`app_runtime`), the frameworks, the
services, the webservers and the images of the containers. Versions compare
number by number, cut to the parts given: `eq 7.4` takes `7.4.33`, and
`8.1.2-1ubuntu2.18` is below `8.2`.

## Security

- Read-only: no tool changes anything. A user sees through the assistant what
  they see in the front, by the [permissions](./permissions.md) of their roles.
  The OIDC groups are those of the login that authorized the assistant, read
  again when the authorization ends (`mcp.grant_lifetime`).
- Anyone may register a client, as OAuth dynamic registration requires: a
  client gives nothing until a user authorizes it, and one nobody authorized
  within a day is deleted. The consent page shows the site the browser goes
  back to: authorize only an assistant you started the connection from.
- PKCE (S256) is required. Codes serve once, for 5 minutes. Refresh tokens
  rotate: a refresh token used twice ends the authorization. Lacis keeps only
  the SHA-256 of the secrets, codes and tokens.
