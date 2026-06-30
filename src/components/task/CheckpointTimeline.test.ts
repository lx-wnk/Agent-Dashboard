import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import CheckpointTimeline from './CheckpointTimeline.vue'

const sampleCheckpoints = [
  { id: 'cp2', taskId: 'task-1', seq: 2, commitSha: 'b', filesChanged: 3, preRevert: false, createdAt: '2026-06-30T10:01:00Z' },
  { id: 'cp1', taskId: 'task-1', seq: 1, commitSha: 'a', filesChanged: 1, preRevert: false, createdAt: '2026-06-30T10:00:00Z' },
]

describe('checkpointTimeline', () => {
  it('renders checkpoint rows', () => {
    const wrapper = mount(CheckpointTimeline, {
      props: { taskId: 'task-1', checkpoints: sampleCheckpoints, loading: false },
    })
    expect(wrapper.findAll('[data-testid="checkpoint-row"]')).toHaveLength(2)
    expect(wrapper.text()).toContain('#2')
  })

  it('shows empty state when no checkpoints', () => {
    const wrapper = mount(CheckpointTimeline, {
      props: { taskId: 'task-1', checkpoints: [], loading: false },
    })
    expect(wrapper.text()).toContain('No checkpoints')
  })

  it('emits revert on button click after confirm', async () => {
    window.confirm = vi.fn(() => true)
    const wrapper = mount(CheckpointTimeline, {
      props: { taskId: 'task-1', checkpoints: sampleCheckpoints, loading: false },
    })
    await wrapper.find('[data-testid="revert-btn-cp2"]').trigger('click')
    expect(wrapper.emitted('revert')).toEqual([['cp2']])
  })

  it('does not emit revert when confirm is dismissed', async () => {
    window.confirm = vi.fn(() => false)
    const wrapper = mount(CheckpointTimeline, {
      props: { taskId: 'task-1', checkpoints: sampleCheckpoints, loading: false },
    })
    await wrapper.find('[data-testid="revert-btn-cp1"]').trigger('click')
    expect(wrapper.emitted('revert')).toBeUndefined()
  })
})
