#!/bin/bash

# Database backup script with rotation

set -e

BACKUP_DIR="backups"
RETENTION_DAYS=30
TIMESTAMP=$(date +%Y%m%d-%H%M%S)
BACKUP_FILE="$BACKUP_DIR/backup-$TIMESTAMP.sql"

mkdir -p "$BACKUP_DIR"

echo "📦 Creating database backup..."
docker exec api-postgres pg_dump -U postgres api > "$BACKUP_FILE"

if [ -f "$BACKUP_FILE" ]; then
    # Compress backup
    gzip "$BACKUP_FILE"
    echo "✅ Backup created: $BACKUP_FILE.gz"
    
    # Calculate size
    SIZE=$(du -h "$BACKUP_FILE.gz" | cut -f1)
    echo "📊 Backup size: $SIZE"
    
    # Remove old backups
    echo "🧹 Cleaning old backups (older than $RETENTION_DAYS days)..."
    find "$BACKUP_DIR" -name "backup-*.sql.gz" -mtime +$RETENTION_DAYS -delete
    
    # Count remaining backups
    COUNT=$(find "$BACKUP_DIR" -name "backup-*.sql.gz" | wc -l)
    echo "📁 Total backups: $COUNT"
else
    echo "❌ Backup failed!"
    exit 1
fi
