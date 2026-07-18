\set ON_ERROR_STOP on

SELECT 'accounts' AS entity,
       (SELECT count(*) FROM legacy_stage.users) AS source_count,
       (SELECT count(*) FROM accounts) AS target_count
UNION ALL
SELECT 'documents',
       (SELECT count(*) FROM legacy_stage.document),
       (SELECT count(*) FROM documents)
UNION ALL
SELECT 'tags',
       (SELECT count(*) FROM legacy_stage.tag),
       (SELECT count(*) FROM tags)
UNION ALL
SELECT 'collections',
       (SELECT count(*) FROM legacy_stage.collection),
       (SELECT count(*) FROM collections);

SELECT count(*) FILTER (WHERE generated_new_id) AS regenerated_account_ids,
       count(*) FILTER (WHERE NOT generated_new_id) AS preserved_account_ids
FROM legacy_migration.account_id_map;

SELECT count(*) FILTER (WHERE generated_new_id) AS regenerated_document_ids,
       count(*) FILTER (WHERE NOT generated_new_id) AS preserved_document_ids
FROM legacy_migration.document_id_map;

SELECT count(*) FILTER (WHERE password IS NOT NULL) AS source_passwords,
       count(*) FILTER (WHERE password IS NOT NULL AND password !~ '^\$2[aby]\$[0-9]{2}\$') AS forced_password_resets
FROM legacy_stage.users;

SELECT source.status AS legacy_status,
       target.status AS migrated_status,
       count(*) AS document_count
FROM legacy_stage.document source
JOIN legacy_migration.document_id_map map ON map.legacy_id = source.id
JOIN documents target ON target.id = map.new_id
GROUP BY source.status, target.status
ORDER BY source.status, target.status;

SELECT count(*) AS skipped_cross_owner_tag_links
FROM legacy_stage.document_tag source
JOIN legacy_stage.document document ON document.id = source.document_id
JOIN legacy_stage.tag tag ON tag.id = source.tag_id
WHERE document.user_id <> tag.user_id;

SELECT count(*) AS skipped_cross_owner_collection_links
FROM legacy_stage.collection_document source
JOIN legacy_stage.document document ON document.id = source.document_id
JOIN legacy_stage.collection collection ON collection.id = source.collection_id
WHERE document.user_id <> collection.user_id;

SELECT count(*) AS documents_without_versions
FROM documents document
LEFT JOIN document_versions version
  ON version.document_id = document.id AND version.version = document.current_version
WHERE version.id IS NULL;

SELECT count(*) AS invalid_current_chunks
FROM chunks chunk
JOIN documents document ON document.id = chunk.document_id
WHERE chunk.document_version <> document.current_version;

SELECT type, state, count(*)
FROM ingestion_jobs
GROUP BY type, state
ORDER BY type, state;
