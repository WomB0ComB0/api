import { t, type UnwrapSchema } from 'elysia';

export const UUID_PATTERN = '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$';

export const ErrorResponse = t.Object({
  error: t.String(),
});

export const RoleSchema = t.Union([t.Literal('user'), t.Literal('admin')]);
export const ApiKeyScopeSchema = t.Union([
  t.Literal('read'),
  t.Literal('write'),
  t.Literal('media'),
  t.Literal('links'),
]);

export const UserSummarySchema = t.Object({
  id: t.String(),
  email: t.String(),
  name: t.String(),
  role: RoleSchema,
  created_at: t.Optional(t.String()),
});

export const UserProfileSchema = t.Object({
  id: t.String(),
  email: t.String(),
  name: t.String(),
  role: RoleSchema,
  is_active: t.Boolean(),
  created_at: t.String(),
  updated_at: t.String(),
});

export const TokenPairSchema = t.Object({
  access_token: t.String(),
  refresh_token: t.String(),
  token_type: t.Literal('Bearer'),
  expires_in: t.String(),
});

export const ApiKeySummarySchema = t.Object({
  id: t.String(),
  name: t.String(),
  key_prefix: t.String(),
  scopes: t.Array(t.String()),
  last_used_at: t.Nullable(t.String()),
  expires_at: t.Nullable(t.String()),
  created_at: t.String(),
});

export const AuthModel = {
  error: ErrorResponse,
  registerBody: t.Object({
    email: t.String({ format: 'email', maxLength: 255, error: 'Valid email is required' }),
    name: t.String({ minLength: 1, maxLength: 255, error: 'Name must be between 1 and 255 characters' }),
    password: t.String({ minLength: 10, maxLength: 128, error: 'Password must be at least 10 characters' }),
  }),
  registerResponse: t.Object({
    user: UserSummarySchema,
    access_token: t.String(),
    refresh_token: t.String(),
    token_type: t.Literal('Bearer'),
    expires_in: t.String(),
  }),
  loginBody: t.Object({
    email: t.String({ format: 'email', maxLength: 255 }),
    password: t.String({ minLength: 1, maxLength: 128 }),
  }),
  loginResponse: t.Object({
    user: UserSummarySchema,
    access_token: t.String(),
    refresh_token: t.String(),
    token_type: t.Literal('Bearer'),
    expires_in: t.String(),
  }),
  refreshBody: t.Object({
    refresh_token: t.String({ minLength: 16 }),
  }),
  refreshResponse: TokenPairSchema,
  meResponse: UserProfileSchema,
  createApiKeyBody: t.Object({
    name: t.String({ minLength: 1, maxLength: 100 }),
    scopes: t.Optional(t.Array(ApiKeyScopeSchema, { minItems: 1 })),
    expires_in_days: t.Optional(t.Integer({ minimum: 1, maximum: 365 })),
  }),
  createApiKeyResponse: t.Composite([
    ApiKeySummarySchema,
    t.Object({ api_key: t.String() }),
  ]),
  listApiKeysResponse: t.Object({
    api_keys: t.Array(ApiKeySummarySchema),
  }),
  idParam: t.Object({
    id: t.String({ pattern: UUID_PATTERN, error: 'Invalid UUID format' }),
  }),
} as const;

export type AuthModel = {
  [K in keyof typeof AuthModel]: UnwrapSchema<(typeof AuthModel)[K]>;
};
