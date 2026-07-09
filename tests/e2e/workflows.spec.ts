import { expect, test } from '@playwright/test'

// VA-2 Workflows view: this happy-path test verifies the new view toggle
// is wired in and that all four chart tabs render without throwing. It
// does NOT assert non-empty SVG content — a clean developer machine
// running this in CI may have zero sessions in the default 7-day window.

/**
 * Mock /api/me so the app does not redirect to the LoginPage when the Go
 * backend is not running (mirrors shell.spec.ts / dashboard.spec.ts). Without
 * this stub the dev-server proxy returns a 500 for /api/me, which flips
 * showLogin true and the test never reaches the Workflows view under test.
 */
async function stubAuthDisabled(page: import('@playwright/test').Page) {
  await page.route('/api/me', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({ user: null, isAdmin: true, authEnabled: false }),
  }))
}

test('workflows view toggle opens the four chart tabs', async ({ page }) => {
  await stubAuthDisabled(page)
  await page.goto('/')
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Dashboard')

  // Switch to Workflows via the main view-mode toggle.
  await page.getByRole('button', { name: 'Workflows' }).click()

  // The four tab buttons must be present.
  for (const label of ['Sankey', 'Session DAG', 'Spawn Tree', 'Co-occurrence']) {
    await expect(page.getByRole('tab', { name: label })).toBeVisible()
  }

  // The Sankey tab is the default; either an SVG or its empty-state copy must appear.
  await expect(
    page.locator('.sankey-chart svg, .sankey-chart >> text=No tool calls'),
  ).toBeVisible({ timeout: 5000 })

  // Spawn Tree is reachable without picking a session ID.
  await page.getByRole('tab', { name: 'Spawn Tree' }).click()
  await expect(
    page.locator('.spawn-tree-chart svg, .spawn-tree-chart >> text=No sessions'),
  ).toBeVisible({ timeout: 5000 })

  // Co-occurrence likewise — aggregate endpoint, no session required.
  await page.getByRole('tab', { name: 'Co-occurrence' }).click()
  await expect(
    page.locator('.co-occurrence-matrix svg, .co-occurrence-matrix >> text=No tool calls'),
  ).toBeVisible({ timeout: 5000 })

  // The DAG tab is only rendered once a session id is selected — it is absent,
  // not merely disabled, when no session id is entered.
  await expect(page.getByRole('tab', { name: 'Session DAG' })).toHaveCount(0)
})
