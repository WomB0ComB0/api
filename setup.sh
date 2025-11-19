#!/bin/bash

set -e

echo "🚀 API Infrastructure Setup"
echo "=========================="
echo ""

# Check prerequisites
check_prerequisite() {
    if ! command -v $1 &> /dev/null; then
        echo "❌ $1 is not installed. Please install it first."
        exit 1
    fi
    echo "✅ $1 is installed"
}

echo "📋 Checking prerequisites..."
check_prerequisite docker
check_prerequisite docker-compose
check_prerequisite openssl

# Generate secrets
echo ""
echo "🔐 Generating secrets..."
JWT_SECRET=$(openssl rand -hex 32)
HASURA_SECRET=$(openssl rand -hex 32)
TRAEFIK_PASSWORD=$(openssl rand -base64 12)
TRAEFIK_PASSWORD_HASH=$(docker run --rm httpd:2.4-alpine htpasswd -nbB admin "$TRAEFIK_PASSWORD" | cut -d ":" -f 2)

echo "✅ Secrets generated"

# Create .env file
echo ""
echo "📝 Creating .env file..."
cat > .env << EOF
# Gateway
TRAEFIK_DASHBOARD_USER=admin
TRAEFIK_DASHBOARD_PASSWORD=$TRAEFIK_PASSWORD_HASH
JWT_SECRET=$JWT_SECRET

# Cloudflare R2 (REPLACE WITH YOUR VALUES)
CLOUDFLARE_R2_ACCOUNT_ID=your-account-id
CLOUDFLARE_R2_ACCESS_KEY_ID=your-access-key
CLOUDFLARE_R2_SECRET_ACCESS_KEY=your-secret-key
CLOUDFLARE_R2_BUCKET=your-bucket-name
CLOUDFLARE_R2_PUBLIC_URL=https://cdn.mikeodnis.dev

# Database
DATABASE_PASSWORD=$(openssl rand -hex 16)
DATABASE_URL=postgresql://postgres:$(openssl rand -hex 16)@postgres:5432/api

# Hasura
HASURA_GRAPHQL_ADMIN_SECRET=$HASURA_SECRET
HASURA_GRAPHQL_JWT_SECRET='{"type":"HS256","key":"$JWT_SECRET"}'
HASURA_GRAPHQL_UNAUTHORIZED_ROLE=anonymous

# Observability
OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4318
BETTERSTACK_SOURCE_TOKEN=your-betterstack-token

# Rate Limiting
RATE_LIMIT_REQUESTS=100
RATE_LIMIT_WINDOW=60s

# Environment
NODE_ENV=development
GO_ENV=development
EOF

echo "✅ .env file created"

# Create necessary directories
echo ""
echo "📁 Creating directories..."
mkdir -p letsencrypt logs backups
touch letsencrypt/acme.json
chmod 600 letsencrypt/acme.json
echo "✅ Directories created"

# Save credentials to file
echo ""
echo "💾 Saving credentials..."
cat > .credentials << EOF
API Infrastructure Credentials
==============================
Generated: $(date)

Traefik Dashboard:
  URL: http://localhost:8080
  Username: admin
  Password: $TRAEFIK_PASSWORD

JWT Secret: $JWT_SECRET

Hasura Admin Secret: $HASURA_SECRET

Database Password: (see .env file)

⚠️  IMPORTANT: Keep this file secure and do not commit it to Git!
EOF

echo "✅ Credentials saved to .credentials"

# Summary
echo ""
echo "✨ Setup Complete!"
echo ""
echo "📝 Next Steps:"
echo "1. Edit .env and add your Cloudflare R2 credentials"
echo "2. Configure DNS to point to your server IP"
echo "3. Run 'make dev' to start development environment"
echo "4. Run 'make prod' to start production environment"
echo ""
echo "📚 Documentation:"
echo "  - README.md - Getting started"
echo "  - docs/DEPLOYMENT.md - Deployment guide"
echo "  - docs/API.md - API documentation"
echo "  - docs/ARCHITECTURE.md - Architecture overview"
echo ""
echo "🔑 Credentials saved in .credentials (keep this file safe!)"
echo ""
echo "Happy coding! 🚀"
