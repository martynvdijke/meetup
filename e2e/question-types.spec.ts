import { test, expect } from '@playwright/test';
import { activateQuestion, createEvent, createQuestion, ensureAdmin, eventStats, findQuestion } from './helpers';

const multiPrompt = 'Pick favorites ' + Date.now();
const rankPrompt = 'Rank these ' + Date.now();
const yesnoPrompt = 'Joining us? ' + Date.now();
const npsPrompt = 'How likely to recommend? ' + Date.now();

let eventId = 0;
let code = '';
let multiId = 0;
let rankId = 0;
let yesnoId = 0;
let npsId = 0;

test.beforeAll(async ({ request }) => {
  await ensureAdmin(request);
  const ev = await createEvent(request, 'Types');
  eventId = ev.id;
  code = ev.code;

  const multi = await createQuestion(request, eventId, {
    kind: 'multi',
    mode: 'live',
    prompt: multiPrompt,
    options: ['Alpha', 'Beta', 'Gamma'],
    show_results: true,
  });
  multiId = multi.id;

  const rank = await createQuestion(request, eventId, {
    kind: 'ranking',
    mode: 'live',
    prompt: rankPrompt,
    options: ['One', 'Two', 'Three'],
    show_results: true,
  });
  rankId = rank.id;

  const yesno = await createQuestion(request, eventId, {
    kind: 'yesno',
    mode: 'live',
    prompt: yesnoPrompt,
    show_results: true,
  });
  yesnoId = yesno.id;

  const nps = await createQuestion(request, eventId, {
    kind: 'nps',
    mode: 'live',
    prompt: npsPrompt,
    show_results: true,
  });
  npsId = nps.id;
});

test('audience can submit a multi-select answer', async ({ page, request }) => {
  await ensureAdmin(request);
  await activateQuestion(request, eventId, multiId);
  await page.goto(`/e/${code}`);
  await expect(page.locator('#live-card')).toContainText(multiPrompt);

  await page.locator('#live-card label.option-btn', { hasText: 'Alpha' }).click();
  await page.locator('#live-card label.option-btn', { hasText: 'Gamma' }).click();
  await page.getByRole('button', { name: 'Submit selection' }).click();
  await expect(page.locator('#live-card').getByText(/Sent|Thanks/)).toBeVisible({ timeout: 10000 });

  await ensureAdmin(request);
  const q = findQuestion(await eventStats(request, eventId), multiId);
  const counts: Record<string, number> = {};
  (q.results || []).forEach((r: any) => {
    counts[r.label] = r.count;
  });
  expect(counts['Alpha']).toBeGreaterThanOrEqual(1);
  expect(counts['Gamma']).toBeGreaterThanOrEqual(1);
  expect(counts['Beta'] || 0).toBe(0);
  expect(q.respondents).toBeGreaterThanOrEqual(1);
});

test('audience can rank options before submitting', async ({ page, request }) => {
  await ensureAdmin(request);
  await activateQuestion(request, eventId, rankId);
  await page.goto(`/e/${code}`);
  await expect(page.locator('#live-card')).toContainText(rankPrompt);

  await page.getByRole('button', { name: 'Move Two up' }).click();
  await page.getByRole('button', { name: 'Submit ranking' }).click();
  await expect(page.locator('#live-card').getByText(/Sent|Thanks/)).toBeVisible({ timeout: 10000 });

  await ensureAdmin(request);
  const q = findQuestion(await eventStats(request, eventId), rankId);
  const labels = (q.results || []).map((r: any) => r.label);
  expect(labels).toEqual(['Two', 'One', 'Three']);
  const scores = (q.results || []).map((r: any) => r.score ?? 0);
  expect(scores[0]).toBeGreaterThan(scores[1]);
  expect(scores[1]).toBeGreaterThan(scores[2]);
});

test('audience can answer a yes/no question', async ({ page, request }) => {
  await ensureAdmin(request);
  await activateQuestion(request, eventId, yesnoId);
  await page.goto(`/e/${code}`);
  await expect(page.locator('#live-card')).toContainText(yesnoPrompt);

  await page.getByRole('button', { name: 'Yes', exact: true }).click();
  await expect(page.locator('#live-card').getByText(/Sent|Thanks/)).toBeVisible({ timeout: 10000 });

  await ensureAdmin(request);
  const q = findQuestion(await eventStats(request, eventId), yesnoId);
  const counts: Record<string, number> = {};
  (q.results || []).forEach((r: any) => {
    counts[r.label] = r.count;
  });
  expect(counts['yes']).toBeGreaterThanOrEqual(1);
  expect(counts['no'] || 0).toBe(0);
});

test('audience can answer an NPS question and results show the score', async ({ page, request }) => {
  await ensureAdmin(request);
  await activateQuestion(request, eventId, npsId);
  await page.goto(`/e/${code}`);
  await expect(page.locator('#live-card')).toContainText(npsPrompt);

  await page.getByRole('button', { name: 'Score 10 out of 10' }).click();
  await expect(page.locator('#live-card').getByText(/Sent|Thanks/)).toBeVisible({ timeout: 10000 });
  await expect(page.locator('#live-card')).toContainText('NPS 100');

  await ensureAdmin(request);
  const q = findQuestion(await eventStats(request, eventId), npsId);
  expect(q.nps).toBe(100);
  expect((q.results || []).length).toBe(11);
  const top = (q.results || []).find((r: any) => r.label === '10');
  expect(top.count).toBeGreaterThanOrEqual(1);
});
