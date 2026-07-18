#!/usr/bin/env bash
set -euo pipefail

source_db="remak_legacy_source_test"
target_db="remak_legacy_target_test"
container_id="$(docker compose ps -q postgres)"

if [[ -z "${container_id}" ]]; then
  echo "postgres compose service is not running" >&2
  exit 1
fi

cleanup() {
  docker exec "${container_id}" psql -v ON_ERROR_STOP=1 -U remak -d postgres \
    -c "DROP DATABASE IF EXISTS ${target_db}" \
    -c "DROP DATABASE IF EXISTS ${source_db}" >/dev/null
}
trap cleanup EXIT

cleanup
docker exec "${container_id}" psql -v ON_ERROR_STOP=1 -U remak -d postgres \
  -c "CREATE DATABASE ${source_db}" \
  -c "CREATE DATABASE ${target_db}" >/dev/null

docker exec -i "${container_id}" psql -v ON_ERROR_STOP=1 -U remak -d "${source_db}" \
  < test/fixtures/legacy.sql >/dev/null

if command -v goose >/dev/null 2>&1; then
  goose -dir db/migrations postgres \
    "postgres://remak:remak@localhost:5432/${target_db}?sslmode=disable" up >/dev/null
else
  go run github.com/pressly/goose/v3/cmd/goose@v3.26.0 \
    -dir db/migrations postgres \
    "postgres://remak:remak@localhost:5432/${target_db}?sslmode=disable" up >/dev/null
fi

docker cp db/legacy/. "${container_id}:/tmp/remak-legacy-rehearsal/"
docker exec \
  -e LEGACY_DB_HOST=127.0.0.1 \
  -e LEGACY_DB_PORT=5432 \
  -e LEGACY_DB_NAME="${source_db}" \
  -e LEGACY_DB_USER=remak \
  -e LEGACY_DB_PASSWORD=remak \
  -e LEGACY_DB_SSLMODE=disable \
  "${container_id}" psql -v ON_ERROR_STOP=1 -U remak -d "${target_db}" \
  -f /tmp/remak-legacy-rehearsal/run.sql >/dev/null

result="$(docker exec "${container_id}" psql -v ON_ERROR_STOP=1 -U remak -d "${target_db}" -Atc "
  SELECT
    (SELECT count(*) FROM accounts) = 2,
    (SELECT count(*) FROM documents) = 3,
    (SELECT count(*) FROM chunks) = 1,
    (SELECT count(*) FROM outbox_events) = 1,
    (SELECT count(*) FROM document_tags) = 2,
    (SELECT count(*) FROM collection_documents) = 1,
    (SELECT count(*) FROM accounts WHERE email='plus@example.com' AND plan='PLUS' AND password_hash IS NULL) = 1,
    (SELECT count(*) FROM document_versions WHERE raw_artifact_key='750e8400-e29b-41d4-a716-446655440000') = 1,
    (SELECT count(*) FROM outbox_events WHERE id::TEXT = payload->>'eventId') = 1;
")"

if [[ "${result}" != "t|t|t|t|t|t|t|t|t" ]]; then
  echo "legacy rehearsal assertions failed: ${result}" >&2
  exit 1
fi

echo "legacy migration rehearsal passed"
