import { generateSecureToken, hashData, type Sha256Hex } from '@resq-systems/security/crypto';
import { toPositiveInt } from '@resq-systems/types';

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

export function sha256Hex(value: string): Sha256Hex {
  return hashData(value);
}

const REFRESH_TOKEN_BYTES = toPositiveInt(48);
const API_KEY_BYTES = toPositiveInt(32);

export function generateRefreshToken(): { raw: string; hash: Sha256Hex } {
  const raw = generateSecureToken(REFRESH_TOKEN_BYTES);
  return { raw, hash: sha256Hex(raw) };
}

export function generateApiKey(): { raw: string; prefix: string; hash: Sha256Hex } {
  const secret = generateSecureToken(API_KEY_BYTES);
  const raw = `mk_live_${secret}`;
  const prefix = raw.slice(0, 14);
  return { raw, prefix, hash: sha256Hex(raw) };
}
