import { record } from '@elysiajs/opentelemetry';
import { Elysia, status } from 'elysia';
import type { Database } from '../../db.js';
import { HealthModel } from './model.js';

export function createHealthModule(db: Database) {
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
