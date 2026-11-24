import { record } from '@elysiajs/opentelemetry';
import { Elysia, status } from 'elysia';
import type { Database } from '../../db.js';
import { HealthModel } from './model.js';

export interface HealthModuleOptions {
  /** Single-flight cache TTL for /health/ready DB probes (ms). Defaults to 2000ms. */
  readyCacheTtlMs?: number;
}

export function createHealthModule(db: Database, options: HealthModuleOptions = {}) {
  const readyCacheTtlMs = options.readyCacheTtlMs ?? 2_000;
  let cachedReady: { ready: boolean; payload: HealthModel['readyResponse']; expiresAt: number } | null = null;
  let inflightReady: Promise<{ ready: boolean; payload: HealthModel['readyResponse'] }> | null = null;

  async function probeReadiness(): Promise<{ ready: boolean; payload: HealthModel['readyResponse'] }> {
    const now = Date.now();
    if (readyCacheTtlMs > 0 && cachedReady && now < cachedReady.expiresAt) {
      return { ready: cachedReady.ready, payload: cachedReady.payload };
    }
    if (readyCacheTtlMs > 0 && inflightReady) {
      return inflightReady;
    }

    const run = (async () => {
      let ready = false;
      try {
        ready = await db.healthCheck();
      } catch {
        ready = false;
      }

      const payload: HealthModel['readyResponse'] = {
        status: ready ? 'ready' : 'not_ready',
        timestamp: new Date().toISOString(),
        checks: {
          database: ready ? 'healthy' : 'unhealthy',
        },
      };

      if (readyCacheTtlMs > 0) {
        cachedReady = { ready, payload, expiresAt: Date.now() + readyCacheTtlMs };
      }
      return { ready, payload };
    })().finally(() => {
      inflightReady = null;
    });

    if (readyCacheTtlMs > 0) {
      inflightReady = run;
    }
    return run;
  }

  return new Elysia({ name: 'core.health' })
    .get(
      '/health',
      () =>
        record('health.liveness.get', () => ({
          status: 'healthy' as const,
          timestamp: new Date().toISOString(),
          service: 'core' as const,
        })),
      {
        response: {
          200: HealthModel.healthResponse,
        },
        detail: {
          tags: ['health'],
          summary: 'Liveness check',
        },
      }
    )
    .head('/health', () => record('health.liveness.head', () => status(200, undefined)), {
      detail: {
        tags: ['health'],
        summary: 'Liveness HEAD probe',
      },
    })
    .get(
      '/health/ready',
      () =>
        record('health.readiness.get', async () => {
          const { ready, payload } = await probeReadiness();
          if (!ready) {
            return status(503, payload);
          }
          return payload;
        }),
      {
        response: {
          200: HealthModel.readyResponse,
          503: HealthModel.readyResponse,
        },
        detail: {
          tags: ['health'],
          summary: 'Readiness check with database connectivity',
        },
      }
    );
}
