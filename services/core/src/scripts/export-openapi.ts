import { mkdirSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { buildApp } from '../app.js';
import { loadConfig } from '../config.js';
import type { Database } from '../db.js';

const noopDb: Database = {
  async query() {
    return { rows: [], rowCount: 0 };
  },
  async healthCheck() {
    return true;
  },
  async close() {},
};

const outPath = resolve(process.argv[2] || 'dist/openapi.json');
const config = loadConfig({ ...process.env, NODE_ENV: 'test' });
const app = buildApp({ config, db: noopDb });

const response = await app.handle(new Request('http://localhost/v1/core/openapi.json'));
if (!response.ok) {
  throw new Error(`Failed to generate OpenAPI spec: HTTP ${response.status}`);
}

const spec = await response.json();
mkdirSync(dirname(outPath), { recursive: true });
writeFileSync(outPath, `${JSON.stringify(spec, null, 2)}\n`, 'utf8');
console.log(`Exported OpenAPI spec to ${outPath}`);
