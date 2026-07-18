-- name: CreateVerificationChallenge :one
INSERT INTO verification_challenges (
  id, email, purpose, code_hash, max_attempts, expires_at
)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetVerificationChallenge :one
SELECT * FROM verification_challenges
WHERE id = $1
  AND email = $2
  AND purpose = $3
  AND consumed_at IS NULL;

-- name: GetLatestOpenChallenge :one
SELECT * FROM verification_challenges
WHERE email = $1
  AND purpose = $2
  AND consumed_at IS NULL
  AND verified_at IS NULL
  AND expires_at > now()
  AND attempt_count < max_attempts
ORDER BY created_at DESC
LIMIT 1;

-- name: GetLatestVerifiedChallenge :one
SELECT * FROM verification_challenges
WHERE email = $1
  AND purpose = $2
  AND consumed_at IS NULL
  AND verified_at IS NOT NULL
  AND expires_at > now()
ORDER BY verified_at DESC
LIMIT 1;

-- name: CountRecentChallenges :one
SELECT COUNT(*)::bigint
FROM verification_challenges
WHERE email = $1
  AND purpose = $2
  AND created_at > now() - interval '1 hour';

-- name: IncrementChallengeAttempt :one
UPDATE verification_challenges
SET attempt_count = attempt_count + 1
WHERE id = $1
  AND consumed_at IS NULL
  AND verified_at IS NULL
  AND expires_at > now()
  AND attempt_count < max_attempts
RETURNING *;

-- name: MarkChallengeVerified :execrows
UPDATE verification_challenges
SET verified_at = now()
WHERE id = $1
  AND verified_at IS NULL
  AND consumed_at IS NULL
  AND expires_at > now()
  AND attempt_count <= max_attempts;

-- name: ConsumeChallenge :execrows
UPDATE verification_challenges
SET consumed_at = now()
WHERE id = $1
  AND verified_at IS NOT NULL
  AND consumed_at IS NULL
  AND expires_at > now();

-- name: DeleteExpiredChallenges :exec
DELETE FROM verification_challenges
WHERE expires_at < now() - interval '7 days';

-- name: DeleteChallengesByEmail :exec
DELETE FROM verification_challenges WHERE email = $1;
