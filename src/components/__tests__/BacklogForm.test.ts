import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

let BacklogForm: any
let toastMod: typeof import('../../composables/useToast')

beforeEach(async () => {
  vi.resetModules()
  toastMod = await import('../../composables/useToast')
  vi.spyOn(toastMod.toast, 'error')
  globalThis.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => ([]) })
  class FakeEventSource {
    static readonly CLOSED = 2
    readyState = 0
    close = vi.fn()
  }
  globalThis.EventSource = FakeEventSource as any
  BacklogForm = (await import('../BacklogForm.vue')).default
})

describe('backlogForm', () => {
  it('calls toast.error for validation failure and renders no inline danger text', async () => {
    const w = mount(BacklogForm, { shallow: true })
    await (w.vm as any).buildTask()
    await nextTick()
    expect(toastMod.toast.error).toHaveBeenCalledWith(expect.stringContaining('required'))
    expect(w.find('.text-danger-text').exists()).toBe(false)
  })
})
