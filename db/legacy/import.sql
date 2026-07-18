\set ON_ERROR_STOP on

BEGIN ISOLATION LEVEL SERIALIZABLE;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM accounts)
     OR EXISTS (SELECT 1 FROM documents)
     OR EXISTS (SELECT 1 FROM tags)
     OR EXISTS (SELECT 1 FROM collections) THEN
    RAISE EXCEPTION 'legacy import requires an empty target database';
  END IF;

  IF EXISTS (
    SELECT lower(email)
    FROM legacy_stage.users
    GROUP BY lower(email)
    HAVING count(*) > 1
  ) THEN
    RAISE EXCEPTION 'legacy users contain case-insensitive duplicate emails';
  END IF;

  IF EXISTS (
    SELECT 1 FROM legacy_stage.document
    WHERE type::TEXT NOT IN ('WEBPAGE', 'MEMO', 'IMAGE', 'FILE')
       OR status::TEXT NOT IN (
         'SCRAPE_PENDING', 'SCRAPE_PROCESSING', 'SCRAPE_REJECTED',
         'EMBED_PENDING', 'EMBED_PROCESSING', 'EMBED_REJECTED', 'COMPLETED'
       )
  ) THEN
    RAISE EXCEPTION 'legacy documents contain an unknown type or status';
  END IF;

  IF EXISTS (
    SELECT 1 FROM legacy_stage.users
    WHERE role::TEXT NOT IN ('BASIC', 'PLUS', 'ADMIN')
  ) THEN
    RAISE EXCEPTION 'legacy users contain an unknown role';
  END IF;

  IF EXISTS (
    SELECT 1 FROM legacy_stage.document
    WHERE status::TEXT LIKE 'SCRAPE_%'
      AND nullif(btrim(url), '') IS NULL
  ) THEN
    RAISE EXCEPTION 'a legacy scrape document has no source URL';
  END IF;
END $$;

CREATE SCHEMA legacy_migration;

CREATE FUNCTION legacy_migration.try_uuid(value TEXT)
RETURNS UUID
LANGUAGE plpgsql
IMMUTABLE
STRICT
AS $$
BEGIN
  RETURN value::UUID;
EXCEPTION WHEN invalid_text_representation THEN
  RETURN NULL;
END;
$$;

CREATE TABLE legacy_migration.account_id_map (
  legacy_id BIGINT PRIMARY KEY,
  legacy_public_id TEXT NOT NULL,
  new_id UUID NOT NULL UNIQUE,
  generated_new_id BOOLEAN NOT NULL
);

CREATE TABLE legacy_migration.document_id_map (
  legacy_id BIGINT PRIMARY KEY,
  legacy_public_id TEXT NOT NULL,
  new_id UUID NOT NULL UNIQUE,
  generated_new_id BOOLEAN NOT NULL
);

CREATE TABLE legacy_migration.tag_id_map (
  legacy_id BIGINT PRIMARY KEY,
  new_id UUID NOT NULL UNIQUE
);

CREATE TABLE legacy_migration.collection_id_map (
  legacy_id BIGINT PRIMARY KEY,
  new_id UUID NOT NULL UNIQUE
);

INSERT INTO legacy_migration.account_id_map (
  legacy_id, legacy_public_id, new_id, generated_new_id
)
SELECT id,
       uid,
       coalesce(legacy_migration.try_uuid(uid), uuidv7()),
       legacy_migration.try_uuid(uid) IS NULL
FROM legacy_stage.users;

INSERT INTO legacy_migration.document_id_map (
  legacy_id, legacy_public_id, new_id, generated_new_id
)
SELECT id,
       doc_id,
       coalesce(legacy_migration.try_uuid(doc_id), uuidv7()),
       legacy_migration.try_uuid(doc_id) IS NULL
FROM legacy_stage.document;

INSERT INTO legacy_migration.tag_id_map (legacy_id, new_id)
SELECT id, uuidv7() FROM legacy_stage.tag;

INSERT INTO legacy_migration.collection_id_map (legacy_id, new_id)
SELECT id, uuidv7() FROM legacy_stage.collection;

INSERT INTO accounts (
  id, email, password_hash, name, image_url, role, plan, created_at, updated_at
)
SELECT map.new_id,
       source.email,
       CASE
         WHEN source.password ~ '^\$2[aby]\$[0-9]{2}\$' THEN source.password
         ELSE NULL
       END,
       source.name,
       source.image_url,
       CASE WHEN source.role::TEXT = 'ADMIN' THEN 'ADMIN'::account_role ELSE 'USER'::account_role END,
       CASE WHEN source.role::TEXT = 'PLUS' THEN 'PLUS'::account_plan ELSE 'FREE'::account_plan END,
       source.created_at AT TIME ZONE 'UTC',
       source.updated_at AT TIME ZONE 'UTC'
FROM legacy_stage.users source
JOIN legacy_migration.account_id_map map ON map.legacy_id = source.id;

INSERT INTO documents (
  id, owner_id, title, type, source_url, content, summary, status,
  current_version, thumbnail_url, file_size, created_at, updated_at
)
SELECT document_map.new_id,
       account_map.new_id,
       source.title,
       (source.type::TEXT)::document_type,
       source.url,
       source.content,
       source.summary,
       CASE
         WHEN source.status::TEXT LIKE 'SCRAPE_%' THEN 'SCRAPE_PENDING'::document_status
         WHEN source.status::TEXT LIKE 'EMBED_%' THEN 'ENRICH_PENDING'::document_status
         ELSE 'COMPLETED'::document_status
       END,
       1,
       source.thumbnail_url,
       greatest(coalesce(source.file_size, 0), 0),
       source.created_at AT TIME ZONE 'UTC',
       source.updated_at AT TIME ZONE 'UTC'
FROM legacy_stage.document source
JOIN legacy_migration.document_id_map document_map ON document_map.legacy_id = source.id
JOIN legacy_migration.account_id_map account_map ON account_map.legacy_id = source.user_id;

INSERT INTO document_versions (
  id, document_id, version, raw_artifact_key, content_hash, title, content,
  summary, extraction_method, created_at, completed_at
)
SELECT uuidv7(),
       document_map.new_id,
       1,
       CASE WHEN source.type::TEXT IN ('IMAGE', 'FILE') THEN source.doc_id ELSE NULL END,
       CASE
         WHEN source.content IS NULL THEN NULL
         ELSE encode(digest(source.content, 'sha256'), 'hex')
       END,
       source.title,
       source.content,
       source.summary,
       'legacy-database',
       source.created_at AT TIME ZONE 'UTC',
       CASE
         WHEN source.status::TEXT = 'COMPLETED' THEN source.updated_at AT TIME ZONE 'UTC'
         ELSE NULL
       END
FROM legacy_stage.document source
JOIN legacy_migration.document_id_map document_map ON document_map.legacy_id = source.id;

INSERT INTO tags (id, owner_id, name, created_at, updated_at)
SELECT tag_map.new_id,
       account_map.new_id,
       CASE
         WHEN char_length(btrim(source.name)) BETWEEN 1 AND 64 THEN btrim(source.name)
         WHEN char_length(btrim(source.name)) = 0 THEN 'legacy-tag-' || source.id
         ELSE left(btrim(source.name), 35) || '~legacy-' || source.id
       END,
       source.created_at AT TIME ZONE 'UTC',
       source.updated_at AT TIME ZONE 'UTC'
FROM legacy_stage.tag source
JOIN legacy_migration.tag_id_map tag_map ON tag_map.legacy_id = source.id
JOIN legacy_migration.account_id_map account_map ON account_map.legacy_id = source.user_id;

INSERT INTO collections (id, owner_id, name, description, created_at, updated_at)
SELECT collection_map.new_id,
       account_map.new_id,
       CASE
         WHEN char_length(btrim(source.name)) BETWEEN 1 AND 100 THEN btrim(source.name)
         WHEN char_length(btrim(source.name)) = 0 THEN 'legacy-collection-' || source.id
         ELSE left(btrim(source.name), 71) || '~legacy-' || source.id
       END,
       source.description,
       source.created_at AT TIME ZONE 'UTC',
       source.updated_at AT TIME ZONE 'UTC'
FROM legacy_stage.collection source
JOIN legacy_migration.collection_id_map collection_map ON collection_map.legacy_id = source.id
JOIN legacy_migration.account_id_map account_map ON account_map.legacy_id = source.user_id;

INSERT INTO document_tags (document_id, tag_id)
SELECT document_map.new_id, tag_map.new_id
FROM legacy_stage.document_tag source
JOIN legacy_migration.document_id_map document_map ON document_map.legacy_id = source.document_id
JOIN legacy_migration.tag_id_map tag_map ON tag_map.legacy_id = source.tag_id
JOIN documents document ON document.id = document_map.new_id
JOIN tags tag ON tag.id = tag_map.new_id AND tag.owner_id = document.owner_id
ON CONFLICT DO NOTHING;

INSERT INTO collection_documents (collection_id, document_id, created_at)
SELECT collection_map.new_id, document_map.new_id, document.created_at
FROM legacy_stage.collection_document source
JOIN legacy_migration.collection_id_map collection_map ON collection_map.legacy_id = source.collection_id
JOIN legacy_migration.document_id_map document_map ON document_map.legacy_id = source.document_id
JOIN documents document ON document.id = document_map.new_id
JOIN collections collection
  ON collection.id = collection_map.new_id AND collection.owner_id = document.owner_id
ON CONFLICT DO NOTHING;

WITH ranked_chunks AS (
  SELECT source.*,
         row_number() OVER (
           PARTITION BY source.document_id
           ORDER BY source.start_page_number NULLS FIRST,
                    source.start_line_number NULLS FIRST,
                    source.id
         ) - 1 AS chunk_index
  FROM legacy_stage.embedded_text source
  WHERE nullif(btrim(source.content), '') IS NOT NULL
)
INSERT INTO chunks (
  id, document_id, document_version, chunk_index, section_title, content,
  page_number, embedding_model, embedding, created_at
)
SELECT uuidv7(),
       document_map.new_id,
       1,
       source.chunk_index::INTEGER,
       source.chapter,
       source.content,
       source.start_page_number,
       'legacy-embedding-1536',
       source.vector,
       source.created_at AT TIME ZONE 'UTC'
FROM ranked_chunks source
JOIN legacy_migration.document_id_map document_map ON document_map.legacy_id = source.document_id;

INSERT INTO ingestion_jobs (
  id, document_id, document_version, type, state, created_at, updated_at
)
SELECT uuidv7(),
       document.id,
       1,
       CASE
         WHEN document.status = 'SCRAPE_PENDING' THEN 'SCRAPE'::ingestion_job_type
         ELSE 'ENRICH'::ingestion_job_type
       END,
       'QUEUED',
       document.updated_at,
       document.updated_at
FROM documents document
WHERE document.status IN ('SCRAPE_PENDING', 'ENRICH_PENDING');

WITH scrape_events AS (
  SELECT uuidv7() AS event_id,
         document.id AS document_id,
         document.updated_at,
         document.source_url,
         job.id AS job_id
  FROM documents document
  JOIN ingestion_jobs job
    ON job.document_id = document.id
   AND job.document_version = 1
   AND job.type = 'SCRAPE'
  WHERE document.status = 'SCRAPE_PENDING'
)
INSERT INTO outbox_events (
  id, event_type, aggregate_id, payload, created_at
)
SELECT event.event_id,
       'document.scrape.requested.v1',
       event.document_id,
       jsonb_build_object(
         'schemaVersion', '1.0',
         'eventId', event.event_id,
         'eventType', 'document.scrape.requested.v1',
         'traceId', 'legacy-migration',
         'occurredAt', now(),
         'data', jsonb_build_object(
           'jobId', event.job_id,
           'documentId', event.document_id,
           'documentVersion', 1,
           'url', event.source_url
         )
       ),
       event.updated_at
FROM scrape_events event;

DO $$
DECLARE
  source_count BIGINT;
  target_count BIGINT;
BEGIN
  SELECT count(*) INTO source_count FROM legacy_stage.users;
  SELECT count(*) INTO target_count FROM accounts;
  IF source_count <> target_count THEN
    RAISE EXCEPTION 'account count mismatch: source %, target %', source_count, target_count;
  END IF;

  SELECT count(*) INTO source_count FROM legacy_stage.document;
  SELECT count(*) INTO target_count FROM documents;
  IF source_count <> target_count THEN
    RAISE EXCEPTION 'document count mismatch: source %, target %', source_count, target_count;
  END IF;

  IF EXISTS (
    SELECT 1
    FROM documents document
    LEFT JOIN document_versions version
      ON version.document_id = document.id AND version.version = 1
    WHERE version.id IS NULL
  ) THEN
    RAISE EXCEPTION 'one or more documents have no version';
  END IF;

  IF EXISTS (
    SELECT 1 FROM ingestion_jobs job
    JOIN documents document ON document.id = job.document_id
    WHERE (job.type = 'SCRAPE' AND document.status <> 'SCRAPE_PENDING')
       OR (job.type = 'ENRICH' AND document.status <> 'ENRICH_PENDING')
  ) THEN
    RAISE EXCEPTION 'job and document status mismatch';
  END IF;
END $$;

COMMIT;
