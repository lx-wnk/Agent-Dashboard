import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import GitHubPanel from './GitHubPanel.vue'

function stubFetch(status: number, body: unknown) {
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })))
}

afterEach(() => vi.unstubAllGlobals())

const others = (state: string) => ['loading', 'notAsked', 'denied', 'empty', 'failed'].filter(s => s !== state)

async function mountPanel() {
  const wrapper = mount(GitHubPanel)
  await flushPromises()
  return wrapper
}

function expectOnly(wrapper: ReturnType<typeof mount>, state: string) {
  expect(wrapper.find(`[data-testid="cockpit-github-${state}"]`).exists()).toBe(true)
  for (const other of others(state))
    expect(wrapper.findAll(`[data-testid="cockpit-github-${other}"]`)).toHaveLength(0)
}

describe('gitHubPanel', () => {
  it('reports a 503 as not configured, never as an empty repository list', async () => {
    stubFetch(503, { error: 'github is not configured' })
    expectOnly(await mountPanel(), 'notAsked')
  })

  it('reports a 403 as denied, with the server reason, and shows no rows', async () => {
    stubFetch(403, { error: 'capability denied: github.read' })
    const wrapper = await mountPanel()
    expectOnly(wrapper, 'denied')
    expect(wrapper.get('[data-testid="cockpit-github-denied"]').text()).toContain('capability denied: github.read')
  })

  it('tells a confirmed-empty answer apart from a refusal', async () => {
    stubFetch(200, { repos: [{ repo: 'lx-wnk/agent-dashboard', pullRequests: [] }] })
    expectOnly(await mountPanel(), 'empty')
  })

  it('reports a 500 as failed, which is not the same as denied', async () => {
    stubFetch(500, { error: 'upstream exploded' })
    const wrapper = await mountPanel()
    expectOnly(wrapper, 'failed')
    expect(wrapper.get('[data-testid="cockpit-github-failed"]').text()).toContain('upstream exploded')
  })

  it('renders the pull requests it was given', async () => {
    stubFetch(200, {
      repos: [{
        repo: 'lx-wnk/agent-dashboard',
        pullRequests: [{ number: 42, title: 'Add the cockpit', author: 'lx-wnk', url: 'https://example.test/42', draft: false, updatedAt: '2026-09-01T10:00:00Z' }],
      }],
    })
    const wrapper = await mountPanel()
    for (const state of ['loading', 'notAsked', 'denied', 'empty', 'failed'])
      expect(wrapper.findAll(`[data-testid="cockpit-github-${state}"]`)).toHaveLength(0)
    expect(wrapper.get('[data-testid="cockpit-github-pr-42"]').text()).toContain('Add the cockpit')
  })
})
