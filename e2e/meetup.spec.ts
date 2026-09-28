import { test, expect } from '@playwright/test';

let code = '';
let eventName = '';
const pollPrompt = 'Smoke poll ' + Date.now();
const pollOptions = ['Red', 'Green', 'Blue'];

async function ensureAuth(request: any) {
  const statusRes = await request.get('/api/setup/status');
  expect(statusRes.ok()).toBeTruthy();
  const { needs_setup } = await statusRes.json();
  if (needs_setup) {
    const r = await request.post('/api/setup', { data: { username: 'admin', password: 'password123' } });
    expect(r.ok()).toBeTruthy();
  }
  const login = await request.post('/api/auth/login', { data: { username: 'admin', password: 'password123' } });
  expect(login.ok()).toBeTruthy();
}

test.beforeAll(async ({ request }) => {
  await ensureAuth(request);
  eventName = 'Smoke ' + Date.now();
  const r = await request.post('/api/admin/events', { data: { name: eventName } });
  expect(r.ok()).toBeTruthy();
  const ev = await r.json();
  code = ev.code;
  expect(code).toBeTruthy();

  const q = await request.post(`/api/admin/events/${ev.id}/questions`, {
    data: { kind: 'poll', mode: 'live', prompt: pollPrompt, options: pollOptions, show_results: true },
  });
  expect(q.ok()).toBeTruthy();
  const qq = await q.json();
  const act = await request.post(`/api/admin/events/${ev.id}/questions/${qq.id}/activate`);
  expect(act.ok()).toBeTruthy();
});

test('audience can see event and vote', async ({ page }) => {
  await page.goto(`/e/${code}`);
  await expect(page.locator('#event-name')).toContainText(eventName);
  // live card shows poll prompt
  await expect(page.locator('#live-card')).toContainText(pollPrompt);
  const first = page.locator('.option-btn').first();
  await expect(first).toBeVisible();
  await first.click();
  // after click button becomes Sent or thanks message appears
  await expect(page.locator('#live-card').getByText(/Sent|Thanks/)).toBeVisible({ timeout: 10000 });
});

test('projector shows prompt and QR', async ({ page }) => {
  await page.goto(`/live/${code}`);
  await expect(page.locator('#prompt-area')).toContainText(pollPrompt);
  const qr = page.locator('#qr-img');
  await expect(qr).toHaveAttribute('src', new RegExp(`/api/events/${code}/qr\\.png`));
  // wait for image to load
  await expect(async () => {
    const w = await qr.evaluate((el: HTMLImageElement) => el.naturalWidth);
    expect(w).toBeGreaterThan(0);
  }).toPass({ timeout: 10000 });
});

test('admin page shows login gate or dashboard', async ({ page }) => {
  await page.goto('/admin');
  await expect(page.locator('body')).toContainText(/Host login|Initial setup|Dashboard|Events/);
});
