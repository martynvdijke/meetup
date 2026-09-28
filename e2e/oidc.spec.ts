import { test, expect } from '@playwright/test';

// Runs against the second web server (port 6281) which boots with a fresh DB
// and OIDC enabled. No live IdP round-trip: this covers the setup screen only.
const oidcBase = 'http://localhost:6281';

test('first-run setup offers local admin and OIDC sign-in', async ({ page }) => {
  await page.goto(`${oidcBase}/admin`);

  await expect(page.locator('#gate-setup')).toBeVisible();
  await expect(page.locator('#setup-user')).toBeVisible();
  await expect(page.locator('#setup-pass')).toBeVisible();

  const oidc = page.locator('#btn-oidc-setup');
  await expect(oidc).toBeVisible();
  await expect(oidc).toHaveAttribute('href', '/api/auth/oidc/login');
  await expect(oidc).toContainText('Sign in with OIDC');
});

test('oidc status reports enabled for configured instances', async ({ request }) => {
  const r = await request.get(`${oidcBase}/api/auth/oidc/status`);
  expect(r.ok()).toBeTruthy();
  const j = await r.json();
  expect(j.enabled).toBe(true);
  expect(j.login_url).toBeTruthy();
});
