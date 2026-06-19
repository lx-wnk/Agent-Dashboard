import { expect, test } from '@playwright/test'

/**
 * Mock /api/me so the auth gate (LoginPage) renders: authEnabled true with no
 * user. Mirrors the stubAuthDisabled helper in dashboard.spec.ts, inverted.
 */
async function stubAuthRequired(page: import('@playwright/test').Page) {
  await page.route('/api/me', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({ user: null, isAdmin: false, authEnabled: true }),
  }))
}

test.describe('login gate', () => {
  test.beforeEach(async ({ page }) => {
    await stubAuthRequired(page)
  })

  test('exposes a main landmark and a single level-1 heading', async ({ page }) => {
    await page.goto('/')
    await expect(page.getByRole('main')).toBeVisible()
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('Claude Agent Dashboard')
  })

  test('moves focus to the login control when the gate appears', async ({ page }) => {
    await page.goto('/')
    await expect(page.getByRole('link', { name: 'Login with GitHub' })).toBeFocused()
  })

  test('announces a mapped alert for ?error=auth_failed', async ({ page }) => {
    await page.goto('/?error=auth_failed')
    const alert = page.getByRole('alert')
    await expect(alert).toBeVisible()
    await expect(alert).toHaveText('Authentication failed')
  })
})
