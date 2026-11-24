import { JsonTransport, LogLevel, Logger } from '@resq-systems/logger';
import { sanitizeForLogging } from '@resq-systems/security/crypto';
import { encodeLogValue } from '@resq-systems/security/validators';
import { isNonEmptyString } from '@resq-systems/types';
import { Elysia } from 'elysia';
import { randomUUID } from 'node:crypto';
import { extractTrustedClientIp } from './rateLimit.js';

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
 * Structured request timing & logging plugin backed by @resq-systems/logger
 * and hardened against CWE-117 log injection / secret leakage via @resq-systems/security.
 */
export function createRequestLoggerPlugin(options: RequestLoggerOptions) {
  const log = createCoreLogger(options.json);

  return new Elysia({ name: 'core.request-logger' })
    .derive({ as: 'global' }, ({ request, set }) => {
      const incomingId = request.headers.get('x-request-id');
      const requestId = isNonEmptyString(incomingId) ? encodeLogValue(incomingId).slice(0, 128) : randomUUID();
      set.headers['x-request-id'] = requestId;
      const clientIp = encodeLogValue(extractTrustedClientIp(request)).slice(0, 64);
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
      const safePath = encodeLogValue(path);
      const meta = sanitizeForLogging({
        service: 'core',
        request_id: requestId,
        method: request.method,
        path: safePath,
        status: statusCode,
        duration_ms: durationMs,
        ip: clientIp,
      });

      if (statusCode >= 500) {
        log.error(`${request.method} ${safePath} -> ${statusCode}`, undefined, meta);
      } else if (statusCode >= 400) {
        log.warn(`${request.method} ${safePath} -> ${statusCode}`, meta);
      } else {
        log.info(`${request.method} ${safePath} -> ${statusCode}`, meta);
      }
    });
}
