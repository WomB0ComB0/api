import { t, type UnwrapSchema } from 'elysia';

export const HealthModel = {
  healthResponse: t.Object({
    status: t.Literal('healthy'),
    timestamp: t.String(),
    service: t.Literal('core'),
  }),
  readyResponse: t.Object({
    status: t.Union([t.Literal('ready'), t.Literal('not_ready')]),
    timestamp: t.String(),
    checks: t.Object({
      database: t.Union([t.Literal('healthy'), t.Literal('unhealthy')]),
    }),
  }),
} as const;

export type HealthModel = {
  [K in keyof typeof HealthModel]: UnwrapSchema<(typeof HealthModel)[K]>;
};
