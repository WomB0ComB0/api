import { createHash, createHmac, timingSafeEqual } from 'node:crypto';
import { LRUCache } from '@resq-systems/dsa';
import { isNonEmptyString } from '@resq-systems/types';
import { Elysia } from 'elysia';

export type CoreRateLimitPolicyName = 'auth' | 'auth-keys' | 'write' | 'read';

export interface CoreRateLimitPolicy {
  readonly name: CoreRateLimitPolicyName;
  readonly limit: number;
  readonly windowMs: number;
  readonly localFallbackLimit: number;
}

export interface RateLimitDecision {
  readonly allowed: boolean;
  readonly limit: number;
  readonly remaining: number;
  readonly resetAtMs: number;
  readonly windowSeconds: number;
  readonly policy: CoreRateLimitPolicyName;
  readonly usedFallback: boolean;
}

export interface RateLimitMetricsSnapshot {
  readonly store_mode: 'redis' | 'memory';
  readonly evaluated: number;
  readonly allowed: number;
  readonly limited_429: number;
  readonly limited_by_policy: Record<string, number>;
  readonly store_errors: number;
  readonly store_timeouts: number;
  readonly circuit_open_trips: number;
  readonly local_fallback_hits: number;
  readonly eval_p95_ms: number;
}

export class RateLimitMetricsTracker {
  private evaluated = 0;
  private allowed = 0;
  private limited429 = 0;
  private storeErrors = 0;
  private storeTimeouts = 0;
  private circuitOpenTrips = 0;
  private localFallbackHits = 0;
  private readonly byPolicy = new Map<string, number>();
  private readonly latencies = new Float64Array(512);
  private latencyCount = 0;
  private latencyIdx = 0;

  constructor(private readonly storeMode: 'redis' | 'memory') {}

  record(input: {
    policy: CoreRateLimitPolicyName;
    allowed: boolean;
    durationMs: number;
    storeError?: boolean;
    storeTimeout?: boolean;
    circuitOpened?: boolean;
    usedFallback?: boolean;
  }): void {
    this.evaluated += 1;
    if (input.allowed) {
      this.allowed += 1;
    } else {
      this.limited429 += 1;
      this.byPolicy.set(input.policy, (this.byPolicy.get(input.policy) ?? 0) + 1);
    }
    if (input.storeError) this.storeErrors += 1;
    if (input.storeTimeout) this.storeTimeouts += 1;
    if (input.circuitOpened) this.circuitOpenTrips += 1;
    if (input.usedFallback) this.localFallbackHits += 1;

    const clamped = Number.isFinite(input.durationMs) && input.durationMs >= 0 ? input.durationMs : 0;
    this.latencies[this.latencyIdx] = clamped;
    this.latencyIdx = (this.latencyIdx + 1) % this.latencies.length;
    if (this.latencyCount < this.latencies.length) this.latencyCount += 1;
  }

  snapshot(): RateLimitMetricsSnapshot {
    let p95 = 0;
    if (this.latencyCount > 0) {
      const sorted = Float64Array.from(this.latencies.subarray(0, this.latencyCount)).sort();
      const idx = Math.min(this.latencyCount - 1, Math.max(0, Math.ceil(0.95 * this.latencyCount) - 1));
      p95 = Number((sorted[idx] ?? 0).toFixed(2));
    }

    const limited_by_policy: Record<string, number> = {};
    for (const [k, v] of this.byPolicy.entries()) {
      limited_by_policy[k] = v;
    }

    return {
      store_mode: this.storeMode,
      evaluated: this.evaluated,
      allowed: this.allowed,
      limited_429: this.limited429,
      limited_by_policy,
      store_errors: this.storeErrors,
      store_timeouts: this.storeTimeouts,
      circuit_open_trips: this.circuitOpenTrips,
      local_fallback_hits: this.localFallbackHits,
      eval_p95_ms: p95,
    };
  }
}

export interface DistributedStore {
  consume(
    key: string,
    limit: number,
    windowMs: number,
    nowMs: number,
    timeoutMs: number
  ): Promise<{ allowed: boolean; remaining: number; resetAtMs: number }>;
}

/**
 * Atomic 1-RTT Sliding-Window Counter Lua script for Redis / Upstash REST.
 * Prevents fixed-window boundary bursts and eliminates GET->INCR races.
 */
const SLIDING_WINDOW_LUA = `
local prev_key = KEYS[1]
local curr_key = KEYS[2]
local limit = tonumber(ARGV[1])
local window_ms = tonumber(ARGV[2])
local now_ms = tonumber(ARGV[3])
local window_start = math.floor(now_ms / window_ms) * window_ms
local elapsed = now_ms - window_start
local weight = math.max(0, 1 - (elapsed / window_ms))
local prev = tonumber(redis.call('GET', prev_key) or '0')
local curr = tonumber(redis.call('GET', curr_key) or '0')
local est = (prev * weight) + curr
if est + 1 > limit then
  return {0, 0, window_start + window_ms}
end
local next_curr = redis.call('INCR', curr_key)
if next_curr == 1 then
  redis.call('PEXPIRE', curr_key, window_ms * 2)
end
local remaining = math.max(0, math.floor(limit - ((prev * weight) + next_curr)))
return {1, remaining, window_start + window_ms}
`;

export function createUpstashRestStore(url: string, token: string): DistributedStore {
  const endpoint = url.replace(/\/+$/, '');
  return {
    async consume(key, limit, windowMs, nowMs, timeoutMs) {
      const windowIdx = Math.floor(nowMs / windowMs);
      const prevKey = `rl:{${key}}:${windowIdx - 1}`;
      const currKey = `rl:{${key}}:${windowIdx}`;

      const response = await fetch(endpoint, {
        method: 'POST',
        headers: {
          Authorization: `Bearer ${token}`,
          'Content-Type': 'application/json',
        },
        body: JSON.stringify([
          'EVAL',
          SLIDING_WINDOW_LUA,
          '2',
          prevKey,
          currKey,
          String(limit),
          String(windowMs),
          String(nowMs),
        ]),
        signal: AbortSignal.timeout(timeoutMs),
      });

      if (!response.ok) {
        throw new Error(`Upstash Redis HTTP ${response.status}`);
      }

      const payload = (await response.json()) as { result?: unknown; error?: string };
      if (payload.error) {
        throw new Error(payload.error);
      }
      if (!Array.isArray(payload.result) || payload.result.length < 3) {
        throw new Error('Malformed sliding-window EVAL response');
      }

      const [allowedFlag, remaining, resetAtMs] = payload.result as [number, number, number];
      return {
        allowed: Number(allowedFlag) === 1,
        remaining: Math.max(0, Number(remaining) || 0),
        resetAtMs: Number(resetAtMs) || (windowIdx + 1) * windowMs,
      };
    },
  };
}

interface LocalWindowBucket {
  windowStart: number;
  prevCount: number;
  currCount: number;
}

class LocalSlidingWindowStore {
  private readonly buckets: LRUCache<string, LocalWindowBucket>;

  constructor(maxBuckets: number, defaultTtlMs: number) {
    this.buckets = new LRUCache<string, LocalWindowBucket>({
      maxSize: maxBuckets,
      defaultTTL: defaultTtlMs * 2,
    });
  }

  consume(
    key: string,
    limit: number,
    windowMs: number,
    nowMs: number
  ): { allowed: boolean; remaining: number; resetAtMs: number } {
    const windowStart = Math.floor(nowMs / windowMs) * windowMs;
    const resetAtMs = windowStart + windowMs;

    const existing = this.buckets.get(key);
    let bucket: LocalWindowBucket;
    if (!existing) {
      bucket = { windowStart, prevCount: 0, currCount: 0 };
    } else if (existing.windowStart === windowStart) {
      bucket = existing;
    } else if (existing.windowStart === windowStart - windowMs) {
      bucket = { windowStart, prevCount: existing.currCount, currCount: 0 };
    } else {
      bucket = { windowStart, prevCount: 0, currCount: 0 };
    }

    const elapsed = Math.max(0, nowMs - windowStart);
    const weight = Math.max(0, 1 - elapsed / windowMs);
    const est = bucket.prevCount * weight + bucket.currCount;

    if (est + 1 > limit) {
      this.buckets.set(key, bucket, windowMs * 2);
      return { allowed: false, remaining: 0, resetAtMs };
    }

    bucket.currCount += 1;
    this.buckets.set(key, bucket, windowMs * 2);
    const remaining = Math.max(0, Math.floor(limit - (bucket.prevCount * weight + bucket.currCount)));
    return { allowed: true, remaining, resetAtMs };
  }
}

export interface RateLimitOptions {
  max: number;
  windowMs: number;
  authMax?: number;
  writeMax?: number;
  maxBuckets?: number;
  enabled?: boolean;
  jwtSecret?: string | undefined;
  upstashRedisUrl?: string | undefined;
  upstashRedisToken?: string | undefined;
  distributedStore?: DistributedStore;
  storeTimeoutMs?: number;
  circuitCooldownMs?: number;
  circuitFailureThreshold?: number;
  metrics?: RateLimitMetricsTracker;
  now?: () => number;
}

const EXEMPT_PATHS = new Set([
  '/v1/core/health',
  '/v1/core/health/ready',
  '/v1/core/openapi.json',
  '/v1/core/docs',
]);

export function classifyCorePolicy(
  method: string,
  pathname: string,
  options: Pick<RateLimitOptions, 'max' | 'windowMs' | 'authMax' | 'writeMax'>
): CoreRateLimitPolicy | null {
  const upper = method.toUpperCase();
  if (upper === 'OPTIONS') return null;
  if (EXEMPT_PATHS.has(pathname) || pathname.startsWith('/v1/core/docs/')) return null;

  const authMax = options.authMax ?? 10;
  const writeMax = options.writeMax ?? 30;

  if (
    pathname === '/v1/core/auth/login' ||
    pathname === '/v1/core/auth/register' ||
    pathname === '/v1/core/auth/refresh' ||
    pathname === '/v1/core/auth/logout'
  ) {
    return {
      name: 'auth',
      limit: authMax,
      windowMs: options.windowMs,
      localFallbackLimit: Math.max(1, Math.ceil(authMax / 2)),
    };
  }

  if (pathname.startsWith('/v1/core/auth/api-keys')) {
    const keyMax = Math.max(10, Math.min(20, writeMax));
    return {
      name: 'auth-keys',
      limit: keyMax,
      windowMs: options.windowMs,
      localFallbackLimit: Math.max(1, Math.ceil(keyMax / 2)),
    };
  }

  if (upper === 'POST' || upper === 'PUT' || upper === 'PATCH' || upper === 'DELETE') {
    return {
      name: 'write',
      limit: writeMax,
      windowMs: options.windowMs,
      localFallbackLimit: Math.max(1, Math.ceil(writeMax / 2)),
    };
  }

  return {
    name: 'read',
    limit: options.max,
    windowMs: options.windowMs,
    localFallbackLimit: options.max,
  };
}

function extractJwtSubVerified(token: string, secret: string, nowSec: number): string | null {
  const parts = token.split('.');
  if (parts.length !== 3) return null;
  const [headerB64, payloadB64, sigB64] = parts as [string, string, string];
  if (!headerB64 || !payloadB64 || !sigB64) return null;

  try {
    const expectedSig = createHmac('sha256', secret)
      .update(`${headerB64}.${payloadB64}`)
      .digest();
    const providedSig = Buffer.from(sigB64, 'base64url');
    if (expectedSig.length !== providedSig.length || !timingSafeEqual(expectedSig, providedSig)) {
      return null;
    }

    const header = JSON.parse(Buffer.from(headerB64, 'base64url').toString('utf8')) as { alg?: unknown };
    if (header.alg !== 'HS256') {
      return null;
    }

    const payload = JSON.parse(Buffer.from(payloadB64, 'base64url').toString('utf8')) as {
      sub?: unknown;
      exp?: unknown;
    };
    if (typeof payload.exp !== 'number' || payload.exp <= nowSec) {
      return null;
    }
    return typeof payload.sub === 'string' && payload.sub.length > 0 ? payload.sub : null;
  } catch {
    return null;
  }
}

export function extractTrustedClientIp(request: Request): string {
  const cfIp = request.headers.get('cf-connecting-ip');
  if (isNonEmptyString(cfIp)) return cfIp.trim().slice(0, 64);

  const realIp = request.headers.get('x-real-ip');
  if (isNonEmptyString(realIp)) return realIp.trim().slice(0, 64);

  const forwarded = request.headers.get('x-forwarded-for');
  if (isNonEmptyString(forwarded)) {
    const first = forwarded.split(',')[0]?.trim();
    if (first) return first.slice(0, 64);
  }

  return 'local';
}

/**
 * Resolve rate-limit identity key without hitting the DB:
 * - `X-API-Key` -> `apikey:<sha256-16>` (never stores the raw key)
 * - Verified HS256 JWT `Authorization: Bearer ...` -> `user:<sub-id>`
 * - Anonymous -> `ip:<trusted-client-ip>`
 */
export function resolveRateLimitIdentity(
  request: Request,
  jwtSecret?: string,
  nowMs = Date.now(),
  policyName?: CoreRateLimitPolicyName
): string {
  // Public auth endpoints (/login, /register, /refresh, /logout) MUST always be keyed by IP so an
  // unauthenticated attacker cannot bypass brute-force rate limits by sending random X-API-Key headers.
  if (policyName === 'auth') {
    return `ip:${extractTrustedClientIp(request)}`;
  }

  const apiKey = request.headers.get('x-api-key');
  if (isNonEmptyString(apiKey)) {
    const trimmed = apiKey.trim();
    if (trimmed.startsWith('mk_live_') && trimmed.length >= 24) {
      const hash = createHash('sha256').update(trimmed).digest('hex').slice(0, 16);
      return `apikey:${hash}`;
    }
  }

  const auth = request.headers.get('authorization');
  if (jwtSecret && isNonEmptyString(auth) && auth.startsWith('Bearer ')) {
    const token = auth.slice('Bearer '.length).trim();
    const sub = extractJwtSubVerified(token, jwtSecret, Math.floor(nowMs / 1000));
    if (sub) {
      return `user:${sub}`;
    }
  }

  return `ip:${extractTrustedClientIp(request)}`;
}

export function createRateLimitPlugin(options: RateLimitOptions) {
  const localStore = new LocalSlidingWindowStore(options.maxBuckets ?? 10_000, options.windowMs);
  const distributedStore: DistributedStore | null =
    options.distributedStore ??
    (options.upstashRedisUrl && options.upstashRedisToken
      ? createUpstashRestStore(options.upstashRedisUrl, options.upstashRedisToken)
      : null);

  const storeTimeoutMs = options.storeTimeoutMs ?? 250;
  const circuitCooldownMs = options.circuitCooldownMs ?? 30_000;
  const circuitFailureThreshold = options.circuitFailureThreshold ?? 2;
  const nowFn = options.now ?? (() => Date.now());
  const metrics = options.metrics ?? new RateLimitMetricsTracker(distributedStore ? 'redis' : 'memory');

  let consecutiveFailures = 0;
  let circuitOpenUntil = 0;

  return new Elysia({ name: 'rate-limit' }).onBeforeHandle({ as: 'global' }, async ({ request, set }) => {
    if (options.enabled === false) return undefined;

    const pathname = new URL(request.url).pathname;
    const policy = classifyCorePolicy(request.method, pathname, options);
    if (!policy) return undefined;

    const nowMs = nowFn();
    const perfStart = performance.now();
    const identity = resolveRateLimitIdentity(request, options.jwtSecret, nowMs, policy.name);
    const key = `${policy.name}:${identity}`;
    const windowSeconds = Math.max(1, Math.round(policy.windowMs / 1000));

    let allowed = true;
    let limit = policy.limit;
    let remaining = policy.limit;
    let resetAtMs = nowMs + policy.windowMs;
    let usedFallback = false;
    let storeError = false;
    let storeTimeout = false;
    let circuitOpened = false;

    if (distributedStore && nowMs >= circuitOpenUntil) {
      try {
        const res = await distributedStore.consume(key, policy.limit, policy.windowMs, nowMs, storeTimeoutMs);
        allowed = res.allowed;
        remaining = res.remaining;
        resetAtMs = res.resetAtMs;
        consecutiveFailures = 0;
      } catch (err: unknown) {
        const errName = err instanceof Error ? err.name : '';
        if (errName === 'TimeoutError' || errName === 'AbortError') {
          storeTimeout = true;
        } else {
          storeError = true;
        }
        consecutiveFailures += 1;
        if (consecutiveFailures >= circuitFailureThreshold) {
          circuitOpenUntil = nowMs + circuitCooldownMs;
          circuitOpened = true;
        }
        // Fail safely to bounded in-process sliding-window fallback
        usedFallback = true;
        limit = policy.localFallbackLimit;
        const local = localStore.consume(key, limit, policy.windowMs, nowMs);
        allowed = local.allowed;
        remaining = local.remaining;
        resetAtMs = local.resetAtMs;
      }
    } else {
      usedFallback = Boolean(distributedStore);
      limit = distributedStore ? policy.localFallbackLimit : policy.limit;
      const local = localStore.consume(key, limit, policy.windowMs, nowMs);
      allowed = local.allowed;
      remaining = local.remaining;
      resetAtMs = local.resetAtMs;
    }

    const deltaSeconds = Math.max(1, Math.ceil((resetAtMs - nowMs) / 1000));

    // Standard IETF draft RateLimit-* headers + legacy X-RateLimit-*
    set.headers['ratelimit-limit'] = String(limit);
    set.headers['ratelimit-remaining'] = String(Math.max(0, remaining));
    set.headers['ratelimit-reset'] = String(deltaSeconds);
    set.headers['ratelimit-policy'] = `${limit};w=${windowSeconds}`;
    set.headers['x-ratelimit-limit'] = String(limit);
    set.headers['x-ratelimit-remaining'] = String(Math.max(0, remaining));
    set.headers['x-ratelimit-reset'] = String(Math.ceil(resetAtMs / 1000));

    metrics.record({
      policy: policy.name,
      allowed,
      durationMs: performance.now() - perfStart,
      storeError,
      storeTimeout,
      circuitOpened,
      usedFallback,
    });

    if (!allowed) {
      set.headers['retry-after'] = String(deltaSeconds);
      set.status = 429;
      return {
        error: 'Too many requests',
        policy: policy.name,
        retry_after_seconds: deltaSeconds,
      };
    }

    return undefined;
  });
}
