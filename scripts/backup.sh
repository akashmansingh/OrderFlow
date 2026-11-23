#!/usr/bin/env bash
# backup.sh: Creates a compressed, timestamped PostgreSQL logical backup
set -euo pipefail

BACKUP_DIR="${BACKUP_DIR:-backups}"
mkdir -p "$BACKUP_DIR"

TIMESTAMP=$(date -u +"%Y%m%d_%H%M%SZ")
BACKUP_FILE="${BACKUP_DIR}/orderflow_${TIMESTAMP}.sql.gz"

DB_HOST="${DB_HOST:-postgres}"
DB_PORT="${DB_PORT:-5432}"
DB_USER="${POSTGRES_USER:-orderflow}"
DB_NAME="${POSTGRES_DB:-orderflow}"

echo "Starting OrderFlow database backup to ${BACKUP_FILE}..."

if command -v docker &> /dev/null && docker compose ps postgres &> /dev/null; then
  docker compose exec -T postgres pg_dump -U "$DB_USER" "$DB_NAME" | gzip -9 > "$BACKUP_FILE"
else
  PGPASSWORD="${POSTGRES_PASSWORD:-local-dev-password}" pg_dump -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" "$DB_NAME" | gzip -9 > "$BACKUP_FILE"
fi

SIZE=$(du -h "$BACKUP_FILE" | cut -f1)
echo "Backup completed successfully: ${BACKUP_FILE} (${SIZE})"

# Optional object storage upload if S3 bucket configured
if [ -n "${S3_BACKUP_BUCKET:-}" ]; then
  echo "Uploading backup to s3://${S3_BACKUP_BUCKET}/..."
  aws s3 cp "$BACKUP_FILE" "s3://${S3_BACKUP_BUCKET}/orderflow_${TIMESTAMP}.sql.gz"
fi
