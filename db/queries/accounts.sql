-- name: CreateAccount :one
INSERT INTO accounts (id, email, password_hash, name)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetAccountByEmail :one
SELECT * FROM accounts
WHERE email = $1 AND deleted_at IS NULL;

-- name: GetAccountByID :one
SELECT * FROM accounts
WHERE id = $1 AND deleted_at IS NULL;

-- name: LockAccountForStorage :one
SELECT * FROM accounts
WHERE id = $1 AND deleted_at IS NULL
FOR UPDATE;

-- name: LockActiveAccount :one
SELECT * FROM accounts
WHERE id = $1 AND deleted_at IS NULL
FOR UPDATE;

-- name: EmailExists :one
SELECT EXISTS (
  SELECT 1 FROM accounts WHERE email = $1 AND deleted_at IS NULL
);

-- name: UpdateAccountProfile :one
UPDATE accounts
SET name = $2,
    image_url = $3,
    updated_at = now()
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: UpdatePassword :exec
UPDATE accounts
SET password_hash = $2, updated_at = now()
WHERE id = $1 AND deleted_at IS NULL;

-- name: SoftDeleteAccount :execrows
UPDATE accounts
SET email = id::text || '@deleted.remak.invalid',
    password_hash = NULL,
    name = NULL,
    image_url = NULL,
    deleted_at = now(),
    updated_at = now()
WHERE id = $1 AND deleted_at IS NULL;

-- name: GetStorageUsage :one
SELECT COALESCE(SUM(file_size), 0)::bigint AS usage
FROM documents
WHERE owner_id = $1 AND deleted_at IS NULL;

-- name: CreateRefreshToken :exec
INSERT INTO refresh_tokens (id, account_id, token_hash, expires_at)
VALUES ($1, $2, $3, $4);

-- name: GetRefreshToken :one
SELECT * FROM refresh_tokens
WHERE token_hash = $1
  AND revoked_at IS NULL
  AND expires_at > now();

-- name: RevokeRefreshToken :exec
UPDATE refresh_tokens SET revoked_at = now()
WHERE token_hash = $1 AND revoked_at IS NULL;

-- name: RevokeAllRefreshTokens :exec
UPDATE refresh_tokens SET revoked_at = now()
WHERE account_id = $1 AND revoked_at IS NULL;

-- name: SoftDeleteAccountDocuments :exec
UPDATE documents
SET deleted_at = COALESCE(deleted_at, now()), updated_at = now()
WHERE owner_id = $1 AND deleted_at IS NULL;

-- name: ScheduleAccountArtifactCleanup :exec
INSERT INTO artifact_cleanup_jobs (id, document_id)
SELECT uuidv7(), document.id
FROM documents document
WHERE document.owner_id = $1
ON CONFLICT (document_id) DO UPDATE
SET state = 'QUEUED',
    attempt_count = 0,
    available_at = now(),
    last_error = NULL,
    completed_at = NULL,
    updated_at = now();

-- name: DeleteExpiredRefreshTokens :execrows
DELETE FROM refresh_tokens
WHERE expires_at < now() - interval '7 days'
   OR revoked_at < now() - interval '7 days';
