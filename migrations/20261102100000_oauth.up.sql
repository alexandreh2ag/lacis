-- OAuth 2.1 of the MCP endpoint: Lacis is its own authorization server.
-- Every secret (client secret, code, token) is kept as its SHA-256 only.

-- The clients registered themselves (RFC 7591), e.g. Claude.
create table oauth_clients (
  id text primary key,
  name text not null,
  redirect_uris text[] not null,
  -- Null for a public client, which proves itself by PKCE only.
  secret_hash text,
  created_at timestamptz not null,
  -- The first authorization a user gave it: a client never authorized is
  -- purged a day after its registration.
  authorized_at timestamptz
);

-- The authorization codes, single use, short-lived.
create table oauth_codes (
  id text primary key,
  client_id text not null references oauth_clients on delete cascade,
  user_id uuid not null references users on delete cascade,
  -- The OIDC groups of the session that authorized: their roles apply.
  oidc_groups text[] not null default '{}',
  redirect_uri text not null,
  code_challenge text not null,
  scope text not null,
  expires_at timestamptz not null
);
create index idx_oauth_codes_expires on oauth_codes (expires_at);

-- An access a user gave a client, with its current tokens: a refresh
-- replaces both. Revoking it ends the access.
create table oauth_grants (
  id uuid primary key,
  client_id text not null references oauth_clients on delete cascade,
  user_id uuid not null references users on delete cascade,
  oidc_groups text[] not null default '{}',
  scope text not null,
  access_hash text not null unique,
  access_expires_at timestamptz not null,
  refresh_hash text not null unique,
  -- The refresh token replaced last: shown again, it was stolen, and the
  -- grant is revoked.
  previous_refresh_hash text unique,
  created_at timestamptz not null,
  last_used_at timestamptz not null,
  -- Past it, the user authorizes again, which reads their OIDC groups
  -- again.
  expires_at timestamptz not null
);
create index idx_oauth_grants_user on oauth_grants (user_id);
create index idx_oauth_grants_expires on oauth_grants (expires_at);
