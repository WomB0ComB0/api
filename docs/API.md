# API Documentation

Interactive OpenAPI documentation for **Core API**, **Media API**, and **CDN Edge** is served at [`https://api.mikeodnis.dev/docs`](https://api.mikeodnis.dev/docs). Machine-readable specs and LLM references are available at `/v1/core/openapi.json`, `/v1/media/openapi.json`, `/llms.txt`, and `/llms-full.txt`.

## Authentication

### 1. HS256 JWT Bearer Tokens (Primary)

Issued by `POST /v1/core/auth/register` or `POST /v1/core/auth/login`:

```http
Authorization: Bearer <access_token>
```

- Access tokens expire in **15 minutes** (`exp` claim is mandatory across all services).
- Refresh tokens rotate atomically (single-use CTE) via `POST /v1/core/auth/refresh` (30-day TTL, `maxLength: 256`) and can be revoked via `POST /v1/core/auth/logout`.
- Tokens include Hasura claims (`https://hasura.io/jwt/claims`) so the same JWT works across Core, Media, and Links GraphQL.

```json
{
  "sub": "user-uuid",
  "email": "user@example.com",
  "role": "user",
  "https://hasura.io/jwt/claims": {
    "x-hasura-user-id": "user-uuid",
    "x-hasura-default-role": "user",
    "x-hasura-allowed-roles": ["user"]
  },
  "iat": 1764000000,
  "exp": 1764000900
}
```

### 2. Scoped API Keys (`mk_live_*`)

Created via `POST /v1/core/auth/api-keys` (Bearer JWT required) with up to 4 unique scopes (`read`, `write`, `media`, `links`):

```http
X-API-Key: mk_live_...
```

- Stored as SHA-256 hashes at rest; the raw key is returned once upon creation.
- API keys require the `read` scope for `GET /v1/core/auth/me` and `GET /v1/core/users/{id}`.
- Account mutations, account deletion, API-key management, and admin routes require a **Bearer JWT** (API keys receive `403`).

---

## Core Service API

**Base URL:** `https://api.mikeodnis.dev/v1/core`

### Utility & Health
- `GET /health` — Liveness probe
- `GET /health/ready` — PostgreSQL readiness probe (2-second coalesced cache)
- `GET /status` — Uptime, memory RSS, and distributed Upstash Redis rate-limiter status (`Cache-Control: no-store`)
- `GET /version`, `GET /info` — Build metadata & companion URLs
- `GET /openapi.json`, `GET /docs` — OpenAPI 3.1 schema & Scalar UI

### Auth & API Keys
- `POST /auth/register` — Create account (`email`, non-whitespace `name`, `password` ≥ 10 chars)
- `POST /auth/login` — Authenticate and receive `{ user, access_token, refresh_token, token_type, expires_in }`
- `POST /auth/refresh` — Single-use atomic refresh token rotation
- `POST /auth/logout` — Revoke refresh token
- `GET /auth/me` — Authenticated user profile (JWT or `read`-scoped API key)
- `POST /auth/api-keys`, `GET /auth/api-keys`, `DELETE /auth/api-keys/{id}` — Manage scoped API keys (Bearer JWT required)

### Users
- `GET /users` — Paginated user list (admin JWT required)
- `GET /users/{id}` — Read user profile (self/admin JWT, or self `read`-scoped API key)
- `PATCH /users/{id}` — Update user profile (Bearer JWT required)
- `DELETE /users/{id}` — Delete account (Bearer JWT required)

---

## Media Service API

**Base URL:** `https://api.mikeodnis.dev/v1/media`

All mutating and read endpoints under `/v1/media/*` (except `/health`, `/health/ready`, and `/openapi.json`) require a valid HS256 **Bearer JWT**.

### Path A: Direct Multipart Upload

```http
POST /v1/media/upload
Authorization: Bearer <token>
Content-Type: multipart/form-data

file: <binary>
visibility: public | private
```

- Enforces 100 MiB max size (2 MiB for SVG), magic-byte MIME verification (513 B sniff sentinel with strict UTF-8 & text/SVG validation), and SVG active-content sanitization/rejection.
- Public assets are stored at `assets/<user_id>/<hash><ext>`; private assets are stored at `private/<user_id>/<hash><ext>`.

**Response (`201 Created`):**
```json
{
  "id": "uuid",
  "filename": "image.jpg",
  "key": "assets/<user_id>/<hash>.jpg",
  "url": "https://cdn.mikeodnis.dev/assets/<user_id>/<hash>.jpg",
  "visibility": "public",
  "size": 1024000,
  "mime_type": "image/jpeg",
  "etag": "\"...\"",
  "sha256": "...",
  "created_at": "2026-01-01T00:00:00Z"
}
```

### Path B: Quarantined Presigned Direct-to-R2 Upload

1. **Request presigned PUT URL** (targets non-servable `staging/*` prefix):
   ```http
   POST /v1/media/presign
   Authorization: Bearer <token>
   Content-Type: application/json

   {
     "filename": "photo.jpg",
     "content_type": "image/jpeg",
     "visibility": "public"
   }
   ```
   Returns `{ upload_url, key, expires_at, required_headers }` where `key` is `staging/<user_id>/...`.

2. **Upload directly to R2:**
   ```bash
   curl -X PUT "$UPLOAD_URL" \
     --upload-file photo.jpg \
     -H "Content-Type: image/jpeg"
   ```

3. **Confirm & promote to live prefix:**
   ```http
   POST /v1/media/assets/confirm
   Authorization: Bearer <token>
   Content-Type: application/json

   {
     "key": "staging/<user_id>/...",
     "filename": "photo.jpg"
   }
   ```
   Media service verifies the staged object's size, magic bytes, and SVG safety, performs an **ETag-pinned `CopyObject`** (`CopySourceIfMatch`) to `assets/<user_id>/...` or `private/<user_id>/...`, deletes the staging object (re-confirming returns `409 Conflict`), and records metadata in PostgreSQL.

### Asset Management, Signing & Cache Purge

- `GET /assets?limit=50&offset=0` — Paginated list of caller's assets (`offset <= 10000`)
- `GET /assets/{id}` — Retrieve single asset metadata
- `DELETE /assets/{id}` — Delete asset from R2 and PostgreSQL (`204 No Content`, `20/min` write tier)
- `POST /sign` — Mint short-lived HMAC-SHA256 signed URL for **`private/*` assets only** (`{ "asset_id": "<uuid>", "expires_in": 120 }` or `{ "key": "private/...", "expires_in": 120 }`)
- `POST /purge` — Purge Cloudflare edge cache for owned asset URLs (`{ "files": ["https://cdn.mikeodnis.dev/assets/..."] }`)

---

## Links Service (GraphQL)

**Base URL:** `https://api.mikeodnis.dev/v1/links/graphql`

The Caddy gateway enforces a **fail-closed allowlist** for Hasura: only `/v1/links/graphql`, `/v1/links/v1/graphql`, and `/v1/links/healthz` are forwarded, `X-Hasura-Admin-Secret` / `X-Hasura-Access-Key` headers are stripped at the edge, GraphQL bodies are capped at **256 KB**, and `/v1/metadata`, `/v1/query`, `/v2/query`, `/console`, and `/v1/version` are blocked (`403`/`404`).

### Example Query (`GetLinkBySlug`)

```graphql
query GetLinkBySlug($slug: String!) {
  links_links(where: { slug: { _eq: $slug }, is_active: { _eq: true } }) {
    id
    slug
    target_url
    title
    description
    clicks
  }
}
```

### Example Mutation (`CreateLink`)

```graphql
mutation CreateLink($slug: String!, $target_url: String!, $title: String, $description: String) {
  insert_links_links_one(object: {
    slug: $slug,
    target_url: $target_url,
    title: $title,
    description: $description
  }) {
    id
    slug
    target_url
    created_at
  }
}
```

### Example Analytics Query (`GetLinkAnalytics`)

```graphql
query GetLinkAnalytics($link_id: uuid!) {
  links_clicks(where: { link_id: { _eq: $link_id } }, order_by: { clicked_at: desc }, limit: 100) {
    id
    clicked_at
    country
    referer
    user_agent
  }
  links_clicks_aggregate(where: { link_id: { _eq: $link_id } }) {
    aggregate {
      count
    }
  }
}
```

---

## Rate Limits (IETF Draft Headers)

All rate-limited responses emit `RateLimit-Limit`, `RateLimit-Remaining`, `RateLimit-Reset`, `RateLimit-Policy`, and `Retry-After` on `429 Too Many Requests`.

| Service | Tier | Limit | Keying |
| :--- | :--- | :--- | :--- |
| Core | `auth` (login / register / refresh / logout) | **10 req / 60s** | Trusted Client IP |
| Core | `auth-keys` (API key CRUD) | **20 req / 60s** | Verified JWT User / Trusted Client IP |
| Core | `write` (`POST`/`PUT`/`PATCH`/`DELETE`) | **30 req / 60s** | Verified JWT User / Dual IP + API Key / IP |
| Core | `read` (`GET`/`HEAD`) | **100 req / 60s** | Verified JWT User / Dual IP + API Key / IP |
| Media | `media-ip` (outer ceiling) | **120 req / 60s** | Trusted Client IP |
| Media | `media-write` (upload / presign / confirm / purge / delete) | **20 req / 60s** | Authenticated User ID |
| Media | `media-read` (list / get / sign) | **100 req / 60s** | Authenticated User ID |
| CDN Edge | `cdn-public` (`/assets/*`, `/health`, discovery) | **300 req / 60s** | Per-visitor IP (`X-Gateway-Client-IP` / `CF-Connecting-IP`) |
| CDN Edge | `cdn-private` (`/private/*`) | **60 req / 60s** | Per-visitor IP |
| CDN Edge | `cdn-transform` (`/img/assets/*`) | **30 req / 60s** | Per-visitor IP |

---

## Error Responses & Security Headers

All API responses (including `4xx`/`5xx`) carry strict security headers (`X-Content-Type-Options: nosniff`, `Cache-Control: no-store` on authenticated/error responses, HSTS, and `Referrer-Policy`).

### Common HTTP Status Codes

- `200 OK` / `201 Created` / `204 No Content` — Success
- `400 Bad Request` — Validation failure, unsafe object key, or invalid content type
- `401 Unauthorized` — Missing, expired, or invalid JWT / API key / HMAC signature
- `403 Forbidden` — Insufficient permissions, missing API key scope, or blocked gateway path
- `404 Not Found` — Resource not found
- `409 Conflict` — Duplicate email or already-promoted staging key
- `413 Payload Too Large` — Body exceeds limit (100 MiB upload, 2 MiB SVG, 256 KB GraphQL)
- `415 Unsupported Media Type` — Disallowed MIME type or magic-byte mismatch
- `429 Too Many Requests` — Rate limit exceeded (see `Retry-After`)
- `502 Bad Gateway` / `503 Service Unavailable` / `504 Gateway Timeout` — Upstream or readiness failure

---

## CORS

Allowed origins include `https://mikeodnis.dev`, `https://www.mikeodnis.dev`, `https://cdn.mikeodnis.dev`, and `http://localhost:*` in development.

## Full Presigned Upload Example (TypeScript)

```typescript
// 1. Request quarantined staging presigned URL
const presignRes = await fetch('https://api.mikeodnis.dev/v1/media/presign', {
  method: 'POST',
  headers: {
    Authorization: `Bearer ${token}`,
    'Content-Type': 'application/json',
  },
  body: JSON.stringify({
    filename: 'photo.jpg',
    content_type: 'image/jpeg',
    visibility: 'public',
  }),
});
const { upload_url, key } = await presignRes.json();

// 2. Upload bytes directly to R2 staging/*
await fetch(upload_url, {
  method: 'PUT',
  headers: { 'Content-Type': 'image/jpeg' },
  body: fileBlob,
});

// 3. Verify magic bytes & promote atomically to assets/*
const confirmRes = await fetch('https://api.mikeodnis.dev/v1/media/assets/confirm', {
  method: 'POST',
  headers: {
    Authorization: `Bearer ${token}`,
    'Content-Type': 'application/json',
  },
  body: JSON.stringify({ key, filename: 'photo.jpg' }),
});
const asset = await confirmRes.json();
// Public URL: asset.url -> https://cdn.mikeodnis.dev/assets/<user_id>/<hash>.jpg
```

## Support & References

- Interactive API Docs (Core + Media + CDN): `https://api.mikeodnis.dev/docs`
- LLM Reference: `https://api.mikeodnis.dev/llms.txt` / `https://api.mikeodnis.dev/llms-full.txt`
- Aggregate Health: `https://api.mikeodnis.dev/health`
