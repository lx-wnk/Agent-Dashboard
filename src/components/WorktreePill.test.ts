import type { WorktreeStatusDTO } from '../sdk.generated'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'

// Mock the composable so we drive the pill from a plain ref the test owns.
const statusRef = ref<WorktreeStatusDTO | null>(null)
const errorRef = ref<string | null>(null)
const isLoadingRef = ref(false)
const refreshMock = vi.fn()

vi.mock('../composables/useWorktreeStatus', () => ({
  useWorktreeStatus: () => ({
    status: statusRef,
    isLoading: isLoadingRef,
    error: errorRef,
    refresh: refreshMock,
  }),
}))

import WorktreePill from './WorktreePill.vue'

function setStatus(dto: WorktreeStatusDTO | null) {
  statusRef.value = dto
}

beforeEach(() => {
  setStatus(null)
  errorRef.value = null
  isLoadingRef.value = false
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('WorktreePill', () => {
  it('renders nothing when status is null (no worktree / not yet loaded)', () => {
    const wrapper = mount(WorktreePill, { props: { taskId: 't1' } })
    expect(wrapper.find('[data-testid="worktree-pill"]').exists()).toBe(false)
  })

  it('renders clean branch (no dirty dot, no counts when both null)', async () => {
    setStatus({ branch: 'feat/foo', dirty: false, fileCount: 0, ahead: undefined, behind: undefined })
    const wrapper = mount(WorktreePill, { props: { taskId: 't1' } })
    await flushPromises()
    expect(wrapper.find('[data-testid="worktree-pill"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="worktree-pill-branch"]').text()).toBe('feat/foo')
    expect(wrapper.find('[data-testid="worktree-pill-counts"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="worktree-pill-dirty"]').exists()).toBe(false)
  })

  it('renders dirty dot when dirty=true', async () => {
    setStatus({ branch: 'feat/foo', dirty: true, fileCount: 3, ahead: 0, behind: 0 })
    const wrapper = mount(WorktreePill, { props: { taskId: 't1' } })
    await flushPromises()
    expect(wrapper.find('[data-testid="worktree-pill-dirty"]').exists()).toBe(true)
  })

  it('renders ahead-only counts', async () => {
    setStatus({ branch: 'feat/foo', dirty: false, fileCount: 0, ahead: 2, behind: undefined })
    const wrapper = mount(WorktreePill, { props: { taskId: 't1' } })
    await flushPromises()
    const counts = wrapper.find('[data-testid="worktree-pill-counts"]')
    expect(counts.exists()).toBe(true)
    expect(counts.text()).toContain('↑2')
    expect(counts.text()).not.toContain('↓')
  })

  it('renders behind-only counts', async () => {
    setStatus({ branch: 'feat/foo', dirty: false, fileCount: 0, ahead: undefined, behind: 5 })
    const wrapper = mount(WorktreePill, { props: { taskId: 't1' } })
    await flushPromises()
    const counts = wrapper.find('[data-testid="worktree-pill-counts"]')
    expect(counts.exists()).toBe(true)
    expect(counts.text()).toContain('↓5')
    expect(counts.text()).not.toContain('↑')
  })

  it('renders no counts when ahead+behind both null (no base branch)', async () => {
    setStatus({ branch: 'feat/foo', dirty: false, fileCount: 0, ahead: undefined, behind: undefined })
    const wrapper = mount(WorktreePill, { props: { taskId: 't1' } })
    await flushPromises()
    expect(wrapper.find('[data-testid="worktree-pill-counts"]').exists()).toBe(false)
  })

  it('truncates long branch names with an ellipsis', async () => {
    setStatus({ branch: 'feat/this-is-a-really-long-branch-name-that-exceeds', dirty: false, fileCount: 0, ahead: 0, behind: 0 })
    const wrapper = mount(WorktreePill, { props: { taskId: 't1' } })
    await flushPromises()
    const branch = wrapper.find('[data-testid="worktree-pill-branch"]').text()
    expect(branch.length).toBeLessThanOrEqual(20)
    expect(branch.endsWith('…')).toBe(true)
  })
})
