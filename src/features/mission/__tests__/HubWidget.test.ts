import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'

const agents = ref([
  {
    pid: 100,
    status: 'waiting',
    projectName: 'dashboard-app',
    working: false,
    sessionId: 's1',
    provider: 'anthropic',
    projectPath: '/path/1',
    cwd: '/path/1',
    entrypoint: 'cli',
    uptime: 0,
    lastActivity: '2026-01-01T00:00:00Z',
    lastTools: [],
    tasks: [],
    subagents: [],
    tokenUsage: { input: 0, output: 0, cacheCreation: 0, cacheRead: 0 },
    costEstimate: 0,
    cacheCreationCostEstimate: 0,
    cacheReadCostEstimate: 0,
    healthScore: 0,
    conversationTurns: 0,
    toolCounts: {},
    channelAvailable: false,
  },
  {
    pid: 101,
    status: 'active',
    projectName: 'kontor-hub',
    working: true,
    sessionId: 's2',
    provider: 'anthropic',
    projectPath: '/path/2',
    cwd: '/path/2',
    entrypoint: 'cli',
    uptime: 0,
    lastActivity: '2026-01-01T00:00:00Z',
    lastTools: [],
    tasks: [],
    subagents: [],
    tokenUsage: { input: 0, output: 0, cacheCreation: 0, cacheRead: 0 },
    costEstimate: 0,
    cacheCreationCostEstimate: 0,
    cacheReadCostEstimate: 0,
    healthScore: 0,
    conversationTurns: 0,
    toolCounts: {},
    channelAvailable: false,
  },
  {
    pid: 102,
    status: 'idle',
    projectName: 'web-app',
    working: false,
    sessionId: 's3',
    provider: 'anthropic',
    projectPath: '/path/3',
    cwd: '/path/3',
    entrypoint: 'cli',
    uptime: 0,
    lastActivity: '2026-01-01T00:00:00Z',
    lastTools: [],
    tasks: [],
    subagents: [],
    tokenUsage: { input: 0, output: 0, cacheCreation: 0, cacheRead: 0 },
    costEstimate: 0,
    cacheCreationCostEstimate: 0,
    cacheReadCostEstimate: 0,
    healthScore: 0,
    conversationTurns: 0,
    toolCounts: {},
    channelAvailable: false,
  },
])

vi.mock('@/features/agents', () => ({
  useAgents: () => ({ agents }),
}))
vi.mock('../components/NeedsYouQueue.vue', () => ({
  default: {
    props: ['variant'],
    template: '<div data-testid="stub-queue">{{ variant }}</div>',
  },
}))

const { default: HubWidget } = await import('../components/HubWidget.vue')

describe('hubWidget', () => {
  it('docks the queue and lists agents, waiting first', () => {
    const w = mount(HubWidget)
    expect(w.get('[data-testid="stub-queue"]').text()).toBe('docked')
    const rows = w.findAll('[data-testid="hub-agent"]').map(r => r.text())
    expect(rows[0]).toContain('waiting')
    expect(rows[1]).toContain('working')
    expect(rows[2]).toContain('idle')
    w.unmount()
  })
})
