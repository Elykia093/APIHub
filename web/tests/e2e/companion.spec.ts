import { expect, test } from '@playwright/test';

const adminToken = 'test-admin-token-1234567890';
const timestamp = '2026-07-22T08:00:00.000Z';
const capabilities = { checkin: true, announcements: true, requiresUserId: true };
const sites = [
  { id: 'site-1', name: '第一站点', baseUrl: 'https://first.example', adapter: 'new-api', userId: '1', enabled: true, checkinEnabled: true, announcementEnabled: true, checkinCron: '0 8 * * *', announcementCron: '0 * * * *', timezone: 'Asia/Shanghai', credentialConfigured: true, consecutiveFailures: 0, capabilities, createdAt: timestamp, updatedAt: timestamp },
  { id: 'site-2', name: '第二站点', baseUrl: 'https://second.example', adapter: 'new-api', userId: '2', enabled: true, checkinEnabled: true, announcementEnabled: true, checkinCron: '0 8 * * *', announcementCron: '0 * * * *', timezone: 'Asia/Shanghai', credentialConfigured: true, consecutiveFailures: 0, capabilities, createdAt: timestamp, updatedAt: timestamp },
];

test('pairs, revokes, and creates a browser companion task', async ({ page }) => {
  let revokedAt: string | null = null;
  let tasks: Record<string, unknown>[] = [];
  let createdTarget = '';

  await page.route('**/api/v1/**', async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname;
    const json = (status: number, body: unknown) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });
    if (request.headers().authorization !== `Bearer ${adminToken}`) {
      return json(401, { error: { code: 'AUTH_REQUIRED', message: 'Unauthorized', retryable: false, requestId: 'e2e' } });
    }
    if (path === '/api/v1/summary') return json(200, { sites: { total: 2, enabled: 2 }, today: {}, unreadAnnouncements: 0 });
    if (path === '/api/v1/sites') return json(200, { data: sites });
    if (path === '/api/v1/companion-devices') return json(200, { data: [{ id: 'device-1', name: 'Chrome', createdAt: timestamp, lastSeenAt: timestamp, revokedAt }] });
    if (path === '/api/v1/browser-tasks') return json(200, { data: tasks });
    if (path === '/api/v1/companion-pairing-codes' && request.method() === 'POST') return json(201, { data: { code: 'ABCDEF0123456789ABCDEF01', expiresAt: '2026-07-22T08:05:00.000Z' } });
    if (path === '/api/v1/companion-devices/device-1/revocations' && request.method() === 'POST') {
      revokedAt = timestamp;
      return json(201, { data: { id: 'device-1', revoked: true } });
    }
    if (path === '/api/v1/sites/site-2/browser-tasks' && request.method() === 'POST') {
      createdTarget = (request.postDataJSON() as { targetUrl: string }).targetUrl;
      tasks = [{ id: 'task-1', siteId: 'site-2', siteName: '第二站点', targetUrl: createdTarget, status: 'queued', assignedDeviceId: null, leaseExpiresAt: null, attemptCount: 0, message: '', balance: null, createdAt: timestamp, startedAt: null, finishedAt: null }];
      return json(201, { data: tasks[0] });
    }
    return json(404, { error: { code: 'NOT_FOUND', message: 'Not found', retryable: false, requestId: 'e2e' } });
  });

  await page.goto('/connect');
  await page.getByLabel('管理员令牌').fill(adminToken);
  await page.getByRole('button', { name: '连接服务器' }).click();
  await page.getByRole('link', { name: '浏览器伴侣' }).click();
  await expect(page.getByRole('heading', { name: '浏览器伴侣' })).toBeVisible();

  await page.getByRole('button', { name: '生成配对码' }).click();
  await expect(page.getByText('ABCDEF0123456789ABCDEF01')).toBeVisible();
  await page.getByRole('button', { name: '撤销' }).click();
  await expect(page.getByText('已撤销')).toBeVisible();

  await page.getByLabel('站点').selectOption('site-2');
  await expect(page.getByLabel('签到页 URL')).toHaveValue('https://second.example');
  await page.getByRole('button', { name: '下发浏览器任务' }).click();
  await expect(page.getByText('https://second.example')).toBeVisible();
  expect(createdTarget).toBe('https://second.example');
});
