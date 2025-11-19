#!/bin/bash

# Production deployment script

set -e

echo "🚀 Deploying to Production"
echo "=========================="
echo ""

# Check if on main branch
BRANCH=$(git rev-parse --abbrev-ref HEAD)
if [ "$BRANCH" != "main" ]; then
    echo "❌ Error: You must be on the main branch to deploy to production"
    exit 1
fi

# Check for uncommitted changes
if ! git diff-index --quiet HEAD --; then
    echo "❌ Error: You have uncommitted changes"
    exit 1
fi

# Pull latest changes
echo "📥 Pulling latest changes..."
git pull origin main

# Backup database
echo ""
echo "💾 Creating database backup..."
./scripts/backup-db.sh

# Build images
echo ""
echo "🏗️  Building images..."
docker-compose build

# Run tests
echo ""
echo "🧪 Running tests..."
make test || {
    echo "❌ Tests failed! Deployment aborted."
    exit 1
}

# Pull latest images
echo ""
echo "📦 Pulling latest images..."
docker-compose pull

# Deploy with zero-downtime
echo ""
echo "🎯 Deploying services..."
docker-compose -f docker-compose.yml -f docker-compose.prod.yml up -d --remove-orphans

# Wait for services to be healthy
echo ""
echo "⏳ Waiting for services to be healthy..."
sleep 30

# Health check
echo ""
echo "🏥 Running health checks..."
./scripts/test-api.sh https://api.mikeodnis.dev || {
    echo "❌ Health check failed! Rolling back..."
    docker-compose -f docker-compose.yml -f docker-compose.prod.yml down
    exit 1
}

# Cleanup
echo ""
echo "🧹 Cleaning up..."
docker system prune -f

echo ""
echo "✅ Deployment successful!"
echo ""
echo "📊 Service Status:"
docker-compose ps
