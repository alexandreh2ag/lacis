# Lacis

Lacis maps a server infrastructure: hosts, services, runtimes, deployed
applications and the links between them. An agent on each host pushes a
snapshot; the server stores it, resolves the dependencies between apps and
services, and serves a navigable graph.

> Status: early development, not released yet. The agent collects and pushes its snapshots, the
> server stores and normalizes them, resolves the links and serves the front: the graph, the
> pages of the hosts, apps and projects, the history and the administration. The API and the
> configuration may still change.

## Why "Lacis"

**English.** In French, a *lacis* is a network of interlaced threads or fibres,
like the fine mesh of a net or of a tissue; in English the word names a lace
made by darning a knotted net. Lacis maps exactly that: hosts, apps and
services held together by the threads of their dependencies. The logo, a
small mesh of nodes, draws it.

**Français.** Un *lacis* est un réseau de fils ou de fibres entrelacés, comme
les mailles fines d'un filet ou d'un tissu. Lacis cartographie justement ce
réseau : des hosts, des apps et des services reliés par les fils de leurs
dépendances. Le logo, une petite maille de nœuds, le dessine.

## Components

| Component      | Path      | Role                                                        |
| -------------- | --------- | ----------------------------------------------------------- |
| `lacis-server` | `/`       | agent API (REST), front API (GraphQL), storage, link engine |
| `lacis-agent`  | `agent/`  | local collection, secret filtering, push over HTTP(S)       |
| `common`       | `common/` | contract shared by both: snapshot, labels, DSN, agent API   |
| front          | `web/`    | React SPA embedded in `lacis-server`                        |

The repository holds three Go modules. `common/` and `agent/` have their own
`go.mod`; `replace` directives point to the local folders during development.
The agent and the server never import each other.

## Requirements

- Go 1.26 or later
- Node.js 22 (20.19 at least) and npm, to build the front
- Docker, for the development database (`compose.yml`)
- `psql`, for the end-to-end tests

## Development

```sh
docker compose up -d          # PostgreSQL on 127.0.0.1:5432
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
         -X github.com/alexandreh2ag/lacis/common/version.Commit=$(git rev-parse --short HEAD) \
         -X github.com/alexandreh2ag/lacis/common/version.Date=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
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
  url: https://lacis.example.com      # base path included; http:// accepted, with a warning
  ca_file: /etc/lacis/ca.pem          # optional
  insecure: false                     # true skips certificate verification (dev only)
auth:
  enrollment_token: lacis_et_…        # from "lacis-server token create", used once
  state_dir: /var/lib/lacis
labels:                               # required: namespace, project, env
  namespace: acme
  project: site
  env: prod
interval: 1h                          # durations accept days, as on the server: 1d, 1d12h
jitter: 5m
collectors:                           # optional, defaults shown
  apps:
    scan_paths: [/var/www]
    exclude: [vendor, node_modules, var/cache, var/log, log, logs, media, uploads,
              public/media, public/uploads, storage, .git/objects]  # whole names, or runs of them
                                      # (var/cache), matched anywhere in the path
    max_depth: 4                      # directories walked down a scan path
    max_files: 5000                   # entries read per scan path: the scan stops there, with a warning
    git_max_depth: 0                  # parent directories of an app root searched for its .git (0: the root only)
    commands: path                    # path: only the program of a launcher command (argv[0]) is sent;
                                      # full: the whole command, arguments (and any password in them) included
    frameworks:                       # declarative detectors; a name of a built-in one replaces it
      - name: remix
        detect: { npm_package: "@remix-run/node" }   # or composer_package, file
        config:                       # sources in load order, relative to the app root, the last file winning
          - { path: .env, format: env }
          - { path: ".env.${NODE_ENV}", format: env } # a variable read so far (NODE_ENV defaults to production)
          - { path: "config/*.yaml", format: yaml }    # also php, ini, json: nested keys flattened (see below)
    extra_config:                     # files added to the apps whose path (as seen or resolved) matches the glob,
      - app: "/var/www/*/current"     # read after those of their framework, the launcher environment still last
        config:
          - { path: /etc/acme/shop.env, format: env }  # absolute, or relative to the app root; globs accepted
          - { path: config/extra.yaml, format: yaml }  # env, php, ini, yaml or json
  envfiles:                           # keys compared case-insensitively
    allow_keys: ["*_URL", "*_DSN", "*_HOST", "*_PORT", APP_ENV, NODE_ENV]
    deny_keys: ["*SECRET*", "*PASS*", "*PWD*", "*TOKEN*", "*KEY*", "*CRED*", "*AUTH*", "*PRIVATE*", "*SALT*", "*COOKIE*", "*WEBHOOK*"]
```

Built-in framework loaders: symfony, next, laravel, nuxt, nestjs (.env files), zend1
(`application.ini`), laminas and zend (`.env`, then `config/autoload/{,*.}global.php` and
`{,*.}local.php`, local over global). A php, ini, yaml or json source is flattened: its nested keys
are joined with `_` and upper-cased (`doctrine.dbal.url` gives `DOCTRINE_DBAL_URL`, its `key_path`
kept), then filtered by `allow_keys`/`deny_keys` like a .env key. PHP files are parsed, never run:
literal arrays, `getenv('X')`, `$_ENV['X']`, `env('X', default)` and `?:`/`??` defaults; any other
expression is skipped. A Symfony `%env(X)%` (with the `resolve`, `int`, `default`… processors) reads
X. An app file may be a symbolic link (Capistrano and Deployer link `releases/X/.env` to
`shared/.env`): up to 5 links are followed, the target must be a regular file.

A supervisor program running `gtask schedule start|run -c <file>` reports the ids and cron
expressions of the `scheduled` tasks of that file (never their commands), when it lies in the app.

Values sent from a URL are its scheme, host and port; the path is only kept as a resource name
(database, Redis index, AMQP vhost) for database and broker schemes.

The services include the object stores MinIO and Silo (its fork by Pigsty). With Docker, every
container, running or not, is sent (name, image, version, state, ports, networks, Compose project
and service, never its environment); the server keeps them per host and shows them in the
Containers tab of the host page, without change events.

After a transient failure (network, `5xx`, `429`) the agent retries with a backoff from 10 seconds
to 5 minutes, or after the `Retry-After` of the server (seconds or HTTP date, 5 minutes at most).

Optional TLS, token and Traefik keys:

```yaml
server:
  ca_only: true                       # trust ca_file only, not the system roots
  cert_file: /etc/lacis/agent.crt     # client certificate (mutual TLS), with key_file
  key_file: /etc/lacis/agent.key
auth:
  enrollment_token_file: /etc/lacis/enrollment-token  # instead of enrollment_token (a world-readable agent.yaml holding the token is warned about)
collectors:
  traefik:
    api_url: http://127.0.0.1:8080    # its entrypoints are reported as the webserver addresses
    username: lacis                   # optional basic auth
    password: change-me
    ca_file: /etc/lacis/traefik-ca.pem  # optional, for an HTTPS API; ca_only and insecure as for server
```

The agent never follows a redirect of the server nor of the Traefik API, and
does not use the proxy of the environment for the Traefik API.

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
  tls:                                # optional: both set, the server serves HTTPS itself
    cert_file: /etc/lacis/server.crt
    key_file: /etc/lacis/server.key
database:
  dsn: postgres://lacis:lacis@localhost:5432/lacis?sslmode=disable
  max_open_conns: 20
  migrate_on_start: true              # run the migrations at start; false leaves them to "db migrate"
auth:
  session:
    secret: change-me-32-characters-minimum
    ttl: 12h                          # a session unused that long ends; each use pushes it back
    max_lifetime: 12h                 # a session ends that long after the login, used or not (OIDC groups read again)
    cookie_secure: true               # HTTPS only; false only to reach the server in plain HTTP (dev)
  local:
    enabled: true                     # login with a username and a password (POST auth/login)
    bootstrap_admin_username: admin   # created at start when no user holds the admin role nor has this username
    bootstrap_admin_password: change-me-please  # 8 to 1024 characters
  oidc:                               # optional login through an OpenID Connect provider
    enabled: false
    issuer: https://sso.acme.example/realms/acme
    client_id: lacis
    client_secret: change-me
    scopes: [openid, profile]
    groups_claim: groups              # the claim listing the groups of the user
    username_claim: preferred_username  # the claim giving the username
agent_api:
  min_interval: 30s                   # least time between two snapshots of an agent, sooner gets 429; 0 = none
  max_body_compressed: 5MB
  max_body_decompressed: 10MB
  accepted_schema_versions: [1]
graphql:
  introspection: false                # schema introspection queries
  max_depth: 10                       # deeper queries are refused
  max_complexity: 1000                # a graph or impact field costs far more than a plain field
graph:
  max_nodes: 500                      # a larger graph is refused (GRAPH_TOO_LARGE): filter it
icons:                                # logos uploaded for technologies (traefik, redis…), stored in the database
  max_size: 256KB                     # PNG, JPEG, WebP or SVG (SVG without script nor external content)
  min_side: 16                        # each side of a raster logo, in pixels
  max_side: 512
linking:
  cdns: []                            # CDNs known by name, ranges built in: cloudflare. A name resolving into them
                                      #   links only through a route, and the link names the CDN
  cdn_ranges: []                      # IP ranges of another CDN, e.g. 198.51.100.0/24, linked the same way
  http_ports: [80, 443]               # ports HTTP is served on, e.g. add 8000, 8080, 8443: a name called without
                                      #   scheme on them may be routed by a webserver (Host() or server_name), and
                                      #   such a port of a host nobody is known to listen on leads to its webserver
history:
  max_events: 100000                  # change events kept per namespace, the latest; 0 keeps all
mcp:                                  # MCP endpoint for AI assistants (Claude…), behind the OAuth server of Lacis
  enabled: false                      # requires listen.public_url
  access_token_ttl: 1h                # the assistant renews it with its refresh token
  grant_lifetime: 30d                 # then the user authorizes the assistant again
defaults:                             # given to a namespace at its creation
  stale_after: 7d                     # a host not seen for longer is stale
  inactive_after: 30d                 # then inactive, left out of the graph; more than stale_after
  snapshot_retention: 30d             # snapshots older are purged, the latest of each host kept
workers:
  enabled: true                       # jobs and scheduled tasks in the start process; false leaves them to "worker"
  concurrency: 4
  schedules:                          # cron in the local time of the server, or @daily…; "" = only run by hand
    purge_snapshots: "15 3 * * *"     # snapshots out of the namespace retention, the latest of each host kept
    purge_removed_entities: "30 3 * * *"  # apps, services, webservers, runtimes, containers removed for longer than below
    purge_expired_tokens: "45 3 * * *"    # enrollment tokens expired that enrolled no agent, sessions and OAuth expired
    detect_token_conflicts: "0 * * * *"   # agents whose snapshots alternate between hostnames: a cloned host
    purge_history: "20 * * * *"       # change events beyond history.max_events of each namespace
    purge_inactive_hosts: ""          # hosts not seen for longer than below, as a deletion by hand
    purge_unused_projects: ""         # projects no host and no app (removed ones aside) names any more
    purge_unused_environments: ""     # environments no host and no app (removed ones aside) names any more
  retention:                          # how long River keeps the finished jobs
    completed: 24h
    cancelled: 24h
    discarded: 7d                     # failed for good
  params:
    purge_removed_entities_after: 90d
    purge_inactive_hosts_after: 180d
log:
  level: info
  format: text                        # or json
```

Durations accept days (`7d`, `1d12h`) and sizes accept units (`5MB`). `metrics.enabled` is
reserved: there is no metrics endpoint yet, and `true` is refused by the validation.

The secrets can be read from a file named by `<VARIABLE>_FILE`, as Docker and Kubernetes secrets
give them: `LACIS_DATABASE_DSN_FILE`, `LACIS_AUTH_SESSION_SECRET_FILE`,
`LACIS_AUTH_LOCAL_BOOTSTRAP_ADMIN_PASSWORD_FILE`, `LACIS_AUTH_OIDC_CLIENT_SECRET_FILE` (setting
both the variable and its `_FILE` is an error). A database reached over the network without TLS
(`sslmode=disable`) logs a warning at start.

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
lacis-server snapshot import -c /etc/lacis testdata/snapshots  # inject snapshots without agent (dev, tests)
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
read at each request. The schema lives in `graph/schema/*.graphqls`; after a change, regenerate the code with
`go tool gqlgen generate`. A user reads the namespaces their roles give them; the `admin` role
reads them all. `namespace { graph(level: PROJECT | HOST | APP, filter) }` serves the graph
at the `project`, `host` and `app` views above; it leaves the inactive hosts out unless
`filter.status` asks for them. `filter.expand` opens project, host or stack nodes with their
content: their apps, services, webservers and runtimes, merged by kind and name, in the group of
the node's id. The front reads the graph through two lenses, Projects (`PROJECT`) and Infra
(`HOST`): a click opens a node in place, one at a time, and the Impact mode shows what depends
on a node, or what it needs, as `namespace { impact(level, filter, node, direction) }` walks it:
through a webserver, it only follows the routes of the names called.

The mutations change the metadata, never what the agents collect: `updateNamespace` (name,
description, freshness thresholds), `updateProject`, `updateEnvironment`,
`reorderEnvironments`, `updateNetwork`, `splitNetwork`, `moveHostsToNetwork`, `deleteNetwork` need
the `metadata` `write` permission on the namespace;
`deleteHost` removes a stale or inactive host gone for good, with its content, and needs `inventory`
`write`. A refusal carries a code in its extensions: `NOT_FOUND`, `FORBIDDEN`, `BAD_USER_INPUT` or
`CONFLICT` (an active host, which its next snapshot would bring back). A refusal a client can tell
in its own words also carries its `reason` (`PASSWORD_TOO_SHORT`, `HOST_NOT_IN_RANGE`…) and the
values it names in `args`; the front translates by them, never by the message.

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
  DNS cannot resolve, media behind a CDN (set `linking.cdns: [cloudflare]`), and an
  unparsable value.

It also holds 4 snapshots of a smaller namespace `globex` (`10.1.1.0/24` and `10.1.2.0/24`), to
have a second namespace: the Laravel project `portal` in `production` (MariaDB on a shared host,
local Redis) and in `staging` (local MariaDB), and the NestJS API `billing`, which uses the shared
MariaDB, calls `portal` by its hostname, and Stripe.

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
its latest `history.max_events` events (100000 by default, 0 keeps all); the scheduled task
`purge_history` (every hour by default) deletes the older ones, so a namespace may hold more between
two runs. List the history, the latest first, or the oldest first with `--oldest-first` (the
History page has the same choice):

```sh
lacis-server history -c tmp --namespace acme
lacis-server history -c tmp --namespace acme --host acme-shop-prod-1 --since 7d
lacis-server history -c tmp --namespace acme --type link --format json
lacis-server history -c tmp --namespace acme --since 1d --oldest-first --limit 0
```

## License

[MIT](LICENSE)
