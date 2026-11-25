# End-to-End Hosting & Deployment Guide

This guide covers deploying the full `api.mikeodnis.dev` + `cdn.mikeodnis.dev` stack end-to-end.

---

## Shared Secrets (Generate Once)

Generate cryptographically strong secrets and store them in your secret manager (never commit them):

```bash
JWT_SECRET=$(openssl rand -hex 32)
SIGNING_SECRET=$(openssl rand -hex 32)
GATEWAY_PROXY_SECRET=$(openssl rand -hex 32)
HASURA_GRAPHQL_ADMIN_SECRET=$(openssl rand -hex 32)
```

> **Critical:**
> - `SIGNING_SECRET` must be identical between `media-service` (`api.mikeodnis.dev`) and `cdn-worker` (`cdn.mikeodnis.dev`).
> - `GATEWAY_PROXY_SECRET` must be identical between Railway `gateway` (Caddy) and `cdn-worker` so per-visitor CDN rate limiting uses authenticated `X-Gateway-Client-IP`.
> - `JWT_SECRET` must match across `core-service`, `media-service`, and `HASURA_GRAPHQL_JWT_SECRET`.

---

## Part 1: Deploy `cdn.mikeodnis.dev` (Cloudflare R2 + Worker)

From `~/github/prs/cdn/cloudflare-worker`:

```bash
# 1. Authenticate Wrangler (one-time interactive login)
bunx wrangler login

# 2. Create the R2 bucket (if not already created)
bunx wrangler r2 bucket create cdn

# 3. Upload shared secrets via stdin (never pass secrets as CLI args)
printf '%s' "$SIGNING_SECRET" | bunx wrangler secret put SIGNING_SECRET
printf '%s' "$GATEWAY_PROXY_SECRET" | bunx wrangler secret put GATEWAY_PROXY_SECRET

# 4. Run typecheck + unit tests & deploy the ES Module Worker
bunx tsc --noEmit && bun test
bunx wrangler deploy

# 5. Verify live edge health
curl -fsS https://cdn.mikeodnis.dev/health
```

---

## Part 2A: Host `api.mikeodnis.dev` on Railway (Production — Recommended)

Railway runs each container on private networking (`*.railway.internal`), with `services/gateway` (Caddy 2.11) as the single public ingress mapped to `api.mikeodnis.dev` (and optionally `cdn.mikeodnis.dev` proxying to the Cloudflare Worker with `GATEWAY_PROXY_SECRET`).

### 1. Create Project & Provision PostgreSQL

```bash
cd ~/github/prs/api
railway init -n api-mikeodnis-dev
railway add --database postgres
```

Initialize the `core`, `media`, and `links` schemas against the Railway Postgres URL:

```bash
DATABASE_URL="$(railway variables --service Postgres --json | jq -r '.DATABASE_URL')" ./scripts/db-init.sh
```

### 2. Deploy the 5 Services

Create each service pointing to this repository with its corresponding `railway.toml` path:

| Railway Service | Config File (`RAILWAY_CONFIG_FILE`) | Internal Port | Required Environment Variables |
| :--- | :--- | :---: | :--- |
| `core-service` | `services/core/railway.toml` | `3000` | `NODE_ENV=production`, `PORT=3000`, `DATABASE_URL=${{Postgres.DATABASE_URL}}`, `JWT_SECRET`, `UPSTASH_REDIS_REST_URL`, `UPSTASH_REDIS_REST_TOKEN` |
| `media-service` | `services/media/railway.toml` | `8080` | `GO_ENV=production`, `PORT=8080`, `DATABASE_URL=${{Postgres.DATABASE_URL}}`, `JWT_SECRET`, `SIGNING_SECRET`, `CLOUDFLARE_R2_ACCOUNT_ID`, `CLOUDFLARE_R2_ACCESS_KEY_ID`, `CLOUDFLARE_R2_SECRET_ACCESS_KEY`, `CLOUDFLARE_R2_BUCKET=cdn`, `CLOUDFLARE_R2_PUBLIC_URL=https://cdn.mikeodnis.dev`, `CLOUDFLARE_ZONE_ID`, `CLOUDFLARE_API_TOKEN` |
| `hasura` | `hasura/railway.toml` | `8080` | `HASURA_GRAPHQL_DATABASE_URL=${{Postgres.DATABASE_URL}}`, `HASURA_GRAPHQL_ADMIN_SECRET`, `HASURA_GRAPHQL_JWT_SECRET={"type":"HS256","key":"<JWT_SECRET>"}`, `HASURA_GRAPHQL_ENABLE_CONSOLE=false` (gateway also enforces fail-closed route allowlist & strips admin headers) |
| `health-aggregator` | `services/health/railway.toml` | `8000` | `PORT=8000`, `SERVICES=core=http://core-service.railway.internal:3000/v1/core/health,media=http://media-service.railway.internal:8080/v1/media/health,hasura=http://hasura.railway.internal:8080/healthz` |
| `gateway` (Public) | `services/gateway/railway.toml` | `8080` | `CORE_UPSTREAM=core-service.railway.internal:3000`, `MEDIA_UPSTREAM=media-service.railway.internal:8080`, `HASURA_UPSTREAM=hasura.railway.internal:8080`, `HEALTH_UPSTREAM=health-aggregator.railway.internal:8000`, `CDN_UPSTREAM=<worker>.workers.dev`, `GATEWAY_PROXY_SECRET` |

### 3. Attach Custom Domains

In Railway → `gateway` service → **Settings → Networking → Custom Domain**:
- Add `api.mikeodnis.dev` (and `cdn.mikeodnis.dev` if fronting the Worker through Caddy) and configure the CNAME records in your DNS provider.
- Verify end-to-end:
  ```bash
  ./scripts/test-api.sh https://api.mikeodnis.dev
  ```

---

## Part 2B: Host `api.mikeodnis.dev` on a Single VPS (Docker Compose + Traefik)

Ideal for Hetzner / DigitalOcean / Oracle Cloud ($5–$20/mo):

```bash
# 1. On the VPS (Ubuntu / Arch / Debian with Docker + Compose v2)
git clone https://github.com/WomB0ComB0/api.git /opt/api
cd /opt/api

# 2. Generate .env and fill in CLOUDFLARE_R2_* + CLOUDFLARE_ZONE_ID/API_TOKEN
./setup.sh
nano .env

# 3. Point DNS A record: api.mikeodnis.dev -> <VPS_PUBLIC_IP> (ports 80 & 443 open)

# 4. Boot production stack (Traefik + Let's Encrypt TLS + Core + Media + Hasura + Postgres + Health)
make prod

# 5. Verify all endpoints
./scripts/test-api.sh https://api.mikeodnis.dev
```

---

## Post-Deploy Smoke Test (End-to-End API ↔ CDN)

```bash
# 1. Register & capture Bearer JWT
TOKEN=$(curl -fsS -X POST https://api.mikeodnis.dev/v1/core/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"smoke@mikeodnis.dev","name":"Smoke Test","password":"StrongPassword123!"}' | jq -r '.access_token')

# 2. Upload a private asset & mint a 120s HMAC-signed CDN URL
ASSET_ID=$(curl -fsS -X POST https://api.mikeodnis.dev/v1/media/upload \
  -H "Authorization: Bearer $TOKEN" \
  -F 'visibility=private' \
  -F 'file=@README.md' | jq -r '.id')

SIGNED_URL=$(curl -fsS -X POST https://api.mikeodnis.dev/v1/media/sign \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"asset_id\":\"$ASSET_ID\",\"expires_in\":120}" | jq -r '.url')

# 3. Download through cdn.mikeodnis.dev Worker (validates HMAC signature + no-store + no-referrer)
curl -fsSI "$SIGNED_URL"

# 4. Clean up test asset
curl -fsS -X DELETE "https://api.mikeodnis.dev/v1/media/assets/$ASSET_ID" \
  -H "Authorization: Bearer $TOKEN"
```
