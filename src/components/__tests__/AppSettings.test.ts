import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

let AppSettings: any
let toastMod: typeof import('../../composables/useToast')

beforeEach(async () => {
  vi.resetModules()
  toastMod = await import('../../composables/useToast')
  vi.spyOn(toastMod.toast, 'error')
  AppSettings = (await import('../AppSettings.vue')).default
})

describe('appSettings', () => {
  it('calls toast.error when save fails and renders no inline danger text', async () => {
    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => [] })
      .mockResolvedValueOnce({ ok: false, status: 500, json: async () => ({}) })
    const w = mount(AppSettings)
    await flushPromises()
    await (w.vm as any).apply({ key: 'some.key', type: 'bool', value: 'false' }, 'true')
    await nextTick()
    expect(toastMod.toast.error).toHaveBeenCalled()
    expect(w.find('.text-danger-text').exists()).toBe(false)
  })
})
