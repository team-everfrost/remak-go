-- +goose Up
ALTER TABLE document_versions ADD COLUMN media_type TEXT;

CREATE TABLE artifact_cleanup_jobs (
  id UUID PRIMARY KEY,
  document_id UUID NOT NULL UNIQUE REFERENCES documents(id) ON DELETE CASCADE,
  state ingestion_job_state NOT NULL DEFAULT 'QUEUED',
  attempt_count INTEGER NOT NULL DEFAULT 0,
  available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_error TEXT,
  started_at TIMESTAMPTZ,
  completed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX artifact_cleanup_jobs_pending_idx
  ON artifact_cleanup_jobs (state, available_at, created_at)
  WHERE state IN ('QUEUED', 'PROCESSING', 'FAILED');

-- +goose Down
DROP TABLE IF EXISTS artifact_cleanup_jobs;
ALTER TABLE document_versions DROP COLUMN IF EXISTS media_type;
