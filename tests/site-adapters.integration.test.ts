import { createServer, type IncomingMessage, type ServerResponse } from 'node:http';
import { once } from 'node:events';
import type { AddressInfo } from 'node:net';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import type { AppRuntime } from '../src/app.js';
import { authHeaders, buildTestApp } from './helpers.js';

type SeenRequest = {
  method: string;
  pathname: string;
  authorization: string;
};

const seen: SeenRequest[] = [];
let baseUrl = '';

function json(response: ServerResponse, status: number, body: unknown): void {
  response.writeHead(status, { 'content-type': 'application/json' });
  response.end(JSON.stringify(body));
}

const upstream = createServer((request: IncomingMessage, response: ServerResponse) => {
  const url = new URL(request.url ?? '/', 'http://127.0.0.1');
  seen.push({
    method: request.method ?? 'GET',
    pathname: url.pathname,
    authorization: String(request.headers.authorization ?? ''),
  });

  if (url.pathname === '/zen/api/public/site-info') {
    json(response, 200, { data: { site_mode: 'public' } });
    return;
  }
  if (url.pathname === '/zen/api/u/checkin' && request.method === 'POST') {
    json(response, 200, { code: 0, message: '签到成功', data: { reward: 2 } });
    return;
  }
  if (url.pathname === '/sub/api/v1/auth/me') {
    json(response, 401, { code: 'UNAUTHORIZED', message: 'Login required' });
    return;
  }
  if (url.pathname === '/sub/api/v1/announcements') {
    json(response, 200, {
      code: 0,
      message: 'ok',
      data: {
        items: [{ id: 7, title: '维护通知', content: '今晚维护', created_at: 1_784_000_000 }],
      },
    });
    return;
  }
  if (url.pathname === '/new/api/status') {
    json(response, 200, { success: true, data: { checkin_enabled: true } });
    return;
  }

  json(response, 404, { message: 'not found' });
});

beforeAll(async () => {
  upstream.listen(0, '127.0.0.1');
  await once(upstream, 'listening');
  const address = upstream.address() as AddressInfo;
  baseUrl = `http://127.0.0.1:${address.port}`;
});

afterAll(async () => {
  upstream.close();
  await once(upstream, 'close');
});

async function createSite(runtime: AppRuntime, payload: Record<string, unknown>) {
  return runtime.app.inject({
    method: 'POST',
    url: '/api/v1/sites',
    headers: authHeaders(),
    payload,
  });
}

describe('site adapter capabilities', () => {
  it('detects ZenAPI, performs Bearer-token check-in, and exposes capabilities', async () => {
    const runtime = await buildTestApp();
    try {
      const catalog = await runtime.app.inject({
        method: 'GET',
        url: '/api/v1/site-adapters',
        headers: authHeaders(),
      });
      expect(catalog.statusCode).toBe(200);
      expect(catalog.json().data).toEqual(expect.arrayContaining([
        expect.objectContaining({ name: 'new-api', capabilities: expect.objectContaining({ checkin: true }) }),
        expect.objectContaining({ name: 'sub2api', capabilities: expect.objectContaining({ checkin: false }) }),
        expect.objectContaining({ name: 'zen-api', capabilities: expect.objectContaining({ announcements: false }) }),
      ]));

      const created = await createSite(runtime, {
        name: 'Zen Station',
        baseUrl: `${baseUrl}/zen`,
        adapter: 'auto',
        accessToken: 'zen-secret',
      });
      expect(created.statusCode).toBe(201);
      expect(created.json().data).toMatchObject({
        adapter: 'zen-api',
        userId: '',
        checkinEnabled: true,
        announcementEnabled: false,
      });

      const checkin = await runtime.app.inject({
        method: 'POST',
        url: `/api/v1/sites/${created.json().data.id}/checkin-runs`,
        headers: authHeaders(),
      });
      expect(checkin.statusCode).toBe(201);
      expect(checkin.json().data).toMatchObject({ status: 'success', rewardValue: 2 });
      expect(seen).toContainEqual(expect.objectContaining({
        pathname: '/zen/api/u/checkin',
        authorization: 'Bearer zen-secret',
      }));
    } finally {
      await runtime.app.close();
    }
  });

  it('detects Sub2API, defaults to announcements only, and rejects unsupported check-in', async () => {
    const runtime = await buildTestApp();
    try {
      const created = await createSite(runtime, {
        name: 'Sub2 Station',
        baseUrl: `${baseUrl}/sub`,
        adapter: 'auto',
        accessToken: 'sub2-jwt',
      });
      expect(created.statusCode).toBe(201);
      expect(created.json().data).toMatchObject({
        adapter: 'sub2api',
        checkinEnabled: false,
        announcementEnabled: true,
      });

      const sync = await runtime.app.inject({
        method: 'POST',
        url: `/api/v1/sites/${created.json().data.id}/announcement-syncs`,
        headers: authHeaders(),
      });
      expect(sync.statusCode).toBe(201);
      expect(sync.json().data).toMatchObject({ status: 'success', addedCount: 1 });

      const announcements = await runtime.app.inject({
        method: 'GET',
        url: '/api/v1/announcements',
        headers: authHeaders(),
      });
      expect(announcements.json().data[0]).toMatchObject({
        content: '今晚维护',
        extra: '维护通知',
      });
      expect(seen).toContainEqual(expect.objectContaining({
        pathname: '/sub/api/v1/announcements',
        authorization: 'Bearer sub2-jwt',
      }));

      const unsupported = await createSite(runtime, {
        name: 'Invalid Sub2 Station',
        baseUrl: `${baseUrl}/sub-explicit`,
        adapter: 'sub2api',
        accessToken: 'sub2-jwt',
        checkinEnabled: true,
        announcementEnabled: true,
      });
      expect(unsupported.statusCode).toBe(422);
      expect(unsupported.json().error.code).toBe('VALIDATION_ERROR');
    } finally {
      await runtime.app.close();
    }
  });

  it('does not guess when automatic detection has no matching evidence', async () => {
    const runtime = await buildTestApp();
    try {
      const detectedNewApi = await createSite(runtime, {
        name: 'New API Station',
        baseUrl: `${baseUrl}/new`,
        adapter: 'auto',
        userId: '42',
        accessToken: 'new-api-token',
      });
      expect(detectedNewApi.statusCode).toBe(201);
      expect(detectedNewApi.json().data.adapter).toBe('new-api');

      const response = await createSite(runtime, {
        name: 'Unknown Station',
        baseUrl: `${baseUrl}/unknown`,
        adapter: 'auto',
        accessToken: 'unknown-token',
      });
      expect(response.statusCode).toBe(422);
      expect(response.json().error).toMatchObject({ code: 'VALIDATION_ERROR' });
    } finally {
      await runtime.app.close();
    }
  });

  it('requires capability flags to be compatible when changing adapters', async () => {
    const runtime = await buildTestApp();
    try {
      const created = await createSite(runtime, {
        name: 'Mutable Station',
        baseUrl: `${baseUrl}/mutable`,
        adapter: 'new-api',
        userId: '42',
        accessToken: 'new-api-token',
      });
      const siteId = created.json().data.id;

      const invalid = await runtime.app.inject({
        method: 'PATCH',
        url: `/api/v1/sites/${siteId}`,
        headers: authHeaders(),
        payload: { adapter: 'sub2api' },
      });
      expect(invalid.statusCode).toBe(422);

      const valid = await runtime.app.inject({
        method: 'PATCH',
        url: `/api/v1/sites/${siteId}`,
        headers: authHeaders(),
        payload: {
          adapter: 'sub2api',
          checkinEnabled: false,
          announcementEnabled: true,
        },
      });
      expect(valid.statusCode).toBe(200);
      expect(valid.json().data).toMatchObject({ adapter: 'sub2api', checkinEnabled: false });
    } finally {
      await runtime.app.close();
    }
  });
});
