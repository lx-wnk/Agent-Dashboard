import type { Page, Route } from '@playwright/test'
import { expect, test } from '@playwright/test'
import { stubAuthDisabled, stubEmptyStream, stubJson } from './helpers'

const ONBOARDING_STATUS = {
  completed: false,
  cliInstalled: true,
  cliVersion: '2.1.0',
  mcpRegistered: false,
}

const REGISTER_MCP_RESULT = {
  ok: true,
  command: 'claude mcp add --scope user --transport http agent-dashboard http://127.0.0.1:13120/api/mcp --header "Authorization: Bearer tok"',
}

const SESSION = {
  sessionId: 'sess-onboard-1',
  projectPath: '/repo/agent-dashboard',
  projectName: 'agent-dashboard',
  lastModified: new Date().toISOString(),
  model: 'claude-opus-4',
  firstPrompt: 'Fix the login bug',
  lastResponse: null,
  totalInputTokens: 100,
  totalOutputTokens: 200,
  costEstimate: 0.05,
  isRunning: false,
}

/**
 * Stubs every endpoint App.vue fetches on mount so the shell renders without
 * reaching the real backend — mirrors dashboard.spec.ts / question-band.spec.ts.
 */
async function stubAppShell(page: Page) {
  await stubAuthDisabled(page)
  await stubJson(page, '/api/config', { mcpServerName: 'agent-dashboard', mcpEndpoint: '/api/mcp', homedir: '/home/u', scriptPath: '/x' })
  await stubJson(page, '/api/agents', [])
  await stubEmptyStream(page, '/api/agents/stream')
  await stubJson(page, '/api/projects', [])
  await stubEmptyStream(page, '/api/projects/stream')
  await stubJson(page, '/api/tasks', [])
  await stubEmptyStream(page, '/api/tasks/stream')
  await stubJson(page, '/api/spawners', [])
  await stubEmptyStream(page, '/api/spawners/stream')
  await stubJson(page, '/api/usage', { windows: [], accounts: [] })
  await stubJson(page, '/api/cost/summary', { totalUsd: 0 })
}

/**
 * Serves /api/onboarding/status for both the initial GET (fetchStatus) and the
 * completion PATCH (complete()) — same path, different method — and records
 * every PATCH body into `patched`.
 */
async function stubOnboardingStatus(page: Page, patched: unknown[]) {
  await page.route(/\/api\/onboarding\/status(\?.*)?$/, async (route: Route) => {
    const req = route.request()
    if (req.method() === 'PATCH') {
      patched.push(req.postDataJSON())
      await route.fulfill({ status: 200, contentType: 'application/json', body: '{}' })
      return
    }
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(ONBOARDING_STATUS),
    })
  })
}

test.describe('onboarding flow', () => {
  test('connecting MCP and a session completes onboarding', async ({ page }) => {
    await stubAppShell(page)
    const patched: unknown[] = []
    await stubOnboardingStatus(page, patched)

    let registerMcpCalled = false
    await page.route('/api/onboarding/register-mcp', async (route: Route) => {
      registerMcpCalled = true
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(REGISTER_MCP_RESULT),
      })
    })

    await stubJson(page, '/api/sessions', [SESSION])

    let spawnBody: unknown = null
    await page.route('/api/agents/spawn', async (route: Route) => {
      spawnBody = route.request().postDataJSON()
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ pid: 4242 }) })
    })

    await page.goto('/')

    const flow = page.getByTestId('onboarding-flow')
    await expect(flow).toBeVisible()
    await expect(page.getByTestId('onboarding-step-cli')).toContainText('v2.1.0 detected')

    // Step 2: connect the dashboard's MCP registration.
    await page.getByTestId('onboarding-connect-mcp').click()
    await expect.poll(() => registerMcpCalled).toBe(true)
    await expect(page.getByTestId('onboarding-step-mcp')).toContainText('Connected')

    // Step 3: the stubbed session is listed and can be made controllable.
    await expect(page.getByTestId('onboarding-step-session')).toContainText('Fix the login bug')
    await page.getByTestId('onboarding-session-connect').click()

    await expect.poll(() => spawnBody).toEqual({
      cwd: SESSION.projectPath,
      prompt: 'continue',
      resumeSessionId: SESSION.sessionId,
      enableChannel: true,
    })

    // connectSession() calls complete() on success, then closes the flow.
    await expect.poll(() => patched).toEqual([{ completed: true }])
    await expect(flow).toBeHidden()
  })

  test('skip completes onboarding without connecting anything', async ({ page }) => {
    await stubAppShell(page)
    const patched: unknown[] = []
    await stubOnboardingStatus(page, patched)
    await stubJson(page, '/api/sessions', [])

    await page.goto('/')

    const flow = page.getByTestId('onboarding-flow')
    await expect(flow).toBeVisible()

    await page.getByTestId('onboarding-skip').click()

    await expect.poll(() => patched).toEqual([{ completed: true }])
    await expect(flow).toBeHidden()
  })
})
