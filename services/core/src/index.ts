import { opentelemetry } from '@elysiajs/opentelemetry';
import { trace } from '@opentelemetry/api';
import { OTLPTraceExporter } from '@opentelemetry/exporter-trace-otlp-http';
import { Elysia } from 'elysia';
import { buildApp } from './app.js';
import { loadConfig, resolveOtelTracesUrl } from './config.js';
import { createDatabase } from './db.js';
import { ensureSchema } from './schema.js';

const config = loadConfig();
const db = createDatabase(config.databaseUrl);

try {
  await ensureSchema(db);
  console.log('Database schema verified/initialized');
} catch (err) {
  console.error('Failed to initialize database schema:', err);
  process.exit(1);
}

const coreApp = buildApp({ config, db });

const server = config.otelEndpoint
  ? new Elysia({ name: 'core-runtime' })
      .use(
        opentelemetry({
          serviceName: config.otelServiceName,
          traceExporter: new OTLPTraceExporter({
            url: resolveOtelTracesUrl(config.otelEndpoint),
            ...(config.otelHeaders ? { headers: config.otelHeaders } : {}),
          }),
        })
      )
      .use(coreApp)
  : coreApp;

const hostname = process.env.HOST || '::';
server.listen({
  port: config.port,
  hostname,
});

console.log(`Core service (Elysia) listening on http://[${hostname}]:${config.port}`);

let shuttingDown = false;
async function shutdown(signal: string) {
  if (shuttingDown) return;
  shuttingDown = true;
  console.log(`Received ${signal}, flushing telemetry and shutting down core service...`);
  try {
    const provider = trace.getTracerProvider() as {
      forceFlush?: () => Promise<void>;
      shutdown?: () => Promise<void>;
    };
    if (typeof provider.forceFlush === 'function') {
      await provider.forceFlush();
    }
    if (typeof provider.shutdown === 'function') {
      await provider.shutdown();
    }
    await server.stop(true);
    await db.close();
    process.exit(0);
  } catch (err) {
    console.error('Error during shutdown:', err);
    process.exit(1);
  }
}

process.on('SIGINT', () => void shutdown('SIGINT'));
process.on('SIGTERM', () => void shutdown('SIGTERM'));
