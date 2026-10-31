-- The admin role can do everything.
insert into roles (id, code, description)
  values ('019a2400-0000-7000-8000-000000000001', 'admin', 'Every permission on every namespace');
insert into namespace_permissions (id, role_id, namespace_id, resource, action)
  values ('019a2400-0000-7000-8000-000000000002', '019a2400-0000-7000-8000-000000000001', null, '*', '*');
insert into global_permissions (id, role_id, resource, action)
  values ('019a2400-0000-7000-8000-000000000003', '019a2400-0000-7000-8000-000000000001', '*', '*');
