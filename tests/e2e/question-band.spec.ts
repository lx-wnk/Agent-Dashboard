import type { Page, Route } from '@playwright/test'
import { expect, test } from '@playwright/test'

async function stubAuthDisabled(page: Page) {
  await page.route('/api/me', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({ user: null, isAdmin: true, authEnabled: false }),
  }))
}

function agentWithQuestion(multiSelect: boolean) {
  return {
    pid: 4242,
    sessionId: 'sess-colour',
    provider: 'claude',
    projectName: 'agent-dashboard',
    projectPath: '/repo/agent-dashboard',
    cwd: '/repo/agent-dashboard',
    status: 'waiting',
    working: false,
    lastActivity: new Date().toISOString(),
    uptime: 120,
    liveInjectable: true,
    channelAvailable: true,
    tokenUsage: { inputTokens: 0, outputTokens: 0, cacheReadTokens: 0, cacheCreationTokens: 0 },
    costEstimate: 0,
    lastTools: [],
    tasks: [],
    subagents: [],
    pendingQuestion: {
      header: 'Colour',
      question: 'Which colour do you prefer?',
      multiSelect,
      options: [
        { index: 1, label: 'Red', description: 'A warm colour' },
        { index: 2, label: 'Green' },
        { index: 3, label: 'Blue' },
      ],
      typeSomethingIndex: 4,
      chatAboutIndex: 5,
    },
  }
}

// The review/submit screen closing out a multi-question flow. It carries no
// meta-rows, so it reaches the client on its own field rather than as a
// pendingQuestion.
function agentWithConfirm() {
  const agent = agentWithQuestion(false) as Record<string, unknown>
  delete agent.pendingQuestion
  agent.pendingConfirm = {
    question: 'Ready to submit your answers?',
    options: [
      { index: 1, label: 'Submit answers' },
      { index: 2, label: 'Cancel' },
    ],
  }
  return agent
}

async function stubAgentsWith(page: Page, makeAgent: () => unknown) {
  await page.route('/api/agents', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify([makeAgent()]),
  }))
  // The SSE stream also feeds the store; one frame carrying the same agent.
  await page.route('/api/agents/stream', route => route.fulfill({
    status: 200,
    contentType: 'text/event-stream',
    body: `data: ${JSON.stringify({ agents: [makeAgent()] })}\n\n`,
  }))
}

async function stubAgents(page: Page, multiSelect: boolean) {
  await stubAgentsWith(page, () => agentWithQuestion(multiSelect))
}

test.describe('AskUserQuestion in the needs-you band', () => {
  test('single-select: answering posts {mode:single,index:0}', async ({ page }) => {
    await stubAuthDisabled(page)
    await stubAgents(page, false)

    let posted: unknown = null
    await page.route('/api/agents/4242/answer-question', async (route: Route) => {
      posted = route.request().postDataJSON()
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ ok: true, transport: 'pty' }),
      })
    })

    await page.goto('/')

    // The answerable question card surfaces in the band.
    await expect(page.getByText('Which colour do you prefer?')).toBeVisible()
    await expect(page.getByText('Red')).toBeVisible()

    // Pick Red (first option) and send.
    await page.locator('input[type="radio"]').first().check()
    await page.locator('[data-testid="detected-send-btn"]').first().click()

    await expect.poll(() => posted).toEqual({ mode: 'single', index: 0 })
  })

  test('multi-select: answering posts {mode:multi,indices:[0,2]}', async ({ page }) => {
    await stubAuthDisabled(page)
    await stubAgents(page, true)

    let posted: unknown = null
    await page.route('/api/agents/4242/answer-question', async (route: Route) => {
      posted = route.request().postDataJSON()
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ ok: true, transport: 'pty' }),
      })
    })

    await page.goto('/')
    await expect(page.getByText('Which colour do you prefer?')).toBeVisible()

    const checkboxes = page.locator('input[type="checkbox"]')
    await checkboxes.nth(0).check()
    await checkboxes.nth(2).check()
    await page.locator('[data-testid="detected-send-btn"]').first().click()

    await expect.poll(() => posted).toEqual({ mode: 'multi', indices: [0, 2] })
  })

  test('confirm screen: submitting posts {mode:single,index:0}', async ({ page }) => {
    await stubAuthDisabled(page)
    await stubAgentsWith(page, agentWithConfirm)

    let posted: unknown = null
    await page.route('/api/agents/4242/answer-question', async (route: Route) => {
      posted = route.request().postDataJSON()
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ ok: true, transport: 'pty' }),
      })
    })

    await page.goto('/')

    await expect(page.getByText('Ready to submit your answers?')).toBeVisible()
    await expect(page.getByText('Submit answers')).toBeVisible()

    // Submit is preselected, so finishing the round is a single click.
    await page.locator('[data-testid="detected-confirm-send-btn"]').first().click()

    await expect.poll(() => posted).toEqual({ mode: 'single', index: 0 })
  })
})
