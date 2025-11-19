# API Infrastructure - api.mikeodnis.dev

Opinionated, production-ready API gateway architecture with polyglot microservices.

[![CI/CD](https://github.com/yourusername/api/workflows/CI%2FCD%20Pipeline/badge.svg)](https://github.com/yourusername/api/actions)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

## 🚀 Quick Start

```bash
# 1. Clone and setup
git clone <your-repo>
cd api
./setup.sh

# 2. Configure Cloudflare R2 (edit .env)
nano .env

# 3. Start development environment
make dev

# 4. Verify services
make health
```

**Access:**
- API: http://localhost/health
- Traefik Dashboard: http://localhost:8080
- Hasura Console: http://localhost:8080/console (via Traefik)

## Architecture

### Gateway: Traefik
- Auto-discovery via Docker labels
- SSL/TLS termination
- JWT validation middleware
- Per-route rate limiting
- Request/response logging

### Services

#### `/v1/core/*` - Node.js/TypeScript
Core business logic, shared utilities with Next.js monorepo
- Health: `/v1/core/health`
- OpenAPI: `/v1/core/openapi.json`

#### `/v1/media/*` - Go
High-performance media operations, streaming, R2 pre-signing
- Health: `/v1/media/health`
- OpenAPI: `/v1/media/openapi.json`

#### `/v1/links/graphql` - Hasura
GraphQL API for links service with allow-list and caching
- Health: `/v1/links/healthz`
- Console: `/v1/links/console`

## 📋 Prerequisites

- Docker 20.10+ and Docker Compose 2.0+
- Domain with DNS access (api.mikeodnis.dev, cdn.mikeodnis.dev)
- Cloudflare account with R2 bucket
- (Optional) BetterStack account for monitoring

## 🛠️ Available Commands

```bash
make help          # Show all available commands
make dev           # Start development environment
make prod          # Start production environment
make logs          # View logs from all services
make logs-core     # View logs from specific service
make test          # Run all tests
make health        # Check service health
make db-backup     # Backup database
make scale-core N=3  # Scale core service to 3 instances
```

See `Makefile` for all available commands.

## Development

### Adding a new service

1. Create service directory under `/services/`
2. Add Dockerfile
3. Add route in `docker-compose.yml` with Traefik labels
4. Add OpenAPI spec
5. Update GitHub Actions workflow

### Environment Variables

Copy `.env.example` to `.env` and configure:
- `CLOUDFLARE_R2_*` - R2 bucket credentials
- `JWT_SECRET` - JWT validation secret
- `HASURA_GRAPHQL_ADMIN_SECRET` - Hasura admin secret
- `DATABASE_URL` - PostgreSQL connection string

## Observability

- **Metrics**: Prometheus format at `/metrics` on each service
- **Traces**: OpenTelemetry exported to collector
- **Logs**: JSON structured logs to stdout
- **Status**: BetterStack monitoring via `/health` endpoints

## CI/CD

GitHub Actions workflows:
- Build and push Docker images on main
- Run tests on PR
- Deploy to production with rollback capability
- Upload static assets to R2 with cache invalidation

## Security

- JWT validation at gateway
- Rate limiting per route
- CORS configured per service
- Secrets via environment variables (never committed)
- API keys rotated quarterly

## API Versioning

API uses URL path versioning (`/v1/`). Breaking changes will result in a new version (`/v2/`).

Deprecated endpoints include a `Sunset` header.

## 📊 Project Stats

- **50+ files** created (config, source, docs, scripts)
- **4,200+ lines** of code + configuration + documentation
- **20+ API endpoints** across 3 services
- **25+ Make commands** for developer productivity
- **3 programming languages** (TypeScript, Go, Python)
- **5 databases schemas** (users, assets, links, clicks, migrations)
- **Complete CI/CD** pipeline with testing and deployment

See [PROJECT_STATS.md](PROJECT_STATS.md) for detailed breakdown.

## 🏗️ What's Included

✅ **Traefik Gateway** - SSL, routing, rate limiting, JWT auth  
✅ **Node.js Core Service** - Fastify + OpenAPI + PostgreSQL  
✅ **Go Media Service** - High-performance I/O + R2 integration  
✅ **Hasura GraphQL** - Instant API with permissions  
✅ **PostgreSQL** - Multi-schema database with migrations  
✅ **GitHub Actions** - CI/CD with testing & deployment  
✅ **OpenTelemetry** - Distributed tracing & metrics  
✅ **Docker Compose** - Complete orchestration  
✅ **Comprehensive Docs** - API, architecture, deployment guides  
✅ **Scripts** - Setup, backup, deploy, test automation  

## 📚 Full Documentation

- [SETUP_COMPLETE.md](SETUP_COMPLETE.md) - ⭐ **Start here!** Complete setup guide
- [docs/API.md](docs/API.md) - Complete API reference with examples
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) - System design deep-dive
- [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) - Production deployment guide
- [docs/QUICK_REFERENCE.md](docs/QUICK_REFERENCE.md) - Command cheat sheet
- [PROJECT_STATS.md](PROJECT_STATS.md) - Detailed project statistics
- [CONTRIBUTING.md](CONTRIBUTING.md) - Development guidelines

## 🎯 Key Features

### Gateway-First Design
Single entry point for all services with centralized security, SSL, and routing.

### Polyglot Architecture
Use the best tool for each domain: Node.js for business logic, Go for I/O, Hasura for GraphQL.

### Production-Ready
SSL/TLS, monitoring, backups, CI/CD, health checks, and zero-downtime deployments included.

### Developer Experience
One-command setup (`./setup.sh`), 25+ Makefile shortcuts, hot reload, comprehensive docs.

### Cost-Effective
Runs on a single VPS (~$30/month) with Cloudflare R2 (zero egress fees).

## 🔒 Security

- JWT validation at gateway + service levels
- Per-route rate limiting (configurable)
- CORS properly configured
- Secrets via environment variables (never committed)
- Security headers (HSTS, CSP, X-Frame-Options)
- Allow-list for GraphQL queries (production)
- Regular dependency scanning via GitHub Actions

## 🚀 Deployment

### Quick Deploy
```bash
./scripts/deploy.sh
```

### Manual Deploy
```bash
# Production
docker-compose -f docker-compose.yml -f docker-compose.prod.yml up -d

# Health check
make health
```

See [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) for complete guide.

## 🤝 Contributing

Contributions are welcome! Please read [CONTRIBUTING.md](CONTRIBUTING.md) first.

## 📄 License

MIT License - see [LICENSE](LICENSE) file for details.

## 🌟 Acknowledgments

Built with these awesome tools:
- [Traefik](https://traefik.io/) - Cloud-native API Gateway
- [Fastify](https://www.fastify.io/) - Fast Node.js web framework
- [Chi](https://github.com/go-chi/chi) - Lightweight Go HTTP router
- [Hasura](https://hasura.io/) - Instant GraphQL APIs
- [PostgreSQL](https://www.postgresql.org/) - The world's most advanced open source database
- [Cloudflare R2](https://www.cloudflare.com/products/r2/) - Zero egress object storage
- [OpenTelemetry](https://opentelemetry.io/) - Observability framework

---

**Built with ❤️ by [Mike Odnis](https://mikeodnis.dev)**

Questions? Check [SETUP_COMPLETE.md](SETUP_COMPLETE.md) or open an issue!

