import { describe, expect, test } from 'bun:test';
import { treaty } from '@elysiajs/eden';
import { randomUUID } from 'node:crypto';
import { buildApp } from '../app.js';
import { loadConfig } from '../config.js';
import type { Database, QueryResult } from '../db.js';
import type { UserRole } from '../auth/tokens.js';

interface StoredUser {
  id: string;
  email: string;
  name: string;
  password_hash: string;
  role: UserRole;
  is_active: boolean;
  created_at: string;
  updated_at: string;
}

interface StoredRefreshToken {
  id: string;
  user_id: string;
  token_hash: string;
  expires_at: string;
  revoked_at: string | null;
}

interface StoredApiKey {
  id: string;
  user_id: string;
  name: string;
  key_prefix: string;
  key_hash: string;
  scopes: string[];
  last_used_at: string | null;
  expires_at: string | null;
  revoked_at: string | null;
  created_at: string;
}

function createInMemoryDb() {
  const users = new Map<string, StoredUser>();
  const refreshTokens = new Map<string, StoredRefreshToken>();
  const apiKeys = new Map<string, StoredApiKey>();
  let healthy = true;
  let queryCount = 0;

  const db: Database & {
    users: Map<string, StoredUser>;
    setHealthy: (v: boolean) => void;
    getQueryCount: () => number;
    resetQueryCount: () => void;
  } = {
    users,
    setHealthy(v: boolean) {
      healthy = v;
    },
    getQueryCount() {
      return queryCount;
    },
    resetQueryCount() {
      queryCount = 0;
    },
    async healthCheck() {
      return healthy;
    },
    async close() {},
    async query<T extends Record<string, unknown>>(
      text: string,
      params: unknown[] = []
    ): Promise<QueryResult<T>> {
      queryCount += 1;
      const sql = text.replace(/\s+/g, ' ').trim();

      // Atomic CTE: register user + initial refresh token
      if (sql.startsWith('WITH created_user AS (')) {
        const [email, name, passwordHash, tokenHash, expiresAt] = params as [
          string,
          string,
          string,
          string,
          string,
        ];
        for (const u of users.values()) {
          if (u.email === email) {
            const err = new Error('duplicate key') as Error & { code: string };
            err.code = '23505';
            throw err;
          }
        }
        const now = new Date().toISOString();
        const user: StoredUser = {
          id: randomUUID(),
          email,
          name,
          password_hash: passwordHash,
          role: 'user',
          is_active: true,
          created_at: now,
          updated_at: now,
        };
        users.set(user.id, user);

        const rt: StoredRefreshToken = {
          id: randomUUID(),
          user_id: user.id,
          token_hash: tokenHash,
          expires_at: expiresAt,
          revoked_at: null,
        };
        refreshTokens.set(rt.id, rt);
        return { rows: [user as unknown as T], rowCount: 1 };
      }

      // Atomic CTE: revoke old refresh token + insert rotated token
      if (sql.startsWith('WITH revoked AS (')) {
        const [oldHash, newHash, newExpiresAt] = params as [string, string, string];
        const rt = [...refreshTokens.values()].find(
          (r) => r.token_hash === oldHash && !r.revoked_at && new Date(r.expires_at).getTime() > Date.now()
        );
        if (!rt) return { rows: [], rowCount: 0 };
        const u = users.get(rt.user_id);
        if (!u || !u.is_active) return { rows: [], rowCount: 0 };

        rt.revoked_at = new Date().toISOString();
        const rotated: StoredRefreshToken = {
          id: randomUUID(),
          user_id: u.id,
          token_hash: newHash,
          expires_at: newExpiresAt,
          revoked_at: null,
        };
        refreshTokens.set(rotated.id, rotated);

        return {
          rows: [
            {
              id: rt.id,
              user_id: rt.user_id,
              expires_at: rt.expires_at,
              revoked_at: rt.revoked_at,
              email: u.email,
              name: u.name,
              role: u.role,
              is_active: u.is_active,
            } as unknown as T,
          ],
          rowCount: 1,
        };
      }

      // Atomic UPDATE ... FROM core.users: verify API key + touch last_used_at
      if (sql.startsWith('UPDATE core.api_keys k SET last_used_at = NOW() FROM core.users u')) {
        const [keyHash] = params as [string];
        const k = [...apiKeys.values()].find(
          (item) =>
            item.key_hash === keyHash &&
            !item.revoked_at &&
            (!item.expires_at || new Date(item.expires_at).getTime() > Date.now())
        );
        if (!k) return { rows: [], rowCount: 0 };
        const u = users.get(k.user_id);
        if (!u || !u.is_active) return { rows: [], rowCount: 0 };
        k.last_used_at = new Date().toISOString();
        return {
          rows: [
            {
              key_id: k.id,
              user_id: k.user_id,
              email: u.email,
              role: u.role,
              is_active: u.is_active,
              scopes: k.scopes,
              expires_at: k.expires_at,
              revoked_at: k.revoked_at,
            } as unknown as T,
          ],
          rowCount: 1,
        };
      }

      if (sql.includes('FROM core.users WHERE email = $1')) {
        const [email] = params as [string];
        const found = [...users.values()].find((u) => u.email === email);
        return { rows: found ? [found as unknown as T] : [], rowCount: found ? 1 : 0 };
      }

      if (sql.includes('FROM core.users WHERE id = $1')) {
        const [id] = params as [string];
        const found = users.get(id);
        return { rows: found ? [found as unknown as T] : [], rowCount: found ? 1 : 0 };
      }

      if (sql.includes('FROM core.users ORDER BY created_at DESC')) {
        const [limit, offset] = params as [number, number];
        const all = [...users.values()];
        const page = all.slice(offset, offset + limit).map((u) => ({
          ...u,
          total_count: all.length,
        }));
        return { rows: page as unknown as T[], rowCount: page.length };
      }

      if (sql.startsWith('UPDATE core.users SET name')) {
        const [name, id] = params as [string | null, string];
        const found = users.get(id);
        if (!found) return { rows: [], rowCount: 0 };
        if (name) found.name = name;
        found.updated_at = new Date().toISOString();
        return { rows: [found as unknown as T], rowCount: 1 };
      }

      if (sql.startsWith('DELETE FROM core.users WHERE id = $1')) {
        const [id] = params as [string];
        const existed = users.delete(id);
        return { rows: existed ? [{ id } as unknown as T] : [], rowCount: existed ? 1 : 0 };
      }

      if (sql.startsWith('INSERT INTO core.refresh_tokens')) {
        const [userId, tokenHash, expiresAt] = params as [string, string, string];
        const rt: StoredRefreshToken = {
          id: randomUUID(),
          user_id: userId,
          token_hash: tokenHash,
          expires_at: expiresAt,
          revoked_at: null,
        };
        refreshTokens.set(rt.id, rt);
        return { rows: [rt as unknown as T], rowCount: 1 };
      }

      if (sql.startsWith('UPDATE core.refresh_tokens SET revoked_at = NOW() WHERE token_hash = $1')) {
        const [tokenHash] = params as [string];
        const rt = [...refreshTokens.values()].find((r) => r.token_hash === tokenHash && !r.revoked_at);
        if (rt) rt.revoked_at = new Date().toISOString();
        return { rows: [], rowCount: rt ? 1 : 0 };
      }

      if (sql.startsWith('INSERT INTO core.api_keys')) {
        const [userId, name, prefix, hash, scopes, expiresAt] = params as [
          string,
          string,
          string,
          string,
          string[],
          string | null,
        ];
        const key: StoredApiKey = {
          id: randomUUID(),
          user_id: userId,
          name,
          key_prefix: prefix,
          key_hash: hash,
          scopes,
          last_used_at: null,
          expires_at: expiresAt,
          revoked_at: null,
          created_at: new Date().toISOString(),
        };
        apiKeys.set(key.id, key);
        return { rows: [key as unknown as T], rowCount: 1 };
      }

      if (sql.includes('FROM core.api_keys WHERE user_id = $1')) {
        const [userId] = params as [string];
        const list = [...apiKeys.values()].filter((k) => k.user_id === userId && !k.revoked_at);
        return { rows: list as unknown as T[], rowCount: list.length };
      }

      if (sql.startsWith('UPDATE core.api_keys SET revoked_at')) {
        const [id, userId] = params as [string, string];
        const k = apiKeys.get(id);
        if (!k || k.user_id !== userId || k.revoked_at) return { rows: [], rowCount: 0 };
        k.revoked_at = new Date().toISOString();
        return { rows: [{ id: k.id } as unknown as T], rowCount: 1 };
      }

      throw new Error(`Unhandled SQL in test mock: ${sql}`);
    },
  };

  return db;
}

function decodeJwtPayload(token: string): Record<string, unknown> {
  const [, payload] = token.split('.');
  return JSON.parse(Buffer.from(payload!, 'base64url').toString('utf8'));
}

describe('Core API Service (Elysia + Eden Treaty)', () => {
  const testConfig = loadConfig({
    NODE_ENV: 'test',
    JWT_SECRET: 'test-secret-with-at-least-thirty-two-bytes!',
  });

  test('health, readiness, and OpenAPI endpoints', async () => {
    const db = createInMemoryDb();
    const app = buildApp({ config: testConfig, db });
    const api = treaty(app);

    const health = await api.v1.core.health.get();
    expect(health.status).toBe(200);
    expect(health.data?.status).toBe('healthy');
    expect(health.data?.service).toBe('core');

    const ready = await api.v1.core.health.ready.get();
    expect(ready.status).toBe(200);
    expect(ready.data?.status).toBe('ready');

    db.setHealthy(false);
    const notReady = await api.v1.core.health.ready.get();
    expect(notReady.status).toBe(503);
    expect(notReady.error?.value).toMatchObject({ status: 'not_ready' });

    const openapiRes = await app.handle(new Request('http://localhost/v1/core/openapi.json'));
    expect(openapiRes.status).toBe(200);
    const spec = (await openapiRes.json()) as { openapi: string; paths: Record<string, unknown> };
    expect(spec.openapi).toMatch(/^3\./);
    expect(spec.paths['/v1/core/auth/login']).toBeDefined();
    expect(spec.paths['/v1/core/users']).toBeDefined();
    expect(spec.paths['/v1/core/status']).toBeDefined();
    expect(spec.paths['/v1/core/version']).toBeDefined();
    expect(spec.paths['/v1/core/info']).toBeDefined();

    // Regression check: Scalar must point to root-relative /v1/core/openapi.json (leading slash)
    const docsRes = await app.handle(new Request('http://localhost/v1/core/docs'));
    expect(docsRes.status).toBe(200);
    const docsHtml = await docsRes.text();
    expect(docsHtml).toContain('/v1/core/openapi.json');
    expect(docsHtml).not.toContain('&quot;url&quot;:&quot;v1/core/openapi.json&quot;');
  });

  test('utility endpoints, HEAD probes, security headers, Server-Timing, and X-Request-ID', async () => {
    const db = createInMemoryDb();
    const app = buildApp({ config: testConfig, db });
    const api = treaty(app);

    const statusRes = await api.v1.core.status.get();
    expect(statusRes.status).toBe(200);
    expect(statusRes.data?.status).toBe('ok');
    expect(statusRes.data?.memory_mb.rss).toBeGreaterThan(0);

    const versionRes = await api.v1.core.version.get();
    expect(versionRes.status).toBe(200);
    expect(versionRes.data?.version).toBe('1.0.0');

    const infoRes = await api.v1.core.info.get();
    expect(infoRes.status).toBe(200);
    expect(infoRes.data?.cdn_url).toBe('https://cdn.mikeodnis.dev');

    // HEAD probes & security/observability headers
    const rawHealth = await app.handle(new Request('http://localhost/v1/core/health'));
    expect(rawHealth.status).toBe(200);
    expect(rawHealth.headers.get('x-frame-options')).toBe('DENY');
    expect(rawHealth.headers.get('x-content-type-options')).toBe('nosniff');
    expect(rawHealth.headers.get('strict-transport-security')).toContain('max-age=31536000');
    expect(rawHealth.headers.get('content-security-policy')).toContain("default-src 'self'");
    expect(rawHealth.headers.get('x-request-id')).toBeTruthy();
    expect(rawHealth.headers.get('server-timing')).toBeTruthy();

    const headHealth = await app.handle(new Request('http://localhost/v1/core/health', { method: 'HEAD' }));
    expect(headHealth.status).toBe(200);

    const headStatus = await app.handle(new Request('http://localhost/v1/core/status', { method: 'HEAD' }));
    expect(headStatus.status).toBe(200);
  });

  test('auth register, login, Hasura JWT claims, refresh rotation, and logout', async () => {
    const db = createInMemoryDb();
    const app = buildApp({ config: testConfig, db });
    const api = treaty(app);

    // Register executes in 1 atomic CTE query
    db.resetQueryCount();
    const reg = await api.v1.core.auth.register.post({
      email: 'Mike@Example.com',
      name: 'Mike',
      password: 'StrongPassword123!',
    });
    expect(reg.status).toBe(201);
    expect(db.getQueryCount()).toBe(1);

    const regData = reg.data!;
    expect(regData.user.email).toBe('mike@example.com');

    const decoded = decodeJwtPayload(regData.access_token);
    expect(decoded['sub']).toBe(regData.user.id);
    expect(decoded['https://hasura.io/jwt/claims']).toEqual({
      'x-hasura-default-role': 'user',
      'x-hasura-allowed-roles': ['user'],
      'x-hasura-user-id': regData.user.id,
    });

    const dup = await api.v1.core.auth.register.post({
      email: 'mike@example.com',
      name: 'Mike2',
      password: 'StrongPassword123!',
    });
    expect(dup.status).toBe(409);

    const badLogin = await api.v1.core.auth.login.post({
      email: 'mike@example.com',
      password: 'WrongPassword!',
    });
    expect(badLogin.status).toBe(401);

    const login = await api.v1.core.auth.login.post({
      email: 'mike@example.com',
      password: 'StrongPassword123!',
    });
    expect(login.status).toBe(200);
    const refreshToken = login.data!.refresh_token;

    // Refresh rotation executes in 1 atomic CTE query (no TOCTOU race)
    db.resetQueryCount();
    const refreshed = await api.v1.core.auth.refresh.post({ refresh_token: refreshToken });
    expect(refreshed.status).toBe(200);
    expect(db.getQueryCount()).toBe(1);
    const rotatedRefresh = refreshed.data!.refresh_token;
    expect(rotatedRefresh).not.toBe(refreshToken);

    const replay = await api.v1.core.auth.refresh.post({ refresh_token: refreshToken });
    expect(replay.status).toBe(401);

    const logout = await api.v1.core.auth.logout.post({ refresh_token: rotatedRefresh });
    expect(logout.status).toBe(204);

    const afterLogout = await api.v1.core.auth.refresh.post({ refresh_token: rotatedRefresh });
    expect(afterLogout.status).toBe(401);
  });

  test('users RBAC, API key auth, and O(1) N+1-free query counts', async () => {
    const db = createInMemoryDb();
    const app = buildApp({ config: testConfig, db });
    const api = treaty(app);

    const unauth = await api.v1.core.users.get();
    expect(unauth.status).toBe(401);

    const reg1 = await api.v1.core.auth.register.post({
      email: 'u1@example.com',
      name: 'User One',
      password: 'StrongPassword123!',
    });
    const u1 = reg1.data!;

    const reg2 = await api.v1.core.auth.register.post({
      email: 'u2@example.com',
      name: 'User Two',
      password: 'StrongPassword123!',
    });
    const u2 = reg2.data!;

    const listForbidden = await api.v1.core.users.get({
      headers: { authorization: `Bearer ${u1.access_token}` },
    });
    expect(listForbidden.status).toBe(403);

    const crossRead = await api.v1.core.users({ id: u2.user.id }).get({
      headers: { authorization: `Bearer ${u1.access_token}` },
    });
    expect(crossRead.status).toBe(403);

    const patchSelf = await api.v1.core.users({ id: u1.user.id }).patch(
      { name: 'Updated One' },
      { headers: { authorization: `Bearer ${u1.access_token}` } }
    );
    expect(patchSelf.status).toBe(200);
    expect(patchSelf.data?.name).toBe('Updated One');

    // Create 10 API keys to test list query count
    let rawKey = '';
    let keyId = '';
    for (let i = 0; i < 10; i++) {
      const keyRes = await api.v1.core.auth['api-keys'].post(
        { name: `script-${i}`, scopes: ['media'] },
        { headers: { authorization: `Bearer ${u1.access_token}` } }
      );
      expect(keyRes.status).toBe(201);
      if (i === 0) {
        rawKey = keyRes.data!.api_key;
        keyId = keyRes.data!.id;
      }
    }
    expect(rawKey).toStartWith('mk_live_');

    // Listing 10 API keys = exactly 1 SQL query (zero N+1)
    db.resetQueryCount();
    const listKeys = await api.v1.core.auth['api-keys'].get({
      headers: { authorization: `Bearer ${u1.access_token}` },
    });
    expect(listKeys.status).toBe(200);
    expect(listKeys.data?.api_keys).toHaveLength(10);
    expect(db.getQueryCount()).toBe(1);

    // Authenticating with API key + fetching /auth/me = 1 atomic key auth query + 1 profile query = 2 total
    db.resetQueryCount();
    const meViaKey = await api.v1.core.auth.me.get({
      headers: { 'x-api-key': rawKey },
    });
    expect(meViaKey.status).toBe(200);
    expect(meViaKey.data?.id).toBe(u1.user.id);
    expect(db.getQueryCount()).toBe(2);

    // Scoped API keys must NEVER be able to mint, list, or revoke API keys (privilege escalation guard)
    const keyMintViaKey = await api.v1.core.auth['api-keys'].post(
      { name: 'escalated', scopes: ['write', 'media', 'links'] },
      { headers: { 'x-api-key': rawKey } }
    );
    expect(keyMintViaKey.status).toBe(403);

    const keyListViaKey = await api.v1.core.auth['api-keys'].get({
      headers: { 'x-api-key': rawKey },
    });
    expect(keyListViaKey.status).toBe(403);

    const keyRevokeViaKey = await api.v1.core.auth['api-keys']({ id: keyId }).delete(undefined, {
      headers: { 'x-api-key': rawKey },
    });
    expect(keyRevokeViaKey.status).toBe(403);

    const revokeRes = await api.v1.core.auth['api-keys']({ id: keyId }).delete(undefined, {
      headers: { authorization: `Bearer ${u1.access_token}` },
    });
    expect(revokeRes.status).toBe(204);

    const meAfterRevoke = await api.v1.core.auth.me.get({
      headers: { 'x-api-key': rawKey },
    });
    expect(meAfterRevoke.status).toBe(401);

    // Seed 23 more users (total = 25) and verify admin list executes exactly 1 query with window COUNT(*) OVER()
    const now = new Date().toISOString();
    for (let i = 0; i < 23; i++) {
      const id = randomUUID();
      db.users.set(id, {
        id,
        email: `seed-${i}@example.com`,
        name: `Seed ${i}`,
        password_hash: 'hash',
        role: 'user',
        is_active: true,
        created_at: now,
        updated_at: now,
      });
    }

    db.users.get(u1.user.id)!.role = 'admin';
    const adminLogin = await api.v1.core.auth.login.post({
      email: 'u1@example.com',
      password: 'StrongPassword123!',
    });

    // Even an admin's API key cannot access JWT-only admin endpoints
    const adminKeyCreate = await api.v1.core.auth['api-keys'].post(
      { name: 'admin-scoped', scopes: ['read'] },
      { headers: { authorization: `Bearer ${adminLogin.data!.access_token}` } }
    );
    expect(adminKeyCreate.status).toBe(201);
    const adminViaApiKey = await api.v1.core.users.get({
      headers: { 'x-api-key': adminKeyCreate.data!.api_key },
    });
    expect(adminViaApiKey.status).toBe(403);

    db.resetQueryCount();
    const adminList = await api.v1.core.users.get({
      query: { limit: 10, offset: 0 },
      headers: { authorization: `Bearer ${adminLogin.data!.access_token}` },
    });
    expect(adminList.status).toBe(200);
    expect(adminList.data?.users).toHaveLength(10);
    expect(adminList.data?.total).toBe(25);
    expect(db.getQueryCount()).toBe(1);
  });

  test('blocks Trojan Source bidi controls and neutralizes CWE-117 log injection via @resq-systems/security', async () => {
    const db = createInMemoryDb();
    const app = buildApp({ config: testConfig, db });
    const api = treaty(app);

    // Trojan Source RLO character U+202E must be rejected with 400
    const bidiReg = await api.v1.core.auth.register.post({
      email: 'bidi@example.com',
      name: 'Alice\u202EAdmin',
      password: 'StrongPassword123!',
    });
    expect(bidiReg.status).toBe(400);

    // ANSI escape log injection in X-Request-ID must be neutralized by encodeLogValue
    const res = await app.handle(
      new Request('http://localhost/v1/core/health', {
        headers: { 'x-request-id': 'req-123\u001b[31mFAKE' },
      })
    );
    expect(res.status).toBe(200);
    const echoedId = res.headers.get('x-request-id') ?? '';
    expect(echoedId).not.toContain('\u001b');
    expect(echoedId).toContain('req-123');
  });
});
