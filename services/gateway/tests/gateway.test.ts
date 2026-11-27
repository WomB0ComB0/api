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
 * list and the site-wide security headers. `caddy validate` only proves the Caddyfile parses; this
 * catches a route that is shadowed, a header that never reaches the response, or a static file
 * that changed without anyone meaning to.
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

/** Requests uncompressed bodies so served bytes can be compared with the files in static/. */
function get(path: string, headers: Record<string, string> = {}): Promise<Response> {
  return fetch(`${BASE_URL}${path}`, {
    redirect: 'manual',
    headers: { 'Accept-Encoding': 'identity', ...headers },
  });
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

  // Today Caddy sends this file_server 404 without the site-wide header block: no nosniff, HSTS or
  // frame headers, and Server: Caddy comes back. A handle_errors block that repeats those headers
  // would cover it; until then this test pins only what matters for caching.
  test('a missing font is a 404 that is never cached as immutable', async () => {
    const response = await get('/_brand/fonts/geist-v0-latin.woff2');
    expect(response.status).toBe(404);
    expect(response.headers.get('content-type')).not.toBe('font/woff2');
    expect(response.headers.get('cache-control') ?? '').not.toMatch(/immutable|max-age=31536000/);
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
