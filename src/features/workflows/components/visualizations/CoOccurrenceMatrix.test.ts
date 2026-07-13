import type { CoOccurrenceData } from '@/sdk.generated'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import CoOccurrenceMatrix from '@/features/workflows/components/visualizations/CoOccurrenceMatrix.vue'
import { axe } from '@/utils/testA11y'

const data: CoOccurrenceData = {
  tools: ['Read', 'Edit', 'Bash'],
  matrix: [
    [4, 3, 0],
    [3, 3, 1],
    [0, 1, 2],
  ],
  lift: [
    [0, 1.5, 0],
    [1.5, 0, 0.8],
    [0, 0.8, 0],
  ],
  meta: { sessionCount: 4, truncated: false },
}

describe('coOccurrenceMatrix — data table', () => {
  it('renders a data-table row per non-zero pair, matching the chart data', async () => {
    const wrapper = mount(CoOccurrenceMatrix, { props: { data, loading: false, error: null } })
    await wrapper.find('button').trigger('click')

    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(2)
    expect(wrapper.text()).toContain('Read')
    expect(wrapper.text()).toContain('Bash')
  })

  it('renders the empty-state row when there are no co-occurring pairs', async () => {
    const empty: CoOccurrenceData = {
      tools: ['Read'],
      matrix: [[4]],
      lift: [[0]],
      meta: { sessionCount: 4, truncated: false },
    }
    const wrapper = mount(CoOccurrenceMatrix, { props: { data: empty, loading: false, error: null } })
    await wrapper.find('button').trigger('click')

    expect(wrapper.find('tbody tr td').text()).toBe('No data')
  })

  it('has no axe violations with the data table revealed', async () => {
    const wrapper = mount(CoOccurrenceMatrix, {
      props: { data, loading: false, error: null },
      attachTo: document.body,
    })
    await wrapper.find('button').trigger('click')

    expect(await axe(wrapper.element as HTMLElement)).toHaveNoViolations()

    wrapper.unmount()
  })
})
