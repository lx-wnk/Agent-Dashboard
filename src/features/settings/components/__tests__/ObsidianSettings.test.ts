import type { Ref } from 'vue'
import type { SettingView } from '@/features/settings/composables/useSettings'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import ObsidianSettings from '@/features/settings/components/ObsidianSettings.vue'
import { useSettings } from '@/features/settings/composables/useSettings'
import { selectByLabel } from '@/utils/testSelect'

vi.mock('@/features/settings/composables/useSettings', async () => {
  const actual = await vi.importActual<typeof import('@/features/settings/composables/useSettings')>('@/features/settings/composables/useSettings')
  return {
    ...actual,
    useSettings: vi.fn(),
  }
})

const MASK = '********'

function settingsFixture(overrides: Partial<Record<string, string>> = {}): SettingView[] {
  const values: Record<string, string> = {
    'obsidian.baseURL': 'https://127.0.0.1:27124',
    'obsidian.vaultRoot': 'claude-memory',
    'obsidian.apiKey': MASK,
    'obsidian.tlsMode': 'verify',
    ...overrides,
  }
  return [
    { key: 'obsidian.baseURL', type: 'string', value: values['obsidian.baseURL'], default: '', apply: 'restart', category: 'obsidian' },
    { key: 'obsidian.vaultRoot', type: 'string', value: values['obsidian.vaultRoot'], default: '', apply: 'restart', category: 'obsidian' },
    { key: 'obsidian.apiKey', type: 'string', value: values['obsidian.apiKey'], default: '', apply: 'restart', category: 'obsidian' },
    { key: 'obsidian.tlsMode', type: 'enum', value: values['obsidian.tlsMode'], default: 'verify', apply: 'restart', category: 'obsidian', enum: ['verify', 'pinned', 'insecure-loopback'] },
  ]
}

afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('obsidianSettings', () => {
  let items: Ref<SettingView[]>
  let update: ReturnType<typeof vi.fn>

  beforeEach(() => {
    items = ref(settingsFixture())
    update = vi.fn(async (key: string, value: string) => {
      items.value = items.value.map(i => (i.key === key ? { ...i, value: key === 'obsidian.apiKey' ? MASK : value } : i))
      return 'restart' as const
    })

    vi.mocked(useSettings).mockReturnValue({
      items,
      loading: ref(false),
      error: ref(null),
      refetch: vi.fn(),
      update,
    } as unknown as ReturnType<typeof useSettings>)
  })

  it('never renders a real API key — only the server-supplied mask', () => {
    const wrapper = mount(ObsidianSettings, { attachTo: document.body })
    const apiKeyInput = wrapper.get('[data-testid="obsidian-apikey"]').element as HTMLInputElement
    expect(apiKeyInput.value).toBe(MASK)
    expect(apiKeyInput.type).toBe('password')
  })

  it('saving without touching the API key PATCHes the mask sentinel unchanged, not an empty string', async () => {
    const wrapper = mount(ObsidianSettings, { attachTo: document.body })

    await wrapper.get('[data-testid="obsidian-vaultroot"]').setValue('new-vault')
    await wrapper.get('[data-testid="obsidian-save"]').trigger('click')
    await flushPromises()

    expect(update).toHaveBeenCalledWith('obsidian.apiKey', MASK)
    expect(update).not.toHaveBeenCalledWith('obsidian.apiKey', '')
    expect(update).toHaveBeenCalledWith('obsidian.vaultRoot', 'new-vault')

    const apiKeyInput = wrapper.get('[data-testid="obsidian-apikey"]').element as HTMLInputElement
    expect(apiKeyInput.value).toBe(MASK)
  })

  it('clearing every field PATCHes all three empty, which is what turns the vault off', async () => {
    const wrapper = mount(ObsidianSettings, { attachTo: document.body })

    await wrapper.get('[data-testid="obsidian-baseurl"]').setValue('')
    await wrapper.get('[data-testid="obsidian-vaultroot"]').setValue('')
    await wrapper.get('[data-testid="obsidian-apikey"]').setValue('')
    await wrapper.get('[data-testid="obsidian-save"]').trigger('click')
    await flushPromises()

    // The API key is the one that could not be cleared before: the field
    // shows the mask, which the server reads as "leave unchanged", so an
    // emptied field HAD to be sent as an empty string or the encrypted row
    // survived and the next boot hit the partial trio and refused to start.
    expect(update).toHaveBeenCalledWith('obsidian.baseURL', '')
    expect(update).toHaveBeenCalledWith('obsidian.vaultRoot', '')
    expect(update).toHaveBeenCalledWith('obsidian.apiKey', '')
  })

  it('sends a freshly typed API key', async () => {
    items.value = settingsFixture({ 'obsidian.apiKey': '' })
    const wrapper = mount(ObsidianSettings, { attachTo: document.body })

    await wrapper.get('[data-testid="obsidian-apikey"]').setValue('real-secret-key')
    await wrapper.get('[data-testid="obsidian-save"]').trigger('click')
    await flushPromises()

    expect(update).toHaveBeenCalledWith('obsidian.apiKey', 'real-secret-key')
  })

  it('renders the seeded TLS mode value, not the pre-load default', () => {
    items.value = settingsFixture({ 'obsidian.tlsMode': 'pinned' })
    const wrapper = mount(ObsidianSettings, { attachTo: document.body })

    expect(wrapper.get('[data-testid="obsidian-tlsmode"]').text()).toContain('pinned')
  })

  it('selecting a different TLS mode and saving PATCHes the chosen value', async () => {
    const wrapper = mount(ObsidianSettings, { attachTo: document.body })

    await selectByLabel(wrapper.get('[data-testid="obsidian-tlsmode"]').element, 'insecure-loopback')
    await wrapper.get('[data-testid="obsidian-save"]').trigger('click')
    await flushPromises()

    expect(update).toHaveBeenCalledWith('obsidian.tlsMode', 'insecure-loopback')
  })

  it('surfaces the restart requirement after a successful save', async () => {
    const toastMod = await import('@/composables/useToast')
    const successSpy = vi.spyOn(toastMod.toast, 'success')
    const wrapper = mount(ObsidianSettings, { attachTo: document.body })

    await wrapper.get('[data-testid="obsidian-save"]').trigger('click')
    await flushPromises()

    expect(successSpy).toHaveBeenCalledWith(expect.stringContaining('restart'))
  })

  it('blocks the save when the trio is incomplete — that state fails the server boot', async () => {
    items.value = settingsFixture({ 'obsidian.vaultRoot': '' })
    const wrapper = mount(ObsidianSettings, { attachTo: document.body })

    expect(wrapper.find('[data-testid="obsidian-trio-warning"]').exists()).toBe(true)

    const saveButton = wrapper.get('[data-testid="obsidian-save"]')
    expect((saveButton.element as HTMLButtonElement).disabled).toBe(true)

    await saveButton.trigger('click')
    await flushPromises()
    expect(update).not.toHaveBeenCalled()
  })

  it('allows the save once the missing member of the trio is filled in', async () => {
    items.value = settingsFixture({ 'obsidian.vaultRoot': '' })
    const wrapper = mount(ObsidianSettings, { attachTo: document.body })

    await wrapper.get('[data-testid="obsidian-vaultroot"]').setValue('claude-memory')
    expect(wrapper.find('[data-testid="obsidian-trio-warning"]').exists()).toBe(false)

    await wrapper.get('[data-testid="obsidian-save"]').trigger('click')
    await flushPromises()
    expect(update).toHaveBeenCalledWith('obsidian.vaultRoot', 'claude-memory')
  })

  it('shows no trio warning when the vault is fully configured', () => {
    const wrapper = mount(ObsidianSettings, { attachTo: document.body })
    expect(wrapper.find('[data-testid="obsidian-trio-warning"]').exists()).toBe(false)
  })

  it('shows no trio warning when the vault is entirely unconfigured', () => {
    items.value = settingsFixture({ 'obsidian.baseURL': '', 'obsidian.vaultRoot': '', 'obsidian.apiKey': '' })
    const wrapper = mount(ObsidianSettings, { attachTo: document.body })
    expect(wrapper.find('[data-testid="obsidian-trio-warning"]').exists()).toBe(false)
  })

  it('reports the indexed count on success', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({ indexed: 7 }) }))
    const wrapper = mount(ObsidianSettings, { attachTo: document.body })

    await wrapper.get('[data-testid="obsidian-index"]').trigger('click')
    await flushPromises()

    expect(fetch).toHaveBeenCalledWith('/api/obsidian/index', expect.objectContaining({ method: 'POST' }))
    expect(wrapper.get('[data-testid="obsidian-index-result"]').text()).toContain('7')
  })

  it('turns a 403 denial into a readable message, not a raw status code', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 403, json: async () => ({ error: 'capability denied' }) }))
    const wrapper = mount(ObsidianSettings, { attachTo: document.body })

    await wrapper.get('[data-testid="obsidian-index"]').trigger('click')
    await flushPromises()

    const text = wrapper.get('[data-testid="obsidian-index-result"]').text()
    expect(text).not.toContain('403')
    expect(text.toLowerCase()).toContain('grant')
  })

  it('turns a 503 unconfigured-vault response into a readable message, not a raw status code', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 503, json: async () => ({ error: 'obsidian vault not configured' }) }))
    const wrapper = mount(ObsidianSettings, { attachTo: document.body })

    await wrapper.get('[data-testid="obsidian-index"]').trigger('click')
    await flushPromises()

    const text = wrapper.get('[data-testid="obsidian-index-result"]').text()
    expect(text).not.toContain('503')
    expect(text.toLowerCase()).toContain('not configured')
  })

  it('turns a 409 in-progress response into a readable message, not a raw status code', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 409, json: async () => ({ error: 'an obsidian index run is already in progress' }) }))
    const wrapper = mount(ObsidianSettings, { attachTo: document.body })

    await wrapper.get('[data-testid="obsidian-index"]').trigger('click')
    await flushPromises()

    const text = wrapper.get('[data-testid="obsidian-index-result"]').text()
    expect(text).not.toContain('409')
    expect(text.toLowerCase()).toContain('already')
  })
})
