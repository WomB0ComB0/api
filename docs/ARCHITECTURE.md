# Architecture Overview

## System Design

```
┌─────────────────────────────────────────────────────────────┐
│                    Cloudflare (DDoS, WAF)                    │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│                  Traefik Gateway (api.mikeodnis.dev)         │
│  • SSL Termination                                           │
│  • JWT Validation                                            │
│  • Rate Limiting                                             │
│  • Request Routing                                           │
│  • Load Balancing                                            │
└──────┬─────────────┬────────────────┬────────────────────────┘
       │             │                │
       ▼             ▼                ▼
┌──────────┐  ┌──────────┐    ┌──────────────┐
│  Core    │  │  Media   │    │   Hasura     │
│ Service  │  │ Service  │    │  GraphQL     │
│          │  │          │    │              │
│ Node.js  │  │    Go    │    │ /v1/links/*  │
│ /v1/core │  │/v1/media │    └──────┬───────┘
└────┬─────┘  └────┬─────┘           │
     │             │                 │
     └─────────────┴─────────────────┘
                   │
                   ▼
         ┌────────────────────┐
         │    PostgreSQL      │
         │  • core schema     │
         │  • media schema    │
         │  • links schema    │
         └────────────────────┘

┌─────────────────────────────────────────────────────────────┐
│                  Cloudflare R2 (cdn.mikeodnis.dev)           │
│  • Media Storage                                             │
│  • Global CDN                                                │
│  • Public/Private Buckets                                    │
└─────────────────────────────────────────────────────────────┘
```

## Technology Stack

### Gateway Layer
- **Traefik v2.11**: API Gateway with automatic service discovery
- **Let's Encrypt**: Automated SSL/TLS certificate management
- **Docker Labels**: Dynamic routing configuration

### Services

#### Core Service (Node.js/TypeScript)
- **Framework**: Fastify (high-performance HTTP server)
- **Validation**: Zod schemas
- **Documentation**: OpenAPI/Swagger
- **Database**: PostgreSQL via pg driver
- **Telemetry**: OpenTelemetry

**Responsibilities:**
- User management
- Shared business logic
- Utilities for Next.js monorepo

#### Media Service (Go)
- **Router**: Chi
- **Storage**: AWS SDK v2 (Cloudflare R2)
- **Streaming**: Native Go HTTP
- **Telemetry**: OpenTelemetry

**Responsibilities:**
- File uploads (multipart)
- Pre-signed URL generation
- Asset management
- S3-compatible operations

#### Links Service (Hasura GraphQL)
- **Engine**: Hasura v2.38
- **Security**: JWT validation, role-based access
- **Features**: Allow-list, query caching

**Responsibilities:**
- Link shortening
- Click tracking
- Analytics
- Real-time subscriptions

### Data Layer
- **Database**: PostgreSQL 16
- **Schemas**: Isolated per service (core, media, links)
- **Storage**: Cloudflare R2 (S3-compatible)

### Observability
- **Traces**: OpenTelemetry → OTLP collector
- **Logs**: Structured JSON to stdout
- **Metrics**: Prometheus format
- **Monitoring**: BetterStack

## Design Principles

### 1. Gateway-First Architecture

All external traffic flows through Traefik, which provides:
- Single entry point for security
- Centralized rate limiting
- Automatic SSL/TLS
- Service discovery via Docker labels

### 2. Polyglot Services

Each service uses the best tool for its domain:
- **Node.js**: Fast iteration, shared types with frontend
- **Go**: High-performance I/O, concurrency
- **Hasura**: Instant GraphQL API with permissions

### 3. OpenAPI-First

All REST services expose OpenAPI specs:
- `/v1/core/openapi.json`
- `/v1/media/openapi.json`

Benefits:
- Client code generation
- API documentation
- Contract testing

### 4. Stable URLs

Service paths are decoupled from implementation:
- `/v1/core/*` can be Node, Deno, or Bun
- `/v1/media/*` can be Go, Rust, or Zig
- Version in path (`/v1/`) for major changes

### 5. Defense in Depth

Multiple security layers:
1. **Cloudflare**: DDoS, WAF, bot protection
2. **Traefik**: Rate limiting, JWT validation
3. **Service**: Input validation, authorization
4. **Database**: Row-level security (Hasura)

## Request Flow

### Authenticated Request

```
Client
  │
  ├─ HTTPS Request + JWT
  │
  ▼
Cloudflare (DDoS protection)
  │
  ▼
Traefik Gateway
  │
  ├─ TLS Termination
  ├─ JWT Validation (middleware)
  ├─ Rate Limit Check (per-route)
  │
  ▼
Service (Core/Media/Hasura)
  │
  ├─ Input Validation
  ├─ Business Logic
  ├─ Database Query
  │
  ▼
PostgreSQL / R2
  │
  ▼
Response (JSON)
```

### File Upload Flow

```
Client
  │
  ├─ 1. POST /v1/media/presign
  │    (Get pre-signed URL)
  │
  ▼
Media Service
  │
  ├─ Generate S3 presigned URL
  │
  ▼
Client
  │
  ├─ 2. PUT <presigned-url>
  │    (Direct upload to R2)
  │
  ▼
Cloudflare R2
  │
  ├─ Store file
  │
  ▼
Client
  │
  ├─ 3. File available at:
  │    https://cdn.mikeodnis.dev/uploads/...
```

## Scalability

### Horizontal Scaling

Services are stateless and can scale independently:

```bash
docker-compose up -d --scale core-service=5 --scale media-service=3
```

Traefik automatically load balances across instances.

### Vertical Scaling

Resource limits in `docker-compose.prod.yml`:

```yaml
deploy:
  resources:
    limits:
      cpus: '2'
      memory: 2G
```

### Database Scaling

- **Read Replicas**: Configure multiple PostgreSQL instances
- **Connection Pooling**: Built into each service
- **Caching**: Hasura query caching

### CDN Scaling

Cloudflare R2 + CDN handles global distribution automatically.

## Resilience

### Health Checks

All services expose `/health` endpoints:
- Liveness: Is the process running?
- Readiness: Can it serve traffic?

Docker automatically restarts unhealthy containers.

### Graceful Shutdown

Services handle SIGTERM:
1. Stop accepting new requests
2. Complete in-flight requests
3. Close database connections
4. Exit

### Circuit Breaking

Traefik can retry failed requests and route around unhealthy instances.

### Backups

- **Database**: Automated daily backups via pg_dump
- **R2**: Object versioning enabled
- **Metadata**: Hasura metadata in Git

## Security

### Authentication

JWT tokens with HS256:
```json
{
  "sub": "user-id",
  "email": "user@example.com",
  "exp": 1234567890
}
```

Validated at gateway (Traefik) and service levels.

### Authorization

- **Core/Media**: Application-level checks
- **Hasura**: Row-level security via permissions

### Secrets Management

All secrets via environment variables:
- Never committed to Git
- Loaded from `.env` (Docker Compose)
- Or from secrets manager (production)

### Rate Limiting

Per-route limits at gateway:
- `/v1/core/*`: 100 req/min
- `/v1/media/*`: 50 req/min
- `/v1/links/*`: 100 req/min

### HTTPS Only

- TLS 1.2+ required
- Automatic HTTP → HTTPS redirect
- HSTS headers

## Monitoring

### Metrics

Prometheus endpoints on all services:
- Request latency (p50, p95, p99)
- Error rates
- Resource usage

### Traces

OpenTelemetry distributed tracing:
- Trace requests across services
- Identify bottlenecks
- Debug failures

### Logs

Structured JSON logs:
```json
{
  "level": "info",
  "msg": "Request completed",
  "method": "POST",
  "path": "/v1/core/users",
  "status": 201,
  "duration_ms": 45,
  "request_id": "abc-123"
}
```

### Alerts

BetterStack monitors:
- Service health endpoints
- Response times
- Error rates
- Certificate expiration

## Cost Optimization

### Cloudflare R2
- $0.015/GB storage
- $0 egress (vs S3)
- Free tier: 10GB storage

### Compute
- Single VPS for low-medium traffic
- Scale horizontally as needed
- Spot instances for non-critical workloads

### Database
- Shared PostgreSQL instance
- Separate schemas per service
- Consider managed DB for production

## Future Enhancements

### Message Queue
Add RabbitMQ/Redis for:
- Async processing
- Event-driven architecture
- Webhook delivery

### Service Mesh
Consider Linkerd/Istio for:
- mTLS between services
- Advanced traffic management
- Observability

### Multi-Region
Deploy to multiple regions:
- Route53 geo-routing
- Regional R2 buckets
- Read replicas per region

### Caching
Add Redis for:
- Session storage
- API response caching
- Rate limit counters
