import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick, ref } from 'vue'

let CoordinationTab: any
let toastMod: typeof import('../../composables/useToast')
let TaskRefKey: typeof import('../../composables/taskModalContext').TaskRefKey

beforeEach(async () => {
  vi.resetModules()
  toastMod = await import('../../composables/useToast')
  vi.spyOn(toastMod.toast, 'error')
  TaskRefKey = (await import('../../composables/taskModalContext')).TaskRefKey
  globalThis.fetch = vi.fn().mockResolvedValue({ ok: false, status: 503, json: async () => ({}) })
  CoordinationTab = (await import('../task/CoordinationTab.vue')).default
})

describe('coordinationTab', () => {
  it('calls toast.error and renders no inline danger text when coordination data fails to load', async () => {
    const w = mount(CoordinationTab, {
      global: { provide: { [TaskRefKey as symbol]: ref({ id: 't1' }) } },
    })
    await flushPromises()
    await nextTick()
    expect(toastMod.toast.error).toHaveBeenCalled()
    expect(w.find('.text-danger-text').exists()).toBe(false)
  })
})
