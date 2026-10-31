delete from global_permissions where resource = 'icons';
alter table global_permissions drop constraint global_permissions_resource_check,
  add constraint global_permissions_resource_check check (resource in ('namespaces', 'tokens', 'users', 'roles', '*'));
drop table if exists tech_icons;
alter table projects add column if not exists logo_url text;
alter table namespaces add column if not exists logo_url text;
