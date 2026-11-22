import { Elysia } from 'elysia';

export interface RateLimitOptions {
  max: number;
  windowMs: number;
  enabled?: boolean;
}

interface Bucket {
  count: number;
  resetAt: number;
}

export function createRateLimitPlugin(options: RateLimitOptions) {
  const buckets = new Map<string, Bucket>();

  return new Elysia({ name: 'rate-limit' }).onBeforeHandle({ as: 'global' }, ({ request, set }) => {
    if (options.enabled === false) return;

    const now = Date.now();
    const forwarded = request.headers.get('x-forwarded-for');
    const ip = forwarded ? forwarded.split(',')[0]!.trim() : request.headers.get('x-real-ip') || 'local';

    let bucket = buckets.get(ip);
    if (!bucket || now >= bucket.resetAt) {
      bucket = { count: 0, resetAt: now + options.windowMs };
      buckets.set(ip, bucket);
    }

    bucket.count += 1;

    // Periodic cleanup to prevent unbounded growth
    if (buckets.size > 10_000) {
      for (const [k, v] of buckets) {
        if (now >= v.resetAt) buckets.delete(k);
      }
    }

    set.headers['x-ratelimit-limit'] = String(options.max);
    set.headers['x-ratelimit-remaining'] = String(Math.max(0, options.max - bucket.count));

    if (bucket.count > options.max) {
      set.status = 429;
      return { error: 'Too many requests' };
    }
    return undefined;
  });
}
