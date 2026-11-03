-- An agent whose token two machines share, a cloned host: the hostnames
-- its snapshots alternate between, found by detect_token_conflicts.
alter table agents add column conflict_at timestamptz, add column conflict_hostnames text[];

-- The tasks global resource: the scheduled tasks, their runs, a run asked by
-- hand.
alter table global_permissions drop constraint global_permissions_resource_check,
  add constraint global_permissions_resource_check check (resource in ('namespaces', 'tokens', 'users', 'roles', 'icons', 'tasks', '*'));
