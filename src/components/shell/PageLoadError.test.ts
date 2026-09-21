import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import PageLoadError from './PageLoadError.vue'

describe('pageLoadError', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  // The desktop app has no browser chrome to reload from, so the failed
  // page needs its own way back (R27).
  it('reloads the page when the Reload button is clicked', async () => {
    const reload = vi.fn()
    vi.stubGlobal('location', { ...window.location, reload })
    const w = mount(PageLoadError)
    await w.get('button').trigger('click')
    expect(reload).toHaveBeenCalledOnce()
    w.unmount()
  })
})
