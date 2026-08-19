import type { Page } from '@playwright/test'
import { expect, test } from '@playwright/test'
import { stubAuthDisabled, stubEmptyStream, stubJson } from './helpers'

function agentWithTools() {
  return {
    pid: 5151,
    sessionId: 'sess-xyz',
    provider: 'claude',
    projectName: 'agent-dashboard',
    projectPath: '/repo/agent-dashboard',
    cwd: '/repo/agent-dashboard',
    status: 'active',
    working: true,
    lastActivity: new Date().toISOString(),
    uptime: 300,
    liveInjectable: true,
    channelAvailable: true,
    tokenUsage: { inputTokens: 0, outputTokens: 0, cacheReadTokens: 0, cacheCreationTokens: 0 },
    costEstimate: 0,
    // The modal's Details panel renders eagerly (its tab is selected by default)
    // and formats these two figures; formatCost() calls .toFixed() unguarded, so
    // omitting them throws during render and the dialog never mounts.
    cacheCreationCostEstimate: 0,
    cacheReadCostEstimate: 0,
    healthScore: 80,
    lastTools: [{ name: 'Read', detail: 'src/main.ts' }],
    tasks: [],
    subagents: [],
  }
}

async function stubAgents(page: Page) {
  await stubJson(page, '/api/agents', [agentWithTools()])
  await stubEmptyStream(page, '/api/agents/stream')
}

test.describe('agent detail modal', () => {
  test.beforeEach(async ({ page }) => {
    await stubAuthDisabled(page)
    await stubAgents(page)
    // The modal's chat stream polls these on mount; stub them so the panel stays
    // quiet (no error toast, no 503 noise) and the run is deterministic.
    await stubJson(page, '/api/agents/sess-xyz/output', { messages: [] })
    await stubJson(page, '/api/agents/sess-xyz/replies', { replies: [] })
  })

  test('opens from the card and closes', async ({ page }) => {
    await page.goto('/')

    // The card body paints over the full-card overlay button; clicking it
    // bubbles to the AppCard @click that emits select and opens the modal.
    await page.getByTestId('agent-card-body').click()

    const dialog = page.getByRole('dialog')
    await expect(dialog).toBeVisible()
    await expect(dialog.getByText('agent-dashboard')).toBeVisible()

    // The transcript is the modal's only view: the bottom drawer is gone, the
    // terminal moved to the agent card, and with the waterfall removed there is
    // no tablist left to switch between.
    await expect(dialog.getByRole('tablist')).toHaveCount(0)
    await expect(dialog.getByRole('tab')).toHaveCount(0)
    await expect(dialog.getByLabel('Chat transcript')).toBeVisible()

    await page.keyboard.press('Escape')
    await expect(dialog).toBeHidden()
  })

  test('closes via the close button', async ({ page }) => {
    await page.goto('/')

    // The card body paints over the full-card overlay button; clicking it
    // bubbles to the AppCard @click that emits select and opens the modal.
    await page.getByTestId('agent-card-body').click()
    const dialog = page.getByRole('dialog')
    await expect(dialog).toBeVisible()

    await page.locator('button[aria-label="Close"]').click()
    await expect(dialog).toBeHidden()
  })
})
