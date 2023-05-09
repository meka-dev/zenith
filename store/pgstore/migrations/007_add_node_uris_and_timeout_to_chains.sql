alter table chains add column node_uris text[] not null default '{}';
alter table chains add column timeout text not null default '1s';
