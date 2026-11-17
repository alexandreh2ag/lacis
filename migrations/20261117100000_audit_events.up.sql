-- What the users did: their calls to the MCP tools, their actions in the
-- front, their sign-ins. The username is kept: the user may be deleted.
create table audit_events (
  id bigserial primary key,
  at timestamptz not null,
  user_id uuid references users (id) on delete set null,
  username text not null,
  source text not null,
  action text not null,
  namespace text,
  project text,
  target text,
  details jsonb not null default '{}'
);

create index idx_audit_events_at on audit_events (at desc, id desc);
create index idx_audit_events_username on audit_events (username, at desc);
