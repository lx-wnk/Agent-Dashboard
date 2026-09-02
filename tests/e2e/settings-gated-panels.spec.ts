import type { Page } from '@playwright/test'
import { expect, test } from '@playwright/test'
import { stubAuthDisabled, stubEmptyStream, stubJson } from './helpers'

// Mirrors MemorySpace (== ResourceView, kind memory_space) as read by
// MemorySettings.vue / useMemory.ts.
function memorySpaceFixture(overrides: Partial<Record<string, unknown>> = {}) {
  return {
    id: 's1',
    kind: 'memory_space',
    slug: 'project-notes',
    name: 'Project notes',
    scopeKind: 'global',
    scopeRef: '',
    nodeId: 'local',
    state: 'enabled',
    version: '',
    origin: 'local',
    originRef: '',
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

// Mirrors MemoryEntryHit as answered by GET /api/memory/entries.
function memoryEntryFixture(overrides: Partial<Record<string, unknown>> = {}) {
  return {
    id: 'e1',
    spaceId: 's1',
    summary: 'The dashboard binds to 127.0.0.1',
    content: 'Long form content.',
    kind: 'fact',
    confidence: 0.9,
    createdAt: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

// Mirrors ResourceView as answered by GET /api/resources.
function resourceFixture(overrides: Partial<Record<string, unknown>> = {}) {
  return {
    id: 'r1',
    kind: 'application',
    slug: 'obsidian',
    name: 'Obsidian',
    scopeKind: 'global',
    scopeRef: '',
    nodeId: 'local',
    state: 'enabled',
    version: '1.0.0',
    origin: 'builtin',
    originRef: 'builtin:obsidian',
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-02T00:00:00Z',
    ...overrides,
  }
}

async function openSettings(page: Page) {
  await page.goto('/')
  await page.locator('button[aria-label="Settings"]').click()
  await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible()
}

/**
 * The dashboard's own left sidebar also has a "Pipeline" nav item, so
 * section buttons must be scoped to the settings modal's own <nav>.
 */
function settingsSectionNav(page: Page) {
  return page.locator('nav').filter({ has: page.getByRole('heading', { name: 'Settings' }) })
}

async function openMemoryPanel(page: Page) {
  await openSettings(page)
  await settingsSectionNav(page).getByRole('button', { name: /Memory/ }).click()
}

async function openRegistryPanel(page: Page) {
  await openSettings(page)
  await settingsSectionNav(page).getByRole('button', { name: /Registry/ }).click()
}

test.describe('Settings gated panels', () => {
  test.beforeEach(async ({ page }) => {
    await stubAuthDisabled(page)
    await stubJson(page, '/api/agents', [])
    await stubEmptyStream(page, '/api/agents/stream')
    await stubJson(page, '/api/config', { mcpServerName: 'agent-dashboard', mcpEndpoint: '/api/mcp' })
    await stubJson(page, '/api/spawners', [])
  })

  test('Memory: denied renders the server message, not an empty table', async ({ page }) => {
    await stubJson(page, '/api/memory/spaces', { error: 'memory.read is not granted in this scope' }, 403)

    await openMemoryPanel(page)

    const denied = page.getByTestId('memory-denied')
    await expect(denied).toBeVisible()
    await expect(denied).toContainText('memory.read is not granted in this scope')
    await expect(page.getByTestId('memory-empty')).toHaveCount(0)
  })

  test('Memory: confirmed empty is distinct from not asked yet', async ({ page }) => {
    await stubJson(page, '/api/memory/spaces', [memorySpaceFixture()])
    await stubJson(page, '/api/memory/entries', [])

    await openMemoryPanel(page)
    await expect(page.getByTestId('memory-space-s1')).toBeVisible()

    // Before any search: "not asked yet", never the confirmed-empty sentence.
    await expect(page.getByTestId('memory-entries-unsearched')).toHaveText('No search has been run yet.')
    await expect(page.getByTestId('memory-entries-empty')).toHaveCount(0)

    await page.getByTestId('memory-search-submit').click()

    // After the search answers with no hits: confirmed empty, and the
    // "not asked yet" sentence must be gone — they are not the same blank list.
    await expect(page.getByTestId('memory-entries-empty')).toHaveText('No entries matched this search.')
    await expect(page.getByTestId('memory-entries-unsearched')).toHaveCount(0)
  })

  test('Memory: browsing a space fetches by spaceId and tells browse-empty apart from search-empty', async ({ page }) => {
    await stubJson(page, '/api/memory/spaces', [
      memorySpaceFixture({ id: 's1', slug: 'has-entries' }),
      memorySpaceFixture({ id: 's2', slug: 'no-entries' }),
    ])

    let lastEntriesUrl = ''
    await page.route(/\/api\/memory\/entries(\?.*)?$/, async (route) => {
      lastEntriesUrl = route.request().url()
      const spaceId = new URL(lastEntriesUrl).searchParams.get('spaceId')
      const hits = spaceId === 's1' ? [memoryEntryFixture({ id: 'e1', spaceId: 's1' })] : []
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(hits) })
    })

    await openMemoryPanel(page)
    await expect(page.getByTestId('memory-space-s1')).toBeVisible()

    await page.getByTestId('memory-space-browse-s1').click()
    await expect(page.getByTestId('memory-entry-e1')).toBeVisible()
    expect(lastEntriesUrl).toContain('spaceId=s1')
    await expect(page.getByTestId('memory-entries-browse-empty')).toHaveCount(0)

    await page.getByTestId('memory-space-browse-s2').click()
    await expect(page.getByTestId('memory-entries-browse-empty')).toHaveText('This space has no entries yet.')
    await expect(page.getByTestId('memory-entries-empty')).toHaveCount(0)
    expect(lastEntriesUrl).toContain('spaceId=s2')
  })

  test('Memory: a failed search is distinct from a denied one', async ({ page }) => {
    await stubJson(page, '/api/memory/spaces', [memorySpaceFixture()])
    await stubJson(page, '/api/memory/entries', { error: 'Internal search index error' }, 500)

    await openMemoryPanel(page)
    await expect(page.getByTestId('memory-space-s1')).toBeVisible()

    await page.getByTestId('memory-search-submit').click()

    const searchError = page.getByTestId('memory-search-error')
    await expect(searchError).toBeVisible()
    await expect(searchError).toContainText('Internal search index error')
    await expect(page.getByTestId('memory-search-denied')).toHaveCount(0)
    await expect(page.getByTestId('memory-denied')).toHaveCount(0)
  })

  test('Registry: Applications renders rows while Memory spaces answers 403', async ({ page }) => {
    await page.route(/\/api\/resources(\?.*)?$/, async (route) => {
      const kind = new URL(route.request().url()).searchParams.get('kind')
      if (kind === 'memory_space') {
        await route.fulfill({
          status: 403,
          contentType: 'application/json',
          body: JSON.stringify({ error: 'memory.read is not granted in this scope' }),
        })
        return
      }
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([resourceFixture()]) })
    })

    await openRegistryPanel(page)

    await expect(page.getByTestId('resource-row-r1')).toBeVisible()
    await expect(page.getByTestId('resource-denied')).toHaveCount(0)

    await page.getByTestId('resource-kind-memory_space').click()

    const denied = page.getByTestId('resource-denied')
    await expect(denied).toBeVisible()
    await expect(denied).toContainText('memory.read is not granted in this scope')
    await expect(page.getByTestId('resource-row-r1')).toHaveCount(0)
  })
})
