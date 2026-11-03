---
sidebar_position: 1
title: Server
---

# Server configuration

`lacis-server` reads a YAML file, then applies `LACIS_*` environment variables
over it. Every key has a default; only `database.dsn` and `auth.session.secret`
have none and must be set.

Check a configuration without starting anything:

```bash
lacis-server validate -c /etc/lacis
```

Every command validates the configuration before running and lists all the errors
found.

## File lookup

`-c` (`--config`) takes a file or a directory; it defaults to `/etc/lacis`.

- A **file** is read as is. A path ending in `.yaml` or `.yml` that does not exist
  is an error.
- A **directory** is searched for `server.yaml`, then `server.yml`. When both exist,
  `server.yaml` wins and a warning names the ignored file.
- When no file is found, the server runs on the defaults and the environment only.
  This suits containers.

## Environment variables

Every key can be overridden by an environment variable: the key in upper case,
prefixed with `LACIS_`, dots becoming underscores.

| Key                                 | Variable                                  |
| ----------------------------------- | ----------------------------------------- |
| `database.dsn`                      | `LACIS_DATABASE_DSN`                      |
| `auth.session.secret`               | `LACIS_AUTH_SESSION_SECRET`               |
| `workers.schedules.purge_snapshots` | `LACIS_WORKERS_SCHEDULES_PURGE_SNAPSHOTS` |

Lists are comma-separated: `LACIS_AUTH_OIDC_SCOPES=openid,profile,groups`.

### Secrets from files

The secrets can be read from a file named by the variable plus `_FILE`, as Docker
and Kubernetes secrets give them. The trailing line breaks of the file are dropped.

| Key                                   | Variable naming the file                         |
| ------------------------------------- | ------------------------------------------------ |
| `database.dsn`                        | `LACIS_DATABASE_DSN_FILE`                        |
| `auth.session.secret`                 | `LACIS_AUTH_SESSION_SECRET_FILE`                 |
| `auth.local.bootstrap_admin_password` | `LACIS_AUTH_LOCAL_BOOTSTRAP_ADMIN_PASSWORD_FILE` |
| `auth.oidc.client_secret`             | `LACIS_AUTH_OIDC_CLIENT_SECRET_FILE`             |

Setting both a variable and its `_FILE` variant is an error.

## Durations and sizes

- **Durations** use the Go syntax (`30s`, `15m`, `12h`) plus a leading day count:
  `7d`, `1d12h`.
- **Sizes** are bytes, or a number with a unit: `512B`, `64KB`, `5MB`, `1GB`. The
  units are powers of 1024; `K`, `KiB`, `M`, `MiB`… are accepted too.

## Full example

```yaml title="/etc/lacis/server.yaml"
listen:
  address: 0.0.0.0:8080
  public_url: https://lacis.example.com
  base_path: /
  tls:
    cert_file: ""
    key_file: ""
database:
  dsn: postgres://lacis:change-me@localhost:5432/lacis?sslmode=disable
  max_open_conns: 20
  migrate_on_start: true
auth:
  session:
    secret: a-random-secret-of-32-characters-or-more
    ttl: 12h
    max_lifetime: 12h
    cookie_secure: true
  local:
    enabled: true
    bootstrap_admin_username: admin
    bootstrap_admin_password: change-me-please
  oidc:
    enabled: false
    issuer: https://sso.acme.example/realms/acme
    client_id: lacis
    client_secret: change-me
    scopes: [openid, profile]
    groups_claim: groups
    username_claim: preferred_username
agent_api:
  min_interval: 30s
  max_body_compressed: 5MB
  max_body_decompressed: 10MB
  accepted_schema_versions: [1]
icons:
  max_size: 256KB
  min_side: 16
  max_side: 512
graphql:
  introspection: false
  max_depth: 10
  max_complexity: 1000
graph:
  max_nodes: 500
linking:
  cdns: [cloudflare]
  cdn_ranges: []
  http_ports: [80, 443]
history:
  max_events: 100000
defaults:
  stale_after: 7d
  inactive_after: 30d
  snapshot_retention: 30d
workers:
  enabled: true
  concurrency: 4
  schedules:
    purge_snapshots: "15 3 * * *"
    purge_removed_entities: "30 3 * * *"
    purge_expired_tokens: "45 3 * * *"
    detect_token_conflicts: "0 * * * *"
    purge_history: "20 * * * *"
    purge_inactive_hosts: ""
  retention:
    completed: 24h
    cancelled: 24h
    discarded: 7d
  params:
    purge_removed_entities_after: 90d
    purge_inactive_hosts_after: 180d
mcp:
  enabled: false
  access_token_ttl: 1h
  grant_lifetime: 30d
log:
  level: info
  format: text
```

## Reference

### `listen`

| Key                    | Type   | Default        | Description |
| ---------------------- | ------ | -------------- | ----------- |
| `listen.address`       | string | `0.0.0.0:8080` | Address and port to listen on. Required. |
| `listen.public_url`    | URL    | (empty)        | How users reach the server, base path included, e.g. `https://lacis.example.com/lacis`. Required when `auth.oidc.enabled` is true: the OIDC callback is built from it. |
| `listen.base_path`     | string | `/`            | Path prefix of every route, e.g. `/lacis`. Must start with `/`. Changing it needs no rebuild of the front. |
| `listen.tls.cert_file` | path   | (empty)        | PEM certificate. With `key_file`, the server serves HTTPS itself. Each requires the other. |
| `listen.tls.key_file`  | path   | (empty)        | PEM private key. |

### `database`

| Key                         | Type   | Default | Description |
| --------------------------- | ------ | ------- | ----------- |
| `database.dsn`              | string | (none)  | PostgreSQL connection string, e.g. `postgres://lacis:secret@db.acme.example:5432/lacis?sslmode=require`. Required. A database reached over the network without TLS (`sslmode=disable` or `allow`) logs a warning at start. |
| `database.max_open_conns`   | int    | `20`    | Maximum open connections of the pool. At least 1. |
| `database.migrate_on_start` | bool   | `true`  | `start` and `worker` apply the pending migrations (Lacis, then River) before anything else. `false` leaves them to `db migrate`. |

### `auth`

| Key                                    | Type     | Default              | Description |
| -------------------------------------- | -------- | -------------------- | ----------- |
| `auth.session.secret`                  | string   | (none)               | Secret of 32 characters or more. Required. It also signs the cookie of an OIDC login in progress. |
| `auth.session.ttl`                     | duration | `12h`                | A session unused that long ends; each use pushes it back. At least `1m`. |
| `auth.session.max_lifetime`            | duration | `12h`                | A session ends that long after the login, used or not; an OIDC user then logs in again, which reads their groups again. At least `1m`. |
| `auth.session.cookie_secure`           | bool     | `true`               | Send the session cookie over HTTPS only. Set `false` only to reach the server in plain HTTP (development); the cookie stays `Secure` anyway when the server serves TLS or `public_url` is `https://`. |
| `auth.local.enabled`                   | bool     | `true`               | Login with a username and a password (`POST auth/login`). |
| `auth.local.bootstrap_admin_username`  | string   | (empty)              | When set (and local login is enabled), `start` creates this user with the `admin` role, unless a user already holds the `admin` role or has this username. An existing user, its password included, is left as is. |
| `auth.local.bootstrap_admin_password`  | string   | (empty)              | Password of the bootstrap admin, 8 to 1024 characters. Required with the username. |
| `auth.oidc.enabled`                    | bool     | `false`              | Login through an OpenID Connect provider. See [OIDC](../oidc.md). |
| `auth.oidc.issuer`                     | URL      | (empty)              | Issuer URL of the provider. Required when enabled. |
| `auth.oidc.client_id`                  | string   | (empty)              | Client ID. Required when enabled. |
| `auth.oidc.client_secret`              | string   | (empty)              | Client secret. Required when enabled. |
| `auth.oidc.scopes`                     | list     | `[openid, profile]`  | Scopes requested. |
| `auth.oidc.groups_claim`               | string   | `groups`             | Claim of the ID token listing the groups of the user. |
| `auth.oidc.username_claim`             | string   | `preferred_username` | Claim giving the username of a user created at its first login. Required when enabled. |

### `agent_api`

| Key                                  | Type        | Default | Description |
| ------------------------------------ | ----------- | ------- | ----------- |
| `agent_api.min_interval`             | duration    | `30s`   | Least time between two snapshots of an agent; a snapshot sooner gets `429 too_soon` with a `Retry-After` header. `0` accepts them all. Counted in memory, per server process. |
| `agent_api.max_body_compressed`      | size        | `5MB`   | Largest gzip body of a snapshot; above, `413 payload_too_large`. |
| `agent_api.max_body_decompressed`    | size        | `10MB`  | Largest snapshot once decompressed. Must be at least `max_body_compressed`. |
| `agent_api.accepted_schema_versions` | list of int | `[1]`   | Snapshot schema versions accepted. At least one, each at least 1. |

Besides these limits, a snapshot is refused (`422 invalid_schema`) when one of its
texts is over 8 KiB or one of its lists or maps holds more than 10,000 items.

### `icons`

Logos uploaded for technologies (`PUT icons/{tech}`): PNG, JPEG, WebP or SVG
(SVG without script nor external content). They are stored in the database.

| Key              | Type | Default | Description |
| ---------------- | ---- | ------- | ----------- |
| `icons.max_size` | size | `256KB` | Largest logo. At least `1KB`. |
| `icons.min_side` | int  | `16`    | Smallest side of a raster logo, in pixels. At least 1. |
| `icons.max_side` | int  | `512`   | Largest side of a raster logo, in pixels. At least `min_side`. |

### `graphql`

| Key                      | Type | Default | Description |
| ------------------------ | ---- | ------- | ----------- |
| `graphql.introspection`  | bool | `false` | Allow schema introspection queries. |
| `graphql.max_depth`      | int  | `10`    | Deepest query accepted. At least 1. |
| `graphql.max_complexity` | int  | `1000`  | Highest query complexity accepted. At least 1. |

### `graph`

| Key               | Type | Default | Description |
| ----------------- | ---- | ------- | ----------- |
| `graph.max_nodes` | int  | `500`   | A graph with more nodes is refused (`GRAPH_TOO_LARGE`): filter it. At least 1. |

### `linking`

| Key                  | Type         | Default     | Description |
| -------------------- | ------------ | ----------- | ----------- |
| `linking.cdns`       | list         | `[]`        | CDNs in front of your sites, by name, their IP ranges built into Lacis: `cloudflare`. A name resolving into them is linked only through a route, and the link names the CDN. An unknown name is refused at startup. See [Linking](../linking.md#cdn). |
| `linking.cdn_ranges` | list of CIDR | `[]`        | IP ranges of a CDN Lacis does not know by name, e.g. `198.51.100.0/24`, linked as `linking.cdns`. |
| `linking.http_ports` | list of int  | `[80, 443]` | Ports HTTP is served on; add `8000`, `8080`, `8443`… when needed. A name called without scheme on one of them may be routed by a webserver, and such a port of a host nobody is known to listen on leads to its webserver. At least one port, 1 to 65535. |

### `history`

| Key                  | Type | Default  | Description |
| -------------------- | ---- | -------- | ----------- |
| `history.max_events` | int  | `100000` | Change events kept per namespace, the latest ones; the task `purge_history` deletes the older ones. `0` keeps all. |

### `defaults`

Thresholds given to a namespace when it is created (by the first snapshot naming
it). They are then edited per namespace in the front.

| Key                           | Type     | Default | Description |
| ----------------------------- | -------- | ------- | ----------- |
| `defaults.stale_after`        | duration | `7d`    | A host not seen for longer is stale. At least `1h`. |
| `defaults.inactive_after`     | duration | `30d`   | A host not seen for longer is inactive, and left out of the graph by default. Must be greater than `stale_after`. |
| `defaults.snapshot_retention` | duration | `30d`   | Snapshots older than this are purged by `purge_snapshots`; the latest of each host is always kept. At least `1h`. |

### `workers`

Background jobs (snapshot normalization, link computation) and scheduled tasks run
on [River](https://riverqueue.com/), in the Lacis database.

| Key                     | Type | Default | Description |
| ----------------------- | ---- | ------- | ----------- |
| `workers.enabled`       | bool | `true`  | Run the jobs and the scheduled tasks in the `start` process. With `false`, run `lacis-server worker` apart. |
| `workers.concurrency`   | int  | `4`     | Jobs worked at once by a process. At least 1. |
| `workers.schedules.<task>` | cron | see below | Schedule of a task: five cron fields or a descriptor such as `@daily`, in the local time of the server (`TZ`). An empty string leaves the task to runs asked by hand in the administration. An unknown task name is an error. |
| `workers.retention.completed` | duration | `24h` | How long River keeps completed jobs. At least `1m`. |
| `workers.retention.cancelled` | duration | `24h` | How long River keeps cancelled jobs. At least `1m`. |
| `workers.retention.discarded` | duration | `7d`  | How long River keeps discarded jobs (failed for good). At least `1m`. |
| `workers.params.purge_removed_entities_after` | duration | `90d`  | Age of removal after which `purge_removed_entities` deletes an entity. At least `1h`. |
| `workers.params.purge_inactive_hosts_after`   | duration | `180d` | Time without snapshot after which `purge_inactive_hosts` deletes a host. At least `1h`. |

The scheduled tasks:

| Task                     | Default schedule | What it does |
| ------------------------ | ---------------- | ------------ |
| `purge_snapshots`        | `15 3 * * *`     | Deletes the snapshots out of the retention of their namespace, keeping the latest of each host. |
| `purge_removed_entities` | `30 3 * * *`     | Deletes the apps, services, webservers, runtimes and containers removed for longer than `purge_removed_entities_after`. |
| `purge_expired_tokens`   | `45 3 * * *`     | Deletes the expired enrollment tokens that enrolled no agent, the expired sessions, the expired OAuth codes and authorizations, and the OAuth clients nobody authorized within a day. |
| `detect_token_conflicts` | `0 * * * *`      | Finds the agents whose snapshots alternate between hostnames: a cloned host sharing one identity. |
| `purge_history`          | `20 * * * *`     | Deletes the change events beyond `history.max_events` in each namespace. |
| `purge_inactive_hosts`   | `""` (manual)    | Deletes the hosts not seen for longer than `purge_inactive_hosts_after`, as a deletion by hand would. |

A scheduled task runs once across all the processes, for 15 minutes at most; River
tries it 5 times with a growing backoff before giving up.

### `mcp`

The MCP endpoint, through which an AI assistant such as Claude reads the
inventory in the name of a user who authorized it. See
[AI assistants (MCP)](../mcp.md).

| Key                    | Type     | Default | Description |
| ---------------------- | -------- | ------- | ----------- |
| `mcp.enabled`          | bool     | `false` | Serves `/mcp` and the OAuth authorization server of Lacis. Requires `listen.public_url`. |
| `mcp.access_token_ttl` | duration | `1h`    | How long an access token serves; the assistant gets another with its refresh token. At least `1m`. |
| `mcp.grant_lifetime`   | duration | `30d`   | An authorization ends that long after it was given: the user authorizes the assistant again, which reads their OIDC groups again. At least `1h`. |

### `log`

| Key          | Type   | Default | Description |
| ------------ | ------ | ------- | ----------- |
| `log.level`  | string | `info`  | `debug`, `info`, `warn` or `error` (upper case accepted). The `-l` flag overrides it. |
| `log.format` | string | `text`  | `text` or `json`. |

### Reserved keys

`metrics.enabled` is reserved for a metrics endpoint that does not exist yet:
`false` (the default) is accepted, `true` is refused by the validation ("not
implemented") rather than silently doing nothing.
