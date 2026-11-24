import { cors } from '@elysiajs/cors';
import { openapi } from '@elysiajs/openapi';
import { serverTiming } from '@elysiajs/server-timing';
import { Elysia, status } from 'elysia';
import pkg from '../package.json' with { type: 'json' };
import type { AppConfig } from './config.js';
import type { Database } from './db.js';
import { createAuthModule } from './modules/auth/index.js';
import { createHealthModule } from './modules/health/index.js';
import { createUsersModule } from './modules/users/index.js';
import { createUtilityModule } from './modules/utility/index.js';
import { createRequestLoggerPlugin } from './plugins/logger.js';
import { createRateLimitPlugin, RateLimitMetricsTracker } from './plugins/rateLimit.js';
import { createSecurityHeadersPlugin } from './plugins/security.js';

export interface BuildAppOptions {
  config: AppConfig;
  db: Database;
  enableRateLimitInTest?: boolean;
}

export function buildApp(options: BuildAppOptions) {
  const { config, db } = options;
  const rateLimitMetrics = new RateLimitMetricsTracker(
    config.upstashRedisUrl && config.upstashRedisToken ? 'redis' : 'memory'
  );

  return new Elysia({ name: 'core-service' })
    .onError(({ code, error }) => {
      if (code === 'VALIDATION') {
        return status(400, {
          error: 'Validation error',
          details: config.nodeEnv === 'production' ? undefined : error.message,
        });
      }
      if (code === 'NOT_FOUND') {
        return status(404, { error: 'Not found' });
      }

      if (config.nodeEnv !== 'test') {
        console.error('Unhandled core-service error:', error);
      }
      return status(500, { error: 'Internal server error' });
    })
    .use(
      createRequestLoggerPlugin({
        enabled: config.nodeEnv !== 'test',
        json: config.nodeEnv === 'production',
      })
    )
    .use(createSecurityHeadersPlugin())
    .use(
      serverTiming({
        enabled: config.nodeEnv !== 'production',
        trace: {
          request: true,
          parse: true,
          transform: true,
          beforeHandle: true,
          handle: true,
          afterHandle: true,
          error: true,
          mapResponse: true,
          total: true,
        },
      })
    )
    .use(
      cors({
        origin: config.corsOrigins,
        methods: ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'OPTIONS', 'HEAD'],
        credentials: true,
        maxAge: 86_400,
        allowedHeaders: ['Accept', 'Authorization', 'Content-Type', 'X-API-Key', 'X-Request-ID'],
        exposeHeaders: [
          'X-Request-ID',
          'RateLimit-Limit',
          'RateLimit-Remaining',
          'RateLimit-Reset',
          'RateLimit-Policy',
          'Retry-After',
          'X-RateLimit-Limit',
          'X-RateLimit-Remaining',
          'X-RateLimit-Reset',
          'Server-Timing',
        ],
      })
    )
    .use(
      createRateLimitPlugin({
        max: config.rateLimitMax,
        windowMs: config.rateLimitWindowMs,
        authMax: config.rateLimitAuthMax,
        writeMax: config.rateLimitWriteMax,
        jwtSecret: config.jwtSecret,
        upstashRedisUrl: config.upstashRedisUrl,
        upstashRedisToken: config.upstashRedisToken,
        metrics: rateLimitMetrics,
        enabled: options.enableRateLimitInTest ?? config.nodeEnv !== 'test',
      })
    )
    .use(
      openapi({
        path: '/v1/core/docs',
        specPath: '/v1/core/openapi.json',
        scalar: {
          // @elysiajs/openapi strips the leading slash from specPath when passing it to Scalar,
          // causing /v1/core/docs to resolve v1/core/openapi.json as /v1/core/v1/core/openapi.json.
          url: '/v1/core/openapi.json',
          theme: 'deepSpace',
        },
        exclude: {
          paths: ['/v1/core/docs', '/v1/core/openapi.json'],
        },
        documentation: {
          info: {
            title: 'api.mikeodnis.dev — Core API',
            version: pkg.version || '1.0.0',
            description: [
              'Core authentication, user management, and utility service for **api.mikeodnis.dev**.',
              '',
              '- Powered by **ElysiaJS** + **Bun** with end-to-end Eden Treaty type safety',
              '- Unified HS256 JWTs with Hasura claims + scoped API keys (`mk_live_*`)',
              '- Security headers, rate limiting, Server-Timing, and OpenTelemetry tracing',
            ].join('\n'),
            contact: {
              name: 'Mike Odnis',
              url: 'https://mikeodnis.dev',
              email: 'admin@mikeodnis.dev',
            },
            license: {
              name: 'Apache-2.0',
              url: 'https://www.apache.org/licenses/LICENSE-2.0',
            },
          },
          externalDocs: {
            description: 'CDN & Media Infrastructure (cdn.mikeodnis.dev)',
            url: 'https://cdn.mikeodnis.dev',
          },
          servers: [
            { url: 'https://api.mikeodnis.dev', description: 'Production' },
            { url: 'http://localhost', description: 'Local Development' },
          ],
          tags: [
            { name: 'utility', description: 'Root, runtime status, version, and API metadata' },
            { name: 'health', description: 'Liveness and database readiness probes' },
            { name: 'auth', description: 'Registration, login, rotating refresh tokens, and API keys' },
            { name: 'users', description: 'User profiles and admin management' },
          ],
          components: {
            securitySchemes: {
              bearerAuth: {
                type: 'http',
                scheme: 'bearer',
                bearerFormat: 'JWT',
                description: 'HS256 JWT access token issued by /v1/core/auth/login',
              },
              apiKeyAuth: {
                type: 'apiKey',
                in: 'header',
                name: 'X-API-Key',
                description: 'Scoped API key (mk_live_*) created via /v1/core/auth/api-keys',
              },
            },
          },
        },
      })
    )
    .group('/v1/core', (group) =>
      group
        .use(createUtilityModule(config, rateLimitMetrics))
        .use(createHealthModule(db))
        .use(createAuthModule(db, config))
        .use(createUsersModule(db, config))
    );
}

export type CoreApp = ReturnType<typeof buildApp>;
