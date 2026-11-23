#!/bin/bash

set -euo pipefail

echo "API Infrastructure Setup (api.mikeodnis.dev)"
echo "============================================"

check_prerequisite() {
    if ! command -v "$1" &> /dev/null; then
        echo "Error: $1 is not installed. Please install it first."
        exit 1
    fi
}

check_prerequisite docker
check_prerequisite openssl

if ! docker compose version &> /dev/null; then
    echo "Error: 'docker compose' plugin is required."
    exit 1
fi

JWT_SECRET=$(openssl rand -hex 32)
SIGNING_SECRET=$(openssl rand -hex 32)
HASURA_SECRET=$(openssl rand -hex 32)
DB_PASSWORD=$(openssl rand -hex 24)
TRAEFIK_PASSWORD=$(openssl rand -base64 16)

if command -v htpasswd &> /dev/null; then
    TRAEFIK_PASSWORD_HASH=$(htpasswd -nbB admin "$TRAEFIK_PASSWORD" | cut -d ":" -f 2)
else
    TRAEFIK_PASSWORD_HASH=$(docker run --rm httpd:2.4-alpine htpasswd -nbB admin "$TRAEFIK_PASSWORD" | cut -d ":" -f 2)
fi
# Escape '$' as '$$' for Docker Compose interpolation
TRAEFIK_PASSWORD_HASH_ESCAPED="${TRAEFIK_PASSWORD_HASH//\$/\$\$}"

umask 077

cat > .env << EOF
# Gateway
TRAEFIK_DASHBOARD_USER=admin
TRAEFIK_DASHBOARD_PASSWORD=$TRAEFIK_PASSWORD_HASH_ESCAPED

# Shared Auth & CDN Signing Secrets
JWT_SECRET=$JWT_SECRET
JWT_ACCESS_TTL=15m
JWT_REFRESH_TTL_DAYS=30
SIGNING_SECRET=$SIGNING_SECRET

# Cloudflare R2 & Cache Purge (REPLACE WITH YOUR VALUES)
CLOUDFLARE_R2_ACCOUNT_ID=your-account-id
CLOUDFLARE_R2_ACCESS_KEY_ID=your-access-key
CLOUDFLARE_R2_SECRET_ACCESS_KEY=your-secret-key
CLOUDFLARE_R2_BUCKET=mikeodnis-cdn
CLOUDFLARE_R2_PUBLIC_URL=https://cdn.mikeodnis.dev
CLOUDFLARE_ZONE_ID=
CLOUDFLARE_API_TOKEN=

# Database
POSTGRES_DB=api
POSTGRES_USER=postgres
DATABASE_PASSWORD=$DB_PASSWORD
DATABASE_URL=postgresql://postgres:$DB_PASSWORD@postgres:5432/api

# Hasura
HASURA_GRAPHQL_ADMIN_SECRET=$HASURA_SECRET
HASURA_GRAPHQL_JWT_SECRET={"type":"HS256","key":"$JWT_SECRET"}
HASURA_GRAPHQL_UNAUTHORIZED_ROLE=anonymous

# Observability (leave blank to disable OTEL export locally)
OTEL_EXPORTER_OTLP_ENDPOINT=
OTEL_EXPORTER_OTLP_HEADERS=
OTEL_EXPORTER_ENDPOINT=
OTEL_EXPORTER_TOKEN=

# Environment
NODE_ENV=development
GO_ENV=development
EOF

chmod 600 .env
mkdir -p letsencrypt logs/traefik backups
touch letsencrypt/acme.json
chmod 600 letsencrypt/acme.json

echo "Created .env (chmod 600)."
echo "Next steps:"
echo "  1. Fill in CLOUDFLARE_R2_* (and optional CLOUDFLARE_ZONE_ID/API_TOKEN) in .env"
echo "  2. Sync SIGNING_SECRET to cdn.mikeodnis.dev Worker: wrangler secret put SIGNING_SECRET"
echo "  3. Start locally: make dev"
echo "  4. Verify: make health"
