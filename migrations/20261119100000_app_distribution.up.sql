-- The product an app is built on, over its framework: Sulu or Akeneo on
-- Symfony.
alter table apps add column distribution text;
alter table apps add column distribution_version text;
