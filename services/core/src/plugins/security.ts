import { Elysia } from 'elysia';

const CSP_API = [
  "default-src 'self'",
  "object-src 'none'",
  "frame-ancestors 'none'",
  "base-uri 'self'",
  "form-action 'self'",
].join('; ');

// Allow Scalar UI assets on the OpenAPI docs route while keeping API responses locked down
const CSP_DOCS = [
  "default-src 'self'",
  "script-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net",
  "style-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net https://fonts.googleapis.com",
  "font-src 'self' data: https://fonts.gstatic.com https://cdn.jsdelivr.net",
  "img-src 'self' data: blob: https:",
  "connect-src 'self' https://api.mikeodnis.dev https://cdn.mikeodnis.dev https://cdn.jsdelivr.net",
  "worker-src 'self' blob:",
  "object-src 'none'",
  "frame-ancestors 'none'",
].join('; ');

/**
 * Helmet-style security headers plugin for Elysia.
 * Uses `onRequest` so security headers and `Cache-Control: no-store` are ALWAYS applied —
 * including when auth macros (401/403), rate limiter (429), validation (400), or onError (404/500)
 * short-circuit before `onAfterHandle` executes.
 */
export function createSecurityHeadersPlugin() {
  return new Elysia({ name: 'core.security-headers' }).onRequest(({ request, set }) => {
    const pathname = new URL(request.url).pathname;
    set.headers['x-content-type-options'] = 'nosniff';
    set.headers['x-frame-options'] = 'DENY';
    set.headers['referrer-policy'] = 'strict-origin-when-cross-origin';
    set.headers['permissions-policy'] = 'camera=(), microphone=(), geolocation=()';
    set.headers['strict-transport-security'] = 'max-age=31536000; includeSubDomains; preload';
    set.headers['content-security-policy'] = pathname.startsWith('/v1/core/docs') ? CSP_DOCS : CSP_API;
    if (
      pathname.startsWith('/v1/core/auth') ||
      pathname.startsWith('/v1/core/users') ||
      pathname === '/v1/core/status' ||
      pathname.startsWith('/v1/core/health')
    ) {
      set.headers['cache-control'] = 'no-store';
    }
  });
}
