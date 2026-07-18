\set ON_ERROR_STOP on

CREATE EXTENSION IF NOT EXISTS postgres_fdw;

DROP SERVER IF EXISTS remak_legacy_source CASCADE;
CREATE SERVER remak_legacy_source
  FOREIGN DATA WRAPPER postgres_fdw
  OPTIONS (
    host :'legacy_host',
    port :'legacy_port',
    dbname :'legacy_database',
    sslmode :'legacy_sslmode'
  );

CREATE USER MAPPING FOR CURRENT_USER
  SERVER remak_legacy_source
  OPTIONS (user :'legacy_user', password :'legacy_password');

DROP SCHEMA IF EXISTS legacy_src CASCADE;
CREATE SCHEMA legacy_src;

CREATE FOREIGN TABLE legacy_src.users (
  id BIGINT,
  uid TEXT,
  email TEXT,
  password TEXT,
  name TEXT,
  image_url TEXT,
  role TEXT,
  created_at TIMESTAMP(3),
  updated_at TIMESTAMP(3)
) SERVER remak_legacy_source OPTIONS (schema_name 'public', table_name 'users');

CREATE FOREIGN TABLE legacy_src.document (
  id BIGINT,
  doc_id TEXT,
  title TEXT,
  type TEXT,
  url TEXT,
  content TEXT,
  summary TEXT,
  status TEXT,
  thumbnail_url TEXT,
  file_size BIGINT,
  user_id BIGINT,
  created_at TIMESTAMP(3),
  updated_at TIMESTAMP(3)
) SERVER remak_legacy_source OPTIONS (schema_name 'public', table_name 'document');

CREATE FOREIGN TABLE legacy_src.tag (
  id BIGINT,
  name TEXT,
  user_id BIGINT,
  created_at TIMESTAMP(3),
  updated_at TIMESTAMP(3)
) SERVER remak_legacy_source OPTIONS (schema_name 'public', table_name 'tag');

CREATE FOREIGN TABLE legacy_src.collection (
  id BIGINT,
  name TEXT,
  description TEXT,
  user_id BIGINT,
  created_at TIMESTAMP(3),
  updated_at TIMESTAMP(3)
) SERVER remak_legacy_source OPTIONS (schema_name 'public', table_name 'collection');

CREATE FOREIGN TABLE legacy_src.embedded_text (
  id BIGINT,
  document_id BIGINT,
  user_id BIGINT,
  type TEXT,
  chapter TEXT,
  content TEXT,
  start_page_number INTEGER,
  start_line_number INTEGER,
  end_page_number INTEGER,
  end_line_number INTEGER,
  created_at TIMESTAMP(3),
  vector vector(1536)
) SERVER remak_legacy_source OPTIONS (schema_name 'public', table_name 'embedded_text');

CREATE FOREIGN TABLE legacy_src.document_tag (
  document_id BIGINT OPTIONS (column_name 'A'),
  tag_id BIGINT OPTIONS (column_name 'B')
) SERVER remak_legacy_source OPTIONS (schema_name 'public', table_name '_DocumentToTag');

CREATE FOREIGN TABLE legacy_src.collection_document (
  collection_id BIGINT OPTIONS (column_name 'A'),
  document_id BIGINT OPTIONS (column_name 'B')
) SERVER remak_legacy_source OPTIONS (schema_name 'public', table_name '_CollectionToDocument');

-- Materialize one consistent source snapshot. Besides preventing changes during
-- import, this converts Prisma enums to the declared TEXT columns before filters
-- are evaluated, avoiding postgres_fdw enum/text operator pushdown surprises.
DROP SCHEMA IF EXISTS legacy_stage CASCADE;
CREATE SCHEMA legacy_stage;

BEGIN ISOLATION LEVEL REPEATABLE READ;
CREATE TABLE legacy_stage.users AS TABLE legacy_src.users;
CREATE TABLE legacy_stage.document AS TABLE legacy_src.document;
CREATE TABLE legacy_stage.tag AS TABLE legacy_src.tag;
CREATE TABLE legacy_stage.collection AS TABLE legacy_src.collection;
CREATE TABLE legacy_stage.embedded_text AS TABLE legacy_src.embedded_text;
CREATE TABLE legacy_stage.document_tag AS TABLE legacy_src.document_tag;
CREATE TABLE legacy_stage.collection_document AS TABLE legacy_src.collection_document;
COMMIT;
