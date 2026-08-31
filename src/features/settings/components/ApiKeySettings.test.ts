import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ApiKeySettings from '@/features/settings/components/ApiKeySettings.vue'

// The nav is data-driven by the in-component SECTIONS array (CQ-35). These specs
// guard that the extraction keeps one nav item per section and that clicking a
// nav item selects it — catching a dropped or mis-wired section.

const NAV_LABELS = [
  'Appearance',
  'API Keys',
  'Grants',
  'Permissions',
  'Analytics',
  'System Prompts',
  'Plugins',
  'Notifications',
  'Providers',
  'Tracker',
  'Projects',
  'Spawners',
  'Pipeline',
  'Server',
]

function mountSettings() {
  return mount(ApiKeySettings, {
    global: {
      stubs: {
        transition: false,
        AppModal: { template: '<div><slot /></div>' },
      },
    },
  })
}

describe('apiKeySettings nav', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({}),
    }))
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('renders exactly one nav item per section (auth off hides My Remotes)', async () => {
    const wrapper = mountSettings()
    await flushPromises()

    const buttons = wrapper.findAll('nav ul li button')
    expect(buttons).toHaveLength(NAV_LABELS.length)
    const labels = buttons.map(b => b.text().replace(/\s+/g, ' ').trim())
    for (const label of NAV_LABELS)
      expect(labels.some(l => l.endsWith(label))).toBe(true)
    // My Remotes is auth-gated and hidden when authEnabled is false.
    expect(labels.some(l => l.includes('My Remotes'))).toBe(false)
  })

  it('marks the clicked nav item as the current page', async () => {
    const wrapper = mountSettings()
    await flushPromises()

    const buttons = wrapper.findAll('nav ul li button')
    const pluginsButton = buttons.find(b => b.text().includes('Plugins'))!
    await pluginsButton.trigger('click')

    expect(pluginsButton.attributes('aria-current')).toBe('page')
  })
})
