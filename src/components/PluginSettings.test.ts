import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { resetSlotCaches } from '../composables/usePluginSlots'
import PluginSettings from './PluginSettings.vue'

function makePlugin(id: string, capabilities: string[], state: 'active' | 'inactive' = 'active') {
  return { id, name: id, version: '1.0.0', state, updateAvailable: false, capabilities, hasSettings: false }
}

function stubFetch(pluginList: object[]) {
  vi.stubGlobal('fetch', vi.fn().mockImplementation((url: string) => {
    if (url === '/api/plugins')
      return Promise.resolve({ ok: true, json: async () => pluginList })
    if (String(url).includes('/deactivate') || String(url).includes('/activate'))
      return Promise.resolve({ ok: true, json: async () => ({}) })
    // /api/settings/plugins → empty list for PluginSlot's loadSlotAddons
    return Promise.resolve({ ok: true, json: async () => [] })
  }))
}

beforeEach(() => resetSlotCaches())
afterEach(() => vi.unstubAllGlobals())

it('shows a reload notice when a ui_extension plugin is deactivated', async () => {
  stubFetch([makePlugin('p1', ['ui_extension'])])
  const wrapper = mount(PluginSettings)
  await flushPromises()

  await wrapper.find('button[role="switch"]').trigger('click')
  await flushPromises()

  expect(wrapper.text().toLowerCase()).toContain('reload')
  expect(wrapper.find('[data-action="reload-now"]').exists()).toBe(true)

  const reload = vi.fn()
  vi.stubGlobal('location', { reload } as any)
  await wrapper.find('[data-action="reload-now"]').trigger('click')
  expect(reload).toHaveBeenCalledOnce()
})

it('clears the reload notice when a ui_extension plugin is re-activated', async () => {
  stubFetch([makePlugin('p1', ['ui_extension'])])
  const wrapper = mount(PluginSettings)
  await flushPromises()

  // deactivate → notice appears
  await wrapper.find('button[role="switch"]').trigger('click')
  await flushPromises()
  expect(wrapper.find('[data-action="reload-now"]').exists()).toBe(true)

  // re-activate → notice clears
  await wrapper.find('button[role="switch"]').trigger('click')
  await flushPromises()
  expect(wrapper.find('[data-action="reload-now"]').exists()).toBe(false)
})

it('does not show the reload notice for a non-ui_extension plugin', async () => {
  stubFetch([makePlugin('p1', ['route_extension'])])
  const wrapper = mount(PluginSettings)
  await flushPromises()

  await wrapper.find('button[role="switch"]').trigger('click')
  await flushPromises()

  expect(wrapper.find('[data-action="reload-now"]').exists()).toBe(false)
})
