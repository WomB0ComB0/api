import { JsonTransport, LogLevel, Logger } from '@resq-systems/logger';
import { isNonEmptyString } from '@resq-systems/types';
import { Elysia } from 'elysia';
import { randomUUID } from 'node:crypto';

export interface RequestLoggerOptions {
  enabled: boolean;
  json: boolean;
}

let jsonTransportAttached = false;

export function createCoreLogger(json: boolean): Logger {
  if (json && !jsonTransportAttached) {
    Logger.addTransport(new JsonTransport({ name: 'core-json' }));
    jsonTransportAttached = true;
  }
  return Logger.getLogger('[core-service]', {
    minLevel: LogLevel.INFO,
    includeTimestamp: true,
    colorize: !json,
  });
}

/**
 * Structured request timing & logging plugin backed by @resq-systems/logger.
 * Propagates/generates `X-Request-ID`, extracts client IP, and logs duration per request.
 */
export function createRequestLoggerPlugin(options: RequestLoggerOptions) {
  const log = createCoreLogger(options.json);

  return new Elysia({ name: 'core.request-logger' })
    .derive({ as: 'global' }, ({ request, set }) => {
      const incomingId = request.headers.get('x-request-id');
      const requestId = isNonEmptyString(incomingId) ? incomingId : randomUUID();
      set.headers['x-request-id'] = requestId;
      const forwarded = request.headers.get('x-forwarded-for');
      const clientIp = isNonEmptyString(forwarded)
        ? forwarded.split(',')[0]!.trim()
        : request.headers.get('x-real-ip') || 'local';
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
      const meta = {
        service: 'core',
        request_id: requestId,
        method: request.method,
        path,
        status: statusCode,
        duration_ms: durationMs,
        ip: clientIp,
      };

      if (statusCode >= 500) {
        log.error(`${request.method} ${path} -> ${statusCode}`, undefined, meta);
      } else if (statusCode >= 400) {
        log.warn(`${request.method} ${path} -> ${statusCode}`, meta);
      } else {
        log.info(`${request.method} ${path} -> ${statusCode}`, meta);
      }
    });
}
