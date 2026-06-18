import type { WorktreeStatusDTO } from '../sdk.generated'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'

const statusRef = ref<WorktreeStatusDTO | null>(null)
const isLoadingRef = ref(false)
const errorRef = ref<string | null>(null)
const refreshMock = vi.fn()
const createMock = vi.fn().mockResolvedValue(undefined)
const removeMock = vi.fn().mockResolvedValue(204)

vi.mock('../composables/useWorktreeStatus', () => ({
  useWorktreeStatus: () => ({
    status: statusRef,
    isLoading: isLoadingRef,
    error: errorRef,
    refresh: refreshMock,
    create: createMock,
    remove: removeMock,
  }),
}))

// Stub localStorage for loadEditorScheme/saveEditorScheme
vi.stubGlobal('localStorage', {
  getItem: vi.fn().mockReturnValue(null),
  setItem: vi.fn(),
})

import WorktreePanel from './WorktreePanel.vue'

function setStatus(dto: WorktreeStatusDTO | null) {
  statusRef.value = dto
}

const clipboardWriteText = vi.fn().mockResolvedValue(undefined)

beforeEach(() => {
  setStatus(null)
  isLoadingRef.value = false
  errorRef.value = null
  removeMock.mockResolvedValue(204)
  createMock.mockResolvedValue(undefined)
  vi.stubGlobal('navigator', { clipboard: { writeText: clipboardWriteText } })
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('WorktreePanel', () => {
  describe('copy-path', () => {
    it('clicking Copy path calls navigator.clipboard.writeText with the path', async () => {
      setStatus({ branch: 'feat/foo', dirty: false, fileCount: 2, ahead: 0, behind: 0 })
      const wrapper = mount(WorktreePanel, {
        props: { taskId: 't1', worktreePath: '/home/user/worktrees/task1', active: true },
      })
      await flushPromises()
      await wrapper.find('[data-testid="worktree-copy-btn"]').trigger('click')
      expect(clipboardWriteText).toHaveBeenCalledWith('/home/user/worktrees/task1')
    })
  })

  describe('remove-clean', () => {
    it('clicking Remove on a clean worktree calls remove(false) and emits change', async () => {
      setStatus({ branch: 'feat/foo', dirty: false, fileCount: 2, ahead: 0, behind: 0 })
      const wrapper = mount(WorktreePanel, {
        props: { taskId: 't1', worktreePath: '/home/user/worktrees/task1', active: true },
      })
      await flushPromises()
      await wrapper.find('[data-testid="worktree-remove-btn"]').trigger('click')
      await flushPromises()
      expect(removeMock).toHaveBeenCalledWith(false)
      expect(wrapper.emitted('change')).toBeTruthy()
    })
  })

  describe('remove-dirty-confirm', () => {
    it('clicking Remove on a dirty worktree shows inline confirm without calling remove', async () => {
      setStatus({ branch: 'feat/foo', dirty: true, fileCount: 3, ahead: 0, behind: 0 })
      const wrapper = mount(WorktreePanel, {
        props: { taskId: 't1', worktreePath: '/home/user/worktrees/task1', active: true },
      })
      await flushPromises()
      await wrapper.find('[data-testid="worktree-remove-btn"]').trigger('click')
      await flushPromises()
      expect(removeMock).not.toHaveBeenCalled()
      expect(wrapper.find('[data-testid="worktree-confirm-remove-btn"]').exists()).toBe(true)
    })

    it('confirming the dirty prompt calls remove(true) and emits change', async () => {
      setStatus({ branch: 'feat/foo', dirty: true, fileCount: 3, ahead: 0, behind: 0 })
      const wrapper = mount(WorktreePanel, {
        props: { taskId: 't1', worktreePath: '/home/user/worktrees/task1', active: true },
      })
      await flushPromises()
      await wrapper.find('[data-testid="worktree-remove-btn"]').trigger('click')
      await flushPromises()
      await wrapper.find('[data-testid="worktree-confirm-remove-btn"]').trigger('click')
      await flushPromises()
      expect(removeMock).toHaveBeenCalledWith(true)
      expect(wrapper.emitted('change')).toBeTruthy()
    })
  })

  describe('create-when-absent', () => {
    it('shows Create worktree button when worktreePath is null', () => {
      const wrapper = mount(WorktreePanel, {
        props: { taskId: 't1', worktreePath: null, active: true },
      })
      expect(wrapper.find('[data-testid="worktree-create-btn"]').exists()).toBe(true)
    })

    it('clicking Create worktree calls create() and emits change', async () => {
      const wrapper = mount(WorktreePanel, {
        props: { taskId: 't1', worktreePath: null, active: true },
      })
      await wrapper.find('[data-testid="worktree-create-btn"]').trigger('click')
      await flushPromises()
      expect(createMock).toHaveBeenCalled()
      expect(wrapper.emitted('change')).toBeTruthy()
    })
  })
})
