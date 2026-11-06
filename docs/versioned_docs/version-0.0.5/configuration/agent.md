---
sidebar_position: 2
title: Agent
---

# Agent configuration

`lacis-agent` reads a YAML file, then applies `LACIS_*` environment variables over
it. Only `server.url` and the three required labels have no default.

`lacis-agent collect` validates the configuration and prints the snapshot that
would be sent, without sending it. Every command except `version` refuses an
invalid configuration and lists all the errors found.

## File lookup

`-c` (`--config`) takes a file or a directory; it defaults to `/etc/lacis`.

- A **file** is read as is. A path ending in `.yaml` or `.yml` that does not exist
  is an error.
- A **directory** is searched for `agent.yaml`, then `agent.yml`. When both exist,
  `agent.yaml` wins and a warning names the ignored file.
- When no file is found, the agent runs on the defaults and the environment only.

When the configuration file holds `auth.enrollment_token` and is readable by every
user, the agent logs a warning: `chmod 600` it, or move the token to
`auth.enrollment_token_file`.

## Environment variables

Every key can be overridden by an environment variable: the key in upper case,
prefixed with `LACIS_`, dots becoming underscores. Lists are comma-separated.

```bash
LACIS_SERVER_URL=https://lacis.example.com
LACIS_LABELS_ENV=staging
LACIS_COLLECTORS_DISABLED=docker,envfiles
```

## Durations

Durations use the Go syntax (`90s`, `30m`, `1h`, `36h`) plus a leading day count:
`1d`, `1d12h`. The agent and the server parse them the same way; a day is 24 hours.

## Full example

```yaml title="/etc/lacis/agent.yaml"
server:
  url: https://lacis.example.com      # base path included; http:// accepted, with a warning
  ca_file: /etc/lacis/ca.pem          # optional
  ca_only: false
  cert_file: /etc/lacis/agent.crt     # optional client certificate (mutual TLS)
  key_file: /etc/lacis/agent.key
  insecure: false
auth:
  enrollment_token_file: /etc/lacis/enrollment-token   # or enrollment_token: lacis_et_…
  state_dir: /var/lib/lacis
hostname: ""                          # override of the machine hostname
labels:
  namespace: acme
  project: shop
  env: production
  index: "1"
  role: front
interval: 1h
jitter: 5m
collectors:
  timeout: 30s
  disabled: []
  apps:
    scan_paths: [/var/www]
    max_depth: 4
    max_files: 5000
    git_max_depth: 0
    commands: path
    exclude: [vendor, node_modules, var/cache, var/log, log, logs, media, uploads,
              public/media, public/uploads, storage, .git/objects]
    frameworks:
      - name: remix
        detect:
          npm_package: "@remix-run/node"
        config:
          - path: .env
            format: env
          - path: ".env.${NODE_ENV}"
            format: env
          - path: "config/*.yaml"
            format: yaml
    extra_config:
      - app: "/var/www/*/current"
        config:
          - path: /etc/acme/shop.env
            format: env
          - path: config/extra.php
            format: php
  envfiles:
    allow_keys: ["*_URL", "*_DSN", "*_HOST", "*_PORT", APP_ENV, NODE_ENV]
    deny_keys: ["*SECRET*", "*PASS*", "*PWD*", "*TOKEN*", "*KEY*", "*CRED*", "*AUTH*",
                "*PRIVATE*", "*SALT*", "*COOKIE*", "*WEBHOOK*"]
  traefik:
    api_url: http://127.0.0.1:8080
    username: lacis
    password: change-me
    ca_file: /etc/lacis/traefik-ca.pem
    ca_only: false
    insecure: false
  docker:
    socket: /var/run/docker.sock
```

## Reference

### `server`

| Key                | Type | Default | Description |
| ------------------ | ---- | ------- | ----------- |
| `server.url`       | URL  | (none)  | Base URL of `lacis-server`, base path included, e.g. `https://lacis.example.com/lacis`. Required; `https://`, or `http://` for a server without TLS: the agent then logs a warning, the tokens and snapshots travelling in clear. |
| `server.ca_file`   | path | (empty) | PEM file of a CA to trust in addition to the system roots. Absolute path. |
| `server.ca_only`   | bool | `false` | Trust `ca_file` only, not the system roots. Requires `ca_file`. |
| `server.cert_file` | path | (empty) | Client certificate presented to the server (mutual TLS). Absolute path. Set both `cert_file` and `key_file`, or none. |
| `server.key_file`  | path | (empty) | Private key of the client certificate. Absolute path. |
| `server.insecure`  | bool | `false` | Skip the verification of the server certificate. Development only: the agent logs a warning. |

The agent connects with TLS 1.2 at least, and never follows a redirect of the
server: following one would send its token again, possibly to another host.

### `auth`

| Key                          | Type   | Default          | Description |
| ---------------------------- | ------ | ---------------- | ----------- |
| `auth.enrollment_token`      | string | (empty)          | Token from `lacis-server token create`. Only used while `state_dir` holds no identity. |
| `auth.enrollment_token_file` | path   | (empty)          | File holding the token instead, so that it is not left in `agent.yaml`. Absolute path. Set one of the two keys only. |
| `auth.state_dir`             | path   | `/var/lib/lacis` | Directory of the agent identity, `identity.json`. Absolute path. The directory is created with mode `0700`, the file with `0600`. |

An `identity.json` readable by the group or by other users is refused: its token
may have leaked. Revoke the agent and enroll it again, or `chmod 600` the file.
To enroll an agent again (after a revocation), delete `<state_dir>/identity.json`
and give it a new enrollment token.

### `hostname`

| Key        | Type   | Default | Description |
| ---------- | ------ | ------- | ----------- |
| `hostname` | string | (empty) | Hostname sent instead of the machine's (`/proc/sys/kernel/hostname`). Must be a valid RFC 1123 hostname. |

### `labels`

| Key                | Required | Description |
| ------------------ | -------- | ----------- |
| `labels.namespace` | yes      | Namespace of the host, e.g. `acme`. It also picks the namespace the agent enrolls into. |
| `labels.project`   | yes      | Default project of the apps of the host. |
| `labels.env`       | yes      | Default env of the apps of the host. |
| `labels.index`     | no       | Free index, e.g. `1` for the first node. |
| `labels.role`      | no       | Free role, only used as a filter. |

`namespace`, `project` and `env` are slugs: lower-case letters, digits, `.`, `_` and
`-`, starting and ending with a letter or a digit, 63 characters at most. `index`
and `role` also accept upper-case letters. The server makes no synonym mapping:
`prod` and `production` are two envs, keep the labels consistent.

An app can override `project` and `env` in its version file, and its env can come
from its path; see [Collectors](../collectors.md#apps).

### `interval` and `jitter`

| Key        | Type     | Default | Description |
| ---------- | -------- | ------- | ----------- |
| `interval` | duration | `1h`    | Time between two snapshots. At least `1m`. |
| `jitter`   | duration | `5m`    | Random delay added to each interval, between 0 and this value, so that the agents do not all send at once. |

After a transient failure (network error, `5xx`, `429`), the agent retries with an
exponential backoff from 10 seconds, capped at 5 minutes and at `interval`. When the
server answers a `Retry-After` header (a number of seconds or an HTTP date), as it
does on `429 too_soon`, the agent waits that long instead, 5 minutes at most. A
snapshot rejected by the server (invalid schema, namespace mismatch…) is not sent
again; the next one comes at the next interval. A refused token (`401`) stops the
agent.

### `collectors`

| Key                   | Type     | Default | Description |
| --------------------- | -------- | ------- | ----------- |
| `collectors.timeout`  | duration | `30s`   | Time limit of each collector. At least `1s`. |
| `collectors.disabled` | list     | `[]`    | Collectors not run: `host`, `runtimes`, `services`, `docker`, `webservers`, `apps`, `envfiles`. An unknown name logs a warning. |

See [Collectors](../collectors.md) for what each one does.

#### `collectors.apps`

| Key                            | Type | Default      | Description |
| ------------------------------ | ---- | ------------ | ----------- |
| `collectors.apps.scan_paths`   | list | `[/var/www]` | Directories searched for app roots, besides the roots found from the vhosts, the supervisor programs and the processes. |
| `collectors.apps.max_depth`    | int  | `4`          | Depth of the search under each scan path. At least 1. |
| `collectors.apps.max_files`    | int  | `5000`       | Directory entries read under each scan path: the scan of that path stops there, with a warning in the collector errors. At least 1. |
| `collectors.apps.git_max_depth` | int | `0`          | Parent directories of the resolved app root searched for its `.git` when the root holds none, as for an app in a subdirectory of its repository: `1` searches the parent too. `0` searches the root only. |
| `collectors.apps.commands`     | string | `path`     | What is sent of a supervisor program command: `path`, the program alone (`argv[0]`); `full`, the whole command, arguments included (and any password in them, such as `mysqldump -pSECRET`). With `full`, the commands of the gtask scheduled tasks are sent too. |
| `collectors.apps.exclude`      | list | see example  | Directories never searched, nor accepted as part of an app root. An entry is a directory name (`vendor`) or a run of them (`var/cache`), matched as whole names anywhere in the path. Hidden directories are always skipped. |
| `collectors.apps.frameworks`   | list | `[]`         | Declarative framework detectors, see below. |
| `collectors.apps.extra_config` | list | `[]`         | Configuration files added to some apps, see below. |

An `exclude` entry holding a `/` is not anchored: `var/cache` excludes
`/var/www/shop/var/cache` as well as `/srv/api/current/var/cache/prod`, never
`/var/www/shop/var/log/cache`. Anchoring it to the app root would miss a directory
holding its own marker (a package under `node_modules` has a `package.json`), and
anchoring it to the scan path would never match, the scan stopping at the app roots.

A framework detector recognizes a framework at an app root. A detector named like
a built-in one (`symfony`, `laminas`, `zend`, `zend1`, `laravel`, `next`, `nuxt`,
`nestjs`) replaces it, and its `config` sources, when it has some, replace the files
the built-in loader reads.

| Key                       | Description |
| ------------------------- | ----------- |
| `name`                    | Framework name. Required. |
| `detect.composer_package` | Package of `composer.lock`; its version becomes the framework version. |
| `detect.npm_package`      | Dependency of `package.json`; its version is read in `node_modules`, else `package-lock.json`, else the range of `package.json`. |
| `detect.file`             | File relative to the app root whose presence detects the framework. |
| `config[].path`           | Configuration file relative to the app root, in load order, the last winning. Globs accepted. It may name a variable read so far, such as `.env.${NODE_ENV}`; `NODE_ENV` and `APP_ENV` default to `production`. Required. |
| `config[].format`         | Required: `env` (a `.env` file), or `php`, `ini`, `yaml`, `json`, whose nested keys are flattened into env keys (`doctrine.dbal.url` gives `DOCTRINE_DBAL_URL`). Another format is refused. |

The first `detect` key set is used, in the order of the table. See
[structured sources](../collectors.md#structured-sources) for how a `php`, `ini`,
`yaml` or `json` file is read: PHP is parsed, never run, and Symfony `%env(X)%`
references are followed.

#### `collectors.apps.extra_config`

Adds configuration files to the apps whose path matches a glob: a configuration
kept out of the app root, such as `/etc/acme/shop.env`, or one its framework loader
does not read. The files are read after those of the framework, in order, the last
winning; the environment of the supervisor programs still comes last.

| Key               | Description |
| ----------------- | ----------- |
| `app`             | Glob matched against the app path, as seen (`/var/www/shop/current`) or resolved (`/var/www/shop/releases/42`). Absolute; `*` does not cross a `/`. Required. |
| `config[].path`   | File, absolute or relative to the app root; globs accepted, the matches read in lexical order. Required. |
| `config[].format` | `env`, `php`, `ini`, `yaml` or `json`, as for the framework sources. Required. |

```yaml
collectors:
  apps:
    extra_config:
      - app: "/var/www/*/current"
        config:
          - { path: /etc/acme/shop.env, format: env }
          - { path: "config/packages/*.yaml", format: yaml }
```

An absolute path may lie anywhere on the host: it is declared by the administrator
of the agent, who runs it as root. The values go through the same filtering as the
framework sources.

#### `collectors.envfiles`

| Key                              | Type | Default | Description |
| -------------------------------- | ---- | ------- | ----------- |
| `collectors.envfiles.allow_keys` | list | `[*_URL, *_DSN, *_HOST, *_PORT, APP_ENV, NODE_ENV]` | Keys read from the app configuration (shell-style patterns). |
| `collectors.envfiles.deny_keys`  | list | `[*SECRET*, *PASS*, *PWD*, *TOKEN*, *KEY*, *CRED*, *AUTH*, *PRIVATE*, *SALT*, *COOKIE*, *WEBHOOK*]` | Keys never read, even when allowed. |

Keys and patterns are compared ignoring case: `db_password` is denied like
`DB_PASSWORD`. See [what is never sent](../collectors.md#what-is-never-sent).

A list given in the configuration **replaces** its default, it does not add to it:
to read one more key, give the default patterns too.

```yaml
collectors:
  envfiles:
    allow_keys: ["*_URL", "*_DSN", "*_HOST", "*_PORT", APP_ENV, NODE_ENV, CLAMAV_ADDR]
```

A `deny_keys` given without the default patterns no longer denies them: a key such
as `DB_PASSWORD` is then read when an allowed pattern matches it.

#### `collectors.traefik`

| Key                           | Type   | Default                 | Description |
| ----------------------------- | ------ | ----------------------- | ----------- |
| `collectors.traefik.api_url`  | URL    | `http://127.0.0.1:8080` (when empty) | Traefik API read for its routers, services and entrypoints. |
| `collectors.traefik.username` | string | (empty)                 | Basic auth user of the API. |
| `collectors.traefik.password` | string | (empty)                 | Basic auth password. Requires `username`. |
| `collectors.traefik.ca_file`  | path   | (empty)                 | CA added to the system roots to verify an HTTPS API. Absolute path. |
| `collectors.traefik.ca_only`  | bool   | `false`                 | Trust `ca_file` only. Requires `ca_file`. |
| `collectors.traefik.insecure` | bool   | `false`                 | Skip the verification of the API certificate. |

The Traefik API is called without the proxy of the environment and without
following redirects, so that its credentials only go to the configured address.

#### `collectors.docker`

| Key                        | Type | Default                | Description |
| -------------------------- | ---- | ---------------------- | ----------- |
| `collectors.docker.socket` | path | `/var/run/docker.sock` | Docker API socket. Absolute path. The `docker` collector is skipped when it does not exist; empty disables it. |
