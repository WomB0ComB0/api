import { t, type UnwrapSchema } from 'elysia';
import { ErrorResponse, UserProfileSchema, UUID_PATTERN } from '../auth/model.js';

export const UsersModel = {
  error: ErrorResponse,
  user: UserProfileSchema,
  idParam: t.Object({
    id: t.String({ pattern: UUID_PATTERN, error: 'Invalid UUID format' }),
  }),
  listQuery: t.Object({
    limit: t.Optional(t.Numeric({ minimum: 1, maximum: 100 })),
    offset: t.Optional(t.Numeric({ minimum: 0, maximum: 10000 })),
  }),
  listResponse: t.Object({
    users: t.Array(UserProfileSchema),
    total: t.Number(),
    limit: t.Number(),
    offset: t.Number(),
  }),
  updateBody: t.Object({
    name: t.Optional(t.String({ minLength: 1, maxLength: 255 })),
  }),
} as const;

export type UsersModel = {
  [K in keyof typeof UsersModel]: UnwrapSchema<(typeof UsersModel)[K]>;
};
