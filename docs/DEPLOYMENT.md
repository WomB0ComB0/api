# Deployment Guide

## Prerequisites

- Docker and Docker Compose installed
- Domain configured (api.mikeodnis.dev, cdn.mikeodnis.dev)
- Cloudflare R2 bucket created
- BetterStack account (optional, for monitoring)

## Initial Setup

### 1. Clone and Configure

```bash
git clone <your-repo>
cd api
cp .env.example .env
```

### 2. Configure Environment Variables

Edit `.env` with your actual values:

```bash
# Essential configurations
JWT_SECRET=$(openssl rand -hex 32)
HASURA_GRAPHQL_ADMIN_SECRET=$(openssl rand -hex 32)

# Cloudflare R2
CLOUDFLARE_R2_ACCOUNT_ID=your-account-id
CLOUDFLARE_R2_ACCESS_KEY_ID=your-access-key
CLOUDFLARE_R2_SECRET_ACCESS_KEY=your-secret-key
CLOUDFLARE_R2_BUCKET=your-bucket-name

# Database
DATABASE_URL=postgresql://postgres:yourpassword@postgres:5432/api
```

### 3. DNS Configuration

Point your domains to your server:

```
A    api.mikeodnis.dev        -> YOUR_SERVER_IP
A    cdn.mikeodnis.dev        -> YOUR_SERVER_IP (or R2 custom domain)
```

### 4. Start Services

```bash
# Development
docker-compose up -d

# Production
docker-compose -f docker-compose.yml -f docker-compose.prod.yml up -d
```

### 5. Verify Deployment

```bash
# Check all services are healthy
curl https://api.mikeodnis.dev/health

# Check individual services
curl https://api.mikeodnis.dev/v1/core/health
curl https://api.mikeodnis.dev/v1/media/health
curl https://api.mikeodnis.dev/v1/links/healthz
```

## Service URLs

- **Gateway Dashboard**: https://api.mikeodnis.dev/dashboard
- **Core API Docs**: https://api.mikeodnis.dev/v1/core/docs
- **Media API Spec**: https://api.mikeodnis.dev/v1/media/openapi.json
- **Hasura Console**: https://api.mikeodnis.dev/v1/links/console
- **GraphQL Playground**: https://api.mikeodnis.dev/v1/links/graphql

## Hasura Setup

### 1. Apply Metadata

```bash
docker exec -it api-hasura hasura-cli metadata apply \
  --admin-secret YOUR_HASURA_SECRET \
  --endpoint http://localhost:8080
```

### 2. Run Migrations

```bash
docker exec -it api-hasura hasura-cli migrate apply \
  --admin-secret YOUR_HASURA_SECRET \
  --endpoint http://localhost:8080
```

### 3. Enable Allow List

The allow list is automatically applied via metadata. Only pre-approved queries can be executed in production.

## SSL/TLS Configuration

Traefik automatically provisions Let's Encrypt certificates. Ensure:

1. Ports 80 and 443 are open
2. Domain DNS is correctly configured
3. `letsencrypt/acme.json` is writable (chmod 600)

```bash
mkdir -p letsencrypt
chmod 600 letsencrypt/acme.json
```

## Scaling

### Horizontal Scaling

Scale individual services:

```bash
docker-compose up -d --scale core-service=3 --scale media-service=2
```

Traefik automatically load balances across instances.

### Vertical Scaling

Adjust resource limits in `docker-compose.prod.yml`.

## Monitoring

### BetterStack Integration

1. Create source in BetterStack
2. Add source token to `.env`:

```bash
BETTERSTACK_SOURCE_TOKEN=your-token
```

3. Health checks are sent via GitHub Actions after each deployment

### Prometheus Metrics

All services expose metrics at `/metrics`:

```bash
curl https://api.mikeodnis.dev/v1/core/metrics
```

### Log Aggregation

Structured JSON logs from all services:

```bash
docker-compose logs -f --tail=100 core-service
docker-compose logs -f --tail=100 media-service
```

## Backup and Recovery

### Database Backup

```bash
docker exec api-postgres pg_dump -U postgres api > backup-$(date +%Y%m%d).sql
```

### Restore Database

```bash
cat backup-20240101.sql | docker exec -i api-postgres psql -U postgres api
```

### R2 Backup

R2 objects are automatically versioned. Enable lifecycle rules in Cloudflare dashboard.

## Troubleshooting

### Services Not Starting

```bash
# Check logs
docker-compose logs -f

# Verify network
docker network inspect api_api-network
```

### Database Connection Issues

```bash
# Test connection
docker exec -it api-postgres psql -U postgres -d api -c "SELECT 1"
```

### SSL Certificate Issues

```bash
# Remove old certificates
rm -rf letsencrypt/acme.json
docker-compose restart traefik
```

### Rate Limiting

Adjust per-service rate limits in `docker-compose.yml`:

```yaml
- "traefik.http.middlewares.core-ratelimit.ratelimit.average=200"
```

## Security Checklist

- [ ] Change default passwords in `.env`
- [ ] Rotate JWT secrets quarterly
- [ ] Enable Cloudflare proxy for DDoS protection
- [ ] Configure firewall (UFW/iptables)
- [ ] Enable fail2ban for SSH
- [ ] Regular security scans (GitHub Actions)
- [ ] Monitor logs for suspicious activity
- [ ] Keep dependencies updated

## Performance Optimization

### Database

```sql
-- Add indexes for frequently queried fields
CREATE INDEX CONCURRENTLY idx_users_email ON core.users(email);

-- Enable query plan caching
ALTER SYSTEM SET plan_cache_mode = 'force_generic_plan';
```

### Traefik

```yaml
# Enable compression
- "--entrypoints.websecure.http.middlewares=compress@file"
```

### R2 CDN

Configure caching headers:

```go
ContentType: aws.String(handler.Header.Get("Content-Type")),
CacheControl: aws.String("public, max-age=31536000, immutable"),
```

## Maintenance

### Update Services

```bash
git pull origin main
docker-compose pull
docker-compose up -d --remove-orphans
docker system prune -f
```

### Database Migrations

```bash
# Create migration
docker exec -it api-hasura hasura-cli migrate create "add_users_table" \
  --admin-secret YOUR_SECRET

# Apply migration
docker exec -it api-hasura hasura-cli migrate apply \
  --admin-secret YOUR_SECRET
```

## Support

For issues or questions:
- Check logs: `docker-compose logs -f`
- Health status: `curl https://api.mikeodnis.dev/health`
- GitHub Issues: [Link to your repo]
