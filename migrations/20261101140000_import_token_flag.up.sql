-- The token of the imported snapshots is found by a flag, not by its label,
-- which an administrator could give another token. The oldest revoked
-- token labelled "snapshot import" is the one the import created.
alter table enrollment_tokens add column for_import boolean not null default false;
update enrollment_tokens set for_import = true
where id = (select id from enrollment_tokens where label = 'snapshot import' and revoked_at is not null
            order by created_at, id limit 1);
create unique index enrollment_tokens_for_import on enrollment_tokens (for_import) where for_import;
