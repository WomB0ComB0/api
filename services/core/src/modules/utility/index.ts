import { record } from '@elysiajs/opentelemetry';
import { Elysia, status } from 'elysia';
import pkg from '../../../package.json' with { type: 'json' };
import type { AppConfig } from '../../config.js';
import { UtilityModel } from './model.js';

const APP_VERSION = pkg.version || '1.0.0';

function toMB(bytes: number): number {
  return Number((bytes / 1024 / 1024).toFixed(2));
}

export function createUtilityModule(config: AppConfig) {
  return new Elysia({ name: 'core.utility' })
    .get(
      '',
      () =>
        record('utility.root.get', () => ({
          message: 'api.mikeodnis.dev core service',
          service: 'core' as const,
          version: APP_VERSION,
          docs: '/v1/core/docs',
        })),
      {
        response: { 200: UtilityModel.rootResponse },
        detail: {
          tags: ['utility'],
          summary: 'Core API root',
        },
      }
    )
    .head('', () => record('utility.root.head', () => status(200, undefined)), {
      detail: { tags: ['utility'], summary: 'Core API root HEAD probe' },
    })
    .get(
      '/status',
      () =>
        record('utility.status.get', () => {
          const mem = process.memoryUsage();
          return {
            status: 'ok' as const,
            service: 'core' as const,
            version: APP_VERSION,
            environment: config.nodeEnv,
            uptime_seconds: Number(process.uptime().toFixed(2)),
            memory_mb: {
              rss: toMB(mem.rss),
              heap_total: toMB(mem.heapTotal),
              heap_used: toMB(mem.heapUsed),
              external: toMB(mem.external),
            },
          };
        }),
      {
        response: { 200: UtilityModel.statusResponse },
        detail: {
          tags: ['utility'],
          summary: 'Runtime status (uptime, memory, version, environment)',
        },
      }
    )
    .head('/status', () => record('utility.status.head', () => status(200, undefined)), {
      detail: { tags: ['utility'], summary: 'Status HEAD probe' },
    })
    .get(
      '/version',
      () =>
        record('utility.version.get', () => ({
          service: 'core' as const,
          version: APP_VERSION,
        })),
      {
        response: { 200: UtilityModel.versionResponse },
        detail: {
          tags: ['utility'],
          summary: 'Get API version',
        },
      }
    )
    .head('/version', () => record('utility.version.head', () => status(200, undefined)), {
      detail: { tags: ['utility'], summary: 'Version HEAD probe' },
    })
    .get(
      '/info',
      () =>
        record('utility.info.get', () => ({
          name: 'api.mikeodnis.dev Core API',
          version: APP_VERSION,
          contact: 'admin@mikeodnis.dev',
          documentation_url: 'https://api.mikeodnis.dev/v1/core/docs',
          cdn_url: 'https://cdn.mikeodnis.dev',
        })),
      {
        response: { 200: UtilityModel.infoResponse },
        detail: {
          tags: ['utility'],
          summary: 'Get API service metadata and links',
        },
      }
    )
    .head('/info', () => record('utility.info.head', () => status(200, undefined)), {
      detail: { tags: ['utility'], summary: 'Info HEAD probe' },
    });
}
