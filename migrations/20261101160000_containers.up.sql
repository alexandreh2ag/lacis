-- The Docker containers of each host, running or not, services or not.
-- Like the other content of a host, a container missing from a snapshot
-- gets removed_at, and is purged later.
create table containers (
  id uuid primary key,
  namespace_id uuid not null,
  host_id uuid not null references hosts on delete cascade,
  name text not null,
  container_id text not null,
  image text not null,
  version text,
  state text not null,
  ports jsonb not null default '[]',
  networks jsonb not null default '[]',
  compose_project text,
  compose_service text,
  first_seen_at timestamptz not null,
  removed_at timestamptz,
  unique (host_id, name)
);
create index idx_containers_namespace on containers (namespace_id) where removed_at is null;
