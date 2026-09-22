import type { GraphResponse } from '../../src/features/hub/graphApi'
import { expect, test } from '@playwright/test'
import { storeLayout } from './helpers'

const FOLDERS = ['Work', 'Private', 'Misc']
const NOTE_COUNT = 300
const LINK_COUNT = 30
const TWO_YEARS_MS = 2 * 365 * 24 * 60 * 60 * 1000

// 300 notes over three top-level folders (the hub's sectors), mtimes spread
// linearly over two years so freshness rings and the notes level have
// something to draw, plus a few dozen links between them.
function fakeGraph(): GraphResponse {
  const now = Date.now()
  const notes: GraphResponse['notes'] = Array.from({ length: NOTE_COUNT }, (_, i) => [
    `${FOLDERS[i % FOLDERS.length]}/note-${i}.md`,
    now - Math.round((i / NOTE_COUNT) * TWO_YEARS_MS),
  ])
  const links: GraphResponse['links'] = Array.from({ length: LINK_COUNT }, (_, i) => [i, (i + 7) % NOTE_COUNT])
  return { configured: true, notes, links }
}

test.afterEach(async ({ request, baseURL }) => {
  await storeLayout(request, baseURL, '')
})

test('+ reaches the notes level within eight presses and 0 returns to the overview', async ({ page }) => {
  // '0' flies back to fit, which animates unless reduced motion is on.
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.route('**/api/obsidian/graph', route => route.fulfill({ json: fakeGraph() }))
  await page.goto('/', { waitUntil: 'domcontentloaded' })

  const stage = page.getByTestId('hub-stage')
  await stage.focus()
  for (let i = 0; i < 8 && await stage.getAttribute('data-level') !== '2'; i++)
    await stage.press('+')
  await expect(stage).toHaveAttribute('data-level', '2')

  await stage.press('0')
  await expect(stage).toHaveAttribute('data-level', '0')
})

test('L lists the agents and recently touched notes, and Escape dismisses the note card, then the list', async ({ page }) => {
  await page.route('**/api/obsidian/graph', route => route.fulfill({ json: fakeGraph() }))
  await page.goto('/', { waitUntil: 'domcontentloaded' })

  const stage = page.getByTestId('hub-stage')
  const list = page.getByRole('dialog', { name: 'Zentrale as a list' })
  await stage.press('l')
  await expect(list).toBeVisible()
  await expect(list.getByText('Agents')).toBeVisible()
  await expect(list.getByText('Recently touched')).toBeVisible()

  const firstNote = list.getByTestId('hub-list-note').first()
  const title = (await firstNote.locator('span').first().textContent())?.trim()
  await firstNote.click()
  // Picking a note closes the list itself (HubWidget's pickNoteFromList), so
  // the card is checked on its own note-card dialog, keyed by the note's title.
  const card = page.getByRole('dialog', { name: title!, exact: true })
  await expect(card).toBeVisible()

  await page.keyboard.press('Escape')
  await expect(card).toBeHidden()

  await stage.press('l')
  await expect(list).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(list).toBeHidden()
})

test('the Connect Obsidian notice renders when the vault is not configured', async ({ page }) => {
  await page.route('**/api/obsidian/graph', route => route.fulfill({ json: { configured: false } }))
  await page.goto('/', { waitUntil: 'domcontentloaded' })
  await expect(page.getByTestId('hub-graph-notice')).toContainText('Connect Obsidian to see your notes here.')
})

test('F widens the hub tile to the page width and back', async ({ page }) => {
  await page.route('**/api/obsidian/graph', route => route.fulfill({ json: fakeGraph() }))
  await page.goto('/', { waitUntil: 'domcontentloaded' })

  const stage = page.getByTestId('hub-stage')
  const grid = page.getByTestId('workspace-grid')
  const hub = page.getByTestId('hub')
  const gridWidth = (await grid.boundingBox())!.width

  await stage.press('f')
  await expect.poll(async () => (await hub.boundingBox())!.width).toBeGreaterThanOrEqual(gridWidth * 0.95)

  await stage.press('f')
  await expect.poll(async () => (await hub.boundingBox())!.width).toBeLessThan(gridWidth * 0.95)
})

// M6: the queue that docks into the hub's top edge must stay inside the hub's
// box even at the widget's minimum size (6×6) and a small viewport.
test('the docked needs-you queue stays inside a 6×6 hub at 1280×700', async ({ page, request, baseURL }) => {
  await storeLayout(request, baseURL, JSON.stringify({
    version: 1,
    pages: [{ id: 'zentrale', title: 'Zentrale', tiles: [{ widget: 'hub', col: 1, row: 1, colSpan: 6, rowSpan: 6 }] }],
  }))
  await page.route('**/api/obsidian/graph', route => route.fulfill({ json: fakeGraph() }))
  await page.setViewportSize({ width: 1280, height: 700 })
  await page.goto('/', { waitUntil: 'domcontentloaded' })

  const hubBox = (await page.getByTestId('hub').boundingBox())!
  const queueBox = (await page.getByTestId('needs-you').boundingBox())!
  expect(queueBox.x).toBeGreaterThanOrEqual(hubBox.x - 0.5)
  expect(queueBox.y).toBeGreaterThanOrEqual(hubBox.y - 0.5)
  expect(queueBox.x + queueBox.width).toBeLessThanOrEqual(hubBox.x + hubBox.width + 0.5)
  expect(queueBox.y + queueBox.height).toBeLessThanOrEqual(hubBox.y + hubBox.height + 0.5)
})
