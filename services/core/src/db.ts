import { Logger, LogLevel } from '@resq-systems/logger';
import pg from 'pg';

const { Pool } = pg;

const dbLog = Logger.getLogger('[core-db]', {
  minLevel: LogLevel.WARN,
  includeTimestamp: true,
  colorize: false,
});

export interface QueryResult<T extends Record<string, unknown> = Record<string, unknown>> {
  rows: T[];
  rowCount: number | null;
}

export interface Database {
  query<T extends Record<string, unknown> = Record<string, unknown>>(
    text: string,
    params?: unknown[]
  ): Promise<QueryResult<T>>;
  healthCheck(): Promise<boolean>;
  close(): Promise<void>;
}

export function createDatabase(connectionString: string): Database {
  const pool = new Pool({
    connectionString,
    max: 20,
    idleTimeoutMillis: 30_000,
    connectionTimeoutMillis: 5_000,
    statement_timeout: 10_000,
    query_timeout: 12_000,
    keepAlive: true,
    keepAliveInitialDelayMillis: 10_000,
    maxUses: 7_500,
  });

  // Prevent unhandled pool errors from crashing the process on transient Postgres restarts
  pool.on('error', (err) => {
    dbLog.error('Unexpected idle PostgreSQL client error', err);
  });

  return {
    async query<T extends Record<string, unknown> = Record<string, unknown>>(
      text: string,
      params?: unknown[]
    ): Promise<QueryResult<T>> {
      const result = await pool.query<T>(text, params);
      return {
        rows: result.rows,
        rowCount: result.rowCount,
      };
    },

    async healthCheck(): Promise<boolean> {
      await pool.query('SELECT 1');
      return true;
    },

    async close(): Promise<void> {
      await pool.end();
    },
  };
}
