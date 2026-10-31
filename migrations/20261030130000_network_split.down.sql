drop table if exists host_networks;
delete from networks where split_from_id is not null;
alter table networks drop column if exists split_from_id;
