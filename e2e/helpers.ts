import { expect, APIRequestContext } from '@playwright/test';

export const adminUser = { username: 'admin', password: 'password123' };

/** Creates the first admin if needed and logs the request context in. */
export async function ensureAdmin(request: APIRequestContext) {
  const statusRes = await request.get('/api/setup/status');
  expect(statusRes.ok()).toBeTruthy();
  const { needs_setup } = await statusRes.json();
  if (needs_setup) {
    const r = await request.post('/api/setup', { data: adminUser });
    expect(r.ok()).toBeTruthy();
  }
  const login = await request.post('/api/auth/login', { data: adminUser });
  expect(login.ok()).toBeTruthy();
}

export interface TestEvent {
  id: number;
  code: string;
  name: string;
}

export async function createEvent(request: APIRequestContext, prefix = 'E2E'): Promise<TestEvent> {
  const name = `${prefix} ${Date.now()}-${Math.floor(Math.random() * 1000)}`;
  const r = await request.post('/api/admin/events', { data: { name } });
  expect(r.ok()).toBeTruthy();
  const ev = await r.json();
  expect(ev.code).toBeTruthy();
  return { id: ev.id, code: ev.code, name: ev.name };
}

export async function createQuestion(request: APIRequestContext, eventId: number, data: Record<string, unknown>) {
  const r = await request.post(`/api/admin/events/${eventId}/questions`, { data });
  expect(r.ok()).toBeTruthy();
  return r.json();
}

export async function activateQuestion(request: APIRequestContext, eventId: number, questionId: number) {
  const r = await request.post(`/api/admin/events/${eventId}/questions/${questionId}/activate`);
  expect(r.ok()).toBeTruthy();
}

/** Fetches the admin event stats payload. */
export async function eventStats(request: APIRequestContext, eventId: number) {
  const r = await request.get(`/api/admin/events/${eventId}/stats`);
  expect(r.ok()).toBeTruthy();
  return r.json();
}

export function findQuestion(stats: any, questionId: number) {
  const q = (stats.questions || []).find((x: any) => x.id === questionId);
  expect(q).toBeTruthy();
  return q;
}
