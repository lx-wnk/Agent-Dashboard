import type { Locator, Page } from '@playwright/test'

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

/**
 * Opens an AppSelect trigger (role="combobox") and returns its listbox panel
 * once visible, without selecting anything — for tests that need to inspect
 * the option list (count, labels) before deciding what to do next. The panel
 * is teleported to <body>, so it must be located via page.getByRole, not as
 * a descendant of the trigger.
 */
export async function openListboxOptions(page: Page, trigger: Locator): Promise<Locator> {
  const listbox = page.getByRole('listbox')
  // Guard on aria-expanded like selectListboxOption does — clicking
  // unconditionally would toggle an already-open panel shut and then hang
  // on the visible-wait below.
  if (await trigger.getAttribute('aria-expanded') !== 'true') {
    await trigger.click()
    await listbox.waitFor({ state: 'visible' })
  }
  return listbox
}

/**
 * Selects an option from an AppSelect listbox by its accessible name. Only
 * one listbox is open at a time, so page.getByRole('listbox') finds it even
 * though the panel is teleported outside the trigger's subtree. If the panel
 * is already open (e.g. after openListboxOptions), it is reused rather than
 * toggled shut by a redundant click on the trigger.
 */
export async function selectListboxOption(page: Page, trigger: Locator, optionName: string | RegExp): Promise<void> {
  const listbox = page.getByRole('listbox')
  if (await trigger.getAttribute('aria-expanded') !== 'true') {
    await trigger.click()
    await listbox.waitFor({ state: 'visible' })
  }
  await listbox.getByRole('option', { name: optionName }).click()
  await listbox.waitFor({ state: 'detached' })
}
