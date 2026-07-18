\set ON_ERROR_STOP on
\getenv legacy_host LEGACY_DB_HOST
\getenv legacy_port LEGACY_DB_PORT
\getenv legacy_database LEGACY_DB_NAME
\getenv legacy_user LEGACY_DB_USER
\getenv legacy_password LEGACY_DB_PASSWORD
\getenv legacy_sslmode LEGACY_DB_SSLMODE

\ir foreign_source.sql
\ir import.sql
\ir verify.sql

DROP SERVER remak_legacy_source CASCADE;
DROP SCHEMA legacy_src;
DROP SCHEMA legacy_stage CASCADE;
