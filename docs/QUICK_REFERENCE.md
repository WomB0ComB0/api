# Quick Reference

## URLs

### Development
- API Gateway: http://localhost
- Traefik Dashboard: http://localhost:8080
- Core API Docs: http://localhost/v1/core/docs
- Hasura Console: http://localhost:8080/console

### Production
- API Gateway: https://api.mikeodnis.dev
- Core API: https://api.mikeodnis.dev/v1/core
- Media API: https://api.mikeodnis.dev/v1/media
- Links GraphQL: https://api.mikeodnis.dev/v1/links/graphql
- CDN: https://cdn.mikeodnis.dev

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
docker-compose logs -f # Follow all logs
```

### Database
```bash
make db-shell          # Open PostgreSQL shell
make db-backup         # Create backup
make db-restore FILE=backup.sql  # Restore from backup
```

### Scaling
```bash
make scale-core N=3    # Run 3 core service instances
make scale-media N=2   # Run 2 media service instances
```

### Health Checks
```bash
make health                              # All services
curl http://localhost/health             # Aggregate health
curl http://localhost/v1/core/health     # Core service
curl http://localhost/v1/media/health    # Media service
```

### Testing
```bash
make test                    # Run all tests
cd services/core && npm test # Test core service
cd services/media && go test # Test media service
./scripts/test-api.sh        # Integration tests
```

## API Examples

### Create User (Core API)
```bash
curl -X POST http://localhost/v1/core/users \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"email":"user@example.com","name":"John Doe"}'
```

### Upload File (Media API)
```bash
curl -X POST http://localhost/v1/media/upload \
  -H "Authorization: Bearer $TOKEN" \
  -F "file=@image.jpg"
```

### GraphQL Query (Links)
```bash
curl -X POST http://localhost/v1/links/graphql \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "query { links_links { id slug target_url } }"
  }'
```

## Environment Variables

### Essential
```bash
JWT_SECRET=xxx                      # JWT signing secret
HASURA_GRAPHQL_ADMIN_SECRET=xxx    # Hasura admin access
DATABASE_URL=postgresql://...       # Database connection
```

### Cloudflare R2
```bash
CLOUDFLARE_R2_ACCOUNT_ID=xxx
CLOUDFLARE_R2_ACCESS_KEY_ID=xxx
CLOUDFLARE_R2_SECRET_ACCESS_KEY=xxx
CLOUDFLARE_R2_BUCKET=xxx
CLOUDFLARE_R2_PUBLIC_URL=https://cdn.mikeodnis.dev
```

### Monitoring (Optional)
```bash
BETTERSTACK_SOURCE_TOKEN=xxx
OTEL_EXPORTER_OTLP_ENDPOINT=http://collector:4318
```

## Troubleshooting

### Services won't start
```bash
# Check logs
make logs

# Verify network
docker network ls
docker network inspect api_api-network
```

### Database connection issues
```bash
# Test connection
make db-shell

# Restart PostgreSQL
docker-compose restart postgres
```

### Port conflicts
```bash
# Check what's using port 80
sudo lsof -i :80

# Change ports in docker-compose.yml
# ports:
#   - "8000:80"  # Use 8000 instead of 80
```

### SSL/Certificate issues
```bash
# Remove old certificates
rm -rf letsencrypt/acme.json
chmod 600 letsencrypt/acme.json
docker-compose restart traefik
```

## File Structure
```
api/
├── services/
│   ├── core/          # Node.js service
│   ├── media/         # Go service
│   └── health/        # Python aggregator
├── database/
│   └── init/          # Database schemas
├── hasura/
│   └── metadata/      # GraphQL config
├── traefik/
│   └── dynamic/       # Gateway config
├── scripts/           # Utility scripts
├── docs/              # Documentation
├── docker-compose.yml
├── Makefile
└── README.md
```

## Security Checklist

- [ ] Change default passwords in `.env`
- [ ] Configure Cloudflare proxy for DDoS protection
- [ ] Enable firewall (ports 80, 443 only)
- [ ] Rotate JWT secrets regularly
- [ ] Keep dependencies updated
- [ ] Review logs for suspicious activity
- [ ] Enable 2FA on all cloud accounts

## Performance Tips

### Database
- Add indexes for frequently queried columns
- Use connection pooling (already configured)
- Monitor slow queries

### Caching
- Configure Hasura query caching
- Add Redis for session storage
- Use CDN for static assets

### Scaling
- Scale services independently
- Use read replicas for database
- Deploy to multiple regions

## Links

- [Full API Documentation](docs/API.md)
- [Deployment Guide](docs/DEPLOYMENT.md)
- [Architecture Details](docs/ARCHITECTURE.md)
- [Contributing Guide](CONTRIBUTING.md)
