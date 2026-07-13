import type { DAGData } from '@/sdk.generated'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import SessionDagChart from '@/features/workflows/components/visualizations/SessionDagChart.vue'

const data: DAGData = {
  nodes: [
    { id: 'u1', type: 'user', label: 'User prompt', ts: '2026-07-01T00:00:00Z' },
    { id: 'a1', type: 'assistant', label: 'Assistant turn', ts: '2026-07-01T00:00:01Z' },
    { id: 't1', type: 'tool', label: 'Read', ts: '2026-07-01T00:00:02Z' },
  ],
  links: [
    { source: 'u1', target: 'a1', kind: 'chrono' },
    { source: 'a1', target: 't1', kind: 'chrono' },
  ],
}

describe('sessionDagChart — data table', () => {
  it('renders a data-table row per node, matching the chart data', async () => {
    const wrapper = mount(SessionDagChart, { props: { data, loading: false, error: null } })
    await wrapper.find('button').trigger('click')

    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(data.nodes.length)
    expect(wrapper.text()).toContain('Assistant turn')
  })

  it('resolves the parent column from the incoming link', async () => {
    const wrapper = mount(SessionDagChart, { props: { data, loading: false, error: null } })
    await wrapper.find('button').trigger('click')

    const toolRow = wrapper.findAll('tbody tr').at(2)
    expect(toolRow?.text()).toContain('Assistant turn')
  })

  it('renders the empty-state row when there are no nodes', async () => {
    const empty: DAGData = { nodes: [], links: [] }
    const wrapper = mount(SessionDagChart, { props: { data: empty, loading: false, error: null } })
    expect(wrapper.text()).toContain('Pick a session to view its DAG.')
  })
})
