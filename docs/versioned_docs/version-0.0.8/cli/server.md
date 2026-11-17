---
sidebar_position: 1
title: lacis-server
---

# `lacis-server`

## Global flags

| Flag            | Default      | Description |
| --------------- | ------------ | ----------- |
| `-c`, `--config` | `/etc/lacis` | Configuration file, or directory holding `server.yaml` or `server.yml`. |
| `-l`, `--level`  | (empty)      | Log level: `DEBUG`, `INFO`, `WARN`, `ERROR`. Overrides `log.level`. |

Every command but `version` loads and validates the configuration first.

## `start`

Serves the API and the front until `SIGINT` or `SIGTERM`.

```bash
lacis-server start -c /etc/lacis
```

At start, it applies the migrations when `database.migrate_on_start` is `true`,
starts the workers and the scheduled tasks when `workers.enabled` is `true`, and
creates the bootstrap admin of `auth.local` when needed. It then serves, under
`listen.base_path`:

| Route | Purpose |
| ----- | ------- |
| `GET healthz` | health check |
| `POST api/v1/agent/enroll` | agent enrollment (bearer enrollment token) |
| `POST api/v1/agent/snapshot` | agent snapshot, gzip JSON (bearer agent token) |
| `POST auth/login`, `POST auth/logout` | local login, logout |
| `GET auth/oidc/login`, `GET auth/oidc/callback` | OIDC login, with `auth.oidc` |
| `POST graphql` | GraphQL API of the front |
| `GET`, `PUT`, `DELETE icons/{tech}` | technology logos |
| everything else | the web front |

A shutdown waits up to 10 seconds for the requests in flight. The bodies of the
front's requests are limited to 1 MiB (more for a logo upload, up to
`icons.max_size`); the agent API has its own limits, `agent_api.*`.

## `worker`

Works the jobs (normalization, links) and runs the scheduled tasks until `SIGINT`
or `SIGTERM`, without serving the API. Use it with `workers.enabled: false` on
`start`. Several workers share the jobs; each scheduled run happens once.

```bash
lacis-server worker -c /etc/lacis
```

It applies the migrations first when `database.migrate_on_start` is `true`.

## `validate`

Validates the configuration without starting anything, and prints
`configuration is valid` or every error found.

```bash
lacis-server validate -c /etc/lacis
```

## `version`

Prints the version, the commit and the build date:
`1.0.0 (commit: abc1234, built: 2026-11-01T10:00:00Z)`. Needs no configuration.

## `db migrate`

Applies the pending migrations of Lacis, then those of River.

```bash
lacis-server db migrate -c /etc/lacis
```

## `user create`

Creates a local user. The password, 8 to 1024 characters, is prompted twice on a
terminal, otherwise read from the first line of the standard input. The username is
stored in lower case.

| Flag           | Default | Description |
| -------------- | ------- | ----------- |
| `--username`   | (none)  | Username, used as login. Required. |
| `--first-name` | (empty) | First name. |
| `--last-name`  | (empty) | Last name. |
| `--role`       | `admin` | Role code; repeatable or comma-separated. |

```bash
lacis-server user create -c /etc/lacis --username admin
echo "$PASSWORD" | lacis-server user create -c /etc/lacis \
  --username bob --first-name Bob --last-name Martin --role acme-viewer
```

## `token create`

Creates an enrollment token, valid for every namespace (the agent's `namespace`
label picks it). The token is printed alone on stdout, once: only its hash is
stored. Its expiry is printed on stderr.

| Flag      | Default | Description |
| --------- | ------- | ----------- |
| `--label` | (empty) | Free label, e.g. what the token is for. |
| `--ttl`   | `7d`    | Lifetime, e.g. `12h`, `7d`, `1d12h`; `0` never expires. |

```bash
lacis-server token create -c /etc/lacis --label web --ttl 1d
lacis-server token create -c /etc/lacis --label ci --ttl 0
```

## `snapshot import`

Imports snapshot files as if their agents sent them, for development and tests: an
agent is created on the first import of a host, and reused after. A directory gives
its `*.json` files. The workers of `start` normalize them.

```bash
lacis-server snapshot import -c /etc/lacis testdata/snapshots
lacis-server snapshot import -c /etc/lacis /tmp/snapshot.json
```

Each file prints `<hostname> (<namespace>): accepted|unchanged[, agent created]`.

## `snapshot normalize`

Normalizes again, in this process, the latest snapshot of every host, then computes
the links of every namespace. Use it after an upgrade changing the normalization or
the linking, or to normalize imported snapshots without running the workers.

```bash
lacis-server snapshot normalize -c /etc/lacis
```

## `graph export`

Exports the graph of a namespace as the links were last computed: Mermaid and DOT
to draw it, JSON as the API serves it. The filters keep the nodes of their
projects, envs and hosts, and the nodes linked to them.

| Flag                | Default   | Description |
| ------------------- | --------- | ----------- |
| `-n`, `--namespace` | (none)    | Slug of the namespace. Required. |
| `-f`, `--format`    | `mermaid` | `mermaid`, `dot` or `json`. |
| `--view`            | `service` | `service`, `host`, `project` or `app`, see below. |
| `--project`         | (all)     | Keep these projects (slugs; repeatable). |
| `--env`             | (all)     | Keep these envs (slugs; repeatable). |
| `--host`            | (all)     | Keep these hosts (hostnames; repeatable). A host named leaves its stack. |
| `-o`, `--output`    | stdout    | File to write. |
| `--labels`          | `false`   | Write on each edge its keys or domains. |
| `--layout`          | `dagre`   | Mermaid layout: `dagre` or `elk` (better for large graphs). |

The views:

- `service` draws every app, service and webserver, boxed by host;
- `app` folds the webserver of a host and the services only it reaches (such as
  php-fpm) into the apps it serves; only the proxies routing to other hosts stay
  nodes;
- `host` merges the content of each host and stacks the similar hosts;
- `project` draws a node per project and env.

```bash
lacis-server graph export -c /etc/lacis --namespace acme > acme.mmd
lacis-server graph export -c /etc/lacis --namespace acme --env production --format dot | dot -Tsvg > prod.svg
lacis-server graph export -c /etc/lacis --namespace acme --view host --format json -o acme.json
```

## `history`

Lists the changes of a namespace, the latest first: hosts, runtimes, services,
webservers and apps added, removed or changed by their snapshots, and links added
or removed by their computation.

| Flag                | Default | Description |
| ------------------- | ------- | ----------- |
| `-n`, `--namespace` | (none)  | Slug of the namespace. Required. |
| `--host`            | (all)   | Keep the changes of this host (hostname). |
| `--type`            | (all)   | Keep one kind of entity: `host`, `runtime`, `service`, `webserver`, `app`, `link`. |
| `--since`           | (all)   | Keep the changes of this last duration, e.g. `12h`, `7d`. |
| `--limit`           | `50`    | Number of changes listed; `0` lists all. |
| `--oldest-first`    | `false` | List in the order they occurred. |
| `-f`, `--format`    | `text`  | `text` or `json`. |

```bash
lacis-server history -c /etc/lacis --namespace acme
lacis-server history -c /etc/lacis --namespace acme --host acme-shop-prod-1 --since 7d
lacis-server history -c /etc/lacis --namespace acme --type link --format json
```
