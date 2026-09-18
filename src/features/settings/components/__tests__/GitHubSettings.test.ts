import type { Ref } from 'vue'
import type { SettingView } from '@/features/settings/composables/useSettings'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import GitHubSettings from '@/features/settings/components/GitHubSettings.vue'
import { useSettings } from '@/features/settings/composables/useSettings'

vi.mock('@/features/settings/composables/useSettings', async () => {
  const actual = await vi.importActual<typeof import('@/features/settings/composables/useSettings')>('@/features/settings/composables/useSettings')
  return { ...actual, useSettings: vi.fn() }
})

const MASK = '********'

function fixture(tokenSource: string, token: string, repos: string): SettingView[] {
  return [
    { key: 'github.tokenSource', type: 'enum', value: tokenSource, default: 'setting', apply: 'restart', category: 'github' },
    { key: 'github.token', type: 'string', value: token, default: '', apply: 'restart', category: 'github' },
    { key: 'github.repos', type: 'string', value: repos, default: '', apply: 'restart', category: 'github' },
    { key: 'github.baseURL', type: 'string', value: 'https://api.github.com', default: 'https://api.github.com', apply: 'restart', category: 'github' },
  ] as SettingView[]
}

const update = vi.fn().mockResolvedValue('restart')

function mountWith(items: SettingView[]) {
  vi.mocked(useSettings).mockReturnValue({
    items: ref(items) as Ref<SettingView[]>,
    loading: ref(false),
    refetch: vi.fn(),
    update,
  } as unknown as ReturnType<typeof useSettings>)
  return mount(GitHubSettings, { attachTo: document.body })
}

beforeEach(() => update.mockClear())
afterEach(() => {
  document.body.innerHTML = ''
})

describe('gitHubSettings token source', () => {
  // With the token coming from gh it is not a setting, so offering a field for
  // it would invite someone to fill in a value that is never read.
  it('hides the token field when the source is the GitHub CLI', async () => {
    const wrapper = mountWith(fixture('gh-cli', '', 'lx-wnk/Agent-Dashboard'))
    await flushPromises()

    expect(wrapper.find('[data-testid="github-token"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="github-token-source"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('shows the token field for the default, stored source', async () => {
    const wrapper = mountWith(fixture('setting', MASK, 'lx-wnk/Agent-Dashboard'))
    await flushPromises()

    expect(wrapper.find('[data-testid="github-token"]').exists()).toBe(true)
    wrapper.unmount()
  })

  // The pair rule exists because the server refuses to BOOT with one half set.
  // Under gh-cli there is no half to miss, so the block must lift — otherwise
  // repositories alone could never be saved.
  it('does not block saving with repositories but no token under gh-cli', async () => {
    const wrapper = mountWith(fixture('gh-cli', '', 'lx-wnk/Agent-Dashboard'))
    await flushPromises()

    expect(wrapper.find('[data-testid="github-pair-warning"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('still blocks a lone token under the stored source', async () => {
    const wrapper = mountWith(fixture('setting', 'ghp_x', ''))
    await flushPromises()

    expect(wrapper.find('[data-testid="github-pair-warning"]').exists()).toBe(true)
    wrapper.unmount()
  })
})
