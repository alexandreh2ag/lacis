delete from global_permissions where resource = 'tasks';
alter table global_permissions drop constraint global_permissions_resource_check,
  add constraint global_permissions_resource_check check (resource in ('namespaces', 'tokens', 'users', 'roles', 'icons', '*'));
alter table agents drop column if exists conflict_hostnames, drop column if exists conflict_at;
