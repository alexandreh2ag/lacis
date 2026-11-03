create extension if not exists pg_trgm;

-- Organisation
create table namespaces (
  id uuid primary key,
  slug text unique not null,
  name text not null,
  logo_url text,
  description text,
  stale_after interval not null default '7 days',
  inactive_after interval not null default '30 days',
  snapshot_retention interval not null default '30 days',
  created_at timestamptz not null default now()
);

create table networks (
  id uuid primary key,
  namespace_id uuid not null references namespaces on delete cascade,
  slug text not null,
  name text not null,
  kind text not null default 'private' check (kind in ('private', 'public')),
  description text,
  unique (namespace_id, slug)
);
create index idx_networks_namespace on networks (namespace_id);

create table network_cidrs (
  network_id uuid not null references networks on delete cascade,
  namespace_id uuid not null,
  cidr cidr not null,
  primary key (network_id, cidr)
);
create index idx_network_cidrs_cidr on network_cidrs using gist (cidr inet_ops);
create index idx_network_cidrs_namespace on network_cidrs (namespace_id);

create table projects (
  id uuid primary key,
  namespace_id uuid not null references namespaces on delete cascade,
  slug text not null,
  name text not null,
  logo_url text,
  description text,
  unique (namespace_id, slug)
);
create index idx_projects_name_trgm on projects using gin (name gin_trgm_ops);

create table environments (
  id uuid primary key,
  namespace_id uuid not null references namespaces on delete cascade,
  slug text not null,
  name text not null,
  sort_order int not null default 0,
  unique (namespace_id, slug)
);

-- Agents and hosts
create table enrollment_tokens (
  id uuid primary key,
  token_hash text not null unique,
  label text,
  expires_at timestamptz,
  revoked_at timestamptz
);

-- An agent is created at enrollment; its host comes with its first snapshot.
create table agents (
  id uuid primary key,
  token_hash text not null unique,
  enrollment_token_id uuid not null references enrollment_tokens,
  created_at timestamptz not null default now(),
  last_seen_at timestamptz,
  revoked_at timestamptz
);
create index idx_agents_enrollment_token on agents (enrollment_token_id);

create table hosts (
  id uuid primary key,
  agent_id uuid not null unique references agents on delete cascade,
  namespace_id uuid not null references namespaces on delete cascade,
  hostname text not null,
  labels jsonb not null default '{}',
  project_id uuid references projects,
  environment_id uuid references environments,
  os jsonb,
  kernel text,
  arch text,
  virtualization text,
  resources jsonb,
  agent_version text,
  last_snapshot_hash text,
  first_seen_at timestamptz not null,
  last_seen_at timestamptz not null
);
create index idx_hosts_namespace_project_env on hosts (namespace_id, project_id, environment_id);
create index idx_hosts_namespace_last_seen on hosts (namespace_id, last_seen_at);
create index idx_hosts_hostname_trgm on hosts using gin (hostname gin_trgm_ops);

create table host_ips (
  host_id uuid not null references hosts on delete cascade,
  interface text,
  ip inet not null,
  -- The prefix of the subnet, which gives the network of the IP when no
  -- known CIDR holds it.
  prefix_len int not null,
  default_route boolean not null default false,
  primary key (host_id, ip)
);
-- The network of an IP is not stored: it is the one whose CIDR contains it.
create index idx_host_ips_ip on host_ips (ip);

create table snapshots (
  id bigserial primary key,
  host_id uuid not null references hosts on delete cascade,
  collected_at timestamptz not null,
  received_at timestamptz not null default now(),
  hash text not null,
  schema_version int not null,
  payload jsonb not null
);
create index idx_snapshots_host_collected on snapshots (host_id, collected_at desc);

-- Host content
create table runtimes (
  id uuid primary key,
  namespace_id uuid not null,
  host_id uuid not null references hosts on delete cascade,
  type text not null,
  version text not null,
  path text not null,
  manager text,
  first_seen_at timestamptz not null,
  removed_at timestamptz,
  unique (host_id, type, path)
);
create index idx_runtimes_namespace_type_version on runtimes (namespace_id, type, version) where removed_at is null;

create table services (
  id uuid primary key,
  namespace_id uuid not null,
  host_id uuid not null references hosts on delete cascade,
  type text not null,
  instance text not null default 'default',
  version text,
  details jsonb not null default '{}',
  first_seen_at timestamptz not null,
  removed_at timestamptz,
  unique (host_id, type, instance)
);
create index idx_services_namespace_type_version on services (namespace_id, type, version) where removed_at is null;

create table apps (
  id uuid primary key,
  namespace_id uuid not null,
  host_id uuid not null references hosts on delete cascade,
  path text not null,
  resolved_path text,
  project_id uuid references projects,
  environment_id uuid references environments,
  name text not null,
  role text not null check (role in ('web', 'worker')),
  framework text,
  framework_version text,
  -- The runtime the app really runs on, such as the PHP of its FPM pool,
  -- which has no row in runtimes.
  runtime jsonb,
  code jsonb,
  launchers jsonb not null default '[]',
  -- The names of the vhosts the agent tied to the app: links are computed
  -- from them.
  vhosts text[] not null default '{}',
  info jsonb,
  first_seen_at timestamptz not null,
  removed_at timestamptz,
  unique (host_id, path)
);
create index idx_apps_namespace_project_env on apps (namespace_id, project_id, environment_id) where removed_at is null;
create index idx_apps_namespace_name on apps (namespace_id, name) where removed_at is null;
create index idx_apps_path_trgm on apps using gin (path gin_trgm_ops);

-- The routing of a webserver, as the snapshot gives it: vhosts and
-- upstreams (nginx, apache), routers and services (traefik), frontends and
-- backends (haproxy).
create table webservers (
  id uuid primary key,
  namespace_id uuid not null,
  host_id uuid not null references hosts on delete cascade,
  type text not null,
  version text,
  routing jsonb not null default '{}',
  first_seen_at timestamptz not null,
  removed_at timestamptz,
  unique (host_id, type)
);
create index idx_webservers_namespace on webservers (namespace_id) where removed_at is null;

create table endpoints (
  id uuid primary key,
  namespace_id uuid not null,
  host_id uuid not null references hosts on delete cascade,
  kind text not null check (kind in ('tcp', 'unix', 'domain')),
  address text not null,
  port int,
  owner_type text not null check (owner_type in ('service', 'app', 'webserver')),
  owner_id uuid not null,
  removed_at timestamptz
);
create index idx_endpoints_namespace_kind_address_port on endpoints (namespace_id, kind, address, port);
create index idx_endpoints_host on endpoints (host_id) where removed_at is null;
create index idx_endpoints_domain_trgm on endpoints using gin (address gin_trgm_ops) where kind = 'domain';

create table app_dependencies (
  id uuid primary key,
  app_id uuid not null references apps on delete cascade,
  key text not null,
  scheme text,
  port int,
  path text,
  -- The target as the host of the app resolved it: kind, value, resolved
  -- IPs, resolver. Several dependencies share a key for a cluster.
  target jsonb,
  -- The proof: file, line, key path.
  source jsonb not null default '{}',
  unparsed boolean not null default false
);
create index idx_app_dependencies_app on app_dependencies (app_id);

-- Computed graph
create table links (
  id uuid primary key,
  namespace_id uuid not null references namespaces on delete cascade,
  from_type text not null,
  from_id uuid not null,
  to_type text not null,
  to_id uuid,
  external_target text,
  source text not null check (source in ('env', 'traefik', 'webserver', 'launcher', 'process')),
  confidence text not null check (confidence in ('exact', 'inferred', 'ambiguous')),
  detail jsonb not null default '{}',
  computed_at timestamptz not null
);
create index idx_links_namespace_from on links (namespace_id, from_type, from_id);
create index idx_links_namespace_to on links (namespace_id, to_type, to_id);

-- History
create table change_events (
  id bigserial primary key,
  namespace_id uuid not null,
  host_id uuid,
  entity_type text not null,
  entity_id uuid,
  kind text not null,
  before jsonb,
  after jsonb,
  occurred_at timestamptz not null
);
create index idx_change_events_namespace_occurred on change_events (namespace_id, occurred_at desc);
create index idx_change_events_entity on change_events (namespace_id, entity_type, entity_id, occurred_at desc);

-- Access (RBAC): users and OIDC groups receive roles, roles carry permissions.
create table users (
  id uuid primary key,
  username text unique not null,
  first_name text,
  last_name text,
  password_hash text,
  oidc_subject text unique,
  created_at timestamptz not null default now()
);

create table roles (
  id uuid primary key,
  code text unique not null,
  description text,
  created_at timestamptz not null default now()
);

create table user_roles (
  user_id uuid not null references users on delete cascade,
  role_id uuid not null references roles on delete cascade,
  primary key (user_id, role_id)
);
create index idx_user_roles_role on user_roles (role_id);

-- Members of an OIDC group receive the role.
create table role_oidc_groups (
  role_id uuid not null references roles on delete cascade,
  group_name text not null,
  primary key (role_id, group_name)
);
create index idx_role_oidc_groups_group on role_oidc_groups (group_name);

-- Permissions on the content of a namespace; a null namespace_id means
-- every namespace.
create table namespace_permissions (
  id uuid primary key,
  role_id uuid not null references roles on delete cascade,
  namespace_id uuid references namespaces on delete cascade,
  resource text not null check (resource in ('inventory', 'metadata', '*')),
  action text not null check (action in ('read', 'write', '*')),
  unique nulls not distinct (role_id, namespace_id, resource, action)
);
create index idx_namespace_permissions_namespace on namespace_permissions (namespace_id);

-- Permissions on what belongs to no namespace.
create table global_permissions (
  id uuid primary key,
  role_id uuid not null references roles on delete cascade,
  resource text not null check (resource in ('namespaces', 'tokens', 'users', 'roles', '*')),
  action text not null check (action in ('read', 'write', '*')),
  unique (role_id, resource, action)
);
