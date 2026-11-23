import { LRUCache } from '@resq-systems/dsa';
import { isNonEmptyString } from '@resq-systems/types';
import { Elysia } from 'elysia';

export interface RateLimitOptions {
  max: number;
  windowMs: number;
  maxBuckets?: number;
  enabled?: boolean;
}

interface Bucket {
  count: number;
  resetAt: number;
}

export function createRateLimitPlugin(options: RateLimitOptions) {
  const buckets = new LRUCache<string, Bucket>({
    maxSize: options.maxBuckets ?? 10_000,
    defaultTTL: options.windowMs,
  });

  return new Elysia({ name: 'rate-limit' }).onBeforeHandle({ as: 'global' }, ({ request, set }) => {
    if (options.enabled === false) return;

    const now = Date.now();
    const forwarded = request.headers.get('x-forwarded-for');
    const rawIp = isNonEmptyString(forwarded)
      ? forwarded.split(',')[0]!.trim()
      : request.headers.get('x-real-ip') || 'local';
    const ip = rawIp.slice(0, 64) || 'local';

    let bucket = buckets.get(ip);
    if (!bucket || now >= bucket.resetAt) {
      bucket = { count: 0, resetAt: now + options.windowMs };
      buckets.set(ip, bucket, options.windowMs);
    }

    bucket.count += 1;

    set.headers['x-ratelimit-limit'] = String(options.max);
    set.headers['x-ratelimit-remaining'] = String(Math.max(0, options.max - bucket.count));

    if (bucket.count > options.max) {
      set.status = 429;
      return { error: 'Too many requests' };
    }
    return undefined;
  });
}
