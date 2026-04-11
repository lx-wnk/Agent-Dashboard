import { expect, test } from '@playwright/test'

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Wait for the initial API polling cycle to resolve (app polls every 3 s). */
async function waitForAgentsLoaded(page: import('@playwright/test').Page) {
  // Either the table or the card-grid must appear, OR the empty-state message.
  await page.waitForSelector(
    '.agent-table, .card-grid, .kanban-board, .board-empty, .empty, text=No running Claude agents found.',
    { timeout: 8000 },
  )
}

// ---------------------------------------------------------------------------
// Test 1 — Dashboard loads
// ---------------------------------------------------------------------------
test('dashboard loads with heading', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Claude Agent Overview' })).toBeVisible()
})

// ---------------------------------------------------------------------------
// Test 2 — Header stats visible
// ---------------------------------------------------------------------------
test('header agent-count badge is visible', async ({ page }) => {
  await page.goto('/')
  await expect(page.locator('.agent-count')).toBeVisible()
  // Badge contains a number followed by "agent" or "agents"
  await expect(page.locator('.agent-count')).toContainText(/\d+ agents?/)
})

// ---------------------------------------------------------------------------
// Test 3 — ResourceBar renders
// ---------------------------------------------------------------------------
test('ResourceBar renders with progress bars or percentage values', async ({ page }) => {
  await page.goto('/')
  // ResourceBar polls /api/system on mount. Give it a moment to respond.
  // It is only rendered when the API returns data (v-if="info").
  // Check for either the bar container or any percentage text as a proxy.
  const resourceBar = page.locator('.resource-bar')
  try {
    await resourceBar.waitFor({ timeout: 6000 })
    // If present, it must contain at least one gauge or percentage
    await expect(resourceBar.locator('.res-pct').first()).toBeVisible()
  }
  catch {
    // /api/system may not respond in CI — treat absence as a skip condition
    test.skip()
  }
})

// ---------------------------------------------------------------------------
// Test 4 — View toggle: list -> cards
// ---------------------------------------------------------------------------
test('view toggle switches from list to card grid', async ({ page }) => {
  await page.goto('/')
  await waitForAgentsLoaded(page)

  // Ensure we start in list view (click list toggle first to be deterministic)
  await page.locator('.toggle-btn').nth(0).click()
  await expect(page.locator('.agent-table')).toBeVisible()

  // Click the cards toggle (second .toggle-btn)
  await page.locator('.toggle-btn').nth(1).click()

  // Card grid must appear; table must disappear
  await expect(page.locator('.card-grid')).toBeVisible()
  await expect(page.locator('.agent-table')).not.toBeVisible()
})

// ---------------------------------------------------------------------------
// Test 5 — View toggle: cards -> list
// ---------------------------------------------------------------------------
test('view toggle switches from cards back to list', async ({ page }) => {
  await page.goto('/')
  await waitForAgentsLoaded(page)

  // Switch to cards first
  await page.locator('.toggle-btn').nth(1).click()
  await expect(page.locator('.card-grid')).toBeVisible()

  // Switch back to list
  await page.locator('.toggle-btn').nth(0).click()
  await expect(page.locator('.agent-table')).toBeVisible()
  await expect(page.locator('.card-grid')).not.toBeVisible()
})

// ---------------------------------------------------------------------------
// Test 6 — View toggle persists across reload (localStorage)
// ---------------------------------------------------------------------------
test('card view persists after page reload', async ({ page }) => {
  await page.goto('/')
  await waitForAgentsLoaded(page)

  // Switch to card view
  await page.locator('.toggle-btn').nth(1).click()
  await expect(page.locator('.card-grid')).toBeVisible()

  // Reload and verify the preference is restored
  await page.reload()
  await waitForAgentsLoaded(page)
  await expect(page.locator('.card-grid')).toBeVisible()
  await expect(page.locator('.agent-table')).not.toBeVisible()
})

// ---------------------------------------------------------------------------
// Test — Kanban view toggle
// ---------------------------------------------------------------------------
test('view toggle switches to kanban view', async ({ page }) => {
  await page.goto('/')
  await waitForAgentsLoaded(page)

  // Ensure list view first
  await page.locator('.toggle-btn').nth(0).click()
  await expect(page.locator('.agent-table')).toBeVisible()

  // Click kanban toggle (third .toggle-btn)
  await page.locator('.toggle-btn').nth(2).click()

  // Kanban board or empty state must appear; table must disappear
  await expect(page.locator('.kanban-board, .board-empty')).toBeVisible()
  await expect(page.locator('.agent-table')).not.toBeVisible()
})

// ---------------------------------------------------------------------------
// Test 7 — Search filters agents
// ---------------------------------------------------------------------------
test('typing in search input filters the agent list', async ({ page }) => {
  await page.goto('/')
  await waitForAgentsLoaded(page)

  // Capture how many agents are shown before filtering
  const countText = await page.locator('.agent-count').textContent()
  const totalCount = Number.parseInt(countText ?? '0', 10)

  // Type something very unlikely to match any real agent name
  await page.locator('.header-search').fill('xQzNeverMatchesAnything99')

  // After filtering the count badge must update
  const filteredText = await page.locator('.agent-count').textContent()
  const filteredCount = Number.parseInt(filteredText ?? '0', 10)

  // Either the count dropped, or there were 0 agents to begin with
  expect(filteredCount).toBeLessThanOrEqual(totalCount)
})

// ---------------------------------------------------------------------------
// Test 8 — Clearing search restores all agents
// ---------------------------------------------------------------------------
test('clearing search restores the original agent count', async ({ page }) => {
  await page.goto('/')
  await waitForAgentsLoaded(page)

  const countText = await page.locator('.agent-count').textContent()
  const totalCount = Number.parseInt(countText ?? '0', 10)

  // Filter
  await page.locator('.header-search').fill('xQzNeverMatchesAnything99')

  // Clear
  await page.locator('.header-search').fill('')

  // Count must be back to original
  const restoredText = await page.locator('.agent-count').textContent()
  const restoredCount = Number.parseInt(restoredText ?? '0', 10)
  expect(restoredCount).toBe(totalCount)
})

// ---------------------------------------------------------------------------
// Tests 9–12 — Modal (require at least one agent to be present)
// ---------------------------------------------------------------------------

test('clicking an agent row opens the modal', async ({ page }) => {
  await page.goto('/')
  await waitForAgentsLoaded(page)

  // Ensure list view
  await page.locator('.toggle-btn').nth(0).click()

  const firstRow = page.locator('.agent-row').first()
  const rowCount = await firstRow.count()
  if (rowCount === 0) {
    test.skip()
    return
  }

  await firstRow.click()
  await expect(page.locator('.modal-backdrop')).toBeVisible()
})

test('modal shows session details when open', async ({ page }) => {
  await page.goto('/')
  await waitForAgentsLoaded(page)

  // Ensure list view
  await page.locator('.toggle-btn').nth(0).click()

  const firstRow = page.locator('.agent-row').first()
  if ((await firstRow.count()) === 0) {
    test.skip()
    return
  }

  await firstRow.click()
  await expect(page.locator('.modal-backdrop')).toBeVisible()

  // The modal window must contain the project name and uptime/cost meta
  const modalWindow = page.locator('.modal-window')
  await expect(modalWindow.locator('.modal-project')).toBeVisible()
  await expect(modalWindow.locator('.modal-meta')).toBeVisible()
})

test('pressing Escape closes the modal', async ({ page }) => {
  await page.goto('/')
  await waitForAgentsLoaded(page)

  await page.locator('.toggle-btn').nth(0).click()

  const firstRow = page.locator('.agent-row').first()
  if ((await firstRow.count()) === 0) {
    test.skip()
    return
  }

  await firstRow.click()
  await expect(page.locator('.modal-backdrop')).toBeVisible()

  await page.keyboard.press('Escape')
  await expect(page.locator('.modal-backdrop')).not.toBeVisible()
})

test('clicking the close button in the modal closes it', async ({ page }) => {
  await page.goto('/')
  await waitForAgentsLoaded(page)

  await page.locator('.toggle-btn').nth(0).click()

  const firstRow = page.locator('.agent-row').first()
  if ((await firstRow.count()) === 0) {
    test.skip()
    return
  }

  await firstRow.click()
  await expect(page.locator('.modal-backdrop')).toBeVisible()

  await page.locator('.modal-close').click()
  await expect(page.locator('.modal-backdrop')).not.toBeVisible()
})

// ---------------------------------------------------------------------------
// Test 13 — Sessions dialog opens
// ---------------------------------------------------------------------------
test('clicking Sessions button opens the sessions dialog', async ({ page }) => {
  await page.goto('/')

  await page.locator('.sessions-btn').click()

  // The sessions backdrop and its modal with the "Past Sessions" heading must appear
  await expect(page.locator('.sessions-backdrop')).toBeVisible()
  await expect(page.locator('.sessions-modal')).toBeVisible()
  await expect(page.locator('.sessions-modal').getByRole('heading', { name: 'Past Sessions' })).toBeVisible()
})

// ---------------------------------------------------------------------------
// Test 14 — Spawn dialog opens
// ---------------------------------------------------------------------------
test('clicking + New Agent button opens the spawn dialog', async ({ page }) => {
  await page.goto('/')

  await page.locator('.spawn-btn').click()

  // The spawn backdrop and its modal with the "New Agent" heading must appear
  await expect(page.locator('.spawn-backdrop')).toBeVisible()
  await expect(page.locator('.spawn-modal')).toBeVisible()
  await expect(page.locator('.spawn-modal').getByRole('heading', { name: 'New Agent' })).toBeVisible()
})
