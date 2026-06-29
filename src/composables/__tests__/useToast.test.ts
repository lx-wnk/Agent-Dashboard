import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

let useToast: typeof import('../useToast').useToast
let dismiss: typeof import('../useToast').dismiss
let pauseToast: typeof import('../useToast').pauseToast
let resumeToast: typeof import('../useToast').resumeToast
let toast: typeof import('../useToast').toast

beforeEach(async () => {
  vi.useFakeTimers()
  vi.resetModules()
  const mod = await import('../useToast')
  useToast = mod.useToast
  dismiss = mod.dismiss
  pauseToast = mod.pauseToast
  resumeToast = mod.resumeToast
  toast = mod.toast
})

afterEach(() => {
  vi.useRealTimers()
})

describe('useToast', () => {
  it('adds a toast and exposes it in toasts', async () => {
    const { toasts } = useToast()
    toast.error('boom')
    await nextTick()
    expect(toasts.value).toHaveLength(1)
    expect(toasts.value[0]).toMatchObject({ message: 'boom', type: 'error' })
  })

  it('stacks multiple concurrent toasts', async () => {
    const { toasts } = useToast()
    toast.error('a')
    toast.success('b')
    toast.info('c')
    await nextTick()
    expect(toasts.value).toHaveLength(3)
  })

  it('auto-dismisses after 5 s', async () => {
    const { toasts } = useToast()
    toast.error('bye')
    await nextTick()
    expect(toasts.value).toHaveLength(1)
    vi.advanceTimersByTime(5000)
    await nextTick()
    expect(toasts.value).toHaveLength(0)
  })

  it('dismiss() removes a specific toast immediately', async () => {
    const { toasts } = useToast()
    toast.error('keep')
    const id = toast.error('remove')
    await nextTick()
    dismiss(id)
    await nextTick()
    expect(toasts.value).toHaveLength(1)
    expect(toasts.value[0].message).toBe('keep')
  })

  it('pause halts auto-dismiss; resume restarts with remaining time', async () => {
    const { toasts } = useToast()
    const id = toast.error('hover', 5000)
    await nextTick()
    vi.advanceTimersByTime(3000)
    pauseToast(id)
    vi.advanceTimersByTime(5000)
    await nextTick()
    expect(toasts.value).toHaveLength(1)
    resumeToast(id)
    vi.advanceTimersByTime(2000)
    await nextTick()
    expect(toasts.value).toHaveLength(0)
  })

  it('respects a custom duration', async () => {
    const { toasts } = useToast()
    toast.info('quick', 1000)
    await nextTick()
    vi.advanceTimersByTime(999)
    await nextTick()
    expect(toasts.value).toHaveLength(1)
    vi.advanceTimersByTime(1)
    await nextTick()
    expect(toasts.value).toHaveLength(0)
  })
})
