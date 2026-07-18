import { z } from 'zod';

const booleanFromEnv = z.preprocess((value) => {
  if (typeof value === 'boolean') return value;
  if (typeof value !== 'string') return value;
  const normalized = value.trim().toLowerCase();
  if (['1', 'true', 'yes', 'on'].includes(normalized)) return true;
  if (['0', 'false', 'no', 'off'].includes(normalized)) return false;
  return value;
}, z.boolean());

const envSchema = z.object({
  NODE_ENV: z.enum(['development', 'test', 'production']).default('development'),
  HOST: z.string().trim().min(1).default('127.0.0.1'),
  PORT: z.coerce.number().int().min(1).max(65535).default(4180),
  DATABASE_URL: z.string().trim().min(1).refine((value) => {
    try {
      const protocol = new URL(value).protocol;
      return protocol === 'postgres:' || protocol === 'postgresql:';
    } catch {
      return false;
    }
  }, 'DATABASE_URL must be a PostgreSQL connection URL'),
  DATABASE_POOL_MAX: z.coerce.number().int().min(1).max(20).default(5),
  DATABASE_IDLE_TIMEOUT_MS: z.coerce.number().int().min(1_000).max(300_000).default(30_000),
  DATABASE_CONNECTION_TIMEOUT_MS: z.coerce.number().int().min(1_000).max(60_000).default(5_000),
  DATABASE_STATEMENT_TIMEOUT_MS: z.coerce.number().int().min(1_000).max(120_000).default(15_000),
  ADMIN_TOKEN: z.string().min(16),
  APP_SECRET: z.string().min(32),
  HTTP_TIMEOUT_MS: z.coerce.number().int().min(1_000).max(60_000).default(10_000),
  MAX_RESPONSE_BYTES: z.coerce.number().int().min(16_384).max(5 * 1024 * 1024).default(1024 * 1024),
  ALLOW_PRIVATE_SITES: booleanFromEnv.default(false),
  ALLOW_INSECURE_HTTP: booleanFromEnv.default(false),
});

export type AppConfig = {
  nodeEnv: 'development' | 'test' | 'production';
  host: string;
  port: number;
  databaseUrl: string;
  databasePoolMax: number;
  databaseIdleTimeoutMs: number;
  databaseConnectionTimeoutMs: number;
  databaseStatementTimeoutMs: number;
  adminToken: string;
  appSecret: string;
  httpTimeoutMs: number;
  maxResponseBytes: number;
  allowPrivateSites: boolean;
  allowInsecureHttp: boolean;
};

export function loadConfig(env: NodeJS.ProcessEnv = process.env): AppConfig {
  const parsed = envSchema.parse(env);
  return {
    nodeEnv: parsed.NODE_ENV,
    host: parsed.HOST,
    port: parsed.PORT,
    databaseUrl: parsed.DATABASE_URL,
    databasePoolMax: parsed.DATABASE_POOL_MAX,
    databaseIdleTimeoutMs: parsed.DATABASE_IDLE_TIMEOUT_MS,
    databaseConnectionTimeoutMs: parsed.DATABASE_CONNECTION_TIMEOUT_MS,
    databaseStatementTimeoutMs: parsed.DATABASE_STATEMENT_TIMEOUT_MS,
    adminToken: parsed.ADMIN_TOKEN,
    appSecret: parsed.APP_SECRET,
    httpTimeoutMs: parsed.HTTP_TIMEOUT_MS,
    maxResponseBytes: parsed.MAX_RESPONSE_BYTES,
    allowPrivateSites: parsed.ALLOW_PRIVATE_SITES,
    allowInsecureHttp: parsed.ALLOW_INSECURE_HTTP,
  };
}
