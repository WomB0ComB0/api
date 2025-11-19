import { FastifyPluginAsync } from 'fastify';
import pg from 'pg';
import { z } from 'zod';

const { Pool } = pg;

const pool = new Pool({
  connectionString: process.env.DATABASE_URL,
});

const createUserSchema = z.object({
  email: z.string().email(),
  name: z.string().min(1).max(255),
});

const updateUserSchema = z.object({
  name: z.string().min(1).max(255).optional(),
});

export const userRoutes: FastifyPluginAsync = async (fastify) => {
  // List users
  fastify.get(
    '/users',
    {
      schema: {
        tags: ['users'],
        description: 'List all users',
        security: [{ bearerAuth: [] }],
        response: {
          200: {
            type: 'object',
            properties: {
              users: {
                type: 'array',
                items: {
                  type: 'object',
                  properties: {
                    id: { type: 'string' },
                    email: { type: 'string' },
                    name: { type: 'string' },
                    created_at: { type: 'string' },
                    updated_at: { type: 'string' },
                  },
                },
              },
            },
          },
        },
      },
    },
    async () => {
      const result = await pool.query(
        'SELECT id, email, name, created_at, updated_at FROM core.users ORDER BY created_at DESC'
      );
      return { users: result.rows };
    }
  );

  // Get user by ID
  fastify.get(
    '/users/:id',
    {
      schema: {
        tags: ['users'],
        description: 'Get user by ID',
        security: [{ bearerAuth: [] }],
        params: {
          type: 'object',
          properties: {
            id: { type: 'string', format: 'uuid' },
          },
        },
        response: {
          200: {
            type: 'object',
            properties: {
              id: { type: 'string' },
              email: { type: 'string' },
              name: { type: 'string' },
              created_at: { type: 'string' },
              updated_at: { type: 'string' },
            },
          },
          404: {
            type: 'object',
            properties: {
              error: { type: 'string' },
            },
          },
        },
      },
    },
    async (request, reply) => {
      const { id } = request.params as { id: string };
      const result = await pool.query(
        'SELECT id, email, name, created_at, updated_at FROM core.users WHERE id = $1',
        [id]
      );

      if (result.rows.length === 0) {
        reply.code(404);
        return { error: 'User not found' };
      }

      return result.rows[0];
    }
  );

  // Create user
  fastify.post(
    '/users',
    {
      schema: {
        tags: ['users'],
        description: 'Create a new user',
        security: [{ bearerAuth: [] }],
        body: {
          type: 'object',
          required: ['email', 'name'],
          properties: {
            email: { type: 'string', format: 'email' },
            name: { type: 'string', minLength: 1, maxLength: 255 },
          },
        },
        response: {
          201: {
            type: 'object',
            properties: {
              id: { type: 'string' },
              email: { type: 'string' },
              name: { type: 'string' },
              created_at: { type: 'string' },
              updated_at: { type: 'string' },
            },
          },
        },
      },
    },
    async (request, reply) => {
      const data = createUserSchema.parse(request.body);
      const result = await pool.query(
        'INSERT INTO core.users (email, name) VALUES ($1, $2) RETURNING id, email, name, created_at, updated_at',
        [data.email, data.name]
      );

      reply.code(201);
      return result.rows[0];
    }
  );

  // Update user
  fastify.patch(
    '/users/:id',
    {
      schema: {
        tags: ['users'],
        description: 'Update user',
        security: [{ bearerAuth: [] }],
        params: {
          type: 'object',
          properties: {
            id: { type: 'string', format: 'uuid' },
          },
        },
        body: {
          type: 'object',
          properties: {
            name: { type: 'string', minLength: 1, maxLength: 255 },
          },
        },
        response: {
          200: {
            type: 'object',
            properties: {
              id: { type: 'string' },
              email: { type: 'string' },
              name: { type: 'string' },
              created_at: { type: 'string' },
              updated_at: { type: 'string' },
            },
          },
        },
      },
    },
    async (request, reply) => {
      const { id } = request.params as { id: string };
      const data = updateUserSchema.parse(request.body);

      const result = await pool.query(
        'UPDATE core.users SET name = COALESCE($1, name) WHERE id = $2 RETURNING id, email, name, created_at, updated_at',
        [data.name, id]
      );

      if (result.rows.length === 0) {
        reply.code(404);
        return { error: 'User not found' };
      }

      return result.rows[0];
    }
  );

  // Delete user
  fastify.delete(
    '/users/:id',
    {
      schema: {
        tags: ['users'],
        description: 'Delete user',
        security: [{ bearerAuth: [] }],
        params: {
          type: 'object',
          properties: {
            id: { type: 'string', format: 'uuid' },
          },
        },
        response: {
          204: {
            type: 'null',
          },
        },
      },
    },
    async (request, reply) => {
      const { id } = request.params as { id: string };
      await pool.query('DELETE FROM core.users WHERE id = $1', [id]);
      reply.code(204);
    }
  );
};
