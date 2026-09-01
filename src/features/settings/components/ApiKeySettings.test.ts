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
  'Registry',
  'Memory',
  'Obsidian',
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

// The panels are lazy, so the section is empty for a tick or two after the
// click; `waitUntil` also fails the test if the nav item opens a different panel.
async function openPanel(wrapper: ReturnType<typeof mountSettings>, label: string, marker: string) {
  const button = wrapper.findAll('nav ul li button').find(b => b.text().includes(label))
  if (!button)
    throw new Error(`nav item "${label}" not found`)
  await button.trigger('click')
  await vi.waitUntil(() => wrapper.find(marker).exists(), { timeout: 2000, interval: 10 })
  await flushPromises()
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

  // Registry and Memory are adjacent nav items rendering sibling panels, so a
  // swapped or deleted section is invisible without pinning which panel each one
  // opens.
  it('opens the panel each nav item names', async () => {
    const wrapper = mountSettings()
    await flushPromises()

    await openPanel(wrapper, 'Registry', '[data-testid="resource-kind-application"]')
    expect(wrapper.find('[data-testid="memory-space-new"]').exists()).toBe(false)

    await openPanel(wrapper, 'Memory', '[data-testid="memory-space-new"]')
    expect(wrapper.find('[data-testid="resource-kind-application"]').exists()).toBe(false)
  })
})
