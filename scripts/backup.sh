#!/usr/bin/env bash
# Daily database backup — run from cron or systemd timer.
# Usage: BACKUP_DIR=/var/backups/whatsapptool DATABASE_URL=postgres://... ./backup.sh

set -euo pipefail

BACKUP_DIR="${BACKUP_DIR:-/var/backups/whatsapptool}"
DATABASE_URL="${DATABASE_URL:?DATABASE_URL must be set}"
RETAIN_DAYS="${RETAIN_DAYS:-14}"
TIMESTAMP=$(date +"%Y%m%d_%H%M%S")
FILE="${BACKUP_DIR}/whatsapptool_${TIMESTAMP}.sql.gz"

mkdir -p "$BACKUP_DIR"

echo "[backup] dumping database → $FILE"
pg_dump --no-owner --no-acl "$DATABASE_URL" | gzip > "$FILE"
echo "[backup] done — $(du -sh "$FILE" | cut -f1)"

# Remove backups older than RETAIN_DAYS
find "$BACKUP_DIR" -name 'whatsapptool_*.sql.gz' -mtime +"$RETAIN_DAYS" -delete
echo "[backup] old backups pruned (retain ${RETAIN_DAYS} days)"
