import type { Page } from '@playwright/test'

/**
 * Mock /api/me so the app never redirects to the LoginPage — mirrors the
 * pattern used across every E2E spec (dashboard.spec.ts, question-band.spec.ts).
 */
export async function stubAuthDisabled(page: Page): Promise<void> {
  await page.route('/api/me', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({ user: null, isAdmin: true, authEnabled: false }),
  }))
}

/** Stubs a GET endpoint to return a fixed JSON body. */
export async function stubJson(page: Page, path: string, body: unknown, status = 200): Promise<void> {
  // Match the exact path with an optional query string, so callers can pass the
  // bare path and still intercept requests that append params (e.g. the
  // visualization endpoints send ?from=&to=). A plain string glob would miss those.
  const escaped = path.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  await page.route(new RegExp(`${escaped}(\\?.*)?$`), route => route.fulfill({
    status,
    contentType: 'application/json',
    body: JSON.stringify(body),
  }))
}

/**
 * Stubs an SSE stream endpoint (e.g. /api/projects/stream) so composables built
 * on createSseResource don't reach out to the real backend. Fulfilling with an
 * empty body closes the connection immediately — callers that need live push
 * updates should route the initial list via `stubJson` instead and treat the
 * stream as a no-op, matching how question-band.spec.ts stubs /api/agents/stream.
 */
export async function stubEmptyStream(page: Page, path: string): Promise<void> {
  await page.route(path, route => route.fulfill({
    status: 200,
    contentType: 'text/event-stream',
    body: '',
  }))
}
