import { FastifyPluginAsync } from 'fastify';
import pg from 'pg';

const { Pool } = pg;

const pool = new Pool({
  connectionString: process.env.DATABASE_URL,
  max: 20,
  idleTimeoutMillis: 30000,
  connectionTimeoutMillis: 2000,
});

export const healthRoutes: FastifyPluginAsync = async (fastify) => {
  fastify.get(
    '/health',
    {
      schema: {
        tags: ['health'],
        description: 'Basic health check',
        response: {
          200: {
            type: 'object',
            properties: {
              status: { type: 'string' },
              timestamp: { type: 'string' },
              service: { type: 'string' },
            },
          },
        },
      },
    },
    async () => {
      return {
        status: 'healthy',
        timestamp: new Date().toISOString(),
        service: 'core',
      };
    }
  );

  fastify.get(
    '/health/ready',
    {
      schema: {
        tags: ['health'],
        description: 'Readiness check with database connectivity',
        response: {
          200: {
            type: 'object',
            properties: {
              status: { type: 'string' },
              timestamp: { type: 'string' },
              checks: {
                type: 'object',
                properties: {
                  database: { type: 'string' },
                },
              },
            },
          },
        },
      },
    },
    async (request, reply) => {
      const checks: Record<string, string> = {};

      // Check database
      try {
        const client = await pool.connect();
        await client.query('SELECT 1');
        client.release();
        checks.database = 'healthy';
      } catch (error) {
        checks.database = 'unhealthy';
        reply.code(503);
      }

      return {
        status: Object.values(checks).every((v) => v === 'healthy')
          ? 'ready'
          : 'not_ready',
        timestamp: new Date().toISOString(),
        checks,
      };
    }
  );

  fastify.get(
    '/openapi.json',
    {
      schema: {
        hide: true,
      },
    },
    async () => {
      return fastify.swagger();
    }
  );
};
