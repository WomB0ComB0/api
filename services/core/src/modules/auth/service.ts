import type { AppConfig } from '../../config.js';
import type { Database } from '../../db.js';
import { hashPassword, verifyPassword } from '../../auth/password.js';
import {
  buildAccessTokenPayload,
  generateApiKey,
  generateRefreshToken,
  sha256Hex,
  type UserRole,
} from '../../auth/tokens.js';
import type { AuthModel } from './model.js';

interface UserProfileRecord extends Record<string, unknown> {
  id: string;
  email: string;
  name: string;
  role: UserRole;
  is_active: boolean;
  created_at: string | Date;
  updated_at: string | Date;
}

interface UserRecord extends UserProfileRecord {
  password_hash: string;
}

interface RefreshTokenRecord extends Record<string, unknown> {
  id: string;
  user_id: string;
  expires_at: string | Date;
  revoked_at: string | Date | null;
  email: string;
  name: string;
  role: UserRole;
  is_active: boolean;
}

interface ApiKeyRecord extends Record<string, unknown> {
  id: string;
  name: string;
  key_prefix: string;
  scopes: string[];
  last_used_at: string | Date | null;
  expires_at: string | Date | null;
  created_at: string | Date;
}

export type SignJwtFn = (payload: Record<string, unknown>) => Promise<string>;

function toIso(value: string | Date): string {
  return value instanceof Date ? value.toISOString() : value;
}

function toNullableIso(value: string | Date | null): string | null {
  if (value === null) return null;
  return toIso(value);
}

/**
 * Decoupled Auth business logic (no Elysia Context dependency).
 */
export class AuthService {
  constructor(
    private readonly db: Database,
    private readonly config: AppConfig
  ) {}

  private async issueTokenPair(
    signJwt: SignJwtFn,
    user: { id: string; email: string; role: UserRole }
  ): Promise<AuthModel['refreshResponse']> {
    const payload = buildAccessTokenPayload(user);
    const accessToken = await signJwt(payload as unknown as Record<string, unknown>);
    const refresh = generateRefreshToken();
    const refreshExpiresAt = new Date(Date.now() + this.config.jwtRefreshTtlDays * 24 * 60 * 60 * 1000);

    await this.db.query(
      'INSERT INTO core.refresh_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)',
      [user.id, refresh.hash, refreshExpiresAt.toISOString()]
    );

    return {
      access_token: accessToken,
      refresh_token: refresh.raw,
      token_type: 'Bearer',
      expires_in: this.config.jwtAccessTtl,
    };
  }

  async register(
    body: AuthModel['registerBody'],
    signJwt: SignJwtFn
  ): Promise<{ ok: true; data: AuthModel['registerResponse'] } | { ok: false; code: 409; error: string }> {
    const email = body.email.trim().toLowerCase();
    const name = body.name.trim();
    const passwordHash = await hashPassword(body.password);
    const refresh = generateRefreshToken();
    const refreshExpiresAt = new Date(Date.now() + this.config.jwtRefreshTtlDays * 24 * 60 * 60 * 1000);

    try {
      // Single atomic CTE: create user and initial refresh token in 1 DB roundtrip
      const result = await this.db.query<UserProfileRecord>(
        `WITH created_user AS (
           INSERT INTO core.users (email, name, password_hash, role)
           VALUES ($1, $2, $3, 'user')
           RETURNING id, email, name, role, is_active, created_at, updated_at
         ),
         created_token AS (
           INSERT INTO core.refresh_tokens (user_id, token_hash, expires_at)
           SELECT id, $4, $5 FROM created_user
           RETURNING id
         )
         SELECT id, email, name, role, is_active, created_at, updated_at FROM created_user`,
        [email, name, passwordHash, refresh.hash, refreshExpiresAt.toISOString()]
      );

      const user = result.rows[0]!;
      const payload = buildAccessTokenPayload({ id: user.id, email: user.email, role: user.role });
      const accessToken = await signJwt(payload as unknown as Record<string, unknown>);

      return {
        ok: true,
        data: {
          user: {
            id: user.id,
            email: user.email,
            name: user.name,
            role: user.role,
            created_at: toIso(user.created_at),
          },
          access_token: accessToken,
          refresh_token: refresh.raw,
          token_type: 'Bearer',
          expires_in: this.config.jwtAccessTtl,
        },
      };
    } catch (err: unknown) {
      const pgErr = err as { code?: string };
      if (pgErr.code === '23505') {
        return { ok: false, code: 409, error: 'Email already registered' };
      }
      throw err;
    }
  }

  async login(
    body: AuthModel['loginBody'],
    signJwt: SignJwtFn
  ): Promise<{ ok: true; data: AuthModel['loginResponse'] } | { ok: false; code: 401; error: string }> {
    const email = body.email.trim().toLowerCase();
    const result = await this.db.query<UserRecord>(
      'SELECT id, email, name, password_hash, role, is_active, created_at, updated_at FROM core.users WHERE email = $1',
      [email]
    );

    const user = result.rows[0];
    if (!user || !user.is_active) {
      return { ok: false, code: 401, error: 'Invalid credentials' };
    }

    const valid = await verifyPassword(body.password, user.password_hash);
    if (!valid) {
      return { ok: false, code: 401, error: 'Invalid credentials' };
    }

    const tokens = await this.issueTokenPair(signJwt, {
      id: user.id,
      email: user.email,
      role: user.role,
    });

    return {
      ok: true,
      data: {
        user: {
          id: user.id,
          email: user.email,
          name: user.name,
          role: user.role,
        },
        ...tokens,
      },
    };
  }

  async refresh(
    refreshToken: string,
    signJwt: SignJwtFn
  ): Promise<{ ok: true; data: AuthModel['refreshResponse'] } | { ok: false; code: 401; error: string }> {
    const tokenHash = sha256Hex(refreshToken);
    const nextRefresh = generateRefreshToken();
    const nextExpiresAt = new Date(Date.now() + this.config.jwtRefreshTtlDays * 24 * 60 * 60 * 1000);

    // Single atomic CTE: validate + revoke old refresh token + insert rotated token in 1 DB roundtrip
    const result = await this.db.query<RefreshTokenRecord>(
      `WITH revoked AS (
         UPDATE core.refresh_tokens rt
         SET revoked_at = NOW()
         FROM core.users u
         WHERE rt.user_id = u.id
           AND rt.token_hash = $1
           AND rt.revoked_at IS NULL
           AND rt.expires_at > NOW()
           AND u.is_active = true
         RETURNING rt.id, rt.user_id, rt.expires_at, rt.revoked_at, u.email, u.name, u.role, u.is_active
       ),
       inserted AS (
         INSERT INTO core.refresh_tokens (user_id, token_hash, expires_at)
         SELECT user_id, $2, $3 FROM revoked
         RETURNING id
       )
       SELECT id, user_id, expires_at, revoked_at, email, name, role, is_active FROM revoked`,
      [tokenHash, nextRefresh.hash, nextExpiresAt.toISOString()]
    );

    const row = result.rows[0];
    if (!row) {
      return { ok: false, code: 401, error: 'Invalid or expired refresh token' };
    }

    const payload = buildAccessTokenPayload({ id: row.user_id, email: row.email, role: row.role });
    const accessToken = await signJwt(payload as unknown as Record<string, unknown>);

    return {
      ok: true,
      data: {
        access_token: accessToken,
        refresh_token: nextRefresh.raw,
        token_type: 'Bearer',
        expires_in: this.config.jwtAccessTtl,
      },
    };
  }

  async logout(refreshToken: string): Promise<void> {
    const tokenHash = sha256Hex(refreshToken);
    await this.db.query(
      'UPDATE core.refresh_tokens SET revoked_at = NOW() WHERE token_hash = $1 AND revoked_at IS NULL',
      [tokenHash]
    );
  }

  async getProfile(userId: string): Promise<AuthModel['meResponse'] | null> {
    const result = await this.db.query<UserProfileRecord>(
      'SELECT id, email, name, role, is_active, created_at, updated_at FROM core.users WHERE id = $1',
      [userId]
    );
    const row = result.rows[0];
    if (!row) return null;
    return {
      id: row.id,
      email: row.email,
      name: row.name,
      role: row.role,
      is_active: row.is_active,
      created_at: toIso(row.created_at),
      updated_at: toIso(row.updated_at),
    };
  }

  async createApiKey(
    userId: string,
    body: AuthModel['createApiKeyBody']
  ): Promise<AuthModel['createApiKeyResponse']> {
    const generated = generateApiKey();
    const scopes = body.scopes && body.scopes.length > 0 ? body.scopes : ['read'];
    const expiresAt = body.expires_in_days
      ? new Date(Date.now() + body.expires_in_days * 24 * 60 * 60 * 1000).toISOString()
      : null;

    const result = await this.db.query<ApiKeyRecord>(
      `INSERT INTO core.api_keys (user_id, name, key_prefix, key_hash, scopes, expires_at)
       VALUES ($1, $2, $3, $4, $5, $6)
       RETURNING id, name, key_prefix, scopes, last_used_at, expires_at, created_at`,
      [userId, body.name.trim(), generated.prefix, generated.hash, scopes, expiresAt]
    );

    const row = result.rows[0]!;
    return {
      id: row.id,
      name: row.name,
      key_prefix: row.key_prefix,
      scopes: row.scopes,
      last_used_at: toNullableIso(row.last_used_at),
      expires_at: toNullableIso(row.expires_at),
      created_at: toIso(row.created_at),
      api_key: generated.raw,
    };
  }

  async listApiKeys(userId: string): Promise<AuthModel['listApiKeysResponse']> {
    const result = await this.db.query<ApiKeyRecord>(
      `SELECT id, name, key_prefix, scopes, last_used_at, expires_at, created_at
       FROM core.api_keys
       WHERE user_id = $1 AND revoked_at IS NULL
       ORDER BY created_at DESC`,
      [userId]
    );

    return {
      api_keys: result.rows.map((row) => ({
        id: row.id,
        name: row.name,
        key_prefix: row.key_prefix,
        scopes: row.scopes,
        last_used_at: toNullableIso(row.last_used_at),
        expires_at: toNullableIso(row.expires_at),
        created_at: toIso(row.created_at),
      })),
    };
  }

  async revokeApiKey(userId: string, keyId: string): Promise<boolean> {
    const result = await this.db.query(
      'UPDATE core.api_keys SET revoked_at = NOW() WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL RETURNING id',
      [keyId, userId]
    );
    return result.rows.length > 0;
  }
}
