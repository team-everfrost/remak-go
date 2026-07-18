-- name: DeleteOldVerificationChallenges :execrows
DELETE FROM verification_challenges
WHERE expires_at < now() - interval '7 days';

-- name: DeleteOldInboxEvents :execrows
DELETE FROM inbox_events
WHERE processed_at < now() - interval '30 days';

-- name: DeleteOldOutboxEvents :execrows
DELETE FROM outbox_events
WHERE published_at < now() - interval '30 days'
   OR dead_lettered_at < now() - interval '90 days';

-- name: DeleteOldIngestionJobs :execrows
DELETE FROM ingestion_jobs
WHERE state IN ('SUCCEEDED', 'CANCELLED')
  AND completed_at < now() - interval '30 days';

-- name: HardDeleteCleanedDocuments :execrows
DELETE FROM documents document
USING artifact_cleanup_jobs cleanup
WHERE cleanup.document_id = document.id
  AND cleanup.state = 'SUCCEEDED'
  AND document.deleted_at < now() - interval '30 days';

-- name: HardDeleteWithdrawnAccounts :execrows
DELETE FROM accounts account
WHERE account.deleted_at < now() - interval '7 days'
  AND NOT EXISTS (
    SELECT 1
    FROM documents document
    LEFT JOIN artifact_cleanup_jobs cleanup ON cleanup.document_id = document.id
    WHERE document.owner_id = account.id
      AND (cleanup.id IS NULL OR cleanup.state <> 'SUCCEEDED')
  );
