-- Sessions of the front: the cookie holds a random token, the table its
-- SHA-256, so that a read of the table gives no usable session.
create table sessions (
  id text primary key,
  user_id uuid not null references users on delete cascade,
  -- The OIDC groups of the user at login: their roles apply for the
  -- session.
  oidc_groups text[] not null default '{}',
  created_at timestamptz not null,
  last_seen_at timestamptz not null,
  expires_at timestamptz not null
);
create index idx_sessions_user on sessions (user_id);
create index idx_sessions_expires on sessions (expires_at);
