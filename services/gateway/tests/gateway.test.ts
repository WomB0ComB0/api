/*
 * Copyright 2026 Mike Odnis
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

/**
 * @file Checks what the running gateway container actually serves: the root page and its JSON
 * twin, the shared /_brand stylesheet and fonts, the static discovery routes, the Hasura block
 * list, the site-wide security headers and the error responses (file_server 404/405 and the 502
 * for an upstream that is down). `caddy validate` only proves the Caddyfile parses; this catches a
 * route that is shadowed, a header that never reaches the response, or a static file that changed
 * without anyone meaning to.
 *
 * The expected values below are copied from the Caddyfile on purpose. Changing a header there
 * should fail this test until it is changed here too, so header changes are always deliberate.
 *
 * Run it locally with `CONTAINER_ENGINE=podman services/gateway/tests/run.sh` (or plain
 * `services/gateway/tests/run.sh` with Docker), which builds the image, starts it and points this
 * file at it. Against a gateway you already started:
 * `GATEWAY_URL=http://127.0.0.1:8091 bun test ./services/gateway/tests/gateway.test.ts`.
 */

import { describe, expect, test } from 'bun:test';
import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

const BASE_URL = (process.env['GATEWAY_URL'] ?? '').replace(/\/+$/, '');
if (!BASE_URL) {
  throw new Error('GATEWAY_URL is not set. Start the gateway with services/gateway/tests/run.sh.');
}

const STATIC_DIR = join(import.meta.dir, '..', 'static');

// cdn.mikeodnis.dev serves a byte-identical copy of this stylesheet and its repo pins the same
// digest. Change both copies and both pins together.
const SUBDOMAIN_CSS_SHA256 = 'bfbe620cd8e9879fcb20468b24b68e41248e99eb6f1c39609437e60b89ba44d2';

const FONTS = ['geist-v1.9-latin.woff2', 'geist-mono-v1.9-latin.woff2'];
const FONT_CACHE = 'public, max-age=31536000, immutable';
const PAGE_CACHE = 'public, max-age=300';
const BROWSER_ACCEPT = 'text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8';

const SITE_HEADERS: Record<string, string> = {
  'x-content-type-options': 'nosniff',
  'x-frame-options': 'DENY',
  'referrer-policy': 'strict-origin-when-cross-origin',
  'permissions-policy': 'camera=(), microphone=(), geolocation=()',
  'strict-transport-security': 'max-age=63072000; includeSubDomains; preload',
};

const DISCOVERY_LINK =
  '<https://api.mikeodnis.dev/llms.txt>; rel="describedby"; type="text/markdown", ' +
  '<https://api.mikeodnis.dev/v1/core/openapi.json>; rel="service-desc"; type="application/openapi+json"';
const ROOT_HTML_LINK = `${DISCOVERY_LINK}, <https://api.mikeodnis.dev/.well-known/security.txt>; rel="security"`;

const ROOT_CSP: Record<string, string[]> = {
  'default-src': ["'self'"],
  'script-src': ["'self'", "'unsafe-inline'"],
  'style-src': ["'self'", "'unsafe-inline'"],
  'img-src': ["'self'", 'data:'],
  'connect-src': ["'self'"],
  'object-src': ["'none'"],
  'base-uri': ["'self'"],
  'form-action': ["'self'"],
  'frame-ancestors': ["'none'"],
};

const ROOT_DESCRIPTOR = {
  service: 'api.mikeodnis.dev',
  routes: ['/health', '/v1/core', '/v1/media', '/v1/links/graphql'],
  docs: ['https://api.mikeodnis.dev/docs', 'https://api.mikeodnis.dev/v1/core/docs'],
  openapi: [
    'https://api.mikeodnis.dev/v1/core/openapi.json',
    'https://api.mikeodnis.dev/v1/media/openapi.json',
  ],
  llms: 'https://api.mikeodnis.dev/llms.txt',
  security: 'https://api.mikeodnis.dev/.well-known/security.txt',
  upload_studio: 'https://cdn.mikeodnis.dev/upload',
};

// What handle_errors answers with, whichever route raised the error: an empty body, never cached,
// and a CSP that allows nothing, since an error carries no content of its own.
const ERROR_CACHE = 'no-store';
const ERROR_CSP: Record<string, string[]> = {
  'default-src': ["'none'"],
  'frame-ancestors': ["'none'"],
};

// The block list answer is a respond, not an error, so handle_errors must leave it exactly as is.
const LINKS_BLOCKED_BODY = '{"error":"Not found"}';
const LINKS_BLOCKED_HEADERS = [
  'cache-control',
  'content-length',
  'content-type',
  'cross-origin-resource-policy',
  'permissions-policy',
  'referrer-policy',
  'strict-transport-security',
  'x-content-type-options',
  'x-frame-options',
];

// proxy_defaults retries a refused dial every lb_try_interval (250ms) for lb_try_duration (3s)
// before Caddy gives up with a 502, so each proxied request below takes about 3s. They run in
// parallel; the fetch timeout stops a hung request from stalling the suite and the test timeout
// leaves room for a slow CI runner.
const UPSTREAM_FETCH_TIMEOUT_MS = 10_000;
const UPSTREAM_TEST_TIMEOUT_MS = 15_000;
const UPSTREAM_DETAIL = /127\.0\.0\.1|dial tcp|connection refused|connect:/i;

/** Requests uncompressed bodies so served bytes can be compared with the files in static/. */
function send(
  method: string,
  path: string,
  headers: Record<string, string> = {},
  signal: AbortSignal | null = null,
): Promise<Response> {
  return fetch(`${BASE_URL}${path}`, {
    method,
    redirect: 'manual',
    headers: { 'Accept-Encoding': 'identity', ...headers },
    signal,
  });
}

function get(path: string, headers: Record<string, string> = {}): Promise<Response> {
  return send('GET', path, headers);
}

function sha256(bytes: Uint8Array): string {
  return createHash('sha256').update(bytes).digest('hex');
}

function staticFile(name: string): Buffer {
  return readFileSync(join(STATIC_DIR, name));
}

async function body(response: Response): Promise<Uint8Array<ArrayBuffer>> {
  return new Uint8Array(await response.arrayBuffer());
}

/** The first capture group of every match of `pattern` (which must be global) in `text`. */
function captures(text: string, pattern: RegExp): string[] {
  return [...text.matchAll(pattern)].map((match) => match[1] ?? '');
}

function parseCsp(header: string): Record<string, string[]> {
  const directives: Record<string, string[]> = {};
  for (const part of header.split(';')) {
    const [name, ...sources] = part.trim().split(/\s+/);
    if (name) directives[name.toLowerCase()] = sources;
  }
  return directives;
}

/** A fetch directive that is absent falls back to default-src (CSP Level 3, section 6.8.3). */
function effectiveSources(csp: Record<string, string[]>, directive: string): string[] {
  return csp[directive] ?? csp['default-src'] ?? [];
}

function isSameOriginPath(href: string): boolean {
  return href.startsWith('/') && !href.startsWith('//');
}

/** Lower-cased Vary tokens. fetch joins repeated Vary lines with ", ", so they are all here. */
function varyTokens(response: Response): string[] {
  return (response.headers.get('vary') ?? '')
    .split(',')
    .map((token) => token.trim().toLowerCase())
    .filter(Boolean);
}

/** Every response header name except Date, lower-cased and sorted. */
function headerNames(response: Response): string[] {
  return [...response.headers.keys()].filter((name) => name !== 'date').sort();
}

/**
 * The handle_errors contract: the status the route raised, every site-wide header, no Server
 * header, never cached, nothing allowed by the CSP and no Content-Type for the empty body. Each
 * value is labelled so a failure names the request it came from.
 */
function expectBareError(
  label: string,
  response: Response,
  status: number,
  corp: 'same-site' | 'cross-origin',
): void {
  expect([label, 'status', response.status]).toEqual([label, 'status', status]);
  for (const [name, value] of Object.entries(SITE_HEADERS)) {
    expect([label, name, response.headers.get(name)]).toEqual([label, name, value]);
  }
  expect([label, 'server', response.headers.get('server')]).toEqual([label, 'server', null]);
  expect([label, 'cache-control', response.headers.get('cache-control')]).toEqual([
    label,
    'cache-control',
    ERROR_CACHE,
  ]);
  expect([label, 'content-type', response.headers.get('content-type')]).toEqual([
    label,
    'content-type',
    null,
  ]);
  expect([label, 'csp', parseCsp(response.headers.get('content-security-policy') ?? '')]).toEqual(
    [label, 'csp', ERROR_CSP],
  );
  expect([label, 'corp', response.headers.get('cross-origin-resource-policy')]).toEqual([
    label,
    'corp',
    corp,
  ]);
}

describe('root page', () => {
  test('a browser gets static/index.html with the root headers', async () => {
    const response = await get('/', { Accept: BROWSER_ACCEPT });
    const html = await body(response);
    expect(response.status).toBe(200);
    expect(response.headers.get('content-type')).toBe('text/html; charset=utf-8');
    expect(response.headers.get('cache-control')).toBe(PAGE_CACHE);
    expect(response.headers.get('link')).toBe(ROOT_HTML_LINK);
    expect(sha256(html)).toBe(sha256(staticFile('index.html')));

    const text = new TextDecoder().decode(html);
    expect(text).toContain('<title>API - Mike Odnis</title>');
    expect(text).toContain('<link rel="stylesheet" href="/_brand/subdomain.css" />');
    expect(text).toContain(
      '<link rel="preload" href="/_brand/fonts/geist-v1.9-latin.woff2" as="font" type="font/woff2" crossorigin />',
    );
  });

  test('the root CSP is the one the Caddyfile sets', async () => {
    const response = await get('/', { Accept: BROWSER_ACCEPT });
    expect(parseCsp(response.headers.get('content-security-policy') ?? '')).toEqual(ROOT_CSP);
  });

  test('the root CSP allows the same-origin stylesheet and fonts the page loads', async () => {
    const response = await get('/', { Accept: BROWSER_ACCEPT });
    const csp = parseCsp(response.headers.get('content-security-policy') ?? '');
    const html = await response.text();
    const css = await (await get('/_brand/subdomain.css')).text();

    const stylesheets = captures(html, /<link rel="stylesheet" href="([^"]+)"/g);
    const fontUrls = [
      ...captures(html, /<link rel="preload" href="([^"]+)" as="font"/g),
      ...captures(css, /url\("([^"]+\.woff2)"\)/g),
    ];
    expect(stylesheets).toEqual(['/_brand/subdomain.css']);
    expect(fontUrls.length).toBeGreaterThan(0);
    expect([...stylesheets, ...fontUrls].filter((href) => !isSameOriginPath(href))).toEqual([]);

    // There is no font-src directive, so fonts are governed by default-src 'self'.
    expect(effectiveSources(csp, 'font-src')).toContain("'self'");
    expect(effectiveSources(csp, 'style-src')).toContain("'self'");
  });

  for (const accept of ['application/json', '*/*']) {
    test(`Accept: ${accept} gets the JSON descriptor`, async () => {
      const response = await get('/', { Accept: accept });
      expect(response.status).toBe(200);
      expect(response.headers.get('content-type')).toBe('application/json');
      expect(response.headers.get('link')).toBe(DISCOVERY_LINK);
      expect(response.headers.get('content-security-policy')).toBeNull();
      expect(await response.json()).toEqual(ROOT_DESCRIPTOR);
    });
  }

  // / answers HTML or JSON depending on Accept, so both answers must say Vary: Accept, or a shared
  // cache could hand the HTML page to an API client or the JSON to a browser. encode adds its own
  // Vary: Accept-Encoding when it compresses, and the two must not overwrite each other.
  test('the HTML answer varies by Accept', async () => {
    const response = await get('/', { Accept: BROWSER_ACCEPT });
    expect(varyTokens(response)).toContain('accept');
  });

  for (const encoding of ['gzip', 'zstd']) {
    test(`the HTML answer compressed with ${encoding} varies by Accept and Accept-Encoding`, async () => {
      const response = await fetch(`${BASE_URL}/`, {
        headers: { Accept: BROWSER_ACCEPT, 'Accept-Encoding': encoding },
        decompress: false,
      });
      const compressed = await body(response);
      const decoded =
        encoding === 'gzip' ? Bun.gunzipSync(compressed) : Bun.zstdDecompressSync(compressed);
      const vary = varyTokens(response);
      expect(response.headers.get('content-encoding')).toBe(encoding);
      expect(vary).toContain('accept');
      expect(vary).toContain('accept-encoding');
      expect(new Set(vary).size).toBe(vary.length);
      expect(sha256(decoded)).toBe(sha256(staticFile('index.html')));
    });
  }

  for (const accept of ['application/json', '*/*']) {
    test(`the JSON answer to Accept: ${accept} varies by Accept, compressed or not`, async () => {
      for (const encoding of ['identity', 'gzip', 'zstd']) {
        const response = await fetch(`${BASE_URL}/`, {
          headers: { Accept: accept, 'Accept-Encoding': encoding },
          decompress: false,
        });
        await response.arrayBuffer();
        const vary = varyTokens(response);
        expect([encoding, response.headers.get('content-type')]).toEqual([
          encoding,
          'application/json',
        ]);
        expect([encoding, vary.includes('accept')]).toEqual([encoding, true]);
        expect([encoding, new Set(vary).size]).toEqual([encoding, vary.length]);
        // The descriptor is under encode's 512-byte minimum, so today it is never compressed. If it
        // grows past that, encode must still be able to add its own token next to Accept.
        if (response.headers.get('content-encoding')) {
          expect([encoding, vary.includes('accept-encoding')]).toEqual([encoding, true]);
        }
      }
    });
  }

  // Every other path gets the same JSON whatever the client accepts, so it stays one cache entry.
  test('other unmatched paths answer JSON without Vary: Accept', async () => {
    for (const accept of [BROWSER_ACCEPT, 'application/json']) {
      const response = await get('/no-such-route', { Accept: accept });
      expect([accept, response.headers.get('content-type')]).toEqual([accept, 'application/json']);
      expect([accept, varyTokens(response).includes('accept')]).toEqual([accept, false]);
    }
  });

  for (const accept of [BROWSER_ACCEPT, 'application/json']) {
    test(`HEAD / (Accept: ${accept.split(',')[0]}) sends the GET headers without a body`, async () => {
      const [head, getResponse] = await Promise.all([
        send('HEAD', '/', { Accept: accept }),
        get('/', { Accept: accept }),
      ]);
      await getResponse.arrayBuffer();
      expect(head.status).toBe(200);
      for (const name of [
        'content-type',
        'content-length',
        'cache-control',
        'vary',
        'link',
        'content-security-policy',
      ]) {
        expect([name, head.headers.get(name)]).toEqual([name, getResponse.headers.get(name)]);
      }
      expect(varyTokens(head)).toContain('accept');
      expect((await body(head)).length).toBe(0);
    });
  }
});

describe('/_brand assets', () => {
  test('subdomain.css is the pinned stylesheet, served as CSS with the page TTL', async () => {
    const response = await get('/_brand/subdomain.css');
    expect(response.status).toBe(200);
    expect(response.headers.get('content-type')).toBe('text/css; charset=utf-8');
    expect(response.headers.get('cache-control')).toBe(PAGE_CACHE);
    expect(sha256(await body(response))).toBe(SUBDOMAIN_CSS_SHA256);
  });

  for (const font of FONTS) {
    test(`${font} is an immutable woff2`, async () => {
      const response = await get(`/_brand/fonts/${font}`);
      const bytes = await body(response);
      expect(response.status).toBe(200);
      expect(response.headers.get('content-type')).toBe('font/woff2');
      expect(response.headers.get('cache-control')).toBe(FONT_CACHE);
      expect(response.headers.get('x-content-type-options')).toBe('nosniff');
      expect(Buffer.from(bytes.subarray(0, 4)).toString('latin1')).toBe('wOF2');
      expect(sha256(bytes)).toBe(sha256(staticFile(`_brand/fonts/${font}`)));
    });
  }

  test('the stylesheet references exactly the fonts the gateway serves', async () => {
    const css = await (await get('/_brand/subdomain.css')).text();
    const referenced = captures(css, /url\("\/_brand\/fonts\/([^"]+\.woff2)"\)/g);
    expect(referenced.sort()).toEqual([...FONTS].sort());
  });

  // A missing name never gets the font headers (they hang off the @brandfonts_found matcher), and
  // its file_server 404 goes through handle_errors like every other error, so it is no-store. The
  // site-wide headers on that 404 are checked under 'error responses' below.
  test('a missing font is a 404 that is never cached', async () => {
    const response = await get('/_brand/fonts/geist-v0-latin.woff2');
    expect(response.status).toBe(404);
    expect(response.headers.get('content-type')).not.toBe('font/woff2');
    expect(response.headers.get('cache-control')).toBe(ERROR_CACHE);
  });

  test('OFL.txt is plain text with a one-day cache', async () => {
    const response = await get('/_brand/fonts/OFL.txt');
    expect(response.status).toBe(200);
    expect(response.headers.get('content-type')).toBe('text/plain; charset=utf-8');
    expect(response.headers.get('cache-control')).toBe('public, max-age=86400');
    expect(sha256(await body(response))).toBe(sha256(staticFile('_brand/fonts/OFL.txt')));
  });

  for (const encoding of ['gzip', 'zstd']) {
    test(`subdomain.css is compressed with ${encoding} when the client asks`, async () => {
      const response = await fetch(`${BASE_URL}/_brand/subdomain.css`, {
        headers: { 'Accept-Encoding': encoding },
        decompress: false,
      });
      const compressed = await body(response);
      const decoded =
        encoding === 'gzip' ? Bun.gunzipSync(compressed) : Bun.zstdDecompressSync(compressed);
      expect(response.headers.get('content-encoding')).toBe(encoding);
      expect(response.headers.get('vary')).toContain('Accept-Encoding');
      expect(sha256(decoded)).toBe(SUBDOMAIN_CSS_SHA256);
    });
  }
});

interface StaticRoute {
  paths: string[];
  file: string;
  contentType?: string;
  cacheControl: string;
  corp: 'same-site' | 'cross-origin';
  link?: string;
}

const STATIC_ROUTES: StaticRoute[] = [
  {
    paths: ['/docs', '/docs/'],
    file: 'docs.html',
    contentType: 'text/html; charset=utf-8',
    cacheControl: PAGE_CACHE,
    corp: 'same-site',
  },
  {
    paths: ['/llms.txt'],
    file: 'llms.txt',
    contentType: 'text/markdown; charset=utf-8',
    cacheControl: 'public, max-age=3600',
    corp: 'cross-origin',
    link: DISCOVERY_LINK,
  },
  {
    paths: ['/llms-full.txt'],
    file: 'llms-full.txt',
    contentType: 'text/markdown; charset=utf-8',
    cacheControl: 'public, max-age=3600',
    corp: 'cross-origin',
    link: DISCOVERY_LINK,
  },
  {
    paths: ['/.well-known/security.txt'],
    file: 'security.txt',
    contentType: 'text/plain; charset=utf-8',
    cacheControl: 'public, max-age=86400',
    corp: 'cross-origin',
  },
  {
    paths: ['/robots.txt'],
    file: 'robots.txt',
    contentType: 'text/plain; charset=utf-8',
    cacheControl: 'public, max-age=3600',
    corp: 'cross-origin',
  },
  {
    paths: ['/sitemap.xml'],
    file: 'sitemap.xml',
    contentType: 'application/xml; charset=utf-8',
    cacheControl: 'public, max-age=3600',
    corp: 'cross-origin',
  },
  {
    paths: ['/opengraph-image', '/opengraph-image.png', '/twitter-image', '/twitter-image.png'],
    file: 'opengraph-image.png',
    contentType: 'image/png',
    cacheControl: 'public, max-age=86400',
    corp: 'cross-origin',
  },
  // The icon block sets no Content-Type, so Caddy picks one from the extension; not pinned here.
  ...['favicon.ico', 'icon.svg', 'apple-touch-icon.png'].map(
    (file): StaticRoute => ({
      paths: [`/${file}`],
      file,
      cacheControl: 'public, max-age=86400',
      corp: 'cross-origin',
    }),
  ),
];

describe('static routes', () => {
  for (const route of STATIC_ROUTES) {
    for (const path of route.paths) {
      test(`${path} serves static/${route.file}`, async () => {
        const response = await get(path);
        expect(response.status).toBe(200);
        if (route.contentType) expect(response.headers.get('content-type')).toBe(route.contentType);
        expect(response.headers.get('cache-control')).toBe(route.cacheControl);
        expect(response.headers.get('cross-origin-resource-policy')).toBe(route.corp);
        if (route.link) expect(response.headers.get('link')).toBe(route.link);
        expect(sha256(await body(response))).toBe(sha256(staticFile(route.file)));
      });
    }
  }

  test('/docs keeps its own CSP, with framing and plugins blocked', async () => {
    const csp = parseCsp((await get('/docs')).headers.get('content-security-policy') ?? '');
    expect(csp['frame-ancestors']).toEqual(["'none'"]);
    expect(csp['object-src']).toEqual(["'none'"]);
    expect(effectiveSources(csp, 'font-src')).toContain("'self'");
  });
});

describe('gateway-owned responses', () => {
  test('/gateway/health answers without any upstream', async () => {
    const response = await get('/gateway/health');
    expect(response.status).toBe(200);
    expect(response.headers.get('content-type')).toBe('application/json');
    expect(await response.json()).toEqual({ status: 'healthy', service: 'gateway' });
  });

  for (const path of [
    '/v1/links',
    '/v1/links/v1/metadata',
    '/v1/links/v1/query',
    '/v1/links/v2/query',
    '/v1/links/v1/version',
    '/v1/links/console',
  ]) {
    test(`${path} is blocked at the gateway`, async () => {
      const response = await get(path);
      expect(response.status).toBe(404);
      expect(response.headers.get('content-type')).toBe('application/json');
      expect(response.headers.get('cache-control')).toBe('no-store');
      expect(await response.json()).toEqual({ error: 'Not found' });
    });
  }
});

describe('site-wide headers', () => {
  const paths: [string, Record<string, string>?][] = [
    ['/', { Accept: BROWSER_ACCEPT }],
    ['/', { Accept: 'application/json' }],
    ['/gateway/health'],
    ['/_brand/subdomain.css'],
    [`/_brand/fonts/${FONTS[0]}`],
    ['/_brand/fonts/OFL.txt'],
    ['/docs'],
    ['/llms.txt'],
    ['/v1/links/v1/metadata'],
    ['/no-such-route'],
  ];

  for (const [path, headers] of paths) {
    const accept = headers?.['Accept']?.split(',')[0];
    test(`${path}${accept ? ` (Accept: ${accept})` : ''} carries the security headers`, async () => {
      const response = await get(path, headers);
      for (const [name, value] of Object.entries(SITE_HEADERS)) {
        expect([name, response.headers.get(name)]).toEqual([name, value]);
      }
      expect(response.headers.get('server')).toBeNull();
    });
  }

  test('CORP defaults to same-site where a route does not widen it', async () => {
    for (const path of [
      '/',
      '/_brand/subdomain.css',
      `/_brand/fonts/${FONTS[0]}`,
      '/gateway/health',
    ]) {
      const response = await get(path, { Accept: BROWSER_ACCEPT });
      expect([path, response.headers.get('cross-origin-resource-policy')]).toEqual([
        path,
        'same-site',
      ]);
    }
  });
});

interface ErrorCase {
  method: string;
  path: string;
  status: number;
  headers?: Record<string, string>;
  /** Routes that widen CORP keep it on an error; everything else gets the same-site default. */
  corp: 'same-site' | 'cross-origin';
}

// Errors are written by Caddy's error path, not by the route, so without handle_errors they lose
// the deferred site-wide header block (and get Server: Caddy back).
const FILE_SERVER_ERRORS: ErrorCase[] = [
  // The only file_server route whose matcher accepts names that may not exist.
  { method: 'GET', path: '/_brand/fonts/does-not-exist.woff2', status: 404, corp: 'same-site' },
  // Every other file_server route matches fixed names of files that exist, so a client reaches
  // their error path with a method file_server refuses. These routes set a long public
  // Cache-Control, a Content-Type and sometimes a CSP before file_server runs, and the error
  // must not keep any of them.
  { method: 'POST', path: '/robots.txt', status: 405, corp: 'cross-origin' },
  { method: 'POST', path: '/opengraph-image', status: 405, corp: 'cross-origin' },
  { method: 'DELETE', path: '/docs', status: 405, corp: 'same-site' },
  {
    method: 'POST',
    path: '/',
    status: 405,
    headers: { Accept: BROWSER_ACCEPT },
    corp: 'same-site',
  },
];

// Every upstream in the test container is a closed port, so each of these is a refused dial.
const PROXIED_PATHS = [
  '/v1/core/health',
  '/v1/media/openapi.json',
  '/health',
  '/v1/links/healthz',
  '/v1/links/graphql',
];

describe('error responses', () => {
  for (const { method, path, status, headers, corp } of FILE_SERVER_ERRORS) {
    test(`${method} ${path} is a bare ${status} with the site-wide headers`, async () => {
      const label = `${method} ${path}`;
      const response = await send(method, path, headers);
      expectBareError(label, response, status, corp);
      if (status === 405) expect(response.headers.get('allow')).toBe('GET, HEAD');
      expect(await response.text()).toBe('');
    });
  }

  test(
    'a refused upstream is a bare 502 with the site-wide headers and no upstream detail',
    async () => {
      const results = await Promise.all(
        PROXIED_PATHS.map(async (path) => {
          const response = await send(
            'GET',
            path,
            { Accept: 'application/json' },
            AbortSignal.timeout(UPSTREAM_FETCH_TIMEOUT_MS),
          );
          return { path, response, text: await response.text() };
        }),
      );
      for (const { path, response, text } of results) {
        expectBareError(path, response, 502, 'same-site');
        expect([path, text]).toEqual([path, '']);
        const leaked = [...response.headers.entries()].filter(([, value]) =>
          UPSTREAM_DETAIL.test(value),
        );
        expect([path, leaked]).toEqual([path, []]);
      }
    },
    UPSTREAM_TEST_TIMEOUT_MS,
  );

  test('the /v1/links block list 404 is a respond, untouched by handle_errors', async () => {
    const response = await get('/v1/links/v1/metadata');
    expect(response.status).toBe(404);
    expect(headerNames(response)).toEqual(LINKS_BLOCKED_HEADERS);
    expect(response.headers.get('content-type')).toBe('application/json');
    expect(response.headers.get('cache-control')).toBe('no-store');
    expect(response.headers.get('content-length')).toBe(String(LINKS_BLOCKED_BODY.length));
    expect(await response.text()).toBe(LINKS_BLOCKED_BODY);
  });
});
