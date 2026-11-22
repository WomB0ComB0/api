import { createHash, randomBytes } from 'node:crypto';

export type UserRole = 'user' | 'admin';

export interface AccessTokenPayload {
  sub: string;
  email: string;
  role: UserRole;
  'https://hasura.io/jwt/claims': {
    'x-hasura-default-role': UserRole;
    'x-hasura-allowed-roles': UserRole[];
    'x-hasura-user-id': string;
  };
}

export function buildAccessTokenPayload(user: {
  id: string;
  email: string;
  role: UserRole;
}): AccessTokenPayload {
  const allowedRoles: UserRole[] = user.role === 'admin' ? ['user', 'admin'] : ['user'];
  return {
    sub: user.id,
    email: user.email,
    role: user.role,
    'https://hasura.io/jwt/claims': {
      'x-hasura-default-role': user.role,
      'x-hasura-allowed-roles': allowedRoles,
      'x-hasura-user-id': user.id,
    },
  };
}

export function sha256Hex(value: string): string {
  return createHash('sha256').update(value).digest('hex');
}

export function generateRefreshToken(): { raw: string; hash: string } {
  const raw = randomBytes(48).toString('base64url');
  return { raw, hash: sha256Hex(raw) };
}

export function generateApiKey(): { raw: string; prefix: string; hash: string } {
  const secret = randomBytes(32).toString('base64url');
  const raw = `mk_live_${secret}`;
  const prefix = raw.slice(0, 14);
  return { raw, prefix, hash: sha256Hex(raw) };
}
