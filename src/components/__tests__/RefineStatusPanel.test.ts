import { mount } from '@vue/test-utils'
import { expect, it } from 'vitest'
import RefineStatusPanel from '../RefineStatusPanel.vue'

it('shows a running badge when status is running', () => {
  const w = mount(RefineStatusPanel, { props: { status: 'running', error: null, lastOutput: '' } })
  expect(w.text().toLowerCase()).toContain('running')
})

it('shows the last output when done', () => {
  const w = mount(RefineStatusPanel, { props: { status: 'done', error: null, lastOutput: 'analysis result' } })
  expect(w.text()).toContain('analysis result')
})

it('shows the error when failed', () => {
  const w = mount(RefineStatusPanel, { props: { status: 'failed', error: 'claude exited: boom', lastOutput: '' } })
  expect(w.text()).toContain('claude exited: boom')
})

it('renders nothing for idle with no output', () => {
  const w = mount(RefineStatusPanel, { props: { status: 'idle', error: null, lastOutput: '' } })
  expect(w.text().trim()).toBe('')
})
