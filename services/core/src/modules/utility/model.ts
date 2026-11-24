import { t, type UnwrapSchema } from 'elysia';

export const UtilityModel = {
  rootResponse: t.Object({
    message: t.String(),
    service: t.Literal('core'),
    version: t.String(),
    docs: t.String(),
  }),
  statusResponse: t.Object({
    status: t.Literal('ok'),
    service: t.Literal('core'),
    version: t.String(),
    environment: t.String(),
    uptime_seconds: t.Number(),
    memory_mb: t.Object({
      rss: t.Number(),
      heap_total: t.Number(),
      heap_used: t.Number(),
      external: t.Number(),
    }),
    rate_limiter: t.Optional(
      t.Object({
        store_mode: t.Union([t.Literal('redis'), t.Literal('memory')]),
        evaluated: t.Number(),
        allowed: t.Number(),
        limited_429: t.Number(),
        limited_by_policy: t.Record(t.String(), t.Number()),
        store_errors: t.Number(),
        store_timeouts: t.Number(),
        circuit_open_trips: t.Number(),
        local_fallback_hits: t.Number(),
        eval_p95_ms: t.Number(),
      })
    ),
  }),
  versionResponse: t.Object({
    service: t.Literal('core'),
    version: t.String(),
  }),
  infoResponse: t.Object({
    name: t.String(),
    version: t.String(),
    contact: t.String(),
    documentation_url: t.String(),
    cdn_url: t.String(),
  }),
} as const;

export type UtilityModel = {
  [K in keyof typeof UtilityModel]: UnwrapSchema<(typeof UtilityModel)[K]>;
};
