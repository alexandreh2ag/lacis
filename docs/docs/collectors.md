---
sidebar_position: 4
title: Collectors
---

# Collectors

The agent builds each snapshot with seven collectors. Each one fills its part of
the snapshot; the snapshot records for each its status (`ok`, `skipped`,
`disabled` or `error`), its duration and its errors (512 bytes at most each). A
collector that fails does not stop the others, and a part that cannot be read is
reported while the rest is still sent.

Run `lacis-agent collect` to print exactly what would be sent, secret filtering
included.

| Collector    | Runs after                                      | Skipped when |
| ------------ | ----------------------------------------------- | ------------ |
| `host`       | -                                               | never |
| `runtimes`   | -                                               | no runtime binary found |
| `services`   | -                                               | no known service process |
| `docker`     | `host`, `services`                              | the Docker socket does not exist |
| `webservers` | -                                               | no known webserver process |
| `apps`       | `webservers`, `runtimes`, `services`, `docker`  | never |
| `envfiles`   | `apps`                                          | never |

Disable one with [`collectors.disabled`](./configuration/agent.md#collectors).
Each collector is bounded by `collectors.timeout` (30 seconds by default). A
collector still blocked 5 seconds past its timeout (a FIFO, a hung NFS mount) is
abandoned: the snapshot is sent without its part.

## How the agent reads the host

The agent runs as root and reads the system from absolute paths (`/proc`, `/sys`,
`/etc`, the app directories). It takes care not to be turned against the host by
what the apps or their users control:

- **Application files** (`.env`, `composer.lock`, `package.json`, PHP
  configuration, version files…) are read only when they are regular files, never a
  FIFO or a device, and up to 8 MiB. Such a file may be a symbolic link, as
  Capistrano and Deployer link `releases/X/.env` to `shared/.env`: up to 5 links are
  followed, resolved one by one, so that a loop or a longer chain is refused. The
  target is opened without following a link (a link swapped in after the
  resolution is refused) and must be a regular file.
- **The `.git` directory and `.nvmrc`** of an app are read confined to the app
  root: `..` and links leading out of it are refused, and only values of the
  expected form (commit ids, ref names, versions) are kept.
- **Configuration files named by a process** (the `-c`/`-f` of nginx, Apache,
  HAProxy, PHP-FPM, supervisord) are read with the rights of that process, through
  the hidden command `lacis-agent internal read` run as its user, up to 4 MiB.
- **Version probes** (`php -v`, `node --version`, `<service> --version`) run as
  `nobody`, else as the user of the process running the binary. A binary of a root
  process that another user can modify is never run.
- The agent never runs a webserver binary: nginx is read from its files, not with
  `nginx -T`.
- The package managers (`dpkg-query`, `rpm`) and `systemd-detect-virt` run as the
  agent.

## `host`

The description of the machine:

- **OS** from `/etc/os-release` (or `/usr/lib/os-release`): ID, ID_LIKE, version,
  pretty name.
- **Kernel** release and **architecture** (as `uname -m`).
- **Virtualization**, from `systemd-detect-virt`, else Docker or Podman markers.
- **CPU** model and cores: visible ones, and effective ones when a cgroup quota
  (v2 or v1) is lower.
- **Memory**: total, and the cgroup limit when lower.
- **Disks**: mount point and size of the mounted disk file systems (ext4, xfs,
  btrfs, zfs…), once per device.
- **Interfaces** that are up with an address, loopback excluded, their IPs in CIDR
  notation, and whether they carry a default route.

Sizes only: Lacis never collects usage (used memory, used disk, load, uptime).

## `runtimes`

Language runtimes installed or running: **PHP**, **Node.js** and **Java**. They
are found from the binaries of the running processes and from well-known paths:
`/usr/bin/php*`, `/usr/local/bin/php*`, Remi's `/opt/remi/php*`; system Node, `n`,
`nvm` (per user or shared) and `volta`; `/usr/lib/jvm/*`. The version is read by
probing the binary (`php -n -v`, `node --version`) or, for Java, from the `release`
file of the JDK/JRE. The Node version manager is recorded.

The **package version** of a runtime installed by a package is recorded too, its
packaging revision included: `php8.1-cli` (Debian, Ubuntu, deb.sury.org) for
`/usr/bin/php8.1`, `php-cli` (RHEL) for `/usr/bin/php`, `php81-php-cli` for Remi's
`/opt/remi/php81`, `nodejs` for `/usr/bin/node`. Distributions patch an
end-of-life version there and keep its version: PHP 8.1.34 patched is still
`8.1.34`, its package `8.1.34-9+ubuntu22.04.1+deb.sury.org+1`. The package is kept
only when its version is the one the binary printed.

## `services`

Services running on the host, recognized by their main process: MySQL, MariaDB,
PostgreSQL, Redis, PHP-FPM, Elasticsearch, Keycloak, ClickHouse, MongoDB,
RabbitMQ, Varnish, Vault, and the object stores MinIO and Silo (the fork of MinIO
by Pigsty, its binary renamed `silo`), from their `minio server` or `silo server`
process. For each:

- the **version**, from its package (`dpkg-query` or `rpm`), else by probing its
  binary (`--version`, `varnishd -V`, `vault version`). A MinIO or Silo package
  version such as `20240116160738.0.0` is given as its release,
  `RELEASE.2024-01-16T16-07-38Z`;
- the **package version**, its packaging revision included
  (`8.2.24-1+deb12u1`), when the service comes from a package;
- the **addresses it listens on** (TCP and unix sockets) across its process tree;
- an **instance** name when a type runs several times: the PostgreSQL cluster of
  Debian (`/etc/postgresql/<version>/<cluster>/`), the PHP version of PHP-FPM, else
  the first address;
- the **pools of PHP-FPM**, read from its configuration and includes: name, listen
  address, user.

## `docker`

Reads the Docker API on its socket.

- **Every container**, running or not, is listed in the `containers` of the
  snapshot: its name, image, version (the image tag, else its OCI version label),
  state (`running`, `exited`, `paused`…), ports (`host:port->port/proto` when
  published), networks, and the Docker Compose project and service of its labels.
  The server keeps them per host and shows them in the **Containers** tab of the
  host page. A container that disappears is marked removed and purged like the
  rest of the host content; containers make no change event in the history (a
  container stopped or started is no change of the host).
- A **running** container whose image is a supported service (`postgres`, `mysql`,
  `mariadb`, `redis`, `valkey`, `mongo`, `rabbitmq`, `elasticsearch`, `opensearch`,
  `clickhouse`, `keycloak`, `memcached`, `minio`, `silo`… official or Bitnami) is
  also added to the services with its container name, image, **published ports**
  and networks. Its version comes from the image tag, refined by the image's
  version variable (`PG_VERSION`, `REDIS_VERSION`…) or the OCI version label.

It also removes the Docker bridges and veth interfaces from the host interfaces:
their subnets are private to the host.

Only the version variables of a container are used; no other environment value is
read or sent.

## `webservers`

Webservers and proxies with their routing. A webserver is both an entry (its
domains and ports) and a source (its roots, FastCGI and proxy targets).

| Type    | Read from |
| ------- | --------- |
| nginx   | Its files: the `-c` of the master (relative to its `-p` prefix), else `conf/nginx.conf` of its prefix, else `/etc/nginx/nginx.conf`, with their includes. Server blocks (names, listen, root, `fastcgi_pass`/`proxy_pass`/`uwsgi_pass`/`scgi_pass`/`grpc_pass` targets, by the `location` passing them) and upstream blocks. |
| Apache  | Its configuration files (`/etc/apache2/apache2.conf`, `/etc/httpd/conf/httpd.conf`, or `-f`/`-d` of the process) with their includes, `Define` and `-D` variables substituted, negative conditional sections left out: virtual hosts, `DocumentRoot`, `ProxyPass` and FastCGI targets, by the path passing them (`ProxyPass /api`, `<Location>`, `<LocationMatch>`). |
| HAProxy | `/etc/haproxy/haproxy.cfg`, or each `-f` of the process: frontends (binds, backends) and backends (servers). |
| Traefik | Its API (`collectors.traefik.api_url`, basic auth and TLS options available): routers (rule, entrypoints, TLS), services (servers) and entrypoints (the addresses it listens on). Works for a Traefik running in a container. |

The version comes from the package, when there is one.

## `apps`

Applications deployed on the host. App roots are found from:

- the roots of the vhosts, and the local processes their proxies reach;
- the supervisor programs (`supervisord.conf` and its `[include]` files): their
  directory or the first app path in their command;
- the Node and PHP processes, from their working directory;
- the scan paths (`collectors.apps.scan_paths`, `/var/www` by default).

A directory is an app root when it holds `composer.json`, `package.json`, `.git` or
a version file. A directory with a `current` symlink is a deployment: only
`current` is an app. Directories under a hidden directory or one of
`collectors.apps.exclude` are not apps. An entry of `exclude` is a run of whole
directory names found anywhere in the path: `vendor` excludes
`/var/www/shop/vendor/acme`, `var/cache` excludes `/var/www/shop/var/cache/prod` but
not `/var/www/shop/var/log/cache`.

The scan of each scan path walks down `collectors.apps.max_depth` directories and
stops once it has read `collectors.apps.max_files` directory entries (5000 by
default), with a warning in the collector errors: a scan path holding a huge tree
does not hold the collection.

For each app:

- **name**: the repository name of its git remote, else its directory name,
  skipping generic directories (`www`, `public`, `current`…), env directories and
  build numbers;
- **role**: `web` when a vhost serves it or one of its processes listens on TCP,
  else `worker`;
- **launchers**: its supervisor programs, each `web` or `worker`, with the program
  of their command only (`collectors.apps.commands: path`, the default) or the
  whole command (`full`). A program running
  [gtask](https://github.com/alexandreh2ag/go-task) (`gtask schedule start` or
  `gtask schedule run` with `-c <file>`) also reports the **scheduled tasks** of
  that file: their ids and cron expressions, their commands only with
  `collectors.apps.commands: full`, never their environment. The file is read confined to the app root (a relative `-c` is
  joined to the directory of the program); a file out of the app is not read;
- **framework**: Symfony, Laminas, Zend, Zend Framework 1, Laravel, Next.js, Nuxt,
  NestJS, or a [declarative detector](./configuration/agent.md#collectorsapps);
- **runtime** it really runs on: the PHP-FPM pool its vhost sends to, the binary
  of its Node process, the PHP of its launcher command, or its `.nvmrc`, with the
  package version of that runtime or of its PHP-FPM;
- **code**: from a version file, else from `.git` (read without the git binary):
  the tag of HEAD, else the branch, the commit and the remote URL. The `.git` is
  that of the resolved app root, else that of the nearest of its
  `collectors.apps.git_max_depth` parent directories (`0` by default: the root
  only), for an app in a subdirectory of its repository;
- **project** and **env**, see below.

The server knows an app of a host by its path. An app found at a new path takes the
place of the app of the same name and project on that host that is no longer at its
own: deploying into a new directory (`builds/shop-api/production/2-5-0`) moves the app,
one change in the history, rather than removing it and adding another.

### Version file

The CI can write a version file at the app root: `.app.lacis.json`,
`.app.lacis.yaml` or `.app.lacis.yml` (in this priority; a warning when several
exist). Every field is optional:

```json title=".app.lacis.json"
{
  "project": "shop",
  "app": "api",
  "env": "production",
  "version": "v2.4.1",
  "commit": "9f1c2ab",
  "branch": "main",
  "tag": "v2.4.1",
  "repository": "https://gitlab.acme.example/shop/api",
  "pipeline_id": "1234",
  "pipeline_url": "https://gitlab.acme.example/shop/api/-/pipelines/1234",
  "deployed_at": "2026-10-30T14:00:00Z",
  "deployed_by": "deploy-bot"
}
```

`version` is the tag deployed or, for an env that deploys a branch, the branch.
`project` and `env` must be slugs, like the labels; another value is ignored with a
warning.

### Env of an app

By priority:

1. `env` of the version file;
2. the deepest directory of its path named `prod`, `production`, `preprod`,
   `staging`, `release` or `preview` (whole directory name);
3. the `env` label of the host.

`project` is the version file's, else the label's. The env is never derived from a
branch, nor from `APP_ENV`/`NODE_ENV`.

The host still belongs to the project and env of the app when they differ from its
labels: it lists among the hosts of that project, in that env.

## `envfiles`

Reads the configuration of each app, in the order its framework loads it, and
turns the connection values into **dependencies**:

| Framework        | Files read, the last winning |
| ---------------- | ---------------------------- |
| Symfony          | `.env.local.php` alone when it exists, else `.env`, `.env.local` (not in `test`), `.env.<APP_ENV>`, `.env.<APP_ENV>.local` |
| Next.js          | `.env`, `.env.production`, `.env.local`, `.env.production.local` |
| Nuxt             | `.env`, `.env.<NODE_ENV>` |
| Laravel, NestJS  | `.env` |
| Laminas, Zend Framework 2/3 | `.env`, then `config/autoload/global.php`, `config/autoload/*.global.php`, `local.php`, `*.local.php` (sorted), merged key by key, a later file overriding an earlier one |
| Zend Framework 1 | `application/configs/application.ini`, the section of the app env and the ones it extends (`APPLICATION_ENV` from `public/.htaccess`) |
| declarative      | its `config` sources, in order, of any format (`env`, `php`, `ini`, `yaml`, `json`) |
| other            | `.env` at the app root |

Then come the files of
[`collectors.apps.extra_config`](./configuration/agent.md#collectorsappsextra_config)
matching the app, and the environment the supervisor programs of the app give them
is read last. `.env` references (`${VAR}`, `${VAR:-default}`) are resolved across
the files. Each file may be a symbolic link (5 hops at most, see
[above](#how-the-agent-reads-the-host)).

### Structured sources

A `php`, `ini`, `yaml` or `json` source is read into values keyed by their dotted
path, such as `doctrine.dbal.url`. In a declarative or an extra source, the path is
**flattened** into an env key: upper-cased, its dots replaced with `_`
(`doctrine.dbal.url` gives `DOCTRINE_DBAL_URL`). The key is then filtered by
`allow_keys` and `deny_keys` and parsed like a `.env` value, `X_HOST`/`X_PORT`
pairing included; the dependency keeps the dotted path as its `key_path`. A file
holds 10,000 values at most.

The Laminas and Zend Framework 1 loaders keep the blocks of their configuration
instead (such as `db.params`): only the `dsn`, `url`, `host`/`hostname`/`server`,
`port`, `dbname`/`database` and `adapter` leaves of a block are read.

**PHP files are parsed, never run.** The file must return a literal array, in the
`array()` or `[]` syntax. Its values may be literals (strings, numbers, booleans,
`null`), nested arrays, concatenations, and the environment through
`getenv('X')`, `$_ENV['X']`, `$_SERVER['X']` or `env('X', default)`, with `?:` and
`??` giving a default; these are resolved against the env of the app read so far.
Any other expression (a constant, a function call, a variable, a closure) is
skipped, its key with it.

A Symfony **`%env(X)%`** reference in a string of a structured file reads `X` from
the env of the app. The processors that keep the value (`string`, `resolve`,
`int`, `float`, `bool`, `not`, `trim`, and `default:<param>:`) are followed; a
reference through another processor (`base64`, `json`, `file`, `key`, `url`…)
skips the value.

Each allowed value is parsed as a URL, a DSN (PDO, libpq key/value) or
`host:port`; `X_HOST` keys take the port of `X_PORT`. A host name is resolved on
the host of the app, `/etc/hosts` first, then the DNS (2 seconds at most per name).
A dependency holds the key, the file and line it was read from, the scheme, the
target (IP, name with its resolved IPs, or unix socket), the port and, for
database and broker schemes, the resource name (database, Redis index, AMQP vhost).

A value holding a **JSON or YAML collection** (starting with `[`, `{` or `- `, or on
several lines) gives one dependency per item, under the same key, its index as
key path (`ELASTIC_SERVERS.0`). An object is read as a block (`host`, `port`, `dsn`,
`url`, `dbname`, `adapter`…); a string as a URL, a DSN or `host:port`:

```dotenv
ELASTIC_SERVERS='[{"host":"172.16.1.12","port":9200},{"host":"172.16.1.13","port":9200}]'
ELASTICSEARCH_SERVERS='["172.16.1.15:9200","172.16.1.16:9200"]'
```

Such keys are read once allowed, for instance with `*_SERVERS` in `allow_keys`.

`APP_ENV` and `NODE_ENV` are sent as information only.

## What is never sent

Credentials are removed **on the host**, before the snapshot leaves it:

- From the app configuration, **only the keys matching `allow_keys` and none of
  `deny_keys` are read**, compared ignoring case. Defaults: allowed `*_URL`,
  `*_DSN`, `*_HOST`, `*_PORT`, `APP_ENV`, `NODE_ENV`; denied `*SECRET*`, `*PASS*`,
  `*PWD*`, `*TOKEN*`, `*KEY*`, `*CRED*`, `*AUTH*`, `*PRIVATE*`, `*SALT*`, `*COOKIE*`, `*WEBHOOK*`.
  A flattened key of a structured source (`DOCTRINE_DBAL_URL`) is filtered the same
  way. In the blocks of Laminas and Zend 1, only the `dsn`, `url`,
  `host`/`hostname`/`server`, `port`, `dbname`/`database` and `adapter` leaves of a
  block are read, and denied keys never. The same goes for the objects of a JSON or
  YAML collection; a string of a collection that is a bare word (no scheme, port,
  dot, nor IP) is reported unparsed, neither sent nor resolved.
- A connection string is reduced to **scheme, host, port, socket** and, for
  database and broker schemes (`mysql`, `postgres`, `redis`, `amqp`, `mongodb`,
  `clickhouse`…), the resource name; an HTTP URL (`http`, `https`, `ws`, `wss`)
  keeps its **path**, the route it calls through the reverse proxies. Everything up
  to the last `@` after `://` is dropped as credentials, the query string and the
  fragment are dropped, and the path of any other URL is dropped. **A URL whose path
  holds a token**, such as a Slack or Teams webhook URL, is to be left out by
  `deny_keys`: `*WEBHOOK*` is denied by default, add the others.
- A value that cannot be parsed is sent as its **key alone** (`unparsed`), never as
  its value.
- The other values of the configuration files are never sent: no raw `.env`
  content, no environment of the supervisor programs.
- **Supervisor program commands** are sent as their program only (`argv[0]`) by
  default: arguments often hold a password (`--dsn=amqp://user:pass@…`,
  `mysqldump -pSECRET`). `collectors.apps.commands: full` sends them whole.
- Git remote URLs and the `repository` of the version file lose their user and
  password (deploy tokens are often written there), even when the URL does not
  parse.
- Of a Docker container, only the version variables of its image are read; its
  environment is never sent.
- Of a gtask configuration, only the ids and cron expressions of the scheduled
  tasks are read, and their commands with `collectors.apps.commands: full`;
  their environment and conditions never.

What **is** sent as is: paths, hostnames, IPs, listen addresses, vhost names and
proxy targets, versions and commits. Check with `lacis-agent collect` before
enrolling a host.
