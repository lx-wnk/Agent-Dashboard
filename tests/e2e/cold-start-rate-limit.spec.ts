import type { Page } from '@playwright/test'
import { expect, test } from '@playwright/test'

/**
 * The rate limiter is per IP, and this server binds loopback only, so every
 * client is 127.0.0.1 and they all share one bucket. Two windows opening at
 * once is the ordinary case — the desktop shell and a browser tab — and it must
 * not throttle the app's own boot.
 */
// Burst over rate of IPRateLimiterConfig (server/internal/api/middleware.go): after
// this much idle time the shared bucket is full, whatever the specs before this one spent.
const BUCKET_REFILL_MS = (120 / 10) * 1000

async function loadAndCollectThrottles(page: Page): Promise<string[]> {
  const throttled: string[] = []
  page.on('response', (response) => {
    if (response.status() === 429)
      throttled.push(`${response.status()} ${response.url()}`)
  })
  await page.goto('/')
  await expect(page.getByTestId('workspace-page-zentrale')).toBeVisible()
  return throttled
}

test.describe('a cold start does not rate-limit itself', () => {
  test('two clients opening at once are never throttled', async ({ browser }) => {
    await new Promise(resolve => setTimeout(resolve, BUCKET_REFILL_MS))
    const [one, two] = await Promise.all([browser.newContext(), browser.newContext()])
    const [pageOne, pageTwo] = await Promise.all([one.newPage(), two.newPage()])

    const [throttledOne, throttledTwo] = await Promise.all([
      loadAndCollectThrottles(pageOne),
      loadAndCollectThrottles(pageTwo),
    ])
    const throttled = [...throttledOne, ...throttledTwo]

    expect(throttled, throttled.join('\n')).toEqual([])

    await one.close()
    await two.close()
  })
})
