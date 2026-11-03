---
sidebar_position: 7
title: OIDC login
---

# OIDC login

Besides local accounts, users can log in through an OpenID Connect provider
(Keycloak, Authentik, Dex, Okta…). Lacis runs the authorization code flow with
PKCE and a nonce.

## Configuration

```yaml title="/etc/lacis/server.yaml"
listen:
  public_url: https://lacis.example.com     # required with OIDC
auth:
  oidc:
    enabled: true
    issuer: https://sso.acme.example/realms/acme
    client_id: lacis
    client_secret: change-me
    scopes: [openid, profile]
    groups_claim: groups
    username_claim: preferred_username
```

See the [reference](./configuration/server.md#auth) for each key. Local login stays
available while `auth.local.enabled` is `true`; set it to `false` to allow OIDC only.

At the provider, register a confidential client with the redirect URI:

```text
<public_url>/auth/oidc/callback
```

for example `https://lacis.example.com/auth/oidc/callback`, or
`https://lacis.example.com/lacis/auth/oidc/callback` when Lacis is served under
`/lacis`. Make the provider put the groups of the user in the ID token, under
`groups_claim` (a list of names, or a single name).

The provider is discovered at its first use: a provider down does not prevent the
server from starting.

## Flow

1. The front sends the browser to `GET auth/oidc/login?redirect=<page>`.
2. Lacis keeps the state, the PKCE verifier, the nonce and the page in a signed
   cookie, and sends the browser to the provider.
3. The provider comes back to `auth/oidc/callback`. Lacis checks the state, trades
   the code for an ID token, checks it and its nonce, opens a session and goes back
   to the page.

On failure, the browser lands on the login page of the front with
`login?error=<code>`:

| Code               | Meaning |
| ------------------ | ------- |
| `oidc_unavailable` | the provider does not answer |
| `oidc_expired`     | the login took more than 10 minutes, or was replayed |
| `oidc_denied`      | the provider refused the login |
| `oidc_failed`      | the flow failed (invalid answer or token) |
| `oidc_account`     | no account can take this identity |

Logging out of Lacis does not log out of the provider.

## Accounts

A user is found by the **subject** of the provider. At the first login, a user is
created **without role**: its username from `username_claim`, its first and last
names from `given_name` and `family_name`.

A username already taken by another account refuses the login (`oidc_account`).
Lacis never ties an OIDC identity to an existing local account by username: no
provider vouches for a username.

## Groups and roles

The groups of the ID token are kept with the session at login. At each request,
the user holds the roles whose **OIDC groups** include one of their groups (set on
the role in the administration), in addition to the roles given to the user. A
group gives nothing by itself, its name matching a role code or not: only the
OIDC groups of a role count. To make the members of a group administrators, add
the group to the OIDC groups of the `admin` role.

A change of the groups at the provider applies at the next login. A session ends
at most `auth.session.max_lifetime` (12 hours by default) after the login, used or
not, so the groups are read again at least that often.

See [Permissions](./permissions.md) for what the roles grant.
