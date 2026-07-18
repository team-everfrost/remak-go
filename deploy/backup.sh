#!/usr/bin/env bash
set -euo pipefail

backup_dir="${BACKUP_DIR:-./backups}"
timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "${backup_dir}"
destination="${backup_dir}/remak-${timestamp}.dump"

docker compose exec -T postgres pg_dump \
  --username "${POSTGRES_USER:-remak}" \
  --dbname "${POSTGRES_DB:-remak}" \
  --format custom \
  --no-owner \
  --no-acl > "${destination}"

docker compose exec -T postgres pg_restore --list < "${destination}" >/dev/null
echo "verified backup: ${destination}"

if [[ -n "${BACKUP_S3_URI:-}" ]]; then
  aws s3 cp "${destination}" "${BACKUP_S3_URI%/}/$(basename "${destination}")" --only-show-errors
  echo "uploaded backup: ${BACKUP_S3_URI%/}/$(basename "${destination}")"
fi
