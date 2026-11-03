-- A network split from another, by an editor: two private networks on the
-- same range, which the IPs alone cannot tell apart. It holds no CIDR of
-- its own: it shares the range of the network it was split from.
alter table networks add column split_from_id uuid references networks on delete cascade;

-- The hosts moved by hand out of the network their IPs give (from_network_id,
-- found by CIDR) into a network split from it; kept across snapshots.
create table host_networks (
  host_id uuid not null references hosts on delete cascade,
  from_network_id uuid not null references networks on delete cascade,
  network_id uuid not null references networks on delete cascade,
  primary key (host_id, from_network_id)
);
create index idx_host_networks_network on host_networks (network_id);
