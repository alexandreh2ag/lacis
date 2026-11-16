-- The last sign-in of a user, with a password or with SSO.
alter table users add column last_login_at timestamptz;
alter table users add column last_login_method text;
