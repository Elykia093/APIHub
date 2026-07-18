import { expect, test } from '@playwright/test';

test('connects with a tab-scoped token and renders responsive navigation', async ({ page }) => {
  await page.route('**/api/v1/**', async (route) => {
    const url = new URL(route.request().url());
    const authorization = route.request().headers().authorization;
    if (authorization !== 'Bearer test-admin-token-1234567890') return route.fulfill({ status:401, contentType:'application/json', body:JSON.stringify({error:{code:'AUTH_REQUIRED',message:'Unauthorized',retryable:false,requestId:'e2e'}}) });
    const body = url.pathname.endsWith('/summary') ? {sites:{total:0,enabled:0},today:{},unreadAnnouncements:0} : {data:[]};
    return route.fulfill({ status:200, contentType:'application/json', body:JSON.stringify(body) });
  });
  await page.goto('/connect');
  await page.getByLabel('管理员令牌').fill('test-admin-token-1234567890');
  await page.getByRole('button',{name:'连接服务器'}).click();
  await expect(page.getByRole('heading',{name:'概览'})).toBeVisible();
  await expect(page.getByRole('navigation',{name:'主导航'})).toBeVisible();
  await expect.poll(() => page.evaluate(() => sessionStorage.getItem('apihub-admin-token'))).toBe('test-admin-token-1234567890');
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth);
  expect(overflow).toBe(false);
});
