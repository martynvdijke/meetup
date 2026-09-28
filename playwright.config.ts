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
  webServer: [
    {
      command: 'rm -f e2e.db* && ./meetup',
      url: 'http://localhost:6280',
      reuseExistingServer: !process.env.CI,
      timeout: 60000,
      env: { PORT: '6280', DB_PATH: './e2e.db', MEDIA_DIR: './e2e-media' },
    },
    {
      // Dedicated instance for the OIDC setup test (fresh DB, OIDC enabled).
      command: 'rm -f e2e-oidc.db* && ./meetup',
      url: 'http://localhost:6281/api/setup/status',
      reuseExistingServer: !process.env.CI,
      timeout: 60000,
      env: {
        PORT: '6281',
        DB_PATH: './e2e-oidc.db',
        MEDIA_DIR: './e2e-oidc-media',
        OIDC_ENABLED: 'true',
        OIDC_ISSUER_URL: 'https://idp.example.test/realms/meetup',
        OIDC_CLIENT_ID: 'meetup-e2e',
        OIDC_CLIENT_SECRET: 'e2e-secret',
        OIDC_REDIRECT_URL: 'http://localhost:6281/api/auth/oidc/callback',
      },
    },
  ],
  projects: [
    {
      name: 'chromium',
      use: { browserName: 'chromium' },
    },
  ],
});
