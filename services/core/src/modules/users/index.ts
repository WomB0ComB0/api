import { Elysia, status, t } from 'elysia';
import type { AppConfig } from '../../config.js';
import type { Database } from '../../db.js';
import { createAuthMacro } from '../../plugins/auth.js';
import { UsersModel } from './model.js';
import { UserService } from './service.js';

export function createUsersModule(db: Database, config: AppConfig) {
  const service = new UserService(db);

  return new Elysia({ name: 'core.users', prefix: '/users' })
    .use(createAuthMacro(db, config))
    .get(
      '',
      async ({ query }) => service.list(query.limit ?? 50, query.offset ?? 0),
      {
        admin: true,
        query: UsersModel.listQuery,
        response: {
          200: UsersModel.listResponse,
          401: UsersModel.error,
          403: UsersModel.error,
        },
        detail: {
          tags: ['users'],
          summary: 'List all users (admin only)',
        },
      }
    )
    .get(
      '/:id',
      async ({ authUser, params }) => {
        if (authUser.role !== 'admin' && authUser.id !== params.id) {
          return status(403, { error: 'Forbidden' });
        }
        const user = await service.getById(params.id);
        if (!user) {
          return status(404, { error: 'User not found' });
        }
        return user;
      },
      {
        auth: true,
        params: UsersModel.idParam,
        response: {
          200: UsersModel.user,
          401: UsersModel.error,
          403: UsersModel.error,
          404: UsersModel.error,
        },
        detail: {
          tags: ['users'],
          summary: 'Get user by ID (self or admin)',
        },
      }
    )
    .patch(
      '/:id',
      async ({ authUser, params, body }) => {
        if (authUser.role !== 'admin' && authUser.id !== params.id) {
          return status(403, { error: 'Forbidden' });
        }
        const updated = await service.updateName(params.id, body.name);
        if (!updated) {
          return status(404, { error: 'User not found' });
        }
        return updated;
      },
      {
        auth: true,
        params: UsersModel.idParam,
        body: UsersModel.updateBody,
        response: {
          200: UsersModel.user,
          401: UsersModel.error,
          403: UsersModel.error,
          404: UsersModel.error,
        },
        detail: {
          tags: ['users'],
          summary: 'Update user profile (self or admin)',
        },
      }
    )
    .delete(
      '/:id',
      async ({ authUser, params }) => {
        if (authUser.role !== 'admin' && authUser.id !== params.id) {
          return status(403, { error: 'Forbidden' });
        }
        const deleted = await service.remove(params.id);
        if (!deleted) {
          return status(404, { error: 'User not found' });
        }
        return status(204, undefined);
      },
      {
        auth: true,
        params: UsersModel.idParam,
        response: {
          204: t.Void(),
          401: UsersModel.error,
          403: UsersModel.error,
          404: UsersModel.error,
        },
        detail: {
          tags: ['users'],
          summary: 'Delete user account (self or admin)',
        },
      }
    );
}
