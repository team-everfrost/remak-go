-- +goose Up
ALTER TABLE documents
  ADD COLUMN search_vector TSVECTOR
  GENERATED ALWAYS AS (
    setweight(to_tsvector('simple', coalesce(title, '')), 'A') ||
    setweight(to_tsvector('simple', coalesce(summary, '')), 'B') ||
    setweight(to_tsvector('simple', coalesce(content, '')), 'C')
  ) STORED;

CREATE INDEX documents_search_vector_idx
  ON documents USING gin (search_vector)
  WHERE deleted_at IS NULL;

CREATE INDEX documents_content_trgm_idx
  ON documents USING gin (content gin_trgm_ops)
  WHERE deleted_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS documents_content_trgm_idx;
DROP INDEX IF EXISTS documents_search_vector_idx;
ALTER TABLE documents DROP COLUMN IF EXISTS search_vector;
