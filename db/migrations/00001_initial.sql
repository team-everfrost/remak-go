-- +goose Up
CREATE EXTENSION IF NOT EXISTS citext;
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TYPE account_role AS ENUM ('USER', 'ADMIN');
CREATE TYPE account_plan AS ENUM ('FREE', 'PLUS');
CREATE TYPE document_type AS ENUM ('WEBPAGE', 'MEMO', 'IMAGE', 'FILE');
CREATE TYPE document_status AS ENUM (
  'DRAFT',
  'SCRAPE_PENDING',
  'SCRAPE_PROCESSING',
  'SCRAPE_REJECTED',
  'ENRICH_PENDING',
  'ENRICH_PROCESSING',
  'ENRICH_REJECTED',
  'COMPLETED'
);
CREATE TYPE ingestion_job_type AS ENUM ('SCRAPE', 'ENRICH', 'THUMBNAIL');
CREATE TYPE ingestion_job_state AS ENUM ('QUEUED', 'PROCESSING', 'SUCCEEDED', 'FAILED', 'CANCELLED');
CREATE TYPE challenge_purpose AS ENUM ('SIGNUP', 'PASSWORD_RESET', 'WITHDRAW');

CREATE TABLE accounts (
  id UUID PRIMARY KEY,
  email CITEXT NOT NULL UNIQUE,
  password_hash TEXT,
  name TEXT,
  image_url TEXT,
  role account_role NOT NULL DEFAULT 'USER',
  plan account_plan NOT NULL DEFAULT 'FREE',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  deleted_at TIMESTAMPTZ
);

CREATE TABLE verification_challenges (
  id UUID PRIMARY KEY,
  email CITEXT NOT NULL,
  purpose challenge_purpose NOT NULL,
  code_hash TEXT NOT NULL,
  attempt_count INTEGER NOT NULL DEFAULT 0,
  max_attempts INTEGER NOT NULL DEFAULT 5,
  expires_at TIMESTAMPTZ NOT NULL,
  verified_at TIMESTAMPTZ,
  consumed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX verification_challenges_lookup_idx
  ON verification_challenges (email, purpose, created_at DESC);

CREATE TABLE refresh_tokens (
  id UUID PRIMARY KEY,
  account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  token_hash BYTEA NOT NULL UNIQUE,
  expires_at TIMESTAMPTZ NOT NULL,
  revoked_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE documents (
  id UUID PRIMARY KEY,
  owner_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  title TEXT,
  type document_type NOT NULL,
  source_url TEXT,
  content TEXT,
  summary TEXT,
  status document_status NOT NULL,
  current_version INTEGER NOT NULL DEFAULT 1 CHECK (current_version > 0),
  thumbnail_url TEXT,
  file_size BIGINT NOT NULL DEFAULT 0 CHECK (file_size >= 0),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  deleted_at TIMESTAMPTZ
);
CREATE INDEX documents_owner_created_cursor_idx
  ON documents (owner_id, created_at DESC, id DESC)
  WHERE deleted_at IS NULL;
CREATE INDEX documents_owner_updated_idx
  ON documents (owner_id, updated_at DESC)
  WHERE deleted_at IS NULL;
CREATE INDEX documents_title_trgm_idx
  ON documents USING gin (title gin_trgm_ops)
  WHERE deleted_at IS NULL;

CREATE TABLE document_versions (
  id UUID PRIMARY KEY,
  document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  version INTEGER NOT NULL CHECK (version > 0),
  raw_artifact_key TEXT,
  content_artifact_key TEXT,
  content_hash TEXT,
  title TEXT,
  content TEXT,
  summary TEXT,
  extraction_method TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  completed_at TIMESTAMPTZ,
  UNIQUE (document_id, version)
);

CREATE TABLE ingestion_jobs (
  id UUID PRIMARY KEY,
  document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  document_version INTEGER NOT NULL CHECK (document_version > 0),
  type ingestion_job_type NOT NULL,
  state ingestion_job_state NOT NULL DEFAULT 'QUEUED',
  attempt_count INTEGER NOT NULL DEFAULT 0,
  last_error_code TEXT,
  last_error_message TEXT,
  available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  started_at TIMESTAMPTZ,
  completed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (document_id, document_version, type)
);
CREATE INDEX ingestion_jobs_state_idx ON ingestion_jobs (type, state, available_at, created_at);

CREATE TABLE outbox_events (
  id UUID PRIMARY KEY,
  event_type TEXT NOT NULL,
  aggregate_id UUID NOT NULL,
  payload JSONB NOT NULL,
  attempt_count INTEGER NOT NULL DEFAULT 0,
  available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  lock_token UUID,
  locked_until TIMESTAMPTZ,
  published_at TIMESTAMPTZ,
  dead_lettered_at TIMESTAMPTZ,
  last_error TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX outbox_events_pending_idx
  ON outbox_events (available_at, created_at)
  WHERE published_at IS NULL AND dead_lettered_at IS NULL;

CREATE TABLE inbox_events (
  event_id TEXT PRIMARY KEY,
  event_type TEXT NOT NULL,
  received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  processed_at TIMESTAMPTZ,
  last_error TEXT
);

CREATE TABLE chunks (
  id UUID PRIMARY KEY,
  document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  document_version INTEGER NOT NULL,
  chunk_index INTEGER NOT NULL,
  section_title TEXT,
  content TEXT NOT NULL,
  page_number INTEGER,
  embedding_model TEXT NOT NULL,
  embedding vector(1536),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (document_id, document_version, chunk_index)
);
CREATE INDEX chunks_document_version_idx
  ON chunks (document_id, document_version, chunk_index);
CREATE INDEX chunks_embedding_hnsw_idx
  ON chunks USING hnsw (embedding vector_cosine_ops);

CREATE TABLE tags (
  id UUID PRIMARY KEY,
  owner_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 64),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (owner_id, name)
);

CREATE TABLE document_tags (
  document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  tag_id UUID NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
  PRIMARY KEY (document_id, tag_id)
);

CREATE TABLE collections (
  id UUID PRIMARY KEY,
  owner_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
  description TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (owner_id, name)
);

CREATE TABLE collection_documents (
  collection_id UUID NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
  document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (collection_id, document_id)
);

-- +goose Down
DROP TABLE IF EXISTS collection_documents;
DROP TABLE IF EXISTS collections;
DROP TABLE IF EXISTS document_tags;
DROP TABLE IF EXISTS tags;
DROP TABLE IF EXISTS chunks;
DROP TABLE IF EXISTS inbox_events;
DROP TABLE IF EXISTS outbox_events;
DROP TABLE IF EXISTS ingestion_jobs;
DROP TABLE IF EXISTS document_versions;
DROP TABLE IF EXISTS documents;
DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS verification_challenges;
DROP TABLE IF EXISTS accounts;
DROP TYPE IF EXISTS challenge_purpose;
DROP TYPE IF EXISTS ingestion_job_state;
DROP TYPE IF EXISTS ingestion_job_type;
DROP TYPE IF EXISTS document_status;
DROP TYPE IF EXISTS document_type;
DROP TYPE IF EXISTS account_plan;
DROP TYPE IF EXISTS account_role;
