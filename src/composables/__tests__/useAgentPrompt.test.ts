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

  it('injects live when liveInjectable — even when status is idle', async () => {
    const agent = makeAgent({ liveInjectable: true, status: 'idle' })
    const { promptInput, handleSend } = useAgentPrompt(() => agent)
    promptInput.value = 'hello'
    await handleSend()
    expect(fetch).toHaveBeenCalledWith(
      '/api/agents/123/message',
      expect.objectContaining({ method: 'POST' }),
    )
  })

  it('does NOT fetch and does NOT call onMessageSent when !liveInjectable — sets resumeConfirm instead', async () => {
    const agent = makeAgent({ channelAvailable: false, liveInjectable: false, status: 'active' })
    const onMessageSent = vi.fn()
    const { promptInput, handleSend, resumeConfirm } = useAgentPrompt(() => agent, onMessageSent)
    promptInput.value = 'hello world'
    await handleSend()
    expect(fetch).not.toHaveBeenCalled()
    expect(onMessageSent).not.toHaveBeenCalled()
    expect(resumeConfirm.value).toBe('hello world')
    expect(promptInput.value).toBe('')
  })

  it('confirmResume POSTs /api/agents/spawn with resumeSessionId and prompt', async () => {
    const agent = makeAgent({ channelAvailable: false, liveInjectable: false, status: 'active', sessionId: 'sess42' })
    const onMessageSent = vi.fn()
    const { promptInput, handleSend, confirmResume, resumeConfirm, sendStatus } = useAgentPrompt(() => agent, onMessageSent)
    promptInput.value = 'do something'
    await handleSend()
    // guard: confirm is set, no fetch yet
    expect(resumeConfirm.value).toBe('do something')
    expect(fetch).not.toHaveBeenCalled()

    await confirmResume()

    expect(fetch).toHaveBeenCalledWith(
      '/api/agents/spawn',
      expect.objectContaining({ method: 'POST' }),
    )
    const body = JSON.parse((fetch as ReturnType<typeof vi.fn>).mock.calls[0][1].body)
    expect(body.resumeSessionId).toBe('sess42')
    expect(body.prompt).toBe('do something')
    expect(onMessageSent).toHaveBeenCalledWith(
      expect.objectContaining({ role: 'human', content: 'do something' }),
    )
    expect(resumeConfirm.value).toBeNull()
    expect(sendStatus.value).toBe('sent')
  })

  it('cancelResume restores promptInput and clears resumeConfirm without fetching', async () => {
    const agent = makeAgent({ channelAvailable: false, liveInjectable: false, status: 'active' })
    const { promptInput, handleSend, cancelResume, resumeConfirm } = useAgentPrompt(() => agent)
    promptInput.value = 'my draft message'
    await handleSend()
    expect(resumeConfirm.value).toBe('my draft message')
    expect(promptInput.value).toBe('')

    cancelResume()

    expect(fetch).not.toHaveBeenCalled()
    expect(resumeConfirm.value).toBeNull()
    expect(promptInput.value).toBe('my draft message')
  })

  it('confirmResume does nothing if resumeConfirm is null', async () => {
    const agent = makeAgent({ liveInjectable: false })
    const { confirmResume, resumeConfirm } = useAgentPrompt(() => agent)
    expect(resumeConfirm.value).toBeNull()
    await confirmResume()
    expect(fetch).not.toHaveBeenCalled()
  })

  it('confirmResume clears state gracefully when getAgent returns null at confirm time', async () => {
    let agent: Agent | null = makeAgent({ liveInjectable: false, sessionId: 'gone' })
    const { promptInput, handleSend, confirmResume, resumeConfirm } = useAgentPrompt(() => agent)
    promptInput.value = 'hi'
    await handleSend()
    expect(resumeConfirm.value).toBe('hi')

    agent = null
    await confirmResume()

    expect(fetch).not.toHaveBeenCalled()
    expect(resumeConfirm.value).toBeNull()
  })
})
