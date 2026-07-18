import { randomUUID } from 'node:crypto';
import { Pool, type PoolClient } from 'pg';
import { AppError } from '../lib/errors.js';
import { localDateForTimezone } from '../lib/time.js';
import type {
  Announcement,
  AnnouncementSource,
  AnnouncementSyncRun,
  CheckinRun,
  CheckinStatus,
  SiteAdapterName,
  SiteRecord,
} from '../domain/types.js';
import { runMigrations } from './migrations.js';

type TimestampValue = Date | string;

type SiteRow = {
  id: string;
  name: string;
  base_url: string;
  adapter: SiteAdapterName;
  user_id: string;
  access_token_ciphertext: string;
  enabled: boolean;
  checkin_enabled: boolean;
  announcement_enabled: boolean;
  checkin_cron: string;
  announcement_cron: string;
  timezone: string;
  consecutive_failures: number;
  created_at: TimestampValue;
  updated_at: TimestampValue;
};

type CheckinRow = {
  id: string;
  site_id: string;
  site_name?: string;
  local_date: Date | string;
  status: CheckinStatus;
  reward_value: string | number | null;
  message: string;
  error_code: string | null;
  attempt_count: number;
  started_at: TimestampValue;
  finished_at: TimestampValue | null;
  request_id: string;
};

type AnnouncementRow = {
  id: string;
  site_id: string;
  site_name?: string;
  source: AnnouncementSource;
  fingerprint: string;
  content: string;
  kind: string;
  extra: string | null;
  published_at: TimestampValue | null;
  first_seen_at: TimestampValue;
  last_seen_at: TimestampValue;
  read_at: TimestampValue | null;
};

type AnnouncementSyncRow = {
  id: string;
  site_id: string;
  status: AnnouncementSyncRun['status'];
  added_count: number;
  message: string;
  started_at: TimestampValue;
  finished_at: TimestampValue | null;
  request_id: string;
};

export type PostgresConnectionOptions = {
  connectionString: string;
  max: number;
  idleTimeoutMs: number;
  connectionTimeoutMs: number;
  statementTimeoutMs: number;
  onPoolError?: (error: Error) => void;
};

function toIsoString(value: TimestampValue): string {
  return value instanceof Date ? value.toISOString() : new Date(value).toISOString();
}

function toNullableIsoString(value: TimestampValue | null): string | null {
  return value === null ? null : toIsoString(value);
}

function toDateString(value: Date | string): string {
  return value instanceof Date ? value.toISOString().slice(0, 10) : value.slice(0, 10);
}

function toSafeNumber(value: string | number | null): number | null {
  if (value === null) return null;
  const result = Number(value);
  if (!Number.isSafeInteger(result)) throw new Error('Database returned an unsafe numeric value');
  return result;
}

function mapSite(row: SiteRow): SiteRecord {
  return {
    id: row.id,
    name: row.name,
    baseUrl: row.base_url,
    adapter: row.adapter,
    userId: row.user_id,
    accessTokenCiphertext: row.access_token_ciphertext,
    enabled: row.enabled,
    checkinEnabled: row.checkin_enabled,
    announcementEnabled: row.announcement_enabled,
    checkinCron: row.checkin_cron,
    announcementCron: row.announcement_cron,
    timezone: row.timezone,
    consecutiveFailures: row.consecutive_failures,
    createdAt: toIsoString(row.created_at),
    updatedAt: toIsoString(row.updated_at),
  };
}

function mapCheckin(row: CheckinRow): CheckinRun {
  return {
    id: row.id,
    siteId: row.site_id,
    ...(row.site_name ? { siteName: row.site_name } : {}),
    localDate: toDateString(row.local_date),
    status: row.status,
    rewardValue: toSafeNumber(row.reward_value),
    message: row.message,
    errorCode: row.error_code,
    attemptCount: row.attempt_count,
    startedAt: toIsoString(row.started_at),
    finishedAt: toNullableIsoString(row.finished_at),
    requestId: row.request_id,
  };
}

function mapAnnouncement(row: AnnouncementRow): Announcement {
  return {
    id: row.id,
    siteId: row.site_id,
    ...(row.site_name ? { siteName: row.site_name } : {}),
    source: row.source,
    fingerprint: row.fingerprint,
    content: row.content,
    kind: row.kind,
    extra: row.extra,
    publishedAt: toNullableIsoString(row.published_at),
    firstSeenAt: toIsoString(row.first_seen_at),
    lastSeenAt: toIsoString(row.last_seen_at),
    readAt: toNullableIsoString(row.read_at),
  };
}

function mapAnnouncementSync(row: AnnouncementSyncRow): AnnouncementSyncRun {
  return {
    id: row.id,
    siteId: row.site_id,
    status: row.status,
    addedCount: row.added_count,
    message: row.message,
    startedAt: toIsoString(row.started_at),
    finishedAt: toNullableIsoString(row.finished_at),
    requestId: row.request_id,
  };
}

function normalizePostgresError(error: unknown): never {
  const code = typeof error === 'object' && error !== null && 'code' in error
    ? (error as { code?: unknown }).code
    : undefined;
  if (code === '23505') {
    throw new AppError(409, 'CONFLICT', 'A resource with the same unique value already exists');
  }
  throw error;
}

export class AppDatabase {
  private constructor(private readonly pool: Pool) {}

  static async connect(options: PostgresConnectionOptions): Promise<AppDatabase> {
    const pool = new Pool({
      connectionString: options.connectionString,
      max: options.max,
      idleTimeoutMillis: options.idleTimeoutMs,
      connectionTimeoutMillis: options.connectionTimeoutMs,
      statement_timeout: options.statementTimeoutMs,
      query_timeout: options.statementTimeoutMs + 1_000,
      application_name: 'apihub',
    });
    pool.on('error', options.onPoolError ?? (() => undefined));
    return AppDatabase.fromPool(pool);
  }

  static async fromPool(pool: Pool, options: { useMigrationLock?: boolean } = {}): Promise<AppDatabase> {
    const database = new AppDatabase(pool);
    let client: PoolClient | undefined;
    try {
      client = await pool.connect();
      await runMigrations(client, options.useMigrationLock !== false);
      return database;
    } catch (error) {
      client?.release();
      client = undefined;
      await pool.end();
      throw error;
    } finally {
      client?.release();
    }
  }

  async close(): Promise<void> {
    await this.pool.end();
  }

  async assertReady(): Promise<void> {
    const client = await this.pool.connect();
    let transactionOpen = false;
    try {
      await client.query('BEGIN');
      transactionOpen = true;
      const result = await client.query<{ ok: number }>('SELECT 1 AS ok');
      if (result.rows[0]?.ok !== 1) throw new Error('Database readiness check failed');
      await client.query('UPDATE sites SET updated_at = updated_at WHERE FALSE');
      await client.query('ROLLBACK');
      transactionOpen = false;
    } finally {
      if (transactionOpen) await client.query('ROLLBACK').catch(() => undefined);
      client.release();
    }
  }

  async createSite(input: Omit<SiteRecord, 'id' | 'consecutiveFailures' | 'createdAt' | 'updatedAt'>): Promise<SiteRecord> {
    const now = new Date().toISOString();
    try {
      const result = await this.pool.query<SiteRow>(`
        INSERT INTO sites (
          id, name, base_url, adapter, user_id, access_token_ciphertext,
          enabled, checkin_enabled, announcement_enabled, checkin_cron,
          announcement_cron, timezone, created_at, updated_at
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $13)
        RETURNING *
      `, [
        randomUUID(), input.name, input.baseUrl, input.adapter, input.userId,
        input.accessTokenCiphertext, input.enabled, input.checkinEnabled,
        input.announcementEnabled, input.checkinCron, input.announcementCron,
        input.timezone, now,
      ]);
      return mapSite(result.rows[0]!);
    } catch (error) {
      normalizePostgresError(error);
    }
  }

  async listSites(): Promise<SiteRecord[]> {
    const result = await this.pool.query<SiteRow>('SELECT * FROM sites ORDER BY lower(name), name, id');
    return result.rows.map(mapSite);
  }

  async getSite(id: string): Promise<SiteRecord | null> {
    const result = await this.pool.query<SiteRow>('SELECT * FROM sites WHERE id = $1', [id]);
    return result.rows[0] ? mapSite(result.rows[0]) : null;
  }

  async getSiteOrThrow(id: string): Promise<SiteRecord> {
    const site = await this.getSite(id);
    if (!site) throw new AppError(404, 'NOT_FOUND', 'Site not found');
    return site;
  }

  async updateSite(
    id: string,
    changes: Partial<Omit<SiteRecord, 'id' | 'createdAt' | 'updatedAt' | 'consecutiveFailures'>>,
  ): Promise<SiteRecord> {
    const columns: Record<string, string> = {
      name: 'name',
      baseUrl: 'base_url',
      adapter: 'adapter',
      userId: 'user_id',
      accessTokenCiphertext: 'access_token_ciphertext',
      enabled: 'enabled',
      checkinEnabled: 'checkin_enabled',
      announcementEnabled: 'announcement_enabled',
      checkinCron: 'checkin_cron',
      announcementCron: 'announcement_cron',
      timezone: 'timezone',
    };
    const entries = Object.entries(changes).filter((entry) => entry[1] !== undefined);
    if (entries.length === 0) return this.getSiteOrThrow(id);
    const assignments = entries.map(([key], index) => `${columns[key]} = $${index + 1}`);
    const values = entries.map((entry) => entry[1]);
    values.push(new Date().toISOString(), id);
    try {
      const result = await this.pool.query<SiteRow>(`
        UPDATE sites
        SET ${assignments.join(', ')}, updated_at = $${entries.length + 1}
        WHERE id = $${entries.length + 2}
        RETURNING *
      `, values);
      if (result.rowCount !== 1) throw new AppError(404, 'NOT_FOUND', 'Site not found');
      return mapSite(result.rows[0]!);
    } catch (error) {
      if (error instanceof AppError) throw error;
      normalizePostgresError(error);
    }
  }

  async setSiteFailureCount(siteId: string, mode: 'reset' | 'increment'): Promise<void> {
    const result = await this.pool.query(`
      UPDATE sites
      SET consecutive_failures = CASE WHEN $1::text = 'reset' THEN 0 ELSE consecutive_failures + 1 END,
          updated_at = $2
      WHERE id = $3
    `, [mode, new Date().toISOString(), siteId]);
    if (result.rowCount !== 1) throw new AppError(404, 'NOT_FOUND', 'Site not found');
  }

  async findCheckinForDate(siteId: string, localDate: string): Promise<CheckinRun | null> {
    const result = await this.pool.query<CheckinRow>(
      'SELECT * FROM checkin_runs WHERE site_id = $1 AND local_date = $2',
      [siteId, localDate],
    );
    return result.rows[0] ? mapCheckin(result.rows[0]) : null;
  }

  async beginCheckin(
    siteId: string,
    localDate: string,
    requestId: string,
  ): Promise<{ run: CheckinRun; acquired: boolean }> {
    const now = new Date().toISOString();
    const retried = await this.pool.query<CheckinRow>(`
      UPDATE checkin_runs SET
        status = 'running',
        reward_value = NULL,
        message = '',
        error_code = NULL,
        attempt_count = checkin_runs.attempt_count + 1,
        started_at = $3,
        finished_at = NULL,
        request_id = $4
      WHERE site_id = $1 AND local_date = $2 AND status IN ('failed', 'skipped')
      RETURNING *
    `, [siteId, localDate, now, requestId]);
    if (retried.rows[0]) return { run: mapCheckin(retried.rows[0]), acquired: true };

    const inserted = await this.pool.query<CheckinRow>(`
      INSERT INTO checkin_runs (
        id, site_id, local_date, status, message, attempt_count, started_at, request_id
      ) VALUES ($1, $2, $3, 'running', '', 1, $4, $5)
      ON CONFLICT (site_id, local_date) DO NOTHING
      RETURNING *
    `, [randomUUID(), siteId, localDate, now, requestId]);
    if (inserted.rows[0]) {
      const run = mapCheckin(inserted.rows[0]);
      return {
        run,
        acquired: run.status === 'running' && run.requestId === requestId,
      };
    }

    const existing = await this.findCheckinForDate(siteId, localDate);
    if (!existing) throw new Error('Failed to create or read check-in run');
    return { run: existing, acquired: false };
  }

  async finishCheckin(id: string, result: {
    status: Exclude<CheckinStatus, 'running'>;
    rewardValue?: number | null;
    message: string;
    errorCode?: string | null;
  }): Promise<CheckinRun> {
    const update = await this.pool.query<CheckinRow>(`
      UPDATE checkin_runs
      SET status = $1, reward_value = $2, message = $3, error_code = $4, finished_at = $5
      WHERE id = $6 AND status = 'running'
      RETURNING *
    `, [
      result.status,
      result.rewardValue ?? null,
      result.message,
      result.errorCode ?? null,
      new Date().toISOString(),
      id,
    ]);
    if (update.rowCount !== 1) throw new AppError(409, 'CONFLICT', 'Check-in run is not active');
    return mapCheckin(update.rows[0]!);
  }

  async listCheckins(input: { siteId?: string; limit: number }): Promise<CheckinRun[]> {
    const result = input.siteId
      ? await this.pool.query<CheckinRow>(`
          SELECT r.*, s.name AS site_name FROM checkin_runs r JOIN sites s ON s.id = r.site_id
          WHERE r.site_id = $1 ORDER BY r.started_at DESC, r.id DESC LIMIT $2
        `, [input.siteId, input.limit])
      : await this.pool.query<CheckinRow>(`
          SELECT r.*, s.name AS site_name FROM checkin_runs r JOIN sites s ON s.id = r.site_id
          ORDER BY r.started_at DESC, r.id DESC LIMIT $1
        `, [input.limit]);
    return result.rows.map(mapCheckin);
  }

  async createAnnouncementSyncRun(siteId: string, requestId: string): Promise<AnnouncementSyncRun> {
    const result = await this.pool.query<AnnouncementSyncRow>(`
      INSERT INTO announcement_sync_runs
      (id, site_id, status, added_count, message, started_at, finished_at, request_id)
      VALUES ($1, $2, 'running', 0, '', $3, NULL, $4)
      RETURNING *
    `, [randomUUID(), siteId, new Date().toISOString(), requestId]);
    return mapAnnouncementSync(result.rows[0]!);
  }

  async finishAnnouncementSyncRun(id: string, result: {
    status: Exclude<AnnouncementSyncRun['status'], 'running'>;
    addedCount: number;
    message: string;
  }): Promise<AnnouncementSyncRun> {
    const update = await this.pool.query<AnnouncementSyncRow>(`
      UPDATE announcement_sync_runs
      SET status = $1, added_count = $2, message = $3, finished_at = $4
      WHERE id = $5 AND status = 'running'
      RETURNING *
    `, [result.status, result.addedCount, result.message, new Date().toISOString(), id]);
    if (update.rowCount !== 1) throw new AppError(409, 'CONFLICT', 'Announcement sync run is not active');
    return mapAnnouncementSync(update.rows[0]!);
  }

  async upsertAnnouncement(
    input: Omit<Announcement, 'id' | 'firstSeenAt' | 'lastSeenAt' | 'readAt' | 'siteName'>,
  ): Promise<boolean> {
    const now = new Date().toISOString();
    const updateExisting = async (): Promise<number | null> => {
      const updated = await this.pool.query(`
        UPDATE announcements SET
          last_seen_at = $1,
          source = CASE WHEN $2::text = 'status' THEN 'status' ELSE source END,
          content = $3,
          kind = CASE WHEN $2::text = 'status' OR source != 'status' THEN $4 ELSE kind END,
          extra = CASE WHEN $2::text = 'status' OR source != 'status' THEN $5 ELSE extra END,
          published_at = CASE WHEN $2::text = 'status' OR source != 'status' THEN $6 ELSE published_at END
        WHERE site_id = $7 AND fingerprint = $8
      `, [
        now, input.source, input.content, input.kind, input.extra, input.publishedAt,
        input.siteId, input.fingerprint,
      ]);
      return updated.rowCount;
    };

    if (await updateExisting() === 1) return false;

    const inserted = await this.pool.query<{ id: string }>(`
      INSERT INTO announcements (
        id, site_id, source, fingerprint, content, kind, extra, published_at, first_seen_at, last_seen_at
      ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)
      ON CONFLICT (site_id, fingerprint) DO NOTHING
      RETURNING id
    `, [
      randomUUID(), input.siteId, input.source, input.fingerprint, input.content,
      input.kind, input.extra, input.publishedAt, now,
    ]);
    if (inserted.rowCount === 1) return true;
    if (await updateExisting() !== 1) throw new Error('Announcement deduplication update failed');
    return false;
  }

  async listAnnouncements(input: { siteId?: string; limit: number }): Promise<Announcement[]> {
    const result = input.siteId
      ? await this.pool.query<AnnouncementRow>(`
          SELECT a.*, s.name AS site_name FROM announcements a JOIN sites s ON s.id = a.site_id
          WHERE a.site_id = $1 ORDER BY COALESCE(a.published_at, a.first_seen_at) DESC, a.id DESC LIMIT $2
        `, [input.siteId, input.limit])
      : await this.pool.query<AnnouncementRow>(`
          SELECT a.*, s.name AS site_name FROM announcements a JOIN sites s ON s.id = a.site_id
          ORDER BY COALESCE(a.published_at, a.first_seen_at) DESC, a.id DESC LIMIT $1
        `, [input.limit]);
    return result.rows.map(mapAnnouncement);
  }

  async setAnnouncementRead(id: string, read: boolean): Promise<Announcement> {
    const result = await this.pool.query<AnnouncementRow>(`
      UPDATE announcements SET read_at = $1 WHERE id = $2 RETURNING *
    `, [read ? new Date().toISOString() : null, id]);
    if (result.rowCount !== 1) throw new AppError(404, 'NOT_FOUND', 'Announcement not found');
    return mapAnnouncement(result.rows[0]!);
  }

  async getSummary(now = new Date()): Promise<{
    sites: { total: number; enabled: number };
    today: Record<string, number>;
    unreadAnnouncements: number;
  }> {
    const candidateDates = [-1, 0, 1].map((offset) => {
      const candidate = new Date(now);
      candidate.setUTCDate(candidate.getUTCDate() + offset);
      return candidate.toISOString().slice(0, 10);
    });
    const [sitesResult, todayResult, unreadResult] = await Promise.all([
      this.pool.query<{ total: number; enabled: number }>(`
        SELECT COUNT(*)::int AS total,
               SUM(CASE WHEN enabled THEN 1 ELSE 0 END)::int AS enabled
        FROM sites
      `),
      this.pool.query<{ status: string; local_date: Date | string; timezone: string }>(`
        SELECT r.status, r.local_date, s.timezone
        FROM checkin_runs r JOIN sites s ON s.id = r.site_id
        WHERE r.local_date IN ($1, $2, $3)
      `, candidateDates),
      this.pool.query<{ count: number }>('SELECT COUNT(*)::int AS count FROM announcements WHERE read_at IS NULL'),
    ]);
    const today = new Map<string, number>();
    for (const row of todayResult.rows) {
      if (toDateString(row.local_date) !== localDateForTimezone(now, row.timezone)) continue;
      today.set(row.status, (today.get(row.status) ?? 0) + 1);
    }
    return {
      sites: {
        total: Number(sitesResult.rows[0]?.total ?? 0),
        enabled: Number(sitesResult.rows[0]?.enabled ?? 0),
      },
      today: Object.fromEntries(today),
      unreadAnnouncements: Number(unreadResult.rows[0]?.count ?? 0),
    };
  }
}
