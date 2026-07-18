import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './tests/e2e',
  webServer: {
    command: 'npm run dev -- --host 127.0.0.1 --port 5191',
    port: 5191,
    reuseExistingServer: !process.env.CI,
  },
  use: { baseURL: 'http://127.0.0.1:5191', trace: 'retain-on-failure', video: 'retain-on-failure', screenshot: 'only-on-failure' },
  projects: [
    { name: 'desktop', use: { ...devices['Desktop Chrome'] } },
    { name: 'mobile', use: { ...devices['Pixel 7'] } },
  ],
});
