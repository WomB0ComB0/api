import { containsBidiControls } from '@resq-systems/security/unicode';
import { Elysia, status, t } from 'elysia';
import type { AppConfig } from '../../config.js';
import type { Database } from '../../db.js';
import { createAuthMacro, createJwtPlugin } from '../../plugins/auth.js';
import { AuthModel } from './model.js';
import { AuthService } from './service.js';

export function createAuthModule(db: Database, config: AppConfig) {
  const service = new AuthService(db, config);

  return new Elysia({ name: 'core.auth', prefix: '/auth' })
    .use(createJwtPlugin(config))
    .use(createAuthMacro(db, config))
    .post(
      '/register',
      async ({ body, jwt }) => {
        if (body.name.trim().length === 0) {
          return status(400, { error: 'Name must not be empty or whitespace-only' });
        }
        if (containsBidiControls(body.name) || containsBidiControls(body.email)) {
          return status(400, { error: 'Input contains disallowed bidirectional control characters' });
        }
        const result = await service.register(body, (p) =>
          jwt.sign(p as Parameters<typeof jwt.sign>[0])
        );
        if (!result.ok) {
          return status(409, { error: result.error });
        }
        return status(201, result.data);
      },
      {
        body: AuthModel.registerBody,
        response: {
          201: AuthModel.registerResponse,
          400: AuthModel.error,
          409: AuthModel.error,
        },
        detail: {
          tags: ['auth'],
          summary: 'Register a new user account',
        },
      }
    )
    .post(
      '/login',
      async ({ body, jwt }) => {
        const result = await service.login(body, (p) =>
          jwt.sign(p as Parameters<typeof jwt.sign>[0])
        );
        if (!result.ok) {
          return status(401, { error: result.error });
        }
        return result.data;
      },
      {
        body: AuthModel.loginBody,
        response: {
          200: AuthModel.loginResponse,
          401: AuthModel.error,
        },
        detail: {
          tags: ['auth'],
          summary: 'Authenticate with email and password',
        },
      }
    )
    .post(
      '/refresh',
      async ({ body, jwt }) => {
        const result = await service.refresh(body.refresh_token, (p) =>
          jwt.sign(p as Parameters<typeof jwt.sign>[0])
        );
        if (!result.ok) {
          return status(401, { error: result.error });
        }
        return result.data;
      },
      {
        body: AuthModel.refreshBody,
        response: {
          200: AuthModel.refreshResponse,
          401: AuthModel.error,
        },
        detail: {
          tags: ['auth'],
          summary: 'Rotate a refresh token for a fresh access/refresh pair',
        },
      }
    )
    .post(
      '/logout',
      async ({ body }) => {
        await service.logout(body.refresh_token);
        return status(204, undefined);
      },
      {
        body: AuthModel.refreshBody,
        response: {
          204: t.Void(),
        },
        detail: {
          tags: ['auth'],
          summary: 'Revoke a refresh token',
        },
      }
    )
    .get(
      '/me',
      async ({ authUser }) => {
        if (authUser.authType === 'api_key' && !authUser.scopes?.includes('read')) {
          return status(403, { error: 'API key lacks required "read" scope' });
        }
        const profile = await service.getProfile(authUser.id);
        if (!profile) {
          return status(404, { error: 'User not found' });
        }
        return profile;
      },
      {
        auth: true,
        response: {
          200: AuthModel.meResponse,
          401: AuthModel.error,
          403: AuthModel.error,
          404: AuthModel.error,
        },
        detail: {
          tags: ['auth'],
          summary: 'Get authenticated user profile',
        },
      }
    )
    .post(
      '/api-keys',
      async ({ authUser, body }) => {
        if (authUser.authType !== 'jwt') {
          return status(403, { error: 'API keys cannot manage API keys; authenticate with a Bearer JWT' });
        }
        if (body.name.trim().length === 0) {
          return status(400, { error: 'API key name must not be empty or whitespace-only' });
        }
        if (containsBidiControls(body.name)) {
          return status(400, { error: 'API key name contains disallowed bidirectional control characters' });
        }
        const created = await service.createApiKey(authUser.id, body);
        return status(201, created);
      },
      {
        auth: true,
        body: AuthModel.createApiKeyBody,
        response: {
          201: AuthModel.createApiKeyResponse,
          400: AuthModel.error,
          401: AuthModel.error,
          403: AuthModel.error,
        },
        detail: {
          tags: ['auth'],
          summary: 'Create a scoped API key (raw key returned once; JWT required)',
        },
      }
    )
    .get(
      '/api-keys',
      async ({ authUser }) => {
        if (authUser.authType !== 'jwt') {
          return status(403, { error: 'API keys cannot list API keys; authenticate with a Bearer JWT' });
        }
        return service.listApiKeys(authUser.id);
      },
      {
        auth: true,
        response: {
          200: AuthModel.listApiKeysResponse,
          401: AuthModel.error,
          403: AuthModel.error,
        },
        detail: {
          tags: ['auth'],
          summary: 'List active API keys for authenticated user (JWT required)',
        },
      }
    )
    .delete(
      '/api-keys/:id',
      async ({ authUser, params }) => {
        if (authUser.authType !== 'jwt') {
          return status(403, { error: 'API keys cannot revoke API keys; authenticate with a Bearer JWT' });
        }
        const revoked = await service.revokeApiKey(authUser.id, params.id);
        if (!revoked) {
          return status(404, { error: 'API key not found' });
        }
        return status(204, undefined);
      },
      {
        auth: true,
        params: AuthModel.idParam,
        response: {
          204: t.Void(),
          401: AuthModel.error,
          403: AuthModel.error,
          404: AuthModel.error,
        },
        detail: {
          tags: ['auth'],
          summary: 'Revoke an API key (JWT required)',
        },
      }
    );
}
