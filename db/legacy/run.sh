#!/usr/bin/env bash
set -euo pipefail

required=(
  DATABASE_URL
  LEGACY_DB_HOST
  LEGACY_DB_PORT
  LEGACY_DB_NAME
  LEGACY_DB_USER
  LEGACY_DB_PASSWORD
  LEGACY_DB_SSLMODE
)

for name in "${required[@]}"; do
  if [[ -z "${!name:-}" ]]; then
    echo "missing required environment variable: ${name}" >&2
    exit 1
  fi
done

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
psql "${DATABASE_URL}" --file "${script_dir}/run.sql"

