import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import ChannelScriptCallout from './ChannelScriptCallout.vue'

describe('channelScriptCallout', () => {
  it('renders the script path from /api/config', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ scriptPath: '/home/u/.claude/channel.mjs', homedir: '/home/u' }),
    }))
    const w = mount(ChannelScriptCallout)
    await flushPromises()
    expect(w.text()).toContain('/home/u/.claude/channel.mjs')
  })

  it('renders nothing when scriptPath is absent', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ homedir: '/home/u' }),
    }))
    const w = mount(ChannelScriptCallout)
    await flushPromises()
    expect(w.text()).not.toContain('Channel script')
  })

  it('copies the path to clipboard on click', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { clipboard: { writeText } })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ scriptPath: '/p/channel.mjs', homedir: '/home/u' }),
    }))
    const w = mount(ChannelScriptCallout)
    await flushPromises()
    await w.get('[data-testid="channel-script-path"]').trigger('click')
    expect(writeText).toHaveBeenCalledWith('/p/channel.mjs')
  })
})
