-- name: CreateIngestionJob :one
INSERT INTO ingestion_jobs (
  id, document_id, document_version, type, state
)
VALUES ($1, $2, $3, $4, 'QUEUED')
ON CONFLICT (document_id, document_version, type)
DO UPDATE SET updated_at = ingestion_jobs.updated_at
RETURNING *;

-- name: GetIngestionJob :one
SELECT * FROM ingestion_jobs WHERE id = $1;

-- name: ClaimEnrichmentJobs :many
UPDATE ingestion_jobs
SET state = 'PROCESSING',
    attempt_count = attempt_count + 1,
    started_at = COALESCE(started_at, now()),
    completed_at = NULL,
    updated_at = now()
WHERE id IN (
  SELECT id
  FROM ingestion_jobs
  WHERE type = 'ENRICH'
    AND (
      state IN ('QUEUED', 'FAILED')
      OR (state = 'PROCESSING' AND updated_at < now() - interval '10 minutes')
    )
    AND available_at <= now()
    AND attempt_count < 5
  ORDER BY created_at
  LIMIT $1
  FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: MarkJobProcessing :execrows
UPDATE ingestion_jobs
SET state = 'PROCESSING',
    attempt_count = attempt_count + 1,
    started_at = COALESCE(started_at, now()),
    completed_at = NULL,
    updated_at = now()
WHERE id = $1 AND state IN ('QUEUED', 'FAILED');

-- name: CompleteJob :execrows
UPDATE ingestion_jobs
SET state = 'SUCCEEDED', completed_at = now(), updated_at = now()
WHERE id = $1 AND state IN ('QUEUED', 'PROCESSING', 'SUCCEEDED');

-- name: FailJob :execrows
UPDATE ingestion_jobs
SET state = 'FAILED',
    last_error_code = $2,
    last_error_message = $3,
    available_at = now() + (sqlc.arg(backoff_seconds)::bigint * interval '1 second'),
    completed_at = now(),
    updated_at = now()
WHERE id = $1 AND state IN ('QUEUED', 'PROCESSING');

-- name: CreateOutboxEvent :exec
INSERT INTO outbox_events (id, event_type, aggregate_id, payload)
VALUES ($1, $2, $3, $4);

-- name: ClaimOutboxEvents :many
UPDATE outbox_events
SET lock_token = sqlc.arg(lock_token),
    locked_until = now() + interval '30 seconds'
WHERE id IN (
  SELECT id
  FROM outbox_events
  WHERE published_at IS NULL
    AND dead_lettered_at IS NULL
    AND available_at <= now()
    AND (locked_until IS NULL OR locked_until < now())
  ORDER BY created_at
  LIMIT sqlc.arg(batch_size)
  FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: MarkOutboxPublished :execrows
UPDATE outbox_events
SET published_at = now(), last_error = NULL
WHERE id = $1 AND lock_token = $2 AND published_at IS NULL;

-- name: RescheduleOutboxEvent :exec
UPDATE outbox_events
SET attempt_count = attempt_count + 1,
    available_at = now() + (sqlc.arg(backoff_seconds)::bigint * interval '1 second'),
    last_error = sqlc.arg(last_error),
    lock_token = NULL,
    locked_until = NULL
WHERE id = sqlc.arg(id) AND lock_token = sqlc.arg(lock_token) AND published_at IS NULL;

-- name: DeadLetterOutboxEvent :execrows
UPDATE outbox_events
SET attempt_count = attempt_count + 1,
    last_error = $2,
    dead_lettered_at = now(),
    lock_token = NULL,
    locked_until = NULL
WHERE id = $1 AND lock_token = $3 AND published_at IS NULL;

-- name: RegisterInboxEvent :one
INSERT INTO inbox_events (event_id, event_type)
VALUES ($1, $2)
ON CONFLICT (event_id) DO NOTHING
RETURNING event_id;

-- name: CompleteInboxEvent :exec
UPDATE inbox_events
SET processed_at = now(), last_error = NULL
WHERE event_id = $1;

-- name: FailInboxEvent :exec
UPDATE inbox_events
SET last_error = $2
WHERE event_id = $1;

-- name: ReplaceChunks :exec
DELETE FROM chunks
WHERE document_id = $1 AND document_version = $2;

-- name: CreateChunk :exec
INSERT INTO chunks (
  id, document_id, document_version, chunk_index, section_title,
  content, page_number, embedding_model, embedding
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: SearchChunksVector :many
WITH vector_settings AS MATERIALIZED (
  SELECT set_config('hnsw.iterative_scan', 'relaxed_order', true)
),
relaxed_results AS MATERIALIZED (
  SELECT c.*, d.id AS document_public_id, d.title AS document_title,
         (c.embedding <=> sqlc.arg(embedding)::vector)::float8 AS distance
  FROM vector_settings
  CROSS JOIN chunks c
  JOIN documents d ON d.id = c.document_id
  WHERE d.owner_id = sqlc.arg(owner_id)::uuid
    AND d.deleted_at IS NULL
    AND c.document_version = d.current_version
    AND c.embedding IS NOT NULL
  ORDER BY c.embedding <=> sqlc.arg(embedding)::vector
  LIMIT sqlc.arg(candidate_limit)::int
)
SELECT id, document_id, document_version, chunk_index, section_title, content,
       page_number, embedding_model, embedding, created_at,
       document_public_id, document_title, (1 - distance)::float8 AS score
FROM relaxed_results
-- pgvector 0.8 relaxed scans can be slightly out of order. PostgreSQL 18 needs
-- the explicit expression to perform a strict final ordering.
ORDER BY distance + 0;
