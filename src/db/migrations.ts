import { createHash } from 'node:crypto';
import type { PoolClient } from 'pg';

type Migration = {
  version: number;
  sql: string;
};

const migrations: Migration[] = [
  {
    version: 1,
    sql: `
      CREATE TABLE sites (
        id UUID PRIMARY KEY,
        name VARCHAR(80) NOT NULL,
        base_url TEXT NOT NULL UNIQUE,
        adapter VARCHAR(32) NOT NULL CHECK (adapter IN ('new-api')),
        user_id VARCHAR(128) NOT NULL,
        access_token_ciphertext TEXT NOT NULL,
        enabled BOOLEAN NOT NULL DEFAULT TRUE,
        checkin_enabled BOOLEAN NOT NULL DEFAULT TRUE,
        announcement_enabled BOOLEAN NOT NULL DEFAULT TRUE,
        checkin_cron VARCHAR(100) NOT NULL,
        announcement_cron VARCHAR(100) NOT NULL,
        timezone VARCHAR(100) NOT NULL,
        consecutive_failures INTEGER NOT NULL DEFAULT 0 CHECK (consecutive_failures >= 0),
        created_at TIMESTAMPTZ NOT NULL,
        updated_at TIMESTAMPTZ NOT NULL
      );

      CREATE TABLE checkin_runs (
        id UUID PRIMARY KEY,
        site_id UUID NOT NULL REFERENCES sites(id) ON DELETE RESTRICT,
        local_date DATE NOT NULL,
        status VARCHAR(32) NOT NULL CHECK (status IN ('running', 'success', 'already_checked', 'manual_required', 'failed', 'skipped')),
        reward_value BIGINT,
        message TEXT NOT NULL DEFAULT '',
        error_code VARCHAR(64),
        attempt_count INTEGER NOT NULL DEFAULT 1 CHECK (attempt_count >= 1),
        started_at TIMESTAMPTZ NOT NULL,
        finished_at TIMESTAMPTZ,
        request_id TEXT NOT NULL,
        UNIQUE (site_id, local_date)
      );

      CREATE INDEX checkin_runs_started_idx ON checkin_runs(started_at DESC, id DESC);
      CREATE INDEX checkin_runs_site_started_idx ON checkin_runs(site_id, started_at DESC, id DESC);
      CREATE INDEX checkin_runs_local_date_status_idx ON checkin_runs(local_date, status);

      CREATE TABLE announcement_sync_runs (
        id UUID PRIMARY KEY,
        site_id UUID NOT NULL REFERENCES sites(id) ON DELETE RESTRICT,
        status VARCHAR(32) NOT NULL CHECK (status IN ('running', 'success', 'partial', 'failed')),
        added_count INTEGER NOT NULL DEFAULT 0 CHECK (added_count >= 0),
        message TEXT NOT NULL DEFAULT '',
        started_at TIMESTAMPTZ NOT NULL,
        finished_at TIMESTAMPTZ,
        request_id TEXT NOT NULL
      );

      CREATE INDEX announcement_sync_runs_started_idx ON announcement_sync_runs(started_at DESC, id DESC);

      CREATE TABLE announcements (
        id UUID PRIMARY KEY,
        site_id UUID NOT NULL REFERENCES sites(id) ON DELETE RESTRICT,
        source VARCHAR(16) NOT NULL CHECK (source IN ('status', 'notice')),
        fingerprint TEXT NOT NULL,
        content TEXT NOT NULL,
        kind VARCHAR(32) NOT NULL DEFAULT 'default',
        extra TEXT,
        published_at TIMESTAMPTZ,
        first_seen_at TIMESTAMPTZ NOT NULL,
        last_seen_at TIMESTAMPTZ NOT NULL,
        read_at TIMESTAMPTZ,
        UNIQUE (site_id, fingerprint)
      );

      CREATE INDEX announcements_feed_idx ON announcements((COALESCE(published_at, first_seen_at)) DESC, id DESC);
      CREATE INDEX announcements_site_feed_idx ON announcements(site_id, (COALESCE(published_at, first_seen_at)) DESC, id DESC);
      CREATE INDEX announcements_unread_idx ON announcements(first_seen_at DESC) WHERE read_at IS NULL;
    `,
  },
  {
    version: 2,
    sql: `
      ALTER TABLE sites DROP CONSTRAINT IF EXISTS sites_adapter_check;
      -- pg-mem names an unnamed CHECK differently; this is a no-op on PostgreSQL.
      ALTER TABLE sites DROP CONSTRAINT IF EXISTS sites_constraint_1;
      ALTER TABLE sites
        ADD CONSTRAINT sites_adapter_check
        CHECK (adapter IN ('new-api', 'sub2api', 'zen-api'));
    `,
  },
];

export async function runMigrations(client: PoolClient, useAdvisoryLock = true): Promise<void> {
  await client.query('BEGIN');
  try {
    if (useAdvisoryLock) {
      await client.query('SELECT pg_advisory_xact_lock($1)', [726_451_238]);
    }
    await client.query(`
      CREATE TABLE IF NOT EXISTS schema_migrations (
        version INTEGER PRIMARY KEY,
        checksum VARCHAR(64) NOT NULL,
        applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
      )
    `);
    const appliedResult = await client.query<{ version: number; checksum: string }>(
      'SELECT version, checksum FROM schema_migrations ORDER BY version',
    );
    const knownVersions = new Set(migrations.map((migration) => migration.version));
    const unsupportedMigration = appliedResult.rows.find((row) => !knownVersions.has(row.version));
    if (unsupportedMigration) {
      throw new Error(`Database schema version ${unsupportedMigration.version} is not supported by this application build`);
    }
    const applied = new Map(appliedResult.rows.map((row) => [row.version, row.checksum]));
    for (const migration of migrations) {
      const checksum = createHash('sha256').update(migration.sql, 'utf8').digest('hex');
      const appliedChecksum = applied.get(migration.version);
      if (appliedChecksum !== undefined) {
        if (appliedChecksum !== checksum) {
          throw new Error(`Database migration ${migration.version} checksum does not match this application build`);
        }
        continue;
      }
      await client.query(migration.sql);
      await client.query(
        'INSERT INTO schema_migrations (version, checksum) VALUES ($1, $2)',
        [migration.version, checksum],
      );
    }
    await client.query('COMMIT');
  } catch (error) {
    await client.query('ROLLBACK');
    throw error;
  }
}
