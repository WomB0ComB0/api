#!/usr/bin/env bash
# Idempotently initialize/update the PostgreSQL schemas for any hosted database (Railway, Neon, VPS, local).
# Usage: DATABASE_URL="postgresql://..." ./scripts/db-init.sh

set -euo pipefail

if [ -z "${DATABASE_URL:-}" ]; then
  echo "Error: DATABASE_URL environment variable is required." >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCHEMA_FILE="${SCRIPT_DIR}/../database/init/01-schema.sql"

echo "Applying schema from ${SCHEMA_FILE}..."
psql "${DATABASE_URL}" -v ON_ERROR_STOP=1 -f "${SCHEMA_FILE}"
echo "Database schema applied successfully."
