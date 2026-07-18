-- name: CreateMemo :one
INSERT INTO documents (
  id, owner_id, title, type, content, status, current_version
)
VALUES ($1, $2, $3, 'MEMO', $4, 'ENRICH_PENDING', 1)
RETURNING *;

-- name: CreateWebpage :one
INSERT INTO documents (
  id, owner_id, title, type, source_url, status, current_version
)
VALUES ($1, $2, $3, 'WEBPAGE', $4, 'SCRAPE_PENDING', 1)
RETURNING *;

-- name: CreateFileDocument :one
INSERT INTO documents (
  id, owner_id, title, type, content, status, current_version, file_size
)
VALUES ($1, $2, $3, $4, $5, $6, 1, $7)
RETURNING *;

-- name: CreateDocumentVersion :one
INSERT INTO document_versions (id, document_id, version, title, content, media_type)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetDocumentVersion :one
SELECT * FROM document_versions
WHERE document_id = $1 AND version = $2;

-- name: GetOwnedCurrentDocumentVersion :one
SELECT dv.*
FROM document_versions dv
JOIN documents d ON d.id = dv.document_id AND d.current_version = dv.version
WHERE d.id = $1
  AND d.owner_id = $2
  AND d.deleted_at IS NULL;

-- name: GetOwnedDocument :one
SELECT * FROM documents
WHERE id = $1
  AND owner_id = $2
  AND deleted_at IS NULL;

-- name: GetDocumentByID :one
SELECT * FROM documents
WHERE id = $1 AND deleted_at IS NULL;

-- name: GetOwnedDocumentsByIDs :many
SELECT * FROM documents
WHERE owner_id = sqlc.arg(owner_id)
  AND id = ANY(sqlc.arg(document_ids)::uuid[])
  AND deleted_at IS NULL;

-- name: CountOwnedDocumentsByIDs :one
SELECT COUNT(*)::bigint
FROM documents
WHERE owner_id = sqlc.arg(owner_id)
  AND id = ANY(sqlc.arg(document_ids)::uuid[])
  AND deleted_at IS NULL;

-- name: ListDocuments :many
SELECT d.* FROM documents d
WHERE d.owner_id = $1
  AND d.deleted_at IS NULL
  AND (
    sqlc.narg(cursor_id)::uuid IS NULL
    OR (d.created_at, d.id) < (
      SELECT cursor_document.created_at, cursor_document.id
      FROM documents cursor_document
      WHERE cursor_document.id = sqlc.narg(cursor_id)::uuid
        AND cursor_document.owner_id = $1
    )
  )
ORDER BY d.created_at DESC, d.id DESC
LIMIT $2;

-- name: UpdateMemo :one
UPDATE documents
SET title = $3,
    content = $4,
    current_version = current_version + 1,
    status = 'ENRICH_PENDING',
    updated_at = now()
WHERE id = $1
  AND owner_id = $2
  AND type = 'MEMO'
  AND deleted_at IS NULL
RETURNING *;

-- name: BeginWebpageRefresh :one
UPDATE documents
SET title = $3,
    source_url = $4,
    current_version = current_version + 1,
    status = 'SCRAPE_PENDING',
    updated_at = now()
WHERE id = $1
  AND owner_id = $2
  AND type = 'WEBPAGE'
  AND deleted_at IS NULL
RETURNING *;

-- name: MarkDocumentProcessing :execrows
UPDATE documents
SET status = 'SCRAPE_PROCESSING', updated_at = now()
WHERE id = $1
  AND current_version = $2
  AND status IN ('SCRAPE_PENDING', 'SCRAPE_REJECTED');

-- name: CompleteScrape :execrows
UPDATE documents
SET title = COALESCE($3, title),
    content = $4,
    thumbnail_url = $5,
    file_size = $6,
    status = 'ENRICH_PENDING',
    updated_at = now()
WHERE id = $1
  AND current_version = $2
  AND deleted_at IS NULL;

-- name: MarkDocumentEnrichProcessing :execrows
UPDATE documents
SET status = 'ENRICH_PROCESSING', updated_at = now()
WHERE id = $1
  AND current_version = $2
  AND status IN ('ENRICH_PENDING', 'ENRICH_REJECTED')
  AND deleted_at IS NULL;

-- name: CompleteEnrichment :execrows
UPDATE documents
SET summary = $3,
    status = 'COMPLETED',
    updated_at = now()
WHERE id = $1
  AND current_version = $2
  AND deleted_at IS NULL;

-- name: RejectEnrichment :execrows
UPDATE documents
SET status = 'ENRICH_REJECTED', updated_at = now()
WHERE id = $1
  AND current_version = $2
  AND deleted_at IS NULL;

-- name: RejectScrape :execrows
UPDATE documents
SET status = 'SCRAPE_REJECTED', updated_at = now()
WHERE id = $1
  AND current_version = $2
  AND deleted_at IS NULL;

-- name: CompleteDocumentVersion :execrows
UPDATE document_versions
SET raw_artifact_key = $3,
    content_artifact_key = $4,
    content_hash = $5,
    title = $6,
    content = $7,
    summary = $8,
    extraction_method = $9,
    completed_at = now()
WHERE document_id = $1 AND version = $2;

-- name: CompleteEnrichedVersion :execrows
UPDATE document_versions
SET summary = $3,
    completed_at = COALESCE(completed_at, now())
WHERE document_id = $1 AND version = $2;

-- name: SaveExtractedContent :execrows
WITH updated_version AS (
  UPDATE document_versions
  SET content = sqlc.arg(content),
      content_hash = sqlc.arg(content_hash),
      extraction_method = sqlc.arg(extraction_method),
      completed_at = COALESCE(completed_at, now())
  WHERE document_id = sqlc.arg(document_id)
    AND version = sqlc.arg(version)
  RETURNING document_id
)
UPDATE documents
SET content = sqlc.arg(content), updated_at = now()
WHERE id IN (SELECT document_id FROM updated_version)
  AND current_version = sqlc.arg(version)
  AND deleted_at IS NULL;

-- name: SoftDeleteDocument :execrows
UPDATE documents
SET deleted_at = now(), updated_at = now()
WHERE id = $1 AND owner_id = $2 AND deleted_at IS NULL;

-- name: CreateArtifactCleanupJob :exec
INSERT INTO artifact_cleanup_jobs (id, document_id)
VALUES ($1, $2)
ON CONFLICT (document_id) DO NOTHING;

-- name: RequeueArtifactCleanupJob :execrows
UPDATE artifact_cleanup_jobs
SET state = 'QUEUED',
    attempt_count = 0,
    available_at = now(),
    last_error = NULL,
    completed_at = NULL,
    updated_at = now()
WHERE document_id = $1;

-- name: ClaimArtifactCleanupJobs :many
UPDATE artifact_cleanup_jobs
SET state = 'PROCESSING',
    attempt_count = attempt_count + 1,
    started_at = COALESCE(started_at, now()),
    updated_at = now()
WHERE id IN (
  SELECT id
  FROM artifact_cleanup_jobs
  WHERE (
      state IN ('QUEUED', 'FAILED')
      OR (state = 'PROCESSING' AND updated_at < now() - interval '10 minutes')
    )
    AND available_at <= now()
    AND attempt_count < 10
  ORDER BY created_at
  LIMIT sqlc.arg(batch_size)
  FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: ListDocumentArtifactKeys :many
SELECT d.type, version.raw_artifact_key AS key
FROM documents d
JOIN document_versions version ON version.document_id = d.id
WHERE d.id = sqlc.arg(document_id)
  AND version.raw_artifact_key IS NOT NULL
UNION ALL
SELECT d.type, version.content_artifact_key AS key
FROM documents d
JOIN document_versions version ON version.document_id = d.id
WHERE d.id = sqlc.arg(document_id)
  AND version.content_artifact_key IS NOT NULL;

-- name: CompleteArtifactCleanupJob :execrows
UPDATE artifact_cleanup_jobs
SET state = 'SUCCEEDED', completed_at = now(), last_error = NULL, updated_at = now()
WHERE id = $1 AND state IN ('PROCESSING', 'SUCCEEDED');

-- name: FailArtifactCleanupJob :execrows
UPDATE artifact_cleanup_jobs
SET state = 'FAILED',
    last_error = $2,
    available_at = now() + (sqlc.arg(backoff_seconds)::bigint * interval '1 second'),
    completed_at = now(),
    updated_at = now()
WHERE id = $1 AND state = 'PROCESSING';

-- name: SearchDocumentsText :many
SELECT * FROM documents
WHERE owner_id = sqlc.arg(owner_id)::uuid
  AND deleted_at IS NULL
  AND (
    title ILIKE '%' || sqlc.arg(query) || '%'
    OR content ILIKE '%' || sqlc.arg(query) || '%'
    OR summary ILIKE '%' || sqlc.arg(query) || '%'
  )
ORDER BY
  CASE WHEN title ILIKE '%' || sqlc.arg(query) || '%' THEN 0 ELSE 1 END,
  similarity(COALESCE(title, ''), sqlc.arg(query)) DESC,
  updated_at DESC
LIMIT sqlc.arg(page_size)::int OFFSET sqlc.arg(page_offset)::int;

-- name: ListDocumentTagNames :many
SELECT dt.document_id, t.name
FROM document_tags dt
JOIN tags t ON t.id = dt.tag_id
JOIN documents d ON d.id = dt.document_id
WHERE d.owner_id = sqlc.arg(owner_id)::uuid
  AND dt.document_id = ANY(sqlc.arg(document_ids)::uuid[])
  AND d.deleted_at IS NULL
ORDER BY t.name;

-- name: ListDocumentsByTag :many
SELECT d.*
FROM documents d
JOIN document_tags dt ON dt.document_id = d.id
JOIN tags t ON t.id = dt.tag_id
WHERE d.owner_id = $1
  AND t.owner_id = $1
  AND t.name = $2
  AND d.deleted_at IS NULL
  AND (
    sqlc.narg(cursor_id)::uuid IS NULL
    OR (d.created_at, d.id) < (
      SELECT cursor_document.created_at, cursor_document.id
      FROM documents cursor_document
      WHERE cursor_document.id = sqlc.narg(cursor_id)::uuid
        AND cursor_document.owner_id = $1
    )
  )
ORDER BY d.created_at DESC, d.id DESC
LIMIT $3;

-- name: ListDocumentsByCollection :many
SELECT d.*
FROM documents d
JOIN collection_documents cd ON cd.document_id = d.id
JOIN collections c ON c.id = cd.collection_id
WHERE d.owner_id = $1
  AND c.owner_id = $1
  AND c.name = $2
  AND d.deleted_at IS NULL
  AND (
    sqlc.narg(cursor_id)::uuid IS NULL
    OR (d.created_at, d.id) < (
      SELECT cursor_document.created_at, cursor_document.id
      FROM documents cursor_document
      WHERE cursor_document.id = sqlc.narg(cursor_id)::uuid
        AND cursor_document.owner_id = $1
    )
  )
ORDER BY d.created_at DESC, d.id DESC
LIMIT $3;
