#!/bin/bash

# Production deployment script for api.mikeodnis.dev

set -euo pipefail

echo "Deploying api.mikeodnis.dev to Production"
echo "========================================="

BRANCH=$(git rev-parse --abbrev-ref HEAD)
if [ "$BRANCH" != "master" ] && [ "$BRANCH" != "main" ]; then
    echo "Error: You must be on master or main branch to deploy (current: $BRANCH)"
    exit 1
fi

if ! git diff-index --quiet HEAD --; then
    echo "Error: You have uncommitted changes"
    exit 1
fi

echo "Pulling latest changes..."
git pull origin "$BRANCH"

echo "Running tests..."
make test

echo "Creating database backup..."
./scripts/backup-db.sh || echo "Warning: backup skipped (postgres may not be running yet)"

echo "Building and deploying services..."
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d --build --remove-orphans

echo "Waiting for services to settle..."
sleep 15

echo "Running health checks..."
./scripts/test-api.sh https://api.mikeodnis.dev

echo "Cleaning up dangling images..."
docker image prune -f

echo "Deployment successful!"
docker compose ps
