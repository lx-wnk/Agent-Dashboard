import { expect, test } from '@playwright/test'
import { stubAuthDisabled, stubEmptyStream, stubJson } from './helpers'

function apiKeyFixture() {
  return {
    id: 'key-1',
    name: 'CI pipeline key',
    scopes: ['tasks:read', 'tasks:write', 'pipeline:control'],
    active: true,
    userId: null,
    createdAt: new Date().toISOString(),
    lastUsedAt: null,
  }
}

function spawnerFixture() {
  return {
    id: 'claude-code',
    name: 'Claude Code',
    slug: 'claude-code',
    command: 'claude',
    args: [],
    env: {},
    adapterType: 'claude',
    adapterConfig: {},
    builtIn: true,
    isDefault: true,
    createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(),
  }
}

function pipelineConfigFixture() {
  return {
    maxParallelOrchestrators: 4,
    stageTimeoutSeconds: 1800,
    maxAutoRetries: 3,
    retryBackoffSeconds: 30,
    stageModels: {
      implementation: 'claude-sonnet-4-5',
      self_review: 'claude-sonnet-4-5',
      finalization: 'claude-haiku-4-5',
    },
    stageSpawners: {
      implementation: 'claude-code',
      self_review: 'claude-code',
      finalization: 'claude-code',
    },
  }
}

async function openSettings(page: import('@playwright/test').Page) {
  await page.goto('/')
  await page.locator('button[aria-label="Settings"]').click()
  await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible()
}

/**
 * The dashboard's own left sidebar also has a "Pipeline" nav item, so
 * section buttons must be scoped to the settings modal's own <nav>.
 */
function settingsSectionNav(page: import('@playwright/test').Page) {
  return page.locator('nav').filter({ has: page.getByRole('heading', { name: 'Settings' }) })
}

test.describe('Settings modal', () => {
  test.beforeEach(async ({ page }) => {
    await stubAuthDisabled(page)
    await stubJson(page, '/api/agents', [])
    await stubEmptyStream(page, '/api/agents/stream')
    await stubJson(page, '/api/config', { mcpServerName: 'agent-dashboard', mcpEndpoint: '/api/mcp' })
    await stubJson(page, '/api/spawners', [spawnerFixture()])
  })

  test('create API key flow reveals the token and the claude mcp add CLI command', async ({ page }) => {
    await stubJson(page, '/api/settings/api-keys', [])

    let posted: unknown = null
    await page.route('/api/settings/api-keys', async (route) => {
      if (route.request().method() !== 'POST') {
        await route.fulfill({ status: 200, contentType: 'application/json', body: '[]' })
        return
      }
      posted = route.request().postDataJSON()
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ key: apiKeyFixture(), token: 'tok_test_123' }),
      })
    })

    await openSettings(page)
    await settingsSectionNav(page).getByRole('button', { name: /API Keys/ }).click()
    await expect(page.getByText('No API keys yet. Create one to allow MCP clients to connect.')).toBeVisible()

    await page.getByRole('button', { name: '+ Add Key' }).click()
    await expect(page.getByRole('heading', { name: 'Create API Key' })).toBeVisible()

    await page.locator('#key-name').fill('CI pipeline key')
    await page.locator('#key-group').selectOption('developer')
    await page.getByRole('button', { name: 'Create Key' }).click()

    await expect.poll(() => posted).toEqual({
      name: 'CI pipeline key',
      scopes: ['tasks:read', 'tasks:write', 'pipeline:control'],
    })

    await expect(page.getByRole('heading', { name: 'Your new API key' })).toBeVisible()

    const cliBlock = page.getByRole('region', { name: 'CLI command' })
    await expect(cliBlock).toBeVisible()
    await expect(cliBlock).toContainText('claude mcp add')
    await expect(cliBlock).toContainText('tok_test_123')
    await expect(page.locator('button[aria-label="Copy CLI command"]')).toBeVisible()

    await page.getByRole('button', { name: 'Done — I have saved the token' }).click()
    await expect(page.getByRole('heading', { name: 'Your new API key' })).toBeHidden()
  })

  test('Spawners section lists configured spawners', async ({ page }) => {
    await stubJson(page, '/api/settings/api-keys', [])

    await openSettings(page)
    await settingsSectionNav(page).getByRole('button', { name: /Spawners/ }).click()

    await expect(page.getByRole('table').getByText('Claude Code')).toBeVisible()
    await expect(page.getByRole('button', { name: '+ New Spawner' })).toBeVisible()
  })

  test('Pipeline section reads maxParallelOrchestrators and stageTimeoutSeconds from config', async ({ page }) => {
    await stubJson(page, '/api/settings/api-keys', [])
    await stubJson(page, '/api/pipeline/config', pipelineConfigFixture())

    await openSettings(page)
    await settingsSectionNav(page).getByRole('button', { name: /Pipeline/ }).click()

    await expect(page.locator('#pc-parallel')).toHaveValue('4')
    await expect(page.locator('#pc-timeout')).toHaveValue('1800')
  })
})
