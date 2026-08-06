import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { defineComponent, h, onBeforeUnmount } from 'vue'

const teardown = vi.fn()

const Probe = defineComponent({
  setup() {
    onBeforeUnmount(teardown)
    return () => h('div')
  },
})

// Guards the enableAutoUnmount() call in setup.ts. Without it a component left
// mounted by one test keeps its timers running into the next one, where its
// polls inflate the call counts of shared module-level mocks (#327). The two
// tests below must stay in this order — the second asserts on what the first
// deliberately failed to clean up.
describe('global test setup', () => {
  it('leaves a mounted wrapper standing at the end of a test', () => {
    mount(Probe)

    expect(teardown).not.toHaveBeenCalled()
  })

  it('unmounts the previous test\'s wrapper before this one runs', () => {
    expect(teardown).toHaveBeenCalledTimes(1)
  })
})
