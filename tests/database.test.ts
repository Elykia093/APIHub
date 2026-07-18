import { afterEach, describe, expect, it } from 'vitest';
import { AppDatabase } from '../src/db/database.js';
import { localDateForTimezone } from '../src/lib/time.js';
import { createTestDatabase, siteInput } from './helpers.js';

describe('AppDatabase', () => {
  let db: AppDatabase | undefined;

  afterEach(async () => db?.close());

  it('runs migrations and enforces unique station URLs', async () => {
    db = await createTestDatabase();
    await db.assertReady();
    await db.createSite(siteInput());

    await expect(db.createSite(siteInput({ name: 'Duplicate' }))).rejects.toMatchObject({
      statusCode: 409,
      code: 'CONFLICT',
    });
    expect(await db.listSites()).toHaveLength(1);
  });

  it('accepts the expanded adapter values after migration v2', async () => {
    db = await createTestDatabase();
    const sub2 = await db.createSite(siteInput({
      name: 'Sub2API',
      baseUrl: 'https://sub2.example.com',
      adapter: 'sub2api',
      userId: '',
      checkinEnabled: false,
    }));
    const zen = await db.createSite(siteInput({
      name: 'ZenAPI',
      baseUrl: 'https://zen.example.com',
      adapter: 'zen-api',
      userId: '',
      announcementEnabled: false,
    }));

    expect([sub2.adapter, zen.adapter]).toEqual(['sub2api', 'zen-api']);
  });

  it('preserves explicit false values in partial updates', async () => {
    db = await createTestDatabase();
    const site = await db.createSite(siteInput());

    const updated = await db.updateSite(site.id, {
      enabled: false,
      checkinEnabled: false,
      announcementEnabled: false,
    });

    expect(updated.enabled).toBe(false);
    expect(updated.checkinEnabled).toBe(false);
    expect(updated.announcementEnabled).toBe(false);
  });

  it('reuses the daily row and increments attempt count when a failed check-in is retried', async () => {
    db = await createTestDatabase();
    const site = await db.createSite(siteInput());
    const first = await db.beginCheckin(site.id, '2026-07-16', 'request-1');
    expect(first.acquired).toBe(true);
    await db.finishCheckin(first.run.id, {
      status: 'failed',
      message: 'temporary failure',
      errorCode: 'UPSTREAM_TIMEOUT',
    });

    const retry = await db.beginCheckin(site.id, '2026-07-16', 'request-2');

    expect(retry.acquired).toBe(true);
    expect(retry.run.id).toBe(first.run.id);
    expect(retry.run.status).toBe('running');
    expect(retry.run.attemptCount).toBe(2);
    expect(await db.listCheckins({ limit: 10 })).toHaveLength(1);
  });

  it('does not acquire an already terminal daily check-in', async () => {
    db = await createTestDatabase();
    const site = await db.createSite(siteInput());
    const first = await db.beginCheckin(site.id, '2026-07-16', 'request-1');
    await db.finishCheckin(first.run.id, { status: 'success', message: 'done' });

    const repeated = await db.beginCheckin(site.id, '2026-07-16', 'request-2');

    expect(repeated.acquired).toBe(false);
    expect(repeated.run.status).toBe('success');
    expect(repeated.run.requestId).toBe('request-1');
  });

  it('deduplicates announcements and supports read/unread transitions', async () => {
    db = await createTestDatabase();
    const site = await db.createSite(siteInput());
    const announcement = {
      siteId: site.id,
      source: 'notice' as const,
      fingerprint: 'fingerprint-1',
      content: 'Maintenance tonight',
      kind: 'warning',
      extra: null,
      publishedAt: null,
    };

    expect(await db.upsertAnnouncement(announcement)).toBe(true);
    expect(await db.upsertAnnouncement(announcement)).toBe(false);
    const [stored] = await db.listAnnouncements({ limit: 10 });
    expect(stored).toBeDefined();
    expect((await db.getSummary()).unreadAnnouncements).toBe(1);

    const read = await db.setAnnouncementRead(stored!.id, true);
    expect(read.readAt).not.toBeNull();
    expect((await db.getSummary()).unreadAnnouncements).toBe(0);

    const unread = await db.setAnnouncementRead(stored!.id, false);
    expect(unread.readAt).toBeNull();
  });

  it('calculates today separately for each station timezone', async () => {
    db = await createTestDatabase();
    const now = new Date('2026-01-01T12:00:00.000Z');
    const east = await db.createSite(siteInput({
      name: 'East',
      baseUrl: 'https://east.example.com',
      timezone: 'Pacific/Kiritimati',
    }));
    const west = await db.createSite(siteInput({
      name: 'West',
      baseUrl: 'https://west.example.com',
      timezone: 'America/Adak',
    }));

    const eastRun = await db.beginCheckin(east.id, localDateForTimezone(now, east.timezone), 'east-request');
    await db.finishCheckin(eastRun.run.id, { status: 'success', message: 'ok' });
    const westRun = await db.beginCheckin(west.id, localDateForTimezone(now, west.timezone), 'west-request');
    await db.finishCheckin(westRun.run.id, { status: 'already_checked', message: 'done' });

    expect((await db.getSummary(now)).today).toEqual({ success: 1, already_checked: 1 });
  });
});
