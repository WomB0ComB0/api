import { z } from 'zod';

const DEFAULT_DEV_JWT_SECRET = 'development-jwt-secret-change-me-32chars!';
const DEFAULT_CORS_ORIGINS = [
  'https://mikeodnis.dev',
  'https://www.mikeodnis.dev',
  'https://cdn.mikeodnis.dev',
  'https://blog.mikeodnis.dev',
  'https://news.mikeodnis.dev',
  'https://links.mikeodnis.dev',
];

const envSchema = z.object({
  NODE_ENV: z.enum(['development', 'test', 'production']).default('development'),
  PORT: z.coerce.number().int().min(1).max(65535).default(3000),
  LOG_LEVEL: z.string().default('info'),
  DATABASE_URL: z.string().optional(),
  JWT_SECRET: z.string().optional(),
  JWT_ACCESS_TTL: z.string().default('15m'),
  JWT_REFRESH_TTL_DAYS: z.coerce.number().int().min(1).max(90).default(30),
  CORS_ORIGINS: z.string().optional(),
  OTEL_EXPORTER_OTLP_ENDPOINT: z.string().optional(),
  OTEL_EXPORTER_OTLP_HEADERS: z.string().optional(),
  OTEL_SERVICE_NAME: z.string().default('core-service'),
  RATE_LIMIT_MAX: z.coerce.number().int().min(1).default(100),
  RATE_LIMIT_WINDOW_MS: z.coerce.number().int().min(1000).default(60_000),
});

export interface AppConfig {
  nodeEnv: 'development' | 'test' | 'production';
  port: number;
  logLevel: string;
  databaseUrl: string;
  jwtSecret: string;
  jwtAccessTtl: string;
  jwtRefreshTtlDays: number;
  corsOrigins: (string | RegExp)[];
  otelEndpoint?: string;
  otelHeaders?: Record<string, string>;
  otelServiceName: string;
  rateLimitMax: number;
  rateLimitWindowMs: number;
}

export function loadConfig(env: NodeJS.ProcessEnv = process.env): AppConfig {
  const parsed = envSchema.parse(env);

  const jwtSecret = parsed.JWT_SECRET || (parsed.NODE_ENV === 'production' ? '' : DEFAULT_DEV_JWT_SECRET);
  if (parsed.NODE_ENV === 'production' && jwtSecret.length < 32) {
    throw new Error('JWT_SECRET must be at least 32 characters in production');
  }

  const databaseUrl =
    parsed.DATABASE_URL ||
    (parsed.NODE_ENV === 'production' ? '' : 'postgresql://postgres:postgres@localhost:5432/api');
  if (parsed.NODE_ENV === 'production' && !databaseUrl) {
    throw new Error('DATABASE_URL is required in production');
  }

  const configuredOrigins = parsed.CORS_ORIGINS
    ? parsed.CORS_ORIGINS.split(',')
        .map((o) => o.trim())
        .filter(Boolean)
    : DEFAULT_CORS_ORIGINS;

  const corsOrigins: (string | RegExp)[] =
    parsed.NODE_ENV === 'production'
      ? configuredOrigins
      : [...configuredOrigins, /^https?:\/\/localhost(:\d+)?$/, /^https?:\/\/127\.0\.0\.1(:\d+)?$/];

  return {
    nodeEnv: parsed.NODE_ENV,
    port: parsed.PORT,
    logLevel: parsed.LOG_LEVEL,
    databaseUrl,
    jwtSecret,
    jwtAccessTtl: parsed.JWT_ACCESS_TTL,
    jwtRefreshTtlDays: parsed.JWT_REFRESH_TTL_DAYS,
    corsOrigins,
    otelEndpoint: parsed.OTEL_EXPORTER_OTLP_ENDPOINT || undefined,
    otelHeaders: parseOtelHeaders(parsed.OTEL_EXPORTER_OTLP_HEADERS),
    otelServiceName: parsed.OTEL_SERVICE_NAME,
    rateLimitMax: parsed.RATE_LIMIT_MAX,
    rateLimitWindowMs: parsed.RATE_LIMIT_WINDOW_MS,
  };
}

export function parseOtelHeaders(raw?: string): Record<string, string> | undefined {
  if (!raw?.trim()) return undefined;
  const headers: Record<string, string> = {};
  for (const part of raw.split(',')) {
    const idx = part.indexOf('=');
    if (idx <= 0) continue;
    const key = part.slice(0, idx).trim();
    const rawVal = part.slice(idx + 1).trim();
    if (!key || !rawVal) continue;
    let val = rawVal;
    try {
      val = decodeURIComponent(rawVal);
    } catch {
      val = rawVal;
    }
    headers[key] = val;
  }
  return Object.keys(headers).length > 0 ? headers : undefined;
}

export function resolveOtelTracesUrl(endpoint: string): string {
  const trimmed = endpoint.trim().replace(/\/+$/, '');
  return trimmed.endsWith('/v1/traces') ? trimmed : `${trimmed}/v1/traces`;
}
