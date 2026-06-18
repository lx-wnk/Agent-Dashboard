// src/components/PluginSlot.test.ts
import type { LoadedAddon, SlotContext, UnmountFn } from '../utils/pluginSlot'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import PluginSlot from './PluginSlot.vue'

function fakeCtx(): SlotContext {
  return { insertText: vi.fn(), setBusy: vi.fn() }
}

describe('PluginSlot', () => {
  it('mounts each addon into its own host element and unmounts on teardown', async () => {
    const unmount = vi.fn()
    const mountFn = vi.fn<(el: HTMLElement, ctx: SlotContext) => UnmountFn>((el) => {
      el.textContent = 'mic'
      return unmount
    })
    const addon: LoadedAddon = { slot: 'refinement-input-addon', mount: mountFn }
    const loader = vi.fn().mockResolvedValue([addon])

    const ctx = fakeCtx()
    const wrapper = mount(PluginSlot, {
      props: { name: 'refinement-input-addon', ctx, loader },
    })
    await flushPromises()

    expect(loader).toHaveBeenCalledWith('refinement-input-addon')
    expect(mountFn).toHaveBeenCalledTimes(1)
    expect(mountFn.mock.calls[0][0]).toBeInstanceOf(HTMLElement)
    expect(mountFn.mock.calls[0][1]).toBe(ctx)
    expect(wrapper.text()).toContain('mic')
    expect(wrapper.findAll('[data-addon-host]').length).toBe(1)

    wrapper.unmount()
    expect(unmount).toHaveBeenCalledTimes(1)
  })

  it('renders nothing when no addons target the slot', async () => {
    const loader = vi.fn().mockResolvedValue([])
    const wrapper = mount(PluginSlot, {
      props: { name: 'refinement-input-addon', ctx: fakeCtx(), loader },
    })
    await flushPromises()
    expect(wrapper.find('[data-addon-host]').exists()).toBe(false)
  })

  it('does not mount addons if unmounted before the loader resolves', async () => {
    let resolveLoader!: (a: LoadedAddon[]) => void
    const pending = new Promise<LoadedAddon[]>((r) => { resolveLoader = r })
    const mountFn = vi.fn(() => () => {})
    const loader = vi.fn().mockReturnValue(pending)

    const wrapper = mount(PluginSlot, {
      props: { name: 'refinement-input-addon', ctx: fakeCtx(), loader },
    })
    // unmount BEFORE the loader resolves
    wrapper.unmount()
    resolveLoader([{ slot: 'refinement-input-addon', mount: mountFn }])
    await flushPromises()

    expect(mountFn).not.toHaveBeenCalled()
  })
})
