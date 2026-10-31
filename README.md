# Lacis

Lacis maps a server infrastructure: hosts, services, runtimes, deployed
applications and the links between them. An agent on each host pushes a
snapshot; the server stores it, resolves the dependencies between apps and
services, and serves a navigable graph.

> Status: early development. The repository holds the foundations: the
> shared snapshot contract, the agent and server CLIs, configuration loading
> and the first HTTP routes. Collectors, storage, link resolution and the
> front come next.

## Components

| Component      | Path      | Role                                                        |
| -------------- | --------- | ----------------------------------------------------------- |
| `lacis-server` | `/`       | agent API (REST), front API (GraphQL), storage, link engine |
| `lacis-agent`  | `agent/`  | local collection, secret filtering, HTTPS push              |
| `common`       | `common/` | contract shared by both: snapshot, labels, DSN, agent API   |
| front          | `web/`    | React SPA embedded in `lacis-server`                        |

The repository holds three Go modules. `common/` and `agent/` have their own
`go.mod`; `replace` directives point to the local folders during development.
The agent and the server never import each other.

## Requirements

- Go 1.26 or later
- Docker, for the development database (`compose.yml`)

## Development

```sh
docker compose up -d          # PostgreSQL on localhost:5432
./bin/mock.sh                 # generate the gomock mocks (not versioned)

go test -race ./...                       # server
go test -race -tags integration ./...     # server, with the tests against PostgreSQL
(cd common && go test -race ./...)        # common
(cd agent && go test -race ./...)         # agent
```

The integration tests create one `lacis_test_<package>` database per test
package. They use the PostgreSQL of `compose.yml`; set
`LACIS_TEST_DATABASE_DSN` to use another one.

The front lives in `web/` (Vite, React, Tailwind, Apollo, React Flow and ELK for the graph, the
Lacis design system in `web/src/styles`). `lacis-server` embeds `web/dist`: build the front before
the server, or the server answers that the front is not built. The types of the queries come from
`graph/schema`; run `npm run codegen` after a change of the schema or of a query.

```sh
cd web
npm ci
npm run codegen   # types of the GraphQL queries
npm run dev       # http://localhost:5173, the API proxied to https://localhost:8443 (LACIS_SERVER)
npm test          # unit tests
npm run build     # type-check, then web/dist
```

The end-to-end tests (Playwright, in `web/e2e`) drive the built front in Chromium. They build
`lacis-server` with `web/dist`, drop and create again the database `lacis_e2e`
(`LACIS_E2E_DATABASE_DSN`; `psql` is needed), import and normalize `testdata/snapshots`, then start the
server on http://127.0.0.1:8643 (`LACIS_E2E_PORT`) and stop it at the end. `LACIS_E2E_SERVER` names a
server already built. What they ran lands in `web/e2e/.run`.

```sh
npx playwright install chromium   # once
npm run build && npm run e2e
```

Build with the version injected:

```sh
LDFLAGS="-X github.com/alexandreh2ag/lacis/common/version.Version=dev \
         -X github.com/alexandreh2ag/lacis/common/version.Commit=$(git rev-parse --short HEAD)"
(cd web && npm ci && npm run build)
CGO_ENABLED=0 go build -ldflags "$LDFLAGS" -o lacis-server .
(cd agent && CGO_ENABLED=0 go build -ldflags "$LDFLAGS" -o lacis-agent .)
```

## Configuration

Both binaries read `/etc/lacis/<name>.yaml` or `/etc/lacis/<name>.yml`
(`agent` or `server`); when both exist, `.yaml` wins and a warning is logged.
`-c` takes a file or a directory. Every key can be overridden by an
environment variable prefixed with `LACIS_`, dots becoming underscores:
`labels.env` → `LACIS_LABELS_ENV`. Lists are comma-separated.

### Agent

```yaml
server:
  url: https://lacis.example.com      # HTTPS only, base path included
  ca_file: /etc/lacis/ca.pem          # optional
  insecure: false                     # true skips certificate verification (dev only)
auth:
  enrollment_token: lacis_et_…        # from "lacis-server token create", used once
lacis-server snapshot import -c /etc/lacis testdata/snapshots  # inject snapshots without agent (dev, tests)
  state_dir: /var/lib/lacis
labels:                               # required: namespace, project, env
  namespace: acme
  project: site
  env: prod
interval: 1h
jitter: 5m
```

```sh
lacis-agent start -c /etc/lacis     # enroll when needed, then send a snapshot every interval + jitter
lacis-agent enroll -c /etc/lacis    # once: identity kept in state_dir/identity.json (0600)
lacis-agent collect -c /etc/lacis   # print the snapshot that would be sent
lacis-agent version
```

### Server

```yaml
listen:
  address: 0.0.0.0:8080
  public_url: https://lacis.acme.example   # how the users reach it, base path included; required by oidc
  base_path: /                        # e.g. /lacis, no rebuild needed
database:
  dsn: postgres://lacis:lacis@localhost:5432/lacis?sslmode=disable
auth:
  session:
    secret: change-me-32-characters-minimum
  local:
    bootstrap_admin_username: admin   # created at start when no user has this username
    bootstrap_admin_password: change-me-please  # 8 characters minimum
  oidc:                               # optional login through an OpenID Connect provider
    enabled: false
    issuer: https://sso.acme.example/realms/acme
    client_id: lacis
    client_secret: change-me
    scopes: [openid, profile]
    groups_claim: groups              # the claim listing the groups of the user
    username_claim: preferred_username  # the claim giving the username
    admin_groups: [lacis-admins]      # these groups hold every permission
icons:                                # logos uploaded for technologies (traefik, redis…), stored in the database
  max_size: 256KB                     # PNG, JPEG, WebP or SVG (SVG without script nor external content)
  min_side: 16                        # each side of a raster logo, in pixels
  max_side: 512
workers:
  enabled: true                       # jobs and scheduled tasks in the start process; false leaves them to "worker"
  concurrency: 4
  schedules:                          # cron in the local time of the server, or @daily…; "" = only run by hand
    purge_snapshots: "15 3 * * *"     # snapshots out of the namespace retention, the latest of each host kept
    purge_removed_entities: "30 3 * * *"  # apps, services, webservers, runtimes removed for longer than below
    purge_expired_tokens: "45 3 * * *"    # enrollment tokens expired that enrolled no agent, sessions expired
    detect_token_conflicts: "0 * * * *"   # agents whose snapshots alternate between hostnames: a cloned host
    purge_inactive_hosts: ""          # hosts not seen for longer than below, as a deletion by hand
  params:
    purge_removed_entities_after: 90d
    purge_inactive_hosts_after: 180d
log:
  level: info
  format: text                        # or json
```

Durations accept days (`7d`, `1d12h`) and sizes accept units (`5MB`).

```sh
lacis-server validate -c /etc/lacis
lacis-server db migrate -c /etc/lacis   # Lacis migrations, then River's
lacis-server user create -c /etc/lacis --username admin --first-name Ada --last-name Lovelace   # role admin, password prompted
lacis-server token create -c /etc/lacis --label web --ttl 7d  # any namespace, --ttl 0 never expires; prints the token alone on stdout
lacis-server start -c /etc/lacis    # migrates when migrate_on_start, then serves under {base_path}:
                                    #   GET healthz, POST api/v1/agent/enroll, POST api/v1/agent/snapshot,
                                    #   POST auth/login, POST auth/logout, POST graphql,
                                    #   GET, PUT, DELETE icons/{tech},
                                    #   GET auth/oidc/login and auth/oidc/callback with auth.oidc
lacis-server worker -c /etc/lacis   # works the jobs and runs the scheduled tasks without serving the API
                                    #   (with workers.enabled false on start); several workers share the jobs
lacis-server version
```

A scheduled task runs once across the processes, for 15 minutes at most, and River tries it 5
times with a growing backoff before giving up. The administration lists the tasks, their last runs
with what they changed, and the jobs failed; it runs a task now or a failed job again (global
`tasks` permission).

## Front API

The front logs in with `POST auth/login` (`{"username": …, "password": …}`), which sets an HttpOnly
session cookie, then queries `POST graphql` with a JSON body; `POST auth/logout` closes the
session.

With `auth.oidc`, the front sends the browser to `GET auth/oidc/login?redirect=<page>`: the
authorization code flow with PKCE runs against the provider, which comes back to
`<public_url>/auth/oidc/callback`, the redirect URI to register at the provider. The callback opens
the session and goes back to the page, or to `login?error=<code>` of the front: `oidc_unavailable`
(the provider does not answer), `oidc_expired` (the login took more than 10 minutes or was
replayed), `oidc_denied`, `oidc_failed`, `oidc_account`. A user is found by the subject of the
provider, else created without role, its username read from `username_claim` and its names from
`given_name` and `family_name`; a username already taken by another account refuses the login
(`oidc_account`): no provider vouches for a username, so a local account is never tied to one. The groups of the ID token give roles through the OIDC groups of the roles,
read at each request; the groups of `admin_groups` hold every permission while the configuration
names them. The schema lives in `graph/schema/*.graphqls`; after a change, regenerate the code with
`go tool gqlgen generate`. A user reads the namespaces their roles give them; the `admin` role
reads them all. `namespace { graph(level: PROJECT | HOST | APP, filter) }` serves the graph
at the `project`, `host` and `app` views above; it leaves the inactive hosts out unless
`filter.status` asks for them. `filter.expand` opens project, host or stack nodes with their
content: their apps, services, webservers and runtimes, merged by kind and name, in the group of
the node's id. The front reads the graph through two lenses, Projets (`PROJECT`) and Infra
(`HOST`): a click opens a node in place, one at a time, and the Impact mode shows what depends
on a node, or what it needs, as `namespace { impact(level, filter, node, direction) }` walks it:
through a webserver, it only follows the routes of the names called.

The mutations change the metadata, never what the agents collect: `updateNamespace` (name,
description, freshness thresholds), `updateProject`, `updateEnvironment`,
`reorderEnvironments`, `updateNetwork`, `splitNetwork`, `moveHostsToNetwork`, `deleteNetwork` need
the `metadata` `write` permission on the namespace;
`deleteHost` removes a stale or inactive host gone for good, with its content, and needs `inventory`
`write`. A refusal carries a code in its extensions: `NOT_FOUND`, `FORBIDDEN`, `BAD_USER_INPUT` or
`CONFLICT` (an active host, which its next snapshot would bring back).

The network of an IP is the one whose CIDR holds it, so two private networks on the same range are
one. `splitNetwork` moves hosts into a new network sharing the range, and the links are computed
again: an IP held by a host of each network is no longer ambiguous. The hosts stay there across
their snapshots; `moveHostsToNetwork` moves them between the networks of the range, and
`deleteNetwork` removes a split network, its hosts going back.

The front shows the logo of a technology from Simple Icons, or its initials. A logo uploaded with
`PUT icons/{tech}` (a multipart body whose `file` field holds the image) shows in its place, or gives
one to a technology without; `DELETE icons/{tech}` resets it. Both need the global `icons` `write`
permission. The format is read from the content and must match the extension; a refusal answers 400
or 413 with a `reason`: `too_big`, `format`, `dimension`, `unsafe` or `tech`. `techIcons` lists them
to every user, at `icons/{tech}?v={version}`, cached for good, and with a sandboxing
Content-Security-Policy; `technologies` lists those of the inventory.

```sh
curl -c jar -H 'Content-Type: application/json' -d '{"username":"admin","password":"…"}' \
  http://localhost:8080/auth/login
curl -b jar -H 'Content-Type: application/json' \
  -d '{"query":"{ namespace(slug: \"acme\") { hosts { total items { hostname status } } } }"}' \
  http://localhost:8080/graphql
```

## Fictitious data

`testdata/snapshots` holds the 25 snapshots of a fictitious namespace `acme`, with a `production`
(`10.0.1.0/24`) and a `staging` (`10.0.2.0/24`) env:

- a Traefik reverse proxy with a public IP, the entry of both envs;
- per env: MongoDB, MariaDB, PostgreSQL, RabbitMQ, Redis and Elasticsearch; in prod, MongoDB is a
  replica set of 3 nodes and MariaDB a cluster of 3 nodes behind an HAProxy;
- the projects `shop` (Symfony API and Next.js front, on 2 load-balanced nodes in prod), `crm`
  (Symfony API and its workers) and `site` (Zend 1 on Debian 10 and Apache);
- the Symfony apps use PostgreSQL, MongoDB, RabbitMQ, Redis and Elasticsearch, and the external
  S3 and Sentry; the Next.js front calls the API by its public domain; the Zend site uses Redis and
  MariaDB.
- every link resolution case: a local Redis on a unix socket and one in Docker on the loopback, a
  host reached by its hostname, a public IP without route, a private IP without agent, a name the
  DNS cannot resolve, media behind a CDN (declare `104.16.0.0/13` in `linking.cdn_ranges`), and an
  unparsable value.

Import them in a local database:

```sh
docker compose up -d
lacis-server snapshot import -c tmp testdata/snapshots
```

The workers of `start` normalize them, then compute the links of the namespace at the end of a
30 seconds window. Without the workers, `snapshot normalize` does both in the process:

```sh
lacis-server snapshot normalize -c tmp
```

Then export the graph, to check the links without the front: Mermaid (paste it in
[mermaid.live](https://mermaid.live)), DOT for Graphviz, or JSON as the API serves it. `--project`,
`--env` and `--host` keep their nodes and the nodes linked to them. The views:

- `service` (default) draws every app, service and webserver, boxed by host;
- `app` folds the webserver of a host and the services only it reaches, such as php-fpm, into the
  apps it serves; only the proxies routing to other hosts stay nodes;
- `host` merges the content of each host and stacks the similar hosts (same project, env, apps,
  services and webservers), telling the apps a stack runs in several versions; a host named by
  `--host` leaves its stack;
- `project` draws a node per project and env, the entry of the front.

At the `host` and `project` views, the external targets of an env merge into one node. The edges leaving a node share its color; `--labels` writes on them their keys or
domains. For a large graph, DOT gives the clearest drawing, and Mermaid draws better with
`--layout elk` (mermaid.live has it).

```sh
lacis-server graph export -c tmp --namespace acme --env production --layout elk
lacis-server graph export -c tmp --namespace acme --format dot | dot -Tsvg > acme.svg
lacis-server graph export -c tmp --namespace acme --view host --format json -o acme.json
```

Each normalization and each computation of the links writes what changed in the history of the
namespace: a host, runtime, service, webserver or app added, removed or changed (a version, the code
deployed, the kernel), a link added or removed. The first snapshot of a host gives a single event,
the host added; the first computation of the links of a namespace gives none. Each namespace keeps
its latest `history.max_events` events (100000 by default, 0 keeps all); the older ones are purged as
new ones are written. List the history, the latest first, or the oldest first with `--oldest-first` (the
Historique page has the same choice):

```sh
lacis-server history -c tmp --namespace acme
lacis-server history -c tmp --namespace acme --host acme-shop-prod-1 --since 7d
lacis-server history -c tmp --namespace acme --type link --format json
lacis-server history -c tmp --namespace acme --since 1d --oldest-first --limit 0
```

## License

[MIT](LICENSE)
