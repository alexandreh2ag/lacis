-- The administration lists the enrollment tokens by creation date; the
-- tokens created before get the date of the migration.
alter table enrollment_tokens add column created_at timestamptz not null default now();
