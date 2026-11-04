-- The version of the system package of a runtime or a service, its
-- packaging revision included ("8.1.34-9+deb.sury.org+1"): distributions
-- patch an end-of-life version there and keep the upstream version.
alter table runtimes add column package_version text;
alter table services add column package_version text;
