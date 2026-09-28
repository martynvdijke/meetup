import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './e2e',
  timeout: 30000,
  retries: 1,
  workers: 1,
  reporter: process.env.CI
    ? [['github'], ['html', { open: 'never' }]]
    : [['list'], ['html', { open: 'never' }]],
  use: {
    baseURL: 'http://localhost:6280',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
  },
  webServer: {
    command: 'rm -f e2e.db* && ./meetup',
    url: 'http://localhost:6280',
    reuseExistingServer: !process.env.CI,
    timeout: 60000,
    env: { PORT: '6280', DB_PATH: './e2e.db', MEDIA_DIR: './e2e-media' },
  },
  projects: [
    {
      name: 'chromium',
      use: { browserName: 'chromium' },
    },
  ],
});
