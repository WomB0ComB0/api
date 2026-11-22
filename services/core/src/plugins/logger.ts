import { Elysia } from 'elysia';
import { randomUUID } from 'node:crypto';

export interface RequestLoggerOptions {
  enabled: boolean;
  json: boolean;
}

/**
 * Lightweight structured request timing & logging plugin.
 * Propagates/generates `X-Request-ID`, extracts client IP, and logs duration per request.
 */
export function createRequestLoggerPlugin(options: RequestLoggerOptions) {
  return new Elysia({ name: 'core.request-logger' })
    .derive({ as: 'global' }, ({ request, set }) => {
      const requestId = request.headers.get('x-request-id') || randomUUID();
      set.headers['x-request-id'] = requestId;
      const forwarded = request.headers.get('x-forwarded-for');
      const clientIp = forwarded ? forwarded.split(',')[0]!.trim() : request.headers.get('x-real-ip') || 'local';
      return {
        requestId,
        clientIp,
        requestStartMs: performance.now(),
      };
    })
    .onAfterResponse({ as: 'global' }, ({ request, path, set, requestId, clientIp, requestStartMs }) => {
      if (!options.enabled) return;

      const durationMs = Number((performance.now() - requestStartMs).toFixed(2));
      const statusCode = typeof set.status === 'number' ? set.status : 200;

      if (options.json) {
        console.info(
          JSON.stringify({
            ts: new Date().toISOString(),
            level: statusCode >= 500 ? 'error' : statusCode >= 400 ? 'warn' : 'info',
            service: 'core',
            request_id: requestId,
            method: request.method,
            path,
            status: statusCode,
            duration_ms: durationMs,
            ip: clientIp,
          })
        );
      } else {
        console.info(
          `[core] ${request.method} ${path} -> ${statusCode} (${durationMs}ms) ip=${clientIp} req=${requestId.slice(0, 8)}`
        );
      }
    });
}
