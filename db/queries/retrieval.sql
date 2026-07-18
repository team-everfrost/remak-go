-- name: SearchDocumentsLexical :many
WITH query_input AS (
  SELECT websearch_to_tsquery('simple', sqlc.arg(query)::text) AS parsed
)
SELECT
  d.id AS document_id,
  (
    ts_rank_cd(d.search_vector, query_input.parsed, 32) * 2.0 +
    greatest(
      similarity(coalesce(d.title, ''), sqlc.arg(query)::text),
      similarity(coalesce(d.summary, ''), sqlc.arg(query)::text),
      similarity(coalesce(d.content, ''), sqlc.arg(query)::text)
    )
  )::float8 AS score
FROM documents d
CROSS JOIN query_input
WHERE d.owner_id = sqlc.arg(owner_id)::uuid
  AND d.deleted_at IS NULL
  AND (
    d.search_vector @@ query_input.parsed
    OR coalesce(d.title, '') ILIKE '%' || sqlc.arg(query)::text || '%'
    OR coalesce(d.summary, '') ILIKE '%' || sqlc.arg(query)::text || '%'
    OR coalesce(d.content, '') ILIKE '%' || sqlc.arg(query)::text || '%'
  )
ORDER BY score DESC, d.updated_at DESC, d.id DESC
LIMIT sqlc.arg(candidate_limit)::int;
