# Quick Reference

## URLs

### Development
- API Gateway: `http://localhost`
- Unified API Docs: `http://localhost/docs`
- Core OpenAPI Docs: `http://localhost/v1/core/docs`
- Traefik Dashboard: `http://localhost:8080`

### Production
- API Gateway & Live Console: `https://api.mikeodnis.dev`
- Unified OpenAPI Docs (Core + Media + CDN): `https://api.mikeodnis.dev/docs`
- LLM Discovery: `https://api.mikeodnis.dev/llms.txt` & `https://api.mikeodnis.dev/llms-full.txt`
- Core API: `https://api.mikeodnis.dev/v1/core`
- Media API: `https://api.mikeodnis.dev/v1/media`
- Links GraphQL: `https://api.mikeodnis.dev/v1/links/graphql`
- Aggregate Health: `https://api.mikeodnis.dev/health`
- CDN Edge & Upload Studio PWA: `https://cdn.mikeodnis.dev` (`https://cdn.mikeodnis.dev/upload`)

## Common Tasks

### Start/Stop Services
```bash
make dev          # Start development
make stop         # Stop all services
make restart      # Restart all services
```

### View Logs
```bash
make logs              # All services
make logs-core         # Core service only
make logs-media        # Media service only
docker compose logs -f # Follow all logs
```

### Database
```bash
make db-shell                    # Open PostgreSQL shell
make db-backup                   # Create backup (umask 077)
make db-restore FILE=backup.sql  # Restore from backup
```

### Scaling
```bash
make scale-core N=3    # Run 3 core service instances
make scale-media N=2   # Run 2 media service instances
```

### Health Checks
```bash
make health                                   # All services
curl -sS https://api.mikeodnis.dev/health     # Aggregate health (2s coalesced)
curl -sS https://api.mikeodnis.dev/v1/core/health/ready
curl -sS https://api.mikeodnis.dev/v1/media/health/ready
curl -sS https://cdn.mikeodnis.dev/health
```

### Testing
```bash
make test                                        # Run all tests
(cd services/core && bun test)                   # Test Core service (Bun + Elysia)
(cd services/media && go vet ./... && go test -race ./...) # Test Media service (Go)
./scripts/test-api.sh                            # Integration tests
```

## API Examples

### Login & Capture JWT (Core API)
```bash
TOKEN=$(curl -fsS -X POST https://api.mikeodnis.dev/v1/core/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"user@example.com","password":"StrongPassword123!"}' | jq -r '.access_token')
```

### Upload File (Media API)
```bash
curl -fsS -X POST https://api.mikeodnis.dev/v1/media/upload \
  -H "Authorization: Bearer $TOKEN" \
  -F "visibility=public" \
  -F "file=@image.jpg"
```

### Mint Signed URL for Private Asset (Media API)
```bash
curl -fsS -X POST https://api.mikeodnis.dev/v1/media/sign \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"asset_id":"<uuid>","expires_in":120}'
```

### GraphQL Query (Links)
```bash
curl -fsS -X POST https://api.mikeodnis.dev/v1/links/graphql \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"query":"query { links_links { id slug target_url } }"}'
```

## Environment Variables

### Essential Shared Secrets
```bash
JWT_SECRET=...                      # >=32B HS256 secret (Core + Media + Hasura)
SIGNING_SECRET=...                  # >=32B HMAC secret (Media + CDN Worker)
GATEWAY_PROXY_SECRET=...            # 32B secret (Railway Gateway + CDN Worker)
HASURA_GRAPHQL_ADMIN_SECRET=...     # Hasura admin secret (blocked at gateway)
DATABASE_URL=postgresql://...       # PostgreSQL 16 connection string
```

### Distributed Rate Limiting (Core — Upstash Redis)
```bash
UPSTASH_REDIS_REST_URL=https://...
UPSTASH_REDIS_REST_TOKEN=...
```

### Cloudflare R2 & Purge (Media)
```bash
CLOUDFLARE_R2_ACCOUNT_ID=...
CLOUDFLARE_R2_ACCESS_KEY_ID=...
CLOUDFLARE_R2_SECRET_ACCESS_KEY=...
CLOUDFLARE_R2_BUCKET=cdn
CLOUDFLARE_R2_PUBLIC_URL=https://cdn.mikeodnis.dev
CLOUDFLARE_ZONE_ID=...
CLOUDFLARE_API_TOKEN=...
```

### Monitoring (Optional — Axiom / OTLP)
```bash
OTEL_EXPORTER_OTLP_ENDPOINT=https://us-east-1.aws.edge.axiom.co
OTEL_EXPORTER_OTLP_HEADERS="Authorization=Bearer xaat-...,X-Axiom-Dataset=api-mikeodnis-dev-traces"
```

## File Structure

```text
api/
├── services/
│   ├── gateway/       # Caddy 2.11 edge proxy, Live Console, Scalar /docs, llms.txt
│   ├── core/          # Bun + ElysiaJS auth, users, API keys, Upstash Redis rate limiter
│   ├── media/         # Go 1.26 hexagonal media service (R2, HMAC sign, purge)
│   └── health/        # Python 3.12 health aggregator
├── database/
│   └── init/          # PostgreSQL 16 schemas & indexes
├── hasura/
│   └── metadata/      # GraphQL metadata & permissions
├── traefik/
│   └── dynamic/       # Optional VPS Traefik config
├── scripts/           # Utility, DB backup, and test scripts
├── docs/              # Documentation
├── docker-compose.yml
├── Makefile
└── README.md
```

## Security Checklist

- [ ] Provision unique ≥32-byte `JWT_SECRET`, `SIGNING_SECRET`, `GATEWAY_PROXY_SECRET`, and `HASURA_GRAPHQL_ADMIN_SECRET`
- [ ] Keep `GATEWAY_PROXY_SECRET` synced between Railway `gateway` and Cloudflare `cdn-worker`
- [ ] Verify Hasura admin paths (`/v1/metadata`, `/v1/query`, `/console`) return `403`/`404` through gateway
- [ ] Keep dependencies updated and run `bun install --frozen-lockfile` / `go test -race ./...`
- [ ] Run DB backups with `umask 077` (built into `scripts/backup-db.sh`)
- [ ] Enable 2FA on Railway, Cloudflare, and GitHub accounts

## Links

- [Full API Documentation](API.md)
- [Deployment Guide](DEPLOYMENT.md)
- [Architecture Details](ARCHITECTURE.md)
- [Contributing Guide](../CONTRIBUTING.md)
