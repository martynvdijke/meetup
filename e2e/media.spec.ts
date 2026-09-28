import { test, expect } from '@playwright/test';
import { readFileSync } from 'fs';
import { join } from 'path';
import { activateQuestion, createEvent, createQuestion, ensureAdmin } from './helpers';

const pixel = readFileSync(join(__dirname, 'fixtures', 'pixel.png'));
const prompt = 'What do you think? ' + Date.now();

let code = '';
let mediaURL = '';

test.beforeAll(async ({ request }) => {
  await ensureAdmin(request);
  const ev = await createEvent(request, 'Media');

  const upload = await request.post(`/api/admin/events/${ev.id}/questions/media`, {
    multipart: {
      file: { name: 'pixel.png', mimeType: 'image/png', buffer: pixel },
    },
  });
  expect(upload.ok()).toBeTruthy();
  const media = await upload.json();
  expect(media.url).toMatch(/^\/media\//);
  expect(media.media_type).toBe('image');
  expect(media.size).toBeGreaterThan(0);
  mediaURL = media.url;

  const q = await createQuestion(request, ev.id, {
    kind: 'poll',
    mode: 'live',
    prompt,
    options: ['Nice', 'Meh'],
    media_url: media.url,
    media_type: media.media_type,
    show_results: true,
  });
  expect(q.media_url).toBe(media.url);
  expect(q.media_type).toBe('image');
  await activateQuestion(request, ev.id, q.id);
  code = ev.code;
});

test('uploaded image is served and rendered in the audience prompt', async ({ page }) => {
  await page.goto(`/e/${code}`);
  await expect(page.locator('#live-card')).toContainText(prompt);

  const img = page.locator(`#live-card img[src="${mediaURL}"]`);
  await expect(img).toBeVisible();
  await expect(async () => {
    const w = await img.evaluate((el: HTMLImageElement) => el.naturalWidth);
    expect(w).toBeGreaterThan(0);
  }).toPass({ timeout: 10000 });
});

test('uploaded media is served with an immutable cache header', async ({ request }) => {
  const r = await request.get(mediaURL);
  expect(r.ok()).toBeTruthy();
  expect(r.headers()['content-type']).toContain('image/png');
  expect(r.headers()['cache-control']).toContain('immutable');
});
