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

  it('stacks toasts as distinct children inside one positioned container', async () => {
    const w = mount(ToastHost)
    toastMod.toast.error('first')
    toastMod.toast.success('second')
    await nextTick()

    const container = w.get('.fixed')
    expect(container.classes()).toEqual(
      expect.arrayContaining(['fixed', 'flex', 'flex-col']),
    )
    const items = container.findAll('.pointer-events-auto')
    expect(items).toHaveLength(2)
  })

  it('styles an error toast with danger and a success toast distinctly', async () => {
    const w = mount(ToastHost)
    toastMod.toast.error('boom')
    toastMod.toast.success('yay')
    await nextTick()

    const items = w.findAll('.pointer-events-auto')
    const errorItem = items.find(i => i.text().includes('boom'))!
    const successItem = items.find(i => i.text().includes('yay'))!
    expect(errorItem.classes()).toContain('text-danger-text')
    expect(successItem.classes()).not.toContain('text-danger-text')
    expect(successItem.classes()).toContain('text-success-text')
  })

  it('dismisses a toast when its close button is clicked', async () => {
    const w = mount(ToastHost)
    toastMod.toast.error('dismiss me')
    await nextTick()
    expect(w.text()).toContain('dismiss me')

    await w.get('button[aria-label="Dismiss"]').trigger('click')
    await nextTick()
    expect(w.text()).not.toContain('dismiss me')
  })
})
