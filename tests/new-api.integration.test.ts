import { createServer, type IncomingMessage, type ServerResponse } from 'node:http';
import { once } from 'node:events';
import type { AddressInfo } from 'node:net';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import type { AppRuntime } from '../src/app.js';
import { authHeaders, buildTestApp, siteBody } from './helpers.js';

type SeenRequest = {
  method: string;
  pathname: string;
  search: string;
  authorization?: string;
  userId?: string;
  body: string;
};

function sendJson(reply: ServerResponse, statusCode: number, body: unknown): void {
  reply.writeHead(statusCode, { 'content-type': 'application/json' });
  reply.end(JSON.stringify(body));
}

async function readBody(request: IncomingMessage): Promise<string> {
  const chunks: Buffer[] = [];
  for await (const chunk of request) chunks.push(Buffer.from(chunk));
  return Buffer.concat(chunks).toString('utf8');
}

describe('New API end-to-end integration', () => {
  const seen: SeenRequest[] = [];
  let origin = '';
  let runtime: AppRuntime;
  const upstream = createServer(async (request, reply) => {
    const url = new URL(request.url ?? '/', 'http://upstream.test');
    const mode = url.pathname.startsWith('/turnstile/')
      ? 'turnstile'
      : url.pathname.startsWith('/already/')
        ? 'already'
        : 'normal';
    const pathname = url.pathname.replace(/^\/(turnstile|already)(?=\/)/, '');
    seen.push({
      method: request.method ?? 'GET',
      pathname: url.pathname,
      search: url.search,
      ...(typeof request.headers.authorization === 'string' ? { authorization: request.headers.authorization } : {}),
      ...(typeof request.headers['new-api-user'] === 'string' ? { userId: request.headers['new-api-user'] } : {}),
      body: await readBody(request),
    });

    if (pathname === '/api/status') {
      sendJson(reply, 200, {
        success: true,
        data: {
          checkin_enabled: true,
          turnstile_check: mode === 'turnstile',
          announcements_enabled: true,
          announcements: [
            { content: 'Maintenance tonight', type: 'warning', extra: '22:00-23:00', publishDate: '2026-07-16T12:00:00Z' },
            { content: 'Welcome to the station', type: 'default' },
          ],
        },
      });
      return;
    }
    if (pathname === '/api/notice') {
      sendJson(reply, 200, { success: true, data: 'Welcome to the station' });
      return;
    }
    if (pathname === '/api/user/checkin' && request.method === 'GET') {
      sendJson(reply, 200, { success: true, data: { stats: { checked_in_today: mode === 'already' } } });
      return;
    }
    if (pathname === '/api/user/checkin' && request.method === 'POST') {
      sendJson(reply, 200, { success: true, message: 'Check-in succeeded', data: { quota_awarded: 1234 } });
      return;
    }
    sendJson(reply, 404, { success: false, message: 'Not found' });
  });

  beforeAll(async () => {
    upstream.listen(0, '127.0.0.1');
    await once(upstream, 'listening');
    const address = upstream.address() as AddressInfo;
    origin = `http://127.0.0.1:${address.port}`;
    runtime = await buildTestApp();
  });

  afterAll(async () => {
    await runtime.app.close();
    upstream.close();
    await once(upstream, 'close');
  });

  async function createSite(path = '', name = 'Integration Station'): Promise<string> {
    const response = await runtime.app.inject({
      method: 'POST',
      url: '/api/v1/sites',
      headers: authHeaders(),
      payload: siteBody({ name, baseUrl: `${origin}${path}`, accessToken: 'raw-access-token', userId: '9988' }),
    });
    expect(response.statusCode).toBe(201);
    return response.json().data.id as string;
  }

  it('uses the real New API headers, keeps daily check-in idempotent, and deduplicates announcements', async () => {
    const siteId = await createSite();
    const first = await runtime.app.inject({
      method: 'POST',
      url: `/api/v1/sites/${siteId}/checkin-runs`,
      headers: authHeaders(),
    });
    expect(first.statusCode).toBe(201);
    expect(first.json().data).toMatchObject({ status: 'success', rewardValue: 1234, attemptCount: 1 });

    const repeated = await runtime.app.inject({
      method: 'POST',
      url: `/api/v1/sites/${siteId}/checkin-runs`,
      headers: authHeaders(),
    });
    expect(repeated.statusCode).toBe(200);
    expect(repeated.json().data.id).toBe(first.json().data.id);

    const checkinRequests = seen.filter((request) => request.pathname === '/api/user/checkin');
    expect(checkinRequests).toHaveLength(2);
    expect(checkinRequests.map((request) => request.method)).toEqual(['GET', 'POST']);
    expect(checkinRequests[0]).toMatchObject({
      authorization: 'raw-access-token',
      userId: '9988',
      search: expect.stringContaining('month='),
    });
    expect(checkinRequests[1]).toMatchObject({ authorization: 'raw-access-token', userId: '9988', body: '{}' });

    const firstSync = await runtime.app.inject({
      method: 'POST',
      url: `/api/v1/sites/${siteId}/announcement-syncs`,
      headers: authHeaders(),
    });
    expect(firstSync.statusCode).toBe(201);
    expect(firstSync.json().data).toMatchObject({ status: 'success', addedCount: 2 });

    const secondSync = await runtime.app.inject({
      method: 'POST',
      url: `/api/v1/sites/${siteId}/announcement-syncs`,
      headers: authHeaders(),
    });
    expect(secondSync.json().data.addedCount).toBe(0);

    const announcements = await runtime.app.inject({
      method: 'GET',
      url: '/api/v1/announcements',
      headers: authHeaders(),
    });
    expect(announcements.json().data).toHaveLength(2);
    expect(announcements.json().data).toContainEqual(expect.objectContaining({
      content: 'Maintenance tonight',
      source: 'status',
      kind: 'warning',
      extra: '22:00-23:00',
      publishedAt: '2026-07-16T12:00:00.000Z',
    }));
    expect(announcements.json().data).toContainEqual(expect.objectContaining({
      content: 'Welcome to the station',
      source: 'status',
    }));
    const announcementId = announcements.json().data[0].id as string;
    const markedRead = await runtime.app.inject({
      method: 'PATCH',
      url: `/api/v1/announcements/${announcementId}`,
      headers: authHeaders(),
      payload: { read: true },
    });
    expect(markedRead.json().data.readAt).not.toBeNull();
  });

  it('marks Turnstile as manual and recognizes an already completed check-in without posting', async () => {
    const turnstileSite = await createSite('/turnstile', 'Turnstile Station');
    const manual = await runtime.app.inject({
      method: 'POST',
      url: `/api/v1/sites/${turnstileSite}/checkin-runs`,
      headers: authHeaders(),
    });
    expect(manual.statusCode).toBe(201);
    expect(manual.json().data).toMatchObject({
      status: 'manual_required',
      errorCode: 'MANUAL_ACTION_REQUIRED',
    });
    expect(seen.some((request) => request.pathname === '/turnstile/api/user/checkin')).toBe(false);

    const alreadySite = await createSite('/already', 'Already Station');
    const already = await runtime.app.inject({
      method: 'POST',
      url: `/api/v1/sites/${alreadySite}/checkin-runs`,
      headers: authHeaders(),
    });
    expect(already.statusCode).toBe(201);
    expect(already.json().data.status).toBe('already_checked');
    const alreadyRequests = seen.filter((request) => request.pathname === '/already/api/user/checkin');
    expect(alreadyRequests).toHaveLength(1);
    expect(alreadyRequests[0]?.method).toBe('GET');
  });
});
