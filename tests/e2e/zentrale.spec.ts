import { expect, test } from '@playwright/test'

// The stored layout is shared server-side state, not per-test-context state —
// reset it after every test so a mutation here can never leak into the next
// spec file or the next run of this one. Origin must match the server's own
// host (see 'Task API needs Origin header' in .agent-context/memory).
test.afterEach(async ({ request, baseURL }) => {
  await request.patch('/api/settings/workspace.layout', {
    headers: { Origin: baseURL ?? 'http://localhost:13199' },
    data: { value: '' },
  })
})

test('the Zentrale is the default page with the nine widgets', async ({ page }) => {
  await page.goto('/', { waitUntil: 'domcontentloaded' })
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Zentrale')
  for (const id of ['live-work', 'agents', 'routines', 'hub', 'kontor', 'github', 'pipeline', 'memory', 'cost-today'])
    await expect(page.getByTestId(`workspace-tile-${id}`)).toBeVisible()
})

test('a moved tile stays moved after a reload', async ({ page }) => {
  await page.goto('/', { waitUntil: 'domcontentloaded' })
  await page.getByTestId('workspace-edit-toggle').click()
  await page.getByTestId('workspace-tile-cost-today').focus()
  // Each keypress saves through a fire-and-forget fetch (useWorkspace's
  // `write`, chained one save after the other), so reloading right after can
  // race an in-flight PATCH and revert to the still-unsaved value (observed
  // flaky) — wait for each save in turn before the next input or the reload.
  const patched = () => page.waitForResponse(resp =>
    resp.url().includes('/api/settings/workspace.layout') && resp.request().method() === 'PATCH')
  const firstSaved = patched()
  await page.keyboard.press('Shift+ArrowUp') // 3×3 → 3×2 frees row 12
  await firstSaved
  const secondSaved = patched()
  await page.keyboard.press('ArrowDown')
  await secondSaved
  await page.getByTestId('workspace-edit-toggle').click()
  await page.reload({ waitUntil: 'domcontentloaded' })
  await expect(page.getByTestId('workspace-tile-cost-today')).toHaveAttribute('style', /--row: 11/)
})

test('"/" opens the Kontor tile and Escape closes it', async ({ page }) => {
  await page.goto('/', { waitUntil: 'domcontentloaded' })
  // Wait for the collapsed tile — the Kontor widget is behind an async chunk,
  // and pressing '/' before it mounts its own key listener is a race.
  await expect(page.getByTestId('kontor-collapsed')).toBeVisible()
  await page.locator('body').press('/')
  await expect(page.getByTestId('kontor-expanded')).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.getByTestId('kontor-expanded')).toBeHidden()
})

// R11: jsdom (the component-test environment) never lays out real pixels, so
// the CSS that keeps the resize handle clickable — its position, its stacking
// order above the tile's own pointer-events:none overlay — can only be
// exercised by a real browser drag.
test('a pointer drag on the resize handle resizes the cost-today tile and the new size persists', async ({ page }) => {
  await page.goto('/', { waitUntil: 'domcontentloaded' })
  await page.getByTestId('workspace-edit-toggle').click()

  // cost-today sits at the bottom of a 12-row grid that is commonly taller
  // than the viewport in edit mode — scroll it into view first, so the raw
  // page.mouse coordinates below land inside the actual visible viewport.
  const handleLocator = page.getByTestId('workspace-resize-cost-today')
  await handleLocator.scrollIntoViewIfNeeded()

  const grid = await page.getByTestId('workspace-grid').boundingBox()
  const handle = await handleLocator.boundingBox()
  if (!grid || !handle)
    throw new Error('workspace grid or resize handle did not render')

  // Default layout: cost-today spans rows 10-12 of 12 — the page's last row.
  // cellAt() (gridGeometry.ts) maps a pointer position to a row via
  // floor((y - top) / (rowHeight + gap)) + 1, so moving the pointer down by
  // roughly one row's pitch from row 12's band reaches row 13 — one row past
  // the grid, which growing the grid allows. Rather than trust one blind
  // pixel offset (observed flaky against sub-pixel layout variance), the drag
  // watches the live ghost preview (workspace-ghost carries the same
  // --row-span the app renders) and stops the moment it shows the target
  // size — the same feedback a real user watches while dragging.
  const rowPitch = (grid.height + 12) / 12
  const x = handle.x + handle.width / 2
  const startY = handle.y + handle.height / 2
  const ghost = page.getByTestId('workspace-ghost')

  // The resize saves through a fire-and-forget fetch (useWorkspace's `write`),
  // so the drop and its PATCH are two different moments — reloading before the
  // request lands reverts to the still-unsaved server value (observed flaky:
  // the ghost proves the drag itself always lands correctly, but the reload
  // assertion below does not). Arm the wait before the drop that triggers it.
  const saved = page.waitForResponse(resp =>
    resp.url().includes('/api/settings/workspace.layout') && resp.request().method() === 'PATCH')

  await page.mouse.move(x, startY)
  await page.mouse.down()
  let reachedTarget = false
  for (let i = 1; i <= 20 && !reachedTarget; i++) {
    await page.mouse.move(x, startY + i * (rowPitch / 4))
    const style = await ghost.getAttribute('style')
    reachedTarget = !!style && /--row-span: 4\b/.test(style)
  }
  await page.mouse.up()
  if (!reachedTarget)
    throw new Error('drag never reached a 4-row ghost preview')

  await expect(page.getByTestId('workspace-tile-cost-today')).toHaveAttribute('style', /--row-span: 4\b/)
  await saved

  await page.getByTestId('workspace-edit-toggle').click()
  await page.reload({ waitUntil: 'domcontentloaded' })
  await expect(page.getByTestId('workspace-tile-cost-today')).toHaveAttribute('style', /--row-span: 4\b/)
})
