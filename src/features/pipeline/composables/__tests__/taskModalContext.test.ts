import type { UseTaskActions } from '@/features/pipeline/composables/useTaskActions'
import type { UseTaskDetails } from '@/features/pipeline/composables/useTaskDetails'
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { defineComponent, ref } from 'vue'
import {
  TaskActionsKey,
  TaskDetailsKey,
  TaskRefKey,
  useInjectedTask,
  useInjectedTaskActions,
  useInjectedTaskDetails,
} from '@/features/pipeline/composables/taskModalContext'

function mountWithSetup(setup: () => void) {
  return mount(defineComponent({
    setup,
    template: '<div />',
  }))
}

describe('taskModalContext — injection keys', () => {
  it('exposes three distinct symbol keys', () => {
    expect(typeof TaskRefKey).toBe('symbol')
    expect(typeof TaskDetailsKey).toBe('symbol')
    expect(typeof TaskActionsKey).toBe('symbol')
    expect(TaskRefKey).not.toBe(TaskDetailsKey)
    expect(TaskDetailsKey).not.toBe(TaskActionsKey)
    expect(TaskRefKey).not.toBe(TaskActionsKey)
  })
})

describe('useInjectedTask', () => {
  it('throws when used outside a TaskModal provider', () => {
    expect(() => mountWithSetup(() => {
      useInjectedTask()
    })).toThrow('useInjectedTask must be used within a TaskModal')
  })

  it('returns the exact ref provided by the TaskModal', () => {
    const task = ref(null)
    let received: unknown
    mount(defineComponent({
      setup() {
        received = useInjectedTask()
        return {}
      },
      template: '<div />',
    }), {
      global: { provide: { [TaskRefKey as symbol]: task } },
    })

    expect(received).toBe(task)
  })
})

describe('useInjectedTaskDetails', () => {
  it('throws when used outside a TaskModal provider', () => {
    expect(() => mountWithSetup(() => {
      useInjectedTaskDetails()
    })).toThrow('useInjectedTaskDetails must be used within a TaskModal')
  })

  it('returns the exact details object provided by the TaskModal', () => {
    const details = { stageRuns: ref([]) } as unknown as UseTaskDetails
    let received: unknown
    mount(defineComponent({
      setup() {
        received = useInjectedTaskDetails()
        return {}
      },
      template: '<div />',
    }), {
      global: { provide: { [TaskDetailsKey as symbol]: details } },
    })

    expect(received).toBe(details)
  })
})

describe('useInjectedTaskActions', () => {
  it('throws when used outside a TaskModal provider', () => {
    expect(() => mountWithSetup(() => {
      useInjectedTaskActions()
    })).toThrow('useInjectedTaskActions must be used within a TaskModal')
  })

  it('returns the exact actions object provided by the TaskModal', () => {
    const actions = { onRetry: vi.fn() } as unknown as UseTaskActions
    let received: unknown
    mount(defineComponent({
      setup() {
        received = useInjectedTaskActions()
        return {}
      },
      template: '<div />',
    }), {
      global: { provide: { [TaskActionsKey as symbol]: actions } },
    })

    expect(received).toBe(actions)
  })
})
