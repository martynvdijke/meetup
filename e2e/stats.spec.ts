import { test, expect } from '@playwright/test';
import { activateQuestion, adminUser, createEvent, createQuestion, ensureAdmin } from './helpers';

const prompt = 'Stats poll ' + Date.now();

test('admin stats reflect audience activity', async ({ page, request }) => {
  await ensureAdmin(request);
  const ev = await createEvent(request, 'Stats');
  const q = await createQuestion(request, ev.id, {
    kind: 'poll',
    mode: 'live',
    prompt,
    options: ['Option A', 'Option B'],
    show_results: true,
  });
  await activateQuestion(request, ev.id, q.id);

  // Answer as an audience participant (sets the participant cookie on this context).
  const state = await request.get(`/api/events/${ev.code}/state`);
  expect(state.ok()).toBeTruthy();
  const answer = await request.post(`/api/events/${ev.code}/answers`, {
    data: { question_id: q.id, value: 'Option A' },
  });
  expect(answer.ok()).toBeTruthy();

  // Log in to the admin UI and open the stats section.
  await page.goto('/admin');
  await page.fill('#login-user', adminUser.username);
  await page.fill('#login-pass', adminUser.password);
  await page.locator('#form-login button[type="submit"]').click();
  await expect(page.locator('#app')).toBeVisible();

  await page.locator('button[data-view="stats"]').click();
  await expect(page.locator('#stats-global')).toContainText('Events');
  await expect(page.locator('#stats-global')).toContainText('Answers');

  const row = page.locator('#stats-events .q-row', { hasText: ev.code });
  await expect(row).toBeVisible();
  await row.getByRole('button', { name: 'Open stats' }).click();

  await expect(page.locator('#stats-event-cards')).toContainText('1 of 1 participants');
  await expect(page.locator('#stats-questions')).toContainText(prompt);
  await expect(page.locator('#stats-questions')).toContainText('1 respondents');

  // Live SSE connection can be established for the selected event.
  await page.locator('#btn-refresh-stats').click();
  await expect(page.locator('#stats-live')).toContainText('Live updates: on', { timeout: 10000 });
});
