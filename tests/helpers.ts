import type { Pool } from 'pg';
import { newDb } from 'pg-mem';
import { buildApp, type AppRuntime } from '../src/app.js';
import type { AppConfig } from '../src/config.js';
import { AppDatabase } from '../src/db/database.js';
import type { SiteRecord } from '../src/domain/types.js';

export const ADMIN_TOKEN = 'test-admin-token-1234567890';
export const APP_SECRET = 'test-app-secret-that-is-at-least-32-characters';

export function testConfig(overrides: Partial<AppConfig> = {}): AppConfig {
  return {
    nodeEnv: 'test',
    host: '127.0.0.1',
    port: 4180,
    databaseUrl: 'postgresql://unused:unused@127.0.0.1:5432/apihub_test',
    databasePoolMax: 5,
    databaseIdleTimeoutMs: 30_000,
    databaseConnectionTimeoutMs: 1_000,
    databaseStatementTimeoutMs: 2_000,
    adminToken: ADMIN_TOKEN,
    appSecret: APP_SECRET,
    httpTimeoutMs: 2_000,
    maxResponseBytes: 64 * 1024,
    allowPrivateSites: true,
    allowInsecureHttp: true,
    ...overrides,
  };
}

export async function createTestDatabase(): Promise<AppDatabase> {
  const memory = newDb({ autoCreateForeignKeyIndices: true, noAstCoverageCheck: true });
  const adapter = memory.adapters.createPg();
  const pool = new adapter.Pool() as Pool;
  return AppDatabase.fromPool(pool, { useMigrationLock: false });
}

export async function buildTestApp(overrides: Partial<AppConfig> = {}): Promise<AppRuntime> {
  const database = await createTestDatabase();
  return buildApp(testConfig(overrides), { startScheduler: false, database });
}

export function authHeaders(): Record<string, string> {
  return { authorization: `Bearer ${ADMIN_TOKEN}` };
}

export function siteInput(
  overrides: Partial<Omit<SiteRecord, 'id' | 'consecutiveFailures' | 'createdAt' | 'updatedAt'>> = {},
): Omit<SiteRecord, 'id' | 'consecutiveFailures' | 'createdAt' | 'updatedAt'> {
  return {
    name: 'Test Station',
    baseUrl: 'https://example.com',
    adapter: 'new-api',
    userId: '42',
    accessTokenCiphertext: 'encrypted-token',
    enabled: true,
    checkinEnabled: true,
    announcementEnabled: true,
    checkinCron: '15 8 * * *',
    announcementCron: '*/30 * * * *',
    timezone: 'Asia/Shanghai',
    ...overrides,
  };
}

export function siteBody(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    name: 'Test Station',
    baseUrl: 'https://example.com',
    userId: '42',
    accessToken: 'station-access-token',
    ...overrides,
  };
}
