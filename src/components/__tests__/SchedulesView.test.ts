import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

let SchedulesView: any
let toastMod: typeof import('../../composables/useToast')

beforeEach(async () => {
  vi.resetModules()
  toastMod = await import('../../composables/useToast')
  vi.spyOn(toastMod.toast, 'error')
  globalThis.fetch = vi.fn().mockResolvedValue({ ok: false, status: 500, json: async () => ({}) })
  class FakeEventSource {
    static readonly CLOSED = 2
    readyState = 0
    close = vi.fn()
  }
  globalThis.EventSource = FakeEventSource as any
  SchedulesView = (await import('../SchedulesView.vue')).default
})

describe('schedulesView', () => {
  it('calls toast.error and renders no inline danger text when load fails', async () => {
    const w = mount(SchedulesView)
    await flushPromises()
    await nextTick()
    expect(toastMod.toast.error).toHaveBeenCalled()
    expect(w.find('.text-danger-text').exists()).toBe(false)
  })
})
