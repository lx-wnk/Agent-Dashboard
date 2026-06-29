import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

let ToastHost: any
let toastMod: typeof import('../../composables/useToast')

beforeEach(async () => {
  vi.useFakeTimers()
  vi.resetModules()
  toastMod = await import('../../composables/useToast')
  ToastHost = (await import('../ToastHost.vue')).default
})

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('toastHost', () => {
  it('renders a toast when added', async () => {
    const w = mount(ToastHost)
    toastMod.toast.error('something failed')
    await nextTick()
    expect(w.text()).toContain('something failed')
  })

  it('removes the toast after auto-dismiss', async () => {
    const w = mount(ToastHost)
    toastMod.toast.error('gone soon')
    await nextTick()
    vi.advanceTimersByTime(5000)
    await nextTick()
    expect(w.text()).not.toContain('gone soon')
  })

  it('renders multiple stacked toasts', async () => {
    const w = mount(ToastHost)
    toastMod.toast.error('first')
    toastMod.toast.success('second')
    await nextTick()
    expect(w.text()).toContain('first')
    expect(w.text()).toContain('second')
  })
})
