import { expect, test } from '@playwright/test';

const adminToken = 'test-admin-token-1234567890';
const timestamp = '2026-07-18T04:00:00.000Z';
const capabilities = { checkin: true, announcements: true, requiresUserId: true };

test('manages a site, check-in, and announcement through the browser', async ({ page }) => {
  let site: Record<string, unknown> | undefined;
  let checkins: Record<string, unknown>[] = [];
  let announcements: Record<string, unknown>[] = [];
  const writes: Record<string, unknown>[] = [];

  await page.route('**/api/v1/**', async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname;
    if (request.headers().authorization !== `Bearer ${adminToken}`) {
      return route.fulfill({ status: 401, contentType: 'application/json', body: JSON.stringify({ error: { code: 'AUTH_REQUIRED', message: 'Unauthorized', retryable: false, requestId: 'e2e' } }) });
    }
    const json = (status: number, body: unknown) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });

    if (path === '/api/v1/summary') return json(200, { sites: { total: site ? 1 : 0, enabled: site?.enabled ? 1 : 0 }, today: {}, unreadAnnouncements: announcements.filter((item) => item.readAt == null).length });
    if (path === '/api/v1/site-adapters') return json(200, { data: [{ name: 'new-api', displayName: 'New API', capabilities }] });
    if (path === '/api/v1/sites' && request.method() === 'GET') return json(200, { data: site ? [site] : [] });
    if (path === '/api/v1/sites' && request.method() === 'POST') {
      const input = request.postDataJSON() as Record<string, unknown>;
      writes.push(input);
      site = { ...input, id: '11111111-1111-4111-8111-111111111111', credentialConfigured: true, consecutiveFailures: 0, capabilities, createdAt: timestamp, updatedAt: timestamp };
      return json(201, { data: site });
    }
    if (path === '/api/v1/checkin-runs' && request.method() === 'GET') return json(200, { data: checkins });
    if (path === '/api/v1/announcements' && request.method() === 'GET') return json(200, { data: announcements });

    const siteMatch = path.match(/^\/api\/v1\/sites\/([^/]+)$/);
    if (siteMatch && request.method() === 'GET') return json(200, { data: site });
    if (siteMatch && request.method() === 'PATCH') {
      const input = request.postDataJSON() as Record<string, unknown>;
      writes.push(input);
      site = { ...site, ...input, updatedAt: timestamp };
      return json(200, { data: site });
    }
    if (/\/checkin-runs$/.test(path) && request.method() === 'POST') {
      checkins = [{ id: 'run-1', siteId: site?.id, siteName: site?.name, localDate: '2026-07-18', status: 'success', rewardValue: 10, message: 'checked', errorCode: null, attemptCount: 1, startedAt: timestamp, finishedAt: timestamp, requestId: 'e2e-run' }];
      return json(201, { data: checkins[0] });
    }
    if (/\/announcement-syncs$/.test(path) && request.method() === 'POST') {
      announcements = [{ id: 'announcement-1', siteId: site?.id, siteName: site?.name, source: 'notice', fingerprint: 'fingerprint', content: '维护公告', kind: 'default', extra: '预计十分钟', publishedAt: timestamp, firstSeenAt: timestamp, lastSeenAt: timestamp, readAt: null }];
      return json(201, { data: { id: 'sync-1', siteId: site?.id, status: 'success', addedCount: 1, message: '', startedAt: timestamp, finishedAt: timestamp, requestId: 'e2e-sync' } });
    }
    const announcementMatch = path.match(/^\/api\/v1\/announcements\/([^/]+)$/);
    if (announcementMatch && request.method() === 'PATCH') {
      const { read } = request.postDataJSON() as { read: boolean };
      announcements = announcements.map((item) => ({ ...item, readAt: read ? timestamp : null }));
      return json(200, { data: announcements[0] });
    }
    return json(404, { error: { code: 'NOT_FOUND', message: 'Not found', retryable: false, requestId: 'e2e' } });
  });

  await page.goto('/connect');
  await page.getByLabel('管理员令牌').fill(adminToken);
  await page.getByRole('button', { name: '连接服务器' }).click();
  await page.getByRole('link', { name: '添加站点' }).first().click();
  await page.getByLabel('站点名称').fill('示例站点');
  await page.getByLabel('站点地址').fill('https://station.example');
  await page.getByLabel('站点类型').selectOption('new-api');
  await page.getByLabel('用户 ID').fill('42');
  await page.getByLabel('访问令牌').fill('station-token');
  await page.getByRole('button', { name: '添加站点' }).click();

  await expect(page.getByRole('heading', { name: '示例站点' })).toBeVisible();
  await page.getByRole('link', { name: '编辑' }).click();
  await page.getByLabel('站点名称').fill('更新站点');
  await page.getByRole('button', { name: '保存更改' }).click();
  await expect(page.getByRole('heading', { name: '更新站点' })).toBeVisible();

  await page.getByRole('button', { name: '停用' }).click();
  await expect(page.getByRole('button', { name: '启用' })).toBeVisible();
  await page.getByRole('button', { name: '启用' }).click();
  await page.getByRole('button', { name: '立即签到' }).click();
  await page.getByRole('link', { name: '签到记录' }).click();
  await expect(page.getByText('checked')).toBeVisible();

  await page.getByRole('link', { name: '站点管理' }).click();
  await page.getByRole('button', { name: '同步公告' }).click();
  await page.getByRole('link', { name: '公告中心' }).click();
  await expect(page.getByText('维护公告')).toBeVisible();
  await expect(page.getByText('预计十分钟')).toBeVisible();
  await page.getByRole('button', { name: '标为已读' }).click();
  await expect(page.getByRole('button', { name: '标为未读' })).toBeVisible();

  expect(writes[0]).toMatchObject({ accessToken: 'station-token', enabled: true, checkinEnabled: true, announcementEnabled: true });
  expect(writes[1]).not.toHaveProperty('accessToken');
});
