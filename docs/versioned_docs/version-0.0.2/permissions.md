---
sidebar_position: 6
title: Permissions
---

# Permissions

Access to the front and the GraphQL API is role-based. A **user** holds
**roles**; a role grants **permissions**. A user's permissions are the union of
those of their roles. Roles are managed in the administration of the front
(`/admin`), or given at creation with `lacis-server user create --role`.

A user without any role can log in but sees no namespace.

## Permissions

A permission is a **resource** and an **action**: `read`, `write` or `*` (both).
`write` does **not** imply `read`: grant both, or `*`.

### Namespace resources

Granted on one namespace, or on **all namespaces** (including those created later).

| Resource    | `read` | `write` |
| ----------- | ------ | ------- |
| `inventory` | see the namespace: hosts, apps, services, graph, history | delete a host gone for good (`deleteHost`) |
| `metadata`  | - | edit the namespace (name, description, freshness thresholds), projects, envs and their order, networks (update, split, move hosts, delete) |
| `*`         | both | both |

A namespace the user cannot `read` on `inventory` does not exist for them: the API
answers as for an unknown namespace.

### Global resources

Granted out of the namespaces.

| Resource     | `read` | `write` |
| ------------ | ------ | ------- |
| `users`      | list the users | create, update, delete users, set their roles |
| `roles`      | list the roles | create, update, delete roles |
| `tokens`     | list the enrollment tokens and the agents | create and revoke tokens, revoke agents |
| `icons`      | list the technologies of the inventory | upload and reset technology logos (`PUT`/`DELETE icons/{tech}`) |
| `tasks`      | list the scheduled tasks, their runs and the failed jobs | run a task now, run a failed job again |
| `namespaces` | (reserved) | (reserved) |
| `*`          | all | all |

Every logged-in user can read the uploaded logos (`techIcons`).

`namespaces` can be granted but guards nothing yet: no operation creates, deletes or
lists the namespaces by hand (see [Namespaces](#namespaces)). It is kept for such
operations.

## Nobody grants more than they hold

Holding `users` `write` or `roles` `write` is not enough to escalate:

- a role can only be created, changed or deleted by a user holding every
  permission it grants, before and after the change;
- a user can only be created, changed, deleted or given roles by a user holding
  every permission of the roles that user holds and gets.

As the `admin` role grants everything, only an admin gives it, takes it, changes it,
or changes an admin. `lacis-server user create` on the command line is not bound by
this rule.

Every user, whatever their roles, has a **profile** page (account menu ›
Profile) to change their first and last names and, for a local account, their
password; it needs no permission. The names of an OIDC user may be set again by
the identity provider at their next login.

Changing one's own password requires the current one. A new password ends the
other sessions of the user. Passwords hold 8 to 1024 characters. A user created
by an OIDC login has none, and cannot set one themselves: only an administrator
(`users` `write`) gives them a first password. They then log in either way, with
SSO or with that password, and change it in their profile like anyone else.

## The `admin` role

The `admin` role is created by the migrations with `*` on `*` in every namespace and
`*` on every global resource. It is built in:

- its code and permissions cannot be changed (its description and OIDC groups can), and it cannot be deleted;
- the last user holding it keeps it;
- nobody can delete their own account.

`lacis-server user create` gives the `admin` role by default, as does the bootstrap
admin of `auth.local`.

## Namespaces

Namespaces are not created by hand: the first snapshot naming a namespace creates
it, with the thresholds of [`defaults`](./configuration/server.md#defaults).
Enrollment tokens are not tied to a namespace either; the `namespace` label of the
agent picks it. Once a host exists, a snapshot naming another namespace for it is
refused (`403 namespace_mismatch`).

There are no automatic per-namespace roles. To give a team read access to `acme`,
create a role, for example `acme-viewer`, with `inventory` `read` on `acme`; for
editors, add `metadata` `write` (and `read`).

## OIDC groups

A role can list **OIDC groups**: a user logged in through OIDC holds the roles
whose groups appear in their ID token. See [OIDC](./oidc.md).

## Errors

A refused GraphQL operation carries a code in its `extensions`:

| Code             | Meaning |
| ---------------- | ------- |
| `NOT_FOUND`      | unknown namespace or entity, or a namespace the user cannot read |
| `FORBIDDEN`      | the permission is missing; the message names it (`forbidden: metadata write is needed`) |
| `BAD_USER_INPUT` | invalid input |
| `CONFLICT`       | the operation conflicts with the state, e.g. deleting an active host (its next snapshot would bring it back), or removing the last admin |
| `UNAUTHENTICATED` | no valid session |
| `GRAPH_TOO_LARGE` | the graph asked holds more nodes than [`graph.max_nodes`](./configuration/server.md#graph): filter it |

A refusal a client can explain in its own words also carries a `reason` in its
extensions (`PASSWORD_TOO_SHORT`, `LAST_ADMIN`, `HOST_NOT_IN_RANGE`…) and the values
it names in `args`. Rely on `code` and `reason`, never on the message.
