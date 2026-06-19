import { mount } from '@vue/test-utils'
import { expect, it } from 'vitest'
import RefineStatusPanel from '../RefineStatusPanel.vue'

it('shows a refining badge when status is refining', () => {
  const w = mount(RefineStatusPanel, { props: { status: 'refining', error: null, lastOutput: '' } })
  expect(w.text().toLowerCase()).toContain('refining')
})

it('shows the last output when draft is ready', () => {
  const w = mount(RefineStatusPanel, { props: { status: 'draft_ready', error: null, lastOutput: 'analysis result' } })
  expect(w.text()).toContain('analysis result')
})

it('shows the error when failed', () => {
  const w = mount(RefineStatusPanel, { props: { status: 'failed', error: 'claude exited: boom', lastOutput: '' } })
  expect(w.text()).toContain('claude exited: boom')
})

it('renders nothing for none with no output', () => {
  const w = mount(RefineStatusPanel, { props: { status: 'none', error: null, lastOutput: '' } })
  expect(w.text().trim()).toBe('')
})

it('renders a phase stepper marking completed phases done', () => {
  const w = mount(RefineStatusPanel, {
    props: { status: 'refining', error: null, lastOutput: '', completedPhases: ['analysis', 'spec'] },
  })
  const text = w.text()
  expect(text).toContain('Analysis')
  expect(text).toContain('Spec')
  expect(text).toContain('Implementation Plan')
  expect(text).toContain('Approval')
  // analysis + spec are done; the first incomplete (implementation_plan) is current.
  expect(w.findAll('[data-phase-state="done"]').length).toBe(2)
  expect(w.findAll('[data-phase-state="current"]').length).toBe(1)
})

it('marks no phase current when draft is ready', () => {
  const w = mount(RefineStatusPanel, {
    props: { status: 'draft_ready', error: null, lastOutput: 'x', completedPhases: ['analysis', 'spec', 'implementation_plan', 'approval'] },
  })
  expect(w.findAll('[data-phase-state="current"]').length).toBe(0)
  expect(w.findAll('[data-phase-state="done"]').length).toBe(4)
})
