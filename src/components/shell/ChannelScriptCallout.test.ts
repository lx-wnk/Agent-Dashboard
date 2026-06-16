import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { useServerConfig } from '../../composables/useServerConfig'

import ChannelScriptCallout from './ChannelScriptCallout.vue'

vi.mock('../../composables/useServerConfig', () => ({
  useServerConfig: vi.fn(),
}))

function mountWithScriptPath(path: string) {
  vi.mocked(useServerConfig).mockReturnValue({
    scriptPath: ref(path),
    mcpServerName: ref(''),
    mcpEndpoint: ref(''),
    homedir: ref(''),
    loaded: ref(true),
    loadServerConfig: vi.fn().mockResolvedValue(undefined),
  })
  return mount(ChannelScriptCallout)
}

describe('channelScriptCallout', () => {
  it('renders the script path from server config', async () => {
    const w = mountWithScriptPath('/home/u/.claude/channel.mjs')
    await flushPromises()
    expect(w.text()).toContain('/home/u/.claude/channel.mjs')
  })

  it('renders nothing when scriptPath is absent', async () => {
    const w = mountWithScriptPath('')
    await flushPromises()
    expect(w.text()).not.toContain('Channel command')
  })

  it('copies the path to clipboard on click', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { clipboard: { writeText } })
    const w = mountWithScriptPath('/p/channel.mjs')
    await flushPromises()
    await w.get('[data-testid="channel-script-path"]').trigger('click')
    expect(writeText).toHaveBeenCalledWith('/p/channel.mjs')
  })

  it('copy target is a native button with a non-empty aria-label', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { clipboard: { writeText } })
    const w = mountWithScriptPath('/q/channel.mjs')
    await flushPromises()
    const btn = w.find('button[data-testid="channel-script-path"]')
    expect(btn.exists()).toBe(true)
    expect(btn.attributes('aria-label')).toBeTruthy()
    expect(btn.attributes('aria-label')).toContain('/q/channel.mjs')
    await btn.trigger('click')
    expect(writeText).toHaveBeenCalledWith('/q/channel.mjs')
  })
})
