---
sidebar_position: 2
title: Installation
---

# Installation

A Lacis installation has three parts:

- **PostgreSQL**, the only storage of the server (inventory, history, sessions,
  background jobs).
- **`lacis-server`**, a single static binary serving the agent API, the GraphQL API
  and the web front. It also runs the background jobs, or leaves them to
  separate `lacis-server worker` processes.
- **`lacis-agent`** on every host to inventory. It runs as root, on the host, and
  only pushes to the server, over HTTPS in production.

Both binaries are built for Linux, on `amd64` and `arm64`.

## Requirements

- PostgreSQL. The development setup and the CI use PostgreSQL 17.
- For the agents: a URL of the server, in **HTTPS** in production. Either let
  `lacis-server` terminate TLS (`listen.tls`) or put it behind a reverse proxy that
  does. TLS is optional: without `listen.tls` the server serves plain HTTP, and the
  agent accepts a `http://` URL, but it logs a warning since the tokens and the
  snapshots then travel in clear. Keep it to a trusted network or to tests.

## Get the binaries

Each [GitHub release](https://github.com/alexandreh2ag/lacis/releases) publishes
the bare binary for each binary and architecture, with nothing to extract, the
version written without its leading `v`:

- `lacis-server_<version>_linux_<arch>`
- `lacis-agent_<version>_linux_<arch>`

where `<arch>` is `amd64` or `arm64`. `checksums.txt` lists the SHA-256 of every
binary.

```bash
VERSION=1.0.0   # without the leading v
ARCH=amd64      # or arm64
BASE=https://github.com/alexandreh2ag/lacis/releases/download/v${VERSION}

curl -fsSLO "${BASE}/lacis-agent_${VERSION}_linux_${ARCH}"
curl -fsSLO "${BASE}/checksums.txt"
sha256sum --ignore-missing -c checksums.txt

sudo install -m 0755 "lacis-agent_${VERSION}_linux_${ARCH}" /usr/local/bin/lacis-agent
lacis-agent version
```

Do the same with `lacis-server` on the server.

### Verify the signature

`checksums.txt` is signed with [cosign](https://docs.sigstore.dev/) in keyless
mode; the bundle is published as `checksums.txt.sigstore.json`. Once the
signature checks, `sha256sum -c` above proves the binaries:

```bash
curl -fsSLO "${BASE}/checksums.txt.sigstore.json"
cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp 'https://github.com/alexandreh2ag/lacis/.github/workflows/release.yml@refs/tags/v.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
```

### Packages

There are no `.deb` or `.rpm` packages yet: install the binaries of the release.

## Server with Docker

The server image is published as `ghcr.io/alexandreh2ag/lacis-server`, for
`linux/amd64` and `linux/arm64`, tagged `X.Y.Z`, `X.Y` and `latest`, and signed with
cosign. It is a distroless image running as the `nonroot` user; its entrypoint is
`lacis-server` and its default command `start -c /etc/lacis`. It listens on port
8080.

```bash
docker run -d --name lacis-server \
  -p 8080:8080 \
  -v /etc/lacis:/etc/lacis:ro \
  ghcr.io/alexandreh2ag/lacis-server:1.0.0
```

Mount the configuration directory read-only. It must be readable by the `nonroot`
user of the image (uid 65532). The configuration can also come from the environment
alone, without any file (see [environment variables](./configuration/server.md#environment-variables)):

```bash
docker run -d --name lacis-server -p 8080:8080 \
  -e LACIS_DATABASE_DSN='postgres://lacis:secret@db.acme.example:5432/lacis?sslmode=require' \
  -e LACIS_AUTH_SESSION_SECRET='a-random-secret-of-32-characters-or-more' \
  -e LACIS_LISTEN_PUBLIC_URL='https://lacis.example.com' \
  ghcr.io/alexandreh2ag/lacis-server:1.0.0
```

The secrets can also come from files, such as Docker or Kubernetes secrets:
`LACIS_DATABASE_DSN_FILE=/run/secrets/lacis-dsn` (see
[secrets from files](./configuration/server.md#secrets-from-files)).

Any other command runs the same way, for example:

```bash
docker run --rm ghcr.io/alexandreh2ag/lacis-server:1.0.0 version
docker run --rm -it -v /etc/lacis:/etc/lacis:ro \
  ghcr.io/alexandreh2ag/lacis-server:1.0.0 user create -c /etc/lacis --username admin
```

The schedules of the background tasks run in the local time of the server; set
`TZ` (for example `-e TZ=Europe/Paris`) to change it from UTC.

## Agent: on the host

The agent is meant to run **directly on the host**, from the release binary, as
root: it reads `/proc`, `/etc`, the application directories and the Docker socket,
and it runs host binaries (`dpkg-query` or `rpm`, and version probes such as `php -v` or
`node --version` as `nobody`).

:::caution[No agent image]
No agent image is published. A `Dockerfile.agent` at the root of the repository builds
one for local tests only. The agent reads absolute paths (`/proc`, `/etc`, the app
directories) and has no way to read the host from a prefix such as `/host`: in a
container it only sees what is mounted at the same paths, and it lacks the host's
binaries, so the `services` collector is skipped (no `dpkg`/`rpm`) and the runtime
versions are missing. Not recommended.

```bash
docker build -f Dockerfile.agent -t lacis-agent .
docker run -d --name lacis-agent \
  --pid=host --network=host --privileged \
  -v /etc:/etc:ro \
  -v /usr/lib/os-release:/usr/lib/os-release:ro \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v /var/lib/lacis:/var/lib/lacis \
  -v /var/www:/var/www:ro \
  lacis-agent start
```

When `/etc/lacis` does not exist on the host, mount the configuration elsewhere,
for example `-v ./agent.yaml:/config/agent.yaml:ro`, and pass `-c /config`.
:::

## First start

### 1. Database

Create a PostgreSQL database and a user owning it, for example:

```sql
CREATE USER lacis WITH PASSWORD 'change-me';
CREATE DATABASE lacis OWNER lacis;
```

### 2. Server configuration

Write `/etc/lacis/server.yaml` (see the [reference](./configuration/server.md)).
The minimum is a database DSN and a session secret:

```yaml title="/etc/lacis/server.yaml"
listen:
  address: 0.0.0.0:8080
  public_url: https://lacis.example.com
database:
  dsn: postgres://lacis:change-me@localhost:5432/lacis?sslmode=disable
auth:
  session:
    secret: a-random-secret-of-32-characters-or-more
  local:
    bootstrap_admin_username: admin
    bootstrap_admin_password: change-me-please
```

The file holds secrets: make it readable by the server user only.

```bash
lacis-server validate -c /etc/lacis
```

### 3. Migrations

`lacis-server start` applies the migrations itself while
`database.migrate_on_start` is `true` (the default). To run them by hand, for
example before a rolling upgrade:

```bash
lacis-server db migrate -c /etc/lacis   # Lacis migrations, then River's
```

### 4. First administrator

Either set `auth.local.bootstrap_admin_username` and
`auth.local.bootstrap_admin_password`, as above: `start` creates that user with the
`admin` role, unless a user already holds the `admin` role or has this username. Or create the user by hand; the
password is prompted:

```bash
lacis-server user create -c /etc/lacis --username admin --first-name Ada --last-name Lovelace
```

`--role` defaults to `admin`.

### 5. Start the server

```bash
lacis-server start -c /etc/lacis
```

Open `listen.public_url` and log in. `GET /healthz` answers when the server is up.

The session cookie is sent over HTTPS only (`auth.session.cookie_secure: true` by
default). To log in over plain HTTP, for example `http://localhost:8080/` in a test,
set `auth.session.cookie_secure: false`.

### 6. Enrollment token

Agents enroll with a token. A token is valid for **every** namespace: the agent's
`namespace` label picks it. It is printed alone on stdout, once (only its hash is
stored):

```bash
lacis-server token create -c /etc/lacis --label web --ttl 7d
```

`--ttl 0` creates a token that never expires. Tokens can also be created in the
administration of the front.

### 7. Agent

On each host, write `/etc/lacis/agent.yaml` (see the
[reference](./configuration/agent.md)):

```yaml title="/etc/lacis/agent.yaml"
server:
  url: https://lacis.example.com
auth:
  enrollment_token_file: /etc/lacis/enrollment-token
labels:
  namespace: acme
  project: shop
  env: production
```

Put the token in its own file, readable by root only (`auth.enrollment_token` in
`agent.yaml` works too; the agent warns when that file is readable by every user):

```bash
echo 'lacis_et_…' | sudo tee /etc/lacis/enrollment-token >/dev/null
sudo chmod 600 /etc/lacis/enrollment-token /etc/lacis/agent.yaml
```

Check what would be sent, then start it:

```bash
sudo lacis-agent collect -c /etc/lacis   # prints the snapshot, sends nothing
sudo lacis-agent start -c /etc/lacis     # enrolls once, then sends a snapshot every interval + jitter
```

At the first start, the agent exchanges the enrollment token for its own identity,
kept in `/var/lib/lacis/identity.json` (mode `0600`; the agent refuses an identity
readable by other users). The token is not used again: delete the token file once
the agent is enrolled. The namespace, its project and
env, and the host appear with the first snapshot.

## systemd

### Agent

```ini title="/etc/systemd/system/lacis-agent.service"
[Unit]
Description=Lacis agent
Documentation=https://alexandreh2ag.github.io/lacis/
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/lacis-agent start -c /etc/lacis
Restart=on-failure
RestartSec=30s
# The agent reads the whole host: it runs as root.
User=root

[Install]
WantedBy=multi-user.target
```

The agent stops by itself, with an error, when the server refuses its token (an
agent revoked or unknown): remove `identity.json` and enroll it again with a new
token.

### Server

```ini title="/etc/systemd/system/lacis-server.service"
[Unit]
Description=Lacis server
Documentation=https://alexandreh2ag.github.io/lacis/
Wants=network-online.target
After=network-online.target postgresql.service

[Service]
Type=simple
User=lacis
Group=lacis
ExecStart=/usr/local/bin/lacis-server start -c /etc/lacis
Restart=on-failure
RestartSec=5s
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

```bash
sudo useradd --system --no-create-home --shell /usr/sbin/nologin lacis
sudo chown root:lacis /etc/lacis/server.yaml && sudo chmod 0640 /etc/lacis/server.yaml
sudo systemctl daemon-reload
sudo systemctl enable --now lacis-server
```

To run the background jobs apart, set `workers.enabled: false` and add a second
unit running `lacis-server worker -c /etc/lacis`. Several workers share the jobs.

## Build from source

See the [Development section of the README](https://github.com/alexandreh2ag/lacis#development):
build the front in `web/` first (the server embeds `web/dist`), then the two Go
binaries.
