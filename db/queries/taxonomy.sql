-- name: ListTags :many
SELECT t.id, t.name, COUNT(dt.document_id)::bigint AS document_count
FROM tags t
LEFT JOIN document_tags dt ON dt.tag_id = t.id
WHERE t.owner_id = sqlc.arg(owner_id)::uuid
  AND (sqlc.arg(query)::text = '' OR t.name ILIKE '%' || sqlc.arg(query) || '%')
GROUP BY t.id, t.name
ORDER BY document_count DESC, t.name
LIMIT sqlc.arg(page_size)::int OFFSET sqlc.arg(page_offset)::int;

-- name: CreateTag :one
INSERT INTO tags (id, owner_id, name)
VALUES ($1, $2, $3)
ON CONFLICT (owner_id, name)
DO UPDATE SET updated_at = now()
RETURNING *;

-- name: AddTagToDocument :exec
INSERT INTO document_tags (document_id, tag_id)
SELECT d.id, t.id
FROM documents d, tags t
WHERE d.id = sqlc.arg(document_id)
  AND t.id = sqlc.arg(tag_id)
  AND d.owner_id = sqlc.arg(owner_id)
  AND t.owner_id = sqlc.arg(owner_id)
  AND d.deleted_at IS NULL
ON CONFLICT DO NOTHING;

-- name: RemoveTagFromDocument :exec
DELETE FROM document_tags dt
USING documents d, tags t
WHERE dt.document_id = d.id
  AND dt.tag_id = t.id
  AND dt.document_id = sqlc.arg(document_id)
  AND dt.tag_id = sqlc.arg(tag_id)
  AND d.owner_id = sqlc.arg(owner_id)
  AND t.owner_id = sqlc.arg(owner_id);

-- name: DeleteDocumentTags :exec
DELETE FROM document_tags WHERE document_id = $1;

-- name: DeleteUnusedTags :exec
DELETE FROM tags tag
WHERE tag.owner_id = $1
  AND NOT EXISTS (
    SELECT 1 FROM document_tags relation WHERE relation.tag_id = tag.id
  );

-- name: DeleteDocumentCollections :exec
DELETE FROM collection_documents WHERE document_id = $1;

-- name: ListCollections :many
SELECT c.id, c.name, c.description, COUNT(cd.document_id)::bigint AS document_count
FROM collections c
LEFT JOIN collection_documents cd ON cd.collection_id = c.id
WHERE c.owner_id = $1
GROUP BY c.id, c.name, c.description
ORDER BY c.updated_at DESC, c.id DESC
LIMIT $2 OFFSET $3;

-- name: GetCollectionByName :one
SELECT * FROM collections WHERE owner_id = $1 AND name = $2;

-- name: CreateCollection :one
INSERT INTO collections (id, owner_id, name, description)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: UpdateCollection :one
UPDATE collections
SET name = $3, description = $4, updated_at = now()
WHERE owner_id = $1 AND name = $2
RETURNING *;

-- name: DeleteCollection :execrows
DELETE FROM collections WHERE owner_id = $1 AND name = $2;

-- name: AddDocumentToCollection :exec
INSERT INTO collection_documents (collection_id, document_id)
SELECT c.id, d.id
FROM collections c, documents d
WHERE c.id = sqlc.arg(collection_id)
  AND d.id = sqlc.arg(document_id)
  AND c.owner_id = sqlc.arg(owner_id)
  AND d.owner_id = sqlc.arg(owner_id)
  AND d.deleted_at IS NULL
ON CONFLICT DO NOTHING;

-- name: RemoveDocumentFromCollection :exec
DELETE FROM collection_documents
WHERE collection_id = $1 AND document_id = $2;

-- name: RemoveOwnedDocumentFromCollection :exec
DELETE FROM collection_documents cd
USING collections c, documents d
WHERE cd.collection_id = c.id
  AND cd.document_id = d.id
  AND cd.collection_id = sqlc.arg(collection_id)
  AND cd.document_id = sqlc.arg(document_id)
  AND c.owner_id = sqlc.arg(owner_id)
  AND d.owner_id = sqlc.arg(owner_id);
