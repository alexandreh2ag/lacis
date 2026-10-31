-- Namespaces and projects show no logo.
alter table namespaces drop column logo_url;
alter table projects drop column logo_url;

-- Logos uploaded for technologies (traefik, redis, symfony…), in place of
-- the one the front embeds, or for one it has none of.
create table tech_icons (
  tech text primary key,
  content_type text not null check (content_type in ('image/png', 'image/jpeg', 'image/webp', 'image/svg+xml')),
  content bytea not null,
  updated_by uuid references users on delete set null,
  updated_at timestamptz not null default now()
);

-- The icons global resource: setting and resetting these logos.
alter table global_permissions drop constraint global_permissions_resource_check,
  add constraint global_permissions_resource_check check (resource in ('namespaces', 'tokens', 'users', 'roles', 'icons', '*'));
