import type { Agent } from '../../types'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useAgentPrompt } from '../useAgentPrompt'

function makeAgent(over: Partial<Agent> = {}): Agent {
  return {
    sessionId: 's1',
    pid: 123,
    cwd: '/projects/x',
    status: 'active',
    channelAvailable: false,
    ...over,
  } as Agent
}

describe('useAgentPrompt routing', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, json: async () => ({}) })))
  })

  it('injects via channel when channelAvailable — even when status is idle', async () => {
    const agent = makeAgent({ channelAvailable: true, status: 'idle' })
    const { promptInput, handleSend } = useAgentPrompt(() => agent)
    promptInput.value = 'hello'
    await handleSend()
    expect(fetch).toHaveBeenCalledWith(
      '/api/agents/123/message',
      expect.objectContaining({ method: 'POST' }),
    )
  })

  it('falls back to spawn/resume when no channel is available', async () => {
    const agent = makeAgent({ channelAvailable: false, status: 'active' })
    const { promptInput, handleSend } = useAgentPrompt(() => agent)
    promptInput.value = 'hello'
    await handleSend()
    expect(fetch).toHaveBeenCalledWith(
      '/api/agents/spawn',
      expect.objectContaining({ method: 'POST' }),
    )
  })
})
