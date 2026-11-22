# API Infrastructure — `api.mikeodnis.dev`

Production-ready, polyglot API platform engineered to run alongside [`cdn.mikeodnis.dev`](https://github.com/WomB0ComB0/cdn).

- **Core Service (`/v1/core/*`)** — **Bun + Elysia** (feature modules, named auth/admin macros, Argon2id + HS256 JWTs with Hasura claims, rotating refresh tokens, scoped API keys, OpenAPI 3.1 + Scalar UI, Eden Treaty end-to-end type safety, Server-Timing, OpenTelemetry).
- **Media Service (`/v1/media/*`)** — **Go (Hexagonal / Ports & Adapters + DDD)** with `chi`, `pgx/v5`, Cloudflare R2 S3 SDK v2, domain events, HMAC-SHA256 private CDN URL signing, Cloudflare cache purging, and automated Go AST hexagonal boundary tests.
- **Links Service (`/v1/links/*`)** — **Hasura GraphQL Engine** with auto-applied metadata, row-level JWT permissions, and production allow-lists.
- **Health Aggregator (`/health`)** — **Zero-dependency Python 3.12** stdlib concurrent upstream probe with per-service latency reporting.
- **Edge CDN (`cdn.mikeodnis.dev`)** — **Cloudflare Worker (ES Module) + R2** serving public immutable `/assets/*`, HMAC-verified `/private/*`, and `/img/*` transforms.

---

## System Architecture

```mermaid
flowchart TB
    Client["Web / Mobile / CLI Clients"]

    subgraph Edge["Cloudflare Edge"]
        DNS["Cloudflare DNS & WAF"]
        Worker["cdn.mikeodnis.dev Worker (ES Module)"]
        R2[("Cloudflare R2 Bucket")]
    end

    subgraph API["api.mikeodnis.dev"]
        GW["API Gateway (Traefik / Caddy)"]

        subgraph Services["Polyglot Microservices"]
            Core["Core Service (Bun + Elysia) /v1/core/*"]
            Media["Media Service (Go Hexagonal) /v1/media/*"]
            Hasura["Hasura GraphQL /v1/links/*"]
            Health["Health Aggregator (Python) /health"]
        end

        PG[("PostgreSQL 16 (core, media, links)")]
        OTEL["OTel Collector + Jaeger :16686"]
    end

    Client -->|"HTTPS API"| DNS --> GW
    Client -->|"HTTPS CDN (public & signed private)"| Worker --> R2

    GW -->|"/v1/core/*"| Core
    GW -->|"/v1/media/*"| Media
    GW -->|"/v1/links/*"| Hasura
    GW -->|"/health"| Health

    Core -->|"Atomic CTE SQL (pg)"| PG
    Media -->|"Batched & Window SQL (pgx)"| PG
    Media -->|"Put / Presign / Delete"| R2
    Media -.->|"Purge Edge Cache API"| Edge
    Hasura -->|"Compiled Lateral-Join SQL"| PG

    Health --> Core
    Health --> Media
    Health --> Hasura

    Core -.->|"OTLP Traces"| OTEL
    Media -.->|"OTLP Traces"| OTEL
```

---

## Go Media Service — Hexagonal Architecture

`services/media` implements **Ports & Adapters (Hexagonal Architecture) + DDD**, inspired by [go-hexagonal](https://github.com/WomB0ComB0/go-hexagonal). Dependencies strictly point **inward** toward the domain core and are enforced automatically on every test run via Go AST inspection in [architecture_test.go](file:///home/wombocombo/github/prs/api/services/media/architecture_test.go).

```mermaid
flowchart LR
    subgraph Primary["Primary / Driving Adapter (internal/api/http)"]
        Router["Chi Router + JWT Middleware"]
        Handlers["Thin HTTP Handlers + AppError Mapper"]
        Router --> Handlers
    end

    subgraph App["Application Layer (internal/application)"]
        Factory["Application Factory"]
        UC["Use Cases: Upload, Presign, Confirm, Get, List, Delete, SignURL, PurgeCache"]
        Factory --> UC
    end

    subgraph Domain["Domain Core (internal/domain) — Zero External SDKs"]
        Model["Asset Aggregate & Value Objects (Visibility, Filename, OwnedKey)"]
        Bus["Domain Event Bus (AssetUploadedEvent, AssetDeletedEvent)"]
        Ports["Outbound Ports (AssetRepository, ObjectStore, URLSigner, CachePurger)"]
    end

    subgraph Secondary["Secondary / Driven Adapters (internal/adapter)"]
        PGAdapter["postgres.AssetRepository (pgx/v5)"]
        R2Adapter["r2.ObjectStore (AWS SDK v2)"]
        CFAdapter["cloudflare.URLSigner & CachePurger + PurgeSubscriber"]
        OtelAdapter["telemetry.InitTelemetry (OTLP)"]
    end

    Handlers --> Factory
    UC --> Model
    UC --> Bus
    UC --> Ports

    PGAdapter -.->|"implements"| Ports
    R2Adapter -.->|"implements"| Ports
    CFAdapter -.->|"implements"| Ports
    CFAdapter -.->|"subscribes"| Bus
```

---

## Core Service — Elysia Request Lifecycle & Modules

`services/core` follows official [ElysiaJS best practices](https://elysiajs.com/essential/best-practice): feature-based modules (`controller` + `service` + TypeBox `model`), named deduplicated auth/admin macros, `return status(...)` for end-to-end **Eden Treaty** + **OpenAPI 3.1** inference, and single-roundtrip atomic SQL CTEs.

```mermaid
flowchart LR
    Req["Incoming HTTP Request"]
    Log["Request Logger (X-Request-ID, IP, duration)"]
    Sec["Security Headers (CSP, HSTS, Frame-Options)"]
    ST["Server-Timing + CORS + Rate Limit"]
    Macro["Auth / Admin Macro (@elysiajs/bearer + @elysiajs/jwt + Atomic API Key)"]

    subgraph Modules["Feature Modules (/v1/core)"]
        Util["utility (/, /status, /version, /info)"]
        Hlth["health (/health, /health/ready)"]
        Auth["auth (/register, /login, /refresh, /logout, /me, /api-keys)"]
        Users["users (/users, /users/:id)"]
    end

    DB[("PostgreSQL (core schema)")]

    Req --> Log --> Sec --> ST --> Macro
    Macro --> Util
    Macro --> Hlth
    Macro --> Auth
    Macro --> Users
    Hlth --> DB
    Auth --> DB
    Users --> DB
```

---

## End-to-End Auth, Private Media & CDN Purge Flow

```mermaid
sequenceDiagram
    actor Client
    participant Core as Core API (/v1/core)
    participant Media as Media API (/v1/media)
    participant R2 as Cloudflare R2
    participant PG as PostgreSQL
    participant CDN as cdn.mikeodnis.dev Worker

    Client->>Core: POST /v1/core/auth/login
    Core->>PG: Verify Argon2id + store refresh token
    Core-->>Client: HS256 JWT (sub, role, Hasura claims) + refresh_token

    Client->>Media: POST /v1/media/upload (Bearer JWT, visibility=private)
    Media->>R2: PutObject private/<user_id>/<sha256>.pdf
    Media->>PG: Upsert media.assets row
    Media-->>Client: AssetDTO + HMAC-signed CDN URL (?exp=...&sig=...)

    Client->>CDN: GET /private/<user_id>/<sha256>.pdf?exp=...&sig=...
    CDN->>CDN: Constant-time HMAC-SHA256 verification (SIGNING_SECRET)
    CDN->>R2: R2_BUCKET.get(key, { range })
    CDN-->>Client: 200 / 206 Streamed private asset

    Client->>Media: DELETE /v1/media/assets/{id} (Bearer JWT)
    Media->>R2: DeleteObject
    Media->>PG: Delete media.assets row
    Media->>CDN: AssetDeletedEvent -> Cloudflare Purge Cache API (public assets)
    Media-->>Client: 204 No Content
```

---

## Database Schema (N+1-Free & Indexed)

All endpoints run in **O(1)** database roundtrips using atomic CTEs (`register`, `refresh`, `api_key` auth), window functions (`COUNT(*) OVER()`), batched lookups (`WHERE id = ANY($1::uuid[])`), and composite indexes on `(user_id, created_at DESC)` and `(link_id, clicked_at DESC)`.

```mermaid
erDiagram
    CORE_USERS ||--o{ CORE_REFRESH_TOKENS : "has"
    CORE_USERS ||--o{ CORE_API_KEYS : "owns"
    CORE_USERS ||--o{ MEDIA_ASSETS : "uploads"
    CORE_USERS ||--o{ LINKS_LINKS : "creates"
    LINKS_LINKS ||--o{ LINKS_CLICKS : "tracks"

    CORE_USERS {
        uuid id PK
        varchar email UK
        varchar name
        text password_hash
        varchar role
        boolean is_active
        timestamptz created_at
        timestamptz updated_at
    }

    CORE_REFRESH_TOKENS {
        uuid id PK
        uuid user_id FK
        varchar token_hash UK
        timestamptz expires_at
        timestamptz revoked_at
    }

    CORE_API_KEYS {
        uuid id PK
        uuid user_id FK
        varchar name
        varchar key_prefix
        varchar key_hash UK
        text_array scopes
        timestamptz last_used_at
        timestamptz expires_at
        timestamptz revoked_at
    }

    MEDIA_ASSETS {
        uuid id PK
        uuid user_id FK
        varchar filename
        varchar mime_type
        bigint size_bytes
        varchar r2_key UK
        text public_url
        varchar visibility
        varchar etag
        varchar sha256
        jsonb metadata
    }

    LINKS_LINKS {
        uuid id PK
        uuid user_id FK
        varchar slug UK
        text target_url
        integer clicks
        boolean is_active
    }

    LINKS_CLICKS {
        uuid id PK
        uuid link_id FK
        inet ip_address
        text user_agent
        text referer
        varchar country
        timestamptz clicked_at
    }
```

---

## API Endpoints

### Core Service (`/v1/core`)
| Method | Path | Auth | Description |
| :--- | :--- | :--- | :--- |
| `GET`/`HEAD` | `/v1/core`, `/status`, `/version`, `/info` | Public | Root, runtime memory/uptime, version, and metadata |
| `GET`/`HEAD` | `/v1/core/health`, `/v1/core/health/ready` | Public | Liveness & PostgreSQL readiness probes |
| `GET` | `/v1/core/docs`, `/v1/core/openapi.json` | Public | Scalar OpenAPI 3.1 UI & raw JSON specification |
| `POST` | `/v1/core/auth/register` | Public | Create account + issue JWT & refresh token (1 CTE) |
| `POST` | `/v1/core/auth/login` | Public | Verify Argon2id password + issue JWT & refresh token |
| `POST` | `/v1/core/auth/refresh` | Public | Atomically rotate refresh token (1 CTE) |
| `POST` | `/v1/core/auth/logout` | Public | Revoke refresh token |
| `GET` | `/v1/core/auth/me` | JWT / API Key | Current authenticated user profile |
| `POST`/`GET`/`DELETE` | `/v1/core/auth/api-keys`, `/api-keys/:id` | JWT | Create, list, and revoke scoped `mk_live_*` keys |
| `GET` | `/v1/core/users` | Admin JWT | Paginated user list with `total` (`COUNT(*) OVER()`) |
| `GET`/`PATCH`/`DELETE` | `/v1/core/users/:id` | Self or Admin | Read, update, or delete user |

### Media Service (`/v1/media`)
| Method | Path | Auth | Description |
| :--- | :--- | :--- | :--- |
| `GET` | `/v1/media/health`, `/health/ready`, `/openapi.json` | Public | Liveness, R2+DB readiness, OpenAPI 3.0.3 spec |
| `POST` | `/v1/media/upload` | JWT | Multipart upload to R2 (`assets/` or `private/`) + DB record |
| `POST` | `/v1/media/presign` | JWT | Direct-to-R2 15-minute presigned PUT URL |
| `POST` | `/v1/media/assets/confirm` | JWT | Verify presigned object via `HeadObject` and persist metadata |
| `POST` | `/v1/media/sign` | JWT | Generate HMAC-SHA256 signed `cdn.mikeodnis.dev/private/*` URL |
| `POST` | `/v1/media/purge` | JWT | Purge Cloudflare edge cache (owner-scoped or admin) |
| `GET` | `/v1/media/assets` | JWT | Paginated asset list with `total` (`COUNT(*) OVER()`) |
| `GET`/`DELETE` | `/v1/media/assets/:id` | Owner / Admin | Fetch metadata or delete from R2 + DB + event-driven CDN purge |

### Links Service (`/v1/links`) & Aggregate Health (`/health`)
| Method | Path | Auth | Description |
| :--- | :--- | :--- | :--- |
| `POST` | `/v1/links/v1/graphql` | Anonymous / JWT | Hasura GraphQL endpoint (allow-listed in prod) |
| `GET` | `/v1/links/healthz` | Public | Hasura health probe |
| `GET` | `/health` | Public | Concurrent aggregate health across Core, Media, and Hasura |

---

## Quick Start (Local Development)

### Prerequisites
- [Docker](https://docs.docker.com/) + Docker Compose v2
- [Bun](https://bun.sh/) 1.4+, [Go](https://go.dev/) 1.25+, Python 3.12+ (for local testing outside Docker)

```bash
# 1. Generate .env with secure random secrets (JWT_SECRET, SIGNING_SECRET, DB password)
./setup.sh

# 2. Fill in CLOUDFLARE_R2_* credentials in .env, then boot the stack
make dev

# Optional: boot with OpenTelemetry Collector + Jaeger UI (http://localhost:16686)
make dev-obs

# 3. Verify all services
make health
./scripts/test-api.sh http://localhost
```

### Running Tests Locally

```bash
# Run all service tests (Core Elysia + Eden Treaty, Go Hexagonal + AST boundary check, Python Health)
make test

# Individual suites
make test-core     # bun test && bun run typecheck
make test-media    # go vet ./... && go test -race -v ./...
make test-health   # python3 -m unittest discover -v
```

---

## End-to-End Hosting

Two complete, production-ready hosting paths are included — see [docs/DEPLOYMENT.md](file:///home/wombocombo/github/prs/api/docs/DEPLOYMENT.md) for the step-by-step runbook:

1. **Managed PaaS (Railway + Cloudflare Workers/R2)** — Zero server maintenance:
   - Each service ships with a `Dockerfile` and `railway.toml` (`services/gateway`, `services/core`, `services/media`, `services/health`, `hasura`) plus `scripts/db-init.sh` for Railway PostgreSQL.
   - `services/gateway` (Caddy) routes `api.mikeodnis.dev` across Railway private networking (`*.railway.internal`).
   - `cdn.mikeodnis.dev` deploys to Cloudflare Workers via `bunx wrangler deploy` using `wrangler.jsonc`.
2. **Single VPS (Docker Compose + Traefik + Let's Encrypt + Cloudflare Workers/R2)** — Lowest cost (~$5–$20/mo):
   - Uses `docker-compose.yml` + `docker-compose.prod.yml` with `traefik/traefik.prod.yml` for automatic HTTPS, HTTP→HTTPS redirects, and container scaling (`replicas: 2`).

---

## Documentation

- [docs/ARCHITECTURE.md](file:///home/wombocombo/github/prs/api/docs/ARCHITECTURE.md) — System design, hexagonal rules, N+1-free SQL patterns, and CDN contract
- [docs/DEPLOYMENT.md](file:///home/wombocombo/github/prs/api/docs/DEPLOYMENT.md) — Complete Railway + Cloudflare and VPS + Traefik hosting runbooks
- [docs/API.md](file:///home/wombocombo/github/prs/api/docs/API.md) — Detailed API reference
- [CONTRIBUTING.md](file:///home/wombocombo/github/prs/api/CONTRIBUTING.md) — Development workflow

---

## License

Apache License 2.0 — see [LICENSE](file:///home/wombocombo/github/prs/api/LICENSE).
