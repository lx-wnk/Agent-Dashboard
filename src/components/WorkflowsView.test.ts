import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import WorkflowsView from './WorkflowsView.vue'

function emptySankey() {
  return { nodes: [], links: [], meta: { sessionCount: 0, callCount: 0 } }
}

describe('WorkflowsView', () => {
  it('renders the four tab buttons', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => emptySankey(),
    }))
    const wrapper = mount(WorkflowsView)
    await flushPromises()
    for (const label of ['Sankey', 'Session DAG', 'Spawn Tree', 'Co-occurrence'])
      expect(wrapper.text()).toContain(label)
  })

  it('shows the empty sankey state when the backend returns no data', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => emptySankey(),
    }))
    const wrapper = mount(WorkflowsView)
    await flushPromises()
    expect(wrapper.text()).toContain('No tool calls found in this window.')
  })

  it('disables the DAG tab when sessionId is empty', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => emptySankey(),
    }))
    const wrapper = mount(WorkflowsView)
    await flushPromises()
    const dagButton = wrapper.findAll('button[role="tab"]').find(b => b.text() === 'Session DAG')
    expect(dagButton).toBeTruthy()
    expect(dagButton!.attributes('disabled')).toBeDefined()
  })
})
