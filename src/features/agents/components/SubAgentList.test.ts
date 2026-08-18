import type { SubAgent } from '@/types'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import SubAgentList from './SubAgentList.vue'

function makeSubagent(id: string, status: 'active' | 'completed'): SubAgent {
  return {
    id,
    type: 'subagent',
    status,
    currentAction: null,
    sessionFile: `/tmp/${id}.jsonl`,
    tokensUsed: 0,
    durationSeconds: 0,
    latestOutput: '',
  }
}

describe('subAgentList', () => {
  const activeAgents = [makeSubagent('active-1', 'active'), makeSubagent('active-2', 'active')]
  const completedAgents = Array.from({ length: 8 }, (_, i) => makeSubagent(`completed-${i}`, 'completed'))
  const longList = [...activeAgents, ...completedAgents]

  it('shows active subagents without interaction and collapses completed ones behind a closed disclosure', () => {
    const w = mount(SubAgentList, { props: { subagents: longList } })

    expect(w.text()).toContain('2 active of 10')

    const details = w.get('details')
    expect(details.element.open).toBe(false)

    const openButtons = w.findAll('[data-testid="subagent-open"]')
    expect(openButtons).toHaveLength(10)
    for (const sa of activeAgents) {
      expect(details.element.contains(w.get(`[aria-label="Open subagent ${sa.id} transcript"]`).element)).toBe(false)
    }
  })

  it('reveals completed subagents when the disclosure is opened', async () => {
    const w = mount(SubAgentList, { props: { subagents: longList } })
    const details = w.get('details')
    expect(details.element.open).toBe(false)

    await w.get('[data-testid="subagent-completed-summary"]').trigger('click')
    expect(details.element.open).toBe(true)
  })

  it('emits open from a row inside the collapsed completed section', async () => {
    const w = mount(SubAgentList, { props: { subagents: longList } })
    const target = completedAgents[0]

    await w.get(`[aria-label="Open subagent ${target.id} transcript"]`).trigger('click')

    expect(w.emitted('open')?.[0]).toEqual([target])
  })

  it('leaves the disclosure open by default when completed count is at or below the threshold', () => {
    const shortList = [...activeAgents, makeSubagent('completed-only', 'completed')]
    const w = mount(SubAgentList, { props: { subagents: shortList } })

    expect(w.get('details').element.open).toBe(true)
  })

  it('scopes its own scroll instead of expanding into the shared modal region', () => {
    const w = mount(SubAgentList, { props: { subagents: longList } })
    const scroll = w.get('[data-testid="subagent-scroll"]')
    expect(scroll.classes()).toContain('overflow-y-auto')
    expect(scroll.classes()).toContain('overflow-x-hidden')
    expect(scroll.classes().some(c => c.startsWith('max-h-'))).toBe(true)
  })

  // SSE re-renders the modal every few seconds with a fresh subagents array.
  // A plain `:open="completedCount <= threshold"` binding is a one-way patch:
  // once a later render flips the computed default, Vue force-writes the
  // <details> attribute and discards whatever the user set natively via
  // clicking <summary>. This pins the fix (a local override ref) in place.
  it('keeps a user-closed disclosure closed across prop updates that flip the auto-collapse default', async () => {
    const belowThreshold = [...activeAgents, makeSubagent('completed-only', 'completed')]
    const w = mount(SubAgentList, { props: { subagents: belowThreshold } })
    expect(w.get('details').element.open).toBe(true)

    await w.get('[data-testid="subagent-completed-summary"]').trigger('click')
    expect(w.get('details').element.open).toBe(false)

    await w.setProps({ subagents: longList })
    expect(w.get('details').element.open).toBe(false)

    await w.setProps({ subagents: belowThreshold })
    expect(w.get('details').element.open).toBe(false)
  })
})
