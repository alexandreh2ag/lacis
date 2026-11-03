---
sidebar_position: 2
title: lacis-agent
---

# `lacis-agent`

The agent runs as root on the host it inventories.

## Global flags

| Flag            | Default      | Description |
| --------------- | ------------ | ----------- |
| `-c`, `--config` | `/etc/lacis` | Configuration file, or directory holding `agent.yaml` or `agent.yml`. |
| `-l`, `--level`  | `INFO`       | Log level: `DEBUG`, `INFO`, `WARN`, `ERROR`. Logs go to stderr. |

Every command but `version` loads and validates the configuration first.

## `start`

Runs the agent until `SIGINT` or `SIGTERM`.

```bash
lacis-agent start -c /etc/lacis
```

1. When `auth.state_dir` holds no identity, it enrolls with
   `auth.enrollment_token` (or the token of `auth.enrollment_token_file`), retrying
   transient failures.
2. It then collects and sends a snapshot right away, and again every `interval`
   plus a random jitter up to `jitter`.

Transient failures (network errors, `5xx`, `429`) are retried with an exponential
backoff from 10 seconds up to 5 minutes, or after the `Retry-After` the server
answered (5 minutes at most). A rejected snapshot waits for the next
interval. When the server refuses the token (`401`), the agent stops with an
error: remove `identity.json` from the state directory and enroll it again.

## `enroll`

Exchanges `auth.enrollment_token` (or the token of `auth.enrollment_token_file`)
for the identity of the agent, kept in `auth.state_dir/identity.json` with mode
`0600`. Nothing is done when the agent is already enrolled.

```bash
lacis-agent enroll -c /etc/lacis
# agent 019a… enrolled
```

`start` enrolls by itself; `enroll` is useful to check a token before starting the
service.

## `collect`

Collects once and prints on stdout, as indented JSON, exactly what would be sent to
the server, secret filtering included. Nothing is sent, and no enrollment is needed.

```bash
sudo lacis-agent collect -c /etc/lacis | less
sudo lacis-agent collect -c /etc/lacis | jq '.apps[] | {name, env, dependencies}'
```

## `version`

Prints the version, the commit and the build date. Needs no configuration.

```bash
lacis-agent version
```

## `internal read` (hidden)

`lacis-agent internal read <path> [--max <bytes>]` prints a regular file of at most
`--max` bytes (4 MiB by default). It is not meant to be run by hand: the agent runs
it as the user of a process (nginx, Apache, HAProxy, PHP-FPM, supervisord) to read
the configuration files named by its command line with no more rights than that
process. It needs no configuration.
