import { bearer } from '@elysiajs/bearer';
import { jwt } from '@elysiajs/jwt';
import { Elysia, status } from 'elysia';
import type { AppConfig } from '../config.js';
import type { Database } from '../db.js';
import { sha256Hex, type AccessTokenPayload, type UserRole } from '../auth/tokens.js';

export interface AuthUser {
  id: string;
  email: string;
  role: UserRole;
  authType: 'jwt' | 'api_key';
  scopes?: string[];
}

interface ApiKeyLookupRow extends Record<string, unknown> {
  key_id: string;
  user_id: string;
  email: string;
  role: UserRole;
  is_active: boolean;
  scopes: string[];
  expires_at: string | Date | null;
  revoked_at: string | Date | null;
}

export function createJwtPlugin(config: AppConfig) {
  return jwt({
    name: 'jwt',
    secret: config.jwtSecret,
    alg: 'HS256',
    exp: config.jwtAccessTtl,
  });
}

/**
 * Named, deduplicated Elysia auth macro plugin using @elysiajs/bearer and @elysiajs/jwt.
 *
 * Usage on any route:
 *   - `{ auth: true }`  -> resolves non-null `authUser` (JWT or API key) or returns 401
 *   - `{ admin: true }` -> implies `auth: true` and enforces `authUser.role === 'admin'` or returns 403
 */
export function createAuthMacro(db: Database, config: AppConfig) {
  return new Elysia({ name: 'core.auth.macro' })
    .use(bearer())
    .use(createJwtPlugin(config))
    .macro('auth', {
      async resolve({ headers, bearer: bearerToken, jwt: jwtHelper }) {
        const apiKeyHeader = headers['x-api-key'];

        let rawApiKey: string | undefined;
        if (typeof apiKeyHeader === 'string' && apiKeyHeader.trim() !== '') {
          rawApiKey = apiKeyHeader.trim();
        } else if (typeof bearerToken === 'string' && bearerToken.startsWith('mk_')) {
          rawApiKey = bearerToken.trim();
        }

        if (rawApiKey) {
          const keyHash = sha256Hex(rawApiKey);
          // Single atomic query: verify key + active user + expiry AND touch last_used_at
          const result = await db.query<ApiKeyLookupRow>(
            `UPDATE core.api_keys k
             SET last_used_at = NOW()
             FROM core.users u
             WHERE k.user_id = u.id
               AND k.key_hash = $1
               AND k.revoked_at IS NULL
               AND (k.expires_at IS NULL OR k.expires_at > NOW())
               AND u.is_active = true
             RETURNING k.id AS key_id, k.user_id, u.email, u.role, u.is_active, k.scopes, k.expires_at, k.revoked_at`,
            [keyHash]
          );

          const row = result.rows[0];
          if (!row) {
            return status(401, { error: 'Invalid, expired, or revoked API key' });
          }

          const authUser: AuthUser = {
            id: row.user_id,
            email: row.email,
            role: row.role,
            authType: 'api_key',
            scopes: row.scopes,
          };
          return { authUser };
        }

        if (!bearerToken) {
          return status(401, { error: 'Unauthorized' });
        }

        const payload = (await jwtHelper.verify(bearerToken)) as Partial<AccessTokenPayload> | false;
        if (
          !payload ||
          typeof payload.sub !== 'string' ||
          typeof payload.email !== 'string' ||
          (payload.role !== 'user' && payload.role !== 'admin')
        ) {
          return status(401, { error: 'Invalid or expired token' });
        }

        const authUser: AuthUser = {
          id: payload.sub,
          email: payload.email,
          role: payload.role,
          authType: 'jwt',
        };
        return { authUser };
      },
      detail: {
        security: [{ bearerAuth: [] }, { apiKeyAuth: [] }],
      },
    })
    .macro('admin', {
      auth: true,
      beforeHandle({ authUser }) {
        if (authUser.role !== 'admin') {
          return status(403, { error: 'Forbidden' });
        }
        return undefined;
      },
      detail: {
        security: [{ bearerAuth: [] }],
      },
    })
    .as('scoped');
}
