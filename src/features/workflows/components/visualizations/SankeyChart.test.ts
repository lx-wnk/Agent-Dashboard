import type { SankeyData } from '@/sdk.generated'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import SankeyChart from '@/features/workflows/components/visualizations/SankeyChart.vue'
import { axe } from '@/utils/testA11y'

const data: SankeyData = {
  nodes: [
    { id: 'Read', name: 'Read' },
    { id: 'Edit', name: 'Edit' },
    { id: 'Bash', name: 'Bash' },
  ],
  links: [
    { source: 'Read', target: 'Edit', value: 3 },
    { source: 'Edit', target: 'Bash', value: 1 },
  ],
  meta: { sessionCount: 4, callCount: 4 },
}

describe('sankeyChart — data table', () => {
  it('renders a data-table row per link, matching the chart data', async () => {
    const wrapper = mount(SankeyChart, { props: { data, loading: false, error: null } })
    await wrapper.find('button').trigger('click')

    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(data.links.length)
    expect(wrapper.text()).toContain('Read')
    expect(wrapper.text()).toContain('Bash')
  })

  it('renders the empty-state row when there are no links', async () => {
    const empty: SankeyData = { nodes: [{ id: 'Read', name: 'Read' }], links: [], meta: { sessionCount: 0, callCount: 0 } }
    const wrapper = mount(SankeyChart, { props: { data: empty, loading: false, error: null } })
    await wrapper.find('button').trigger('click')

    expect(wrapper.find('tbody tr td').text()).toBe('No data')
  })

  it('has no axe violations with the data table revealed', async () => {
    const wrapper = mount(SankeyChart, {
      props: { data, loading: false, error: null },
      attachTo: document.body,
    })
    await wrapper.find('button').trigger('click')

    expect(await axe(wrapper.element as HTMLElement)).toHaveNoViolations()

    wrapper.unmount()
  })
})
