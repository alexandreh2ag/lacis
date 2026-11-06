-- The path of an HTTP URL an app calls: the route it takes through the
-- webservers.
alter table app_dependencies add column url_path text;
