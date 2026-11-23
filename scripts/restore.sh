#!/usr/bin/env bash
# restore.sh: Restores OrderFlow PostgreSQL database from a backup file and runs invariant verification
set -euo pipefail

BACKUP_FILE="${1:-}"
if [ -z "$BACKUP_FILE" ] || [ ! -f "$BACKUP_FILE" ]; then
  echo "Usage: $0 <path-to-backup.sql.gz>"
  exit 1
fi

DB_USER="${POSTGRES_USER:-orderflow}"
DB_NAME="${POSTGRES_DB:-orderflow}"

echo "Restoring database ${DB_NAME} from ${BACKUP_FILE}..."

if command -v docker &> /dev/null && docker compose ps postgres &> /dev/null; then
  # Stream decompress into Docker container psql
  gunzip -c "$BACKUP_FILE" | docker compose exec -T postgres psql -U "$DB_USER" -d "$DB_NAME"
else
  gunzip -c "$BACKUP_FILE" | psql -U "$DB_USER" -d "$DB_NAME"
fi

echo "Database restored successfully."
echo "Running invariant verification audit..."

if command -v go &> /dev/null; then
  go run ./cmd/verify -database-url "postgres://${DB_USER}:local-dev-password@localhost:5433/${DB_NAME}?sslmode=disable"
fi
