import cors from '@fastify/cors';
import jwt from '@fastify/jwt';
import swagger from '@fastify/swagger';
import swaggerUi from '@fastify/swagger-ui';
import Fastify from 'fastify';
import { healthRoutes } from './routes/health.js';
import { userRoutes } from './routes/users.js';
import { initTelemetry } from './telemetry.js';

// Initialize OpenTelemetry
initTelemetry();

const fastify = Fastify({
  logger: {
    level: process.env.LOG_LEVEL || 'info',
    transport:
      process.env.NODE_ENV === 'development'
        ? {
            target: 'pino-pretty',
            options: {
              colorize: true,
              translateTime: 'HH:MM:ss Z',
              ignore: 'pid,hostname',
            },
          }
        : undefined,
  },
  requestIdHeader: 'x-request-id',
  disableRequestLogging: false,
  trustProxy: true,
});

// CORS
await fastify.register(cors, {
  origin: [
    'https://mikeodnis.dev',
    'https://www.mikeodnis.dev',
    /localhost/,
  ],
  credentials: true,
});

// JWT
await fastify.register(jwt, {
  secret: process.env.JWT_SECRET || 'development-secret-change-me',
  sign: {
    expiresIn: '7d',
  },
});

// OpenAPI Documentation
await fastify.register(swagger, {
  openapi: {
    info: {
      title: 'Core API',
      description: 'Core business logic and shared utilities',
      version: '1.0.0',
    },
    servers: [
      {
        url: 'https://api.mikeodnis.dev/v1/core',
        description: 'Production',
      },
    ],
    tags: [
      { name: 'health', description: 'Health check endpoints' },
      { name: 'users', description: 'User management' },
    ],
    components: {
      securitySchemes: {
        bearerAuth: {
          type: 'http',
          scheme: 'bearer',
          bearerFormat: 'JWT',
        },
      },
    },
  },
});

await fastify.register(swaggerUi, {
  routePrefix: '/v1/core/docs',
  uiConfig: {
    docExpansion: 'list',
    deepLinking: true,
  },
});

// Routes
await fastify.register(healthRoutes, { prefix: '/v1/core' });
await fastify.register(userRoutes, { prefix: '/v1/core' });

// Start server
const start = async () => {
  try {
    const port = parseInt(process.env.PORT || '3000', 10);
    await fastify.listen({ port, host: '0.0.0.0' });
    console.log(`🚀 Core service listening on port ${port}`);
  } catch (err) {
    fastify.log.error(err);
    process.exit(1);
  }
};

start();
