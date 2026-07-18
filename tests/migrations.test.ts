import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import type { Pool, PoolClient } from 'pg';
import { newDb } from 'pg-mem';
import { afterEach, describe, expect, it } from 'vitest';
import { runMigrations } from '../src/db/migrations.js';

type CompatibilityVectors = {
  migrations: Array<{ version: number; sha256: string; bytes: number }>;
};

const compatibilityVectors = JSON.parse(
  readFileSync(new URL('./fixtures/compatibility-vectors.json', import.meta.url), 'utf8'),
) as CompatibilityVectors;

function migrationSqlBytes(): Array<{ version: number; sha256: string; bytes: number }> {
  const source = readFileSync(new URL('../src/db/migrations.ts', import.meta.url), 'utf8');
  return [...source.matchAll(/version:\s*(\d+),\s*sql:\s*`([\s\S]*?)`/g)].map((match) => {
    const sql = match[2]!;
    return {
      version: Number(match[1]),
      sha256: createHash('sha256').update(sql, 'utf8').digest('hex'),
      bytes: Buffer.byteLength(sql, 'utf8'),
    };
  });
}

describe('PostgreSQL migrations', () => {
  let pool: Pool | undefined;
  let client: PoolClient | undefined;

  afterEach(async () => {
    client?.release();
    client = undefined;
    await pool?.end();
    pool = undefined;
  });

  async function connect(): Promise<PoolClient> {
    const memory = newDb({ autoCreateForeignKeyIndices: true, noAstCoverageCheck: true });
    const adapter = memory.adapters.createPg();
    pool = new adapter.Pool() as Pool;
    client = await pool.connect();
    return client;
  }

  it('is idempotent and records a migration checksum', async () => {
    const connection = await connect();

    await runMigrations(connection, false);
    await runMigrations(connection, false);

    const result = await connection.query<{ version: number; checksum: string }>(
      'SELECT version, checksum FROM schema_migrations',
    );
    expect(result.rows).toEqual(compatibilityVectors.migrations.map((migration) => ({
      version: migration.version,
      checksum: migration.sha256,
    })));
    expect(migrationSqlBytes()).toEqual(compatibilityVectors.migrations);
  });

  it('refuses to start against an unknown schema version', async () => {
    const connection = await connect();
    await runMigrations(connection, false);
    await connection.query(
      'INSERT INTO schema_migrations (version, checksum) VALUES ($1, $2)',
      [999, '0'.repeat(64)],
    );

    await expect(runMigrations(connection, false)).rejects.toThrow('schema version 999');
  });

  it('refuses to start when an applied migration checksum has drifted', async () => {
    const connection = await connect();
    await runMigrations(connection, false);
    await connection.query(
      'UPDATE schema_migrations SET checksum = $1 WHERE version = $2',
      ['f'.repeat(64), 1],
    );

    await expect(runMigrations(connection, false)).rejects.toThrow('checksum does not match');
  });
});
