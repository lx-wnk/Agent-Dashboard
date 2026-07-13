import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { axe } from '../../utils/testA11y'
import ChartDataTable from './ChartDataTable.vue'

const columns = [
  { key: 'source', label: 'Source' },
  { key: 'target', label: 'Target' },
  { key: 'value', label: 'Value' },
]

const rows = [
  { source: 'Read', target: 'Edit', value: 3 },
  { source: 'Edit', target: 'Bash', value: 1 },
]

describe('chartDataTable', () => {
  it('renders a caption and one th per column', () => {
    const wrapper = mount(ChartDataTable, { props: { caption: 'Tool flow', columns, rows } })
    expect(wrapper.find('caption').text()).toBe('Tool flow')
    const headers = wrapper.findAll('th')
    expect(headers).toHaveLength(columns.length)
    expect(headers[0]?.attributes('scope')).toBe('col')
  })

  it('renders one tr per row', () => {
    const wrapper = mount(ChartDataTable, { props: { caption: 'Tool flow', columns, rows } })
    expect(wrapper.findAll('tbody tr')).toHaveLength(rows.length)
    expect(wrapper.text()).toContain('Read')
    expect(wrapper.text()).toContain('Bash')
  })

  it('renders an empty-state row when rows is empty', () => {
    const wrapper = mount(ChartDataTable, { props: { caption: 'Tool flow', columns, rows: [] } })
    const cells = wrapper.findAll('tbody tr td')
    expect(cells).toHaveLength(1)
    expect(cells[0]?.text()).toBe('No data')
  })

  it('table is visually hidden until the toggle is activated', async () => {
    const wrapper = mount(ChartDataTable, { props: { caption: 'Tool flow', columns, rows } })
    expect(wrapper.find('table').classes()).toContain('sr-only')

    const button = wrapper.find('button')
    expect(button.attributes('aria-expanded')).toBe('false')

    await button.trigger('click')

    expect(wrapper.find('table').classes()).not.toContain('sr-only')
    expect(button.attributes('aria-expanded')).toBe('true')
  })

  it('has no axe violations, hidden and revealed', async () => {
    const wrapper = mount(ChartDataTable, {
      props: { caption: 'Tool flow', columns, rows },
      attachTo: document.body,
    })
    expect(await axe(wrapper.element as HTMLElement)).toHaveNoViolations()

    await wrapper.find('button').trigger('click')
    expect(await axe(wrapper.element as HTMLElement)).toHaveNoViolations()

    wrapper.unmount()
  })
})
