import { describe, expect, test } from 'bun:test';
import { createHmac } from 'node:crypto';
import { Elysia } from 'elysia';
import {
  classifyCorePolicy,
  createRateLimitPlugin,
  type DistributedStore,
  RateLimitMetricsTracker,
  resolveRateLimitIdentity,
} from '../plugins/rateLimit.js';

function makeHs256Jwt(secret: string, sub: string, expOffsetSec = 300): string {
  const header = Buffer.from(JSON.stringify({ alg: 'HS256', typ: 'JWT' })).toString('base64url');
  const payload = Buffer.from(
    JSON.stringify({ sub, exp: Math.floor(Date.now() / 1000) + expOffsetSec })
  ).toString('base64url');
  const sig = createHmac('sha256', secret).update(`${header}.${payload}`).digest('base64url');
  return `${header}.${payload}.${sig}`;
}

describe('Core Distributed Rate Limiter Plugin', () => {
  const secret = 'test-secret-with-at-least-thirty-two-bytes!';

  test('classifies routes into auth, auth-keys, write, read, and exempt health/docs', () => {
    const opts = { max: 100, windowMs: 60_000, authMax: 10, writeMax: 30 };
    expect(classifyCorePolicy('GET', '/v1/core/health', opts)).toBeNull();
    expect(classifyCorePolicy('GET', '/v1/core/health/ready', opts)).toBeNull();
    expect(classifyCorePolicy('GET', '/v1/core/openapi.json', opts)).toBeNull();
    expect(classifyCorePolicy('OPTIONS', '/v1/core/users', opts)).toBeNull();
    expect(classifyCorePolicy('POST', '/v1/core/auth/login', opts)?.name).toBe('auth');
    expect(classifyCorePolicy('POST', '/v1/core/auth/register', opts)?.name).toBe('auth');
    expect(classifyCorePolicy('POST', '/v1/core/auth/api-keys', opts)?.name).toBe('auth-keys');
    expect(classifyCorePolicy('PATCH', '/v1/core/users/123', opts)?.name).toBe('write');
    expect(classifyCorePolicy('GET', '/v1/core/users', opts)?.name).toBe('read');
  });

  test('resolves identity by hashed API key or verified JWT sub (never raw secret) before IP', () => {
    const rawKey = 'mk_live_very_secret_key_value_12345';
    const apiKeyReq = new Request('http://localhost/v1/core/users', {
      headers: { 'x-api-key': rawKey, 'x-real-ip': '203.0.113.10' },
    });
    const apiKeyId = resolveRateLimitIdentity(apiKeyReq, secret);
    expect(apiKeyId).toMatch(/^apikey:[0-9a-f]{16}$/);
    expect(apiKeyId).not.toContain('mk_live_');

    const jwt = makeHs256Jwt(secret, 'user-abc-123');
    const jwtReq = new Request('http://localhost/v1/core/users', {
      headers: { authorization: `Bearer ${jwt}`, 'x-real-ip': '203.0.113.10' },
    });
    expect(resolveRateLimitIdentity(jwtReq, secret)).toBe('user:user-abc-123');

    // Tampered JWT must NOT be trusted for identity keying -> falls back to IP
    const badJwtReq = new Request('http://localhost/v1/core/users', {
      headers: { authorization: `Bearer ${jwt}tampered`, 'x-real-ip': '203.0.113.10' },
    });
    expect(resolveRateLimitIdentity(badJwtReq, secret)).toBe('ip:203.0.113.10');
  });

  test('emits IETF RateLimit-* + RateLimit-Policy + Retry-After headers and enforces strict auth limits', async () => {
    let clock = 60_000;
    const metrics = new RateLimitMetricsTracker('memory');
    const app = new Elysia()
      .use(
        createRateLimitPlugin({
          max: 100,
          authMax: 2,
          windowMs: 60_000,
          jwtSecret: secret,
          metrics,
          now: () => clock,
        })
      )
      .post('/v1/core/auth/login', () => ({ ok: true }))
      .get('/v1/core/health', () => ({ status: 'healthy' }));

    const req = () =>
      new Request('http://localhost/v1/core/auth/login', {
        method: 'POST',
        headers: { 'cf-connecting-ip': '198.51.100.5' },
      });

    const r1 = await app.handle(req());
    expect(r1.status).toBe(200);
    expect(r1.headers.get('ratelimit-limit')).toBe('2');
    expect(r1.headers.get('ratelimit-remaining')).toBe('1');
    expect(r1.headers.get('ratelimit-policy')).toBe('2;w=60');
    expect(r1.headers.get('x-ratelimit-limit')).toBe('2');

    const r2 = await app.handle(req());
    expect(r2.status).toBe(200);
    expect(r2.headers.get('ratelimit-remaining')).toBe('0');

    const r3 = await app.handle(req());
    expect(r3.status).toBe(429);
    expect(r3.headers.get('retry-after')).toBeTruthy();
    const body = (await r3.json()) as { error: string; policy: string };
    expect(body.policy).toBe('auth');

    // Spoofed random X-API-Key header MUST NOT bypass auth brute-force rate limit!
    const spoofedBypass = await app.handle(
      new Request('http://localhost/v1/core/auth/login', {
        method: 'POST',
        headers: {
          'cf-connecting-ip': '198.51.100.5',
          'x-api-key': 'mk_live_random_spoofed_bypass_attempt_999',
        },
      })
    );
    expect(spoofedBypass.status).toBe(429);

    // Health endpoint stays exempt even when client is rate-limited
    const health = await app.handle(
      new Request('http://localhost/v1/core/health', {
        headers: { 'cf-connecting-ip': '198.51.100.5' },
      })
    );
    expect(health.status).toBe(200);

    const snap = metrics.snapshot();
    expect(snap.evaluated).toBe(4);
    expect(snap.allowed).toBe(2);
    expect(snap.limited_429).toBe(2);
    expect(snap.limited_by_policy['auth']).toBe(2);
  });

  test('trips circuit breaker on distributed store failures and enforces strict local fallback', async () => {
    let clock = 120_000;
    let storeCalls = 0;
    const failingStore: DistributedStore = {
      async consume() {
        storeCalls += 1;
        const err = new Error('timeout');
        err.name = 'TimeoutError';
        throw err;
      },
    };
    const metrics = new RateLimitMetricsTracker('redis');
    const app = new Elysia()
      .use(
        createRateLimitPlugin({
          max: 100,
          authMax: 4, // localFallbackLimit = 2
          windowMs: 60_000,
          distributedStore: failingStore,
          circuitFailureThreshold: 2,
          circuitCooldownMs: 30_000,
          metrics,
          now: () => clock,
        })
      )
      .post('/v1/core/auth/login', () => ({ ok: true }));

    const makeReq = () =>
      new Request('http://localhost/v1/core/auth/login', {
        method: 'POST',
        headers: { 'x-real-ip': '203.0.113.99' },
      });

    // 1st & 2nd timeout -> trip circuit breaker, allowed by local strict fallback (cap = 2)
    expect((await app.handle(makeReq())).status).toBe(200);
    clock += 100;
    expect((await app.handle(makeReq())).status).toBe(200);
    expect(storeCalls).toBe(2);

    // 3rd request during cooldown -> skips failing store AND blocked by local strict fallback!
    clock += 100;
    const blocked = await app.handle(makeReq());
    expect(blocked.status).toBe(429);
    expect(storeCalls).toBe(2);

    const snap = metrics.snapshot();
    expect(snap.store_timeouts).toBe(2);
    expect(snap.circuit_open_trips).toBe(1);
    expect(snap.local_fallback_hits).toBe(3);
    expect(snap.limited_429).toBe(1);
  });
});
