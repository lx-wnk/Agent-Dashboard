import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import RefinementChat from '@/features/pipeline/components/RefinementChat.vue'

// Prepared answer buttons let the user pick one of the agent's suggested
// replies instead of typing. They come from the last assistant message's
// `options` field and disappear once there is nothing to pick from.

const sendMessageMock = vi.fn().mockResolvedValue(undefined)

// Read by the useRefinementChat mock below on every mount — reassigned per
// test before mounting, so each `it()` controls its own message list.
let mockMessages: Array<{ role: 'user' | 'assistant', content: string, options?: string[] }> = []

vi.mock('@/features/pipeline/composables/useTasks', () => ({
  createTask: vi.fn(),
}))

vi.mock('@/utils/markdown', () => ({
  renderMarkdown: (text: string) => text,
}))

vi.mock('@/features/pipeline/composables/useRefinementChat', () => ({
  useRefinementChat: () => ({
    messages: ref(mockMessages),
    isStreaming: ref(false),
    error: ref(null),
    approvalReady: ref(false),
    runStatus: ref('done'),
    syncStatus: vi.fn().mockResolvedValue(undefined),
    stop: vi.fn(),
    loadHistory: vi.fn().mockResolvedValue(undefined),
    sendMessage: sendMessageMock,
    confirm: vi.fn().mockResolvedValue(null),
    phaseLabel: (p: string) => p,
  }),
}))

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({ ok: true, status: 200, json: async () => [] })))
  vi.stubGlobal('EventSource', class {
    static CONNECTING = 0
    static OPEN = 1
    static CLOSED = 2
    onmessage: ((e: MessageEvent) => void) | null = null
    onerror: ((e: Event) => void) | null = null
    readyState = 0
    close() {}
  })
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

function mountWithTask() {
  return mount(RefinementChat, {
    props: {
      open: true,
      task: { id: 't1', slug: 's', title: 'T', currentStage: 'backlog', createdAt: '', updatedAt: '' } as any,
    },
    attachTo: document.body,
  })
}

describe('refinementChat — prepared answer buttons', () => {
  it('renders a button for each option on the last assistant message', async () => {
    mockMessages = [
      { role: 'user', content: 'build X' },
      { role: 'assistant', content: 'Pick one', options: ['Yes', 'No', 'Maybe'] },
    ]
    const wrapper = mountWithTask()
    await flushPromises()

    const optionButtons = wrapper.findAll('button.rounded-full')
    expect(optionButtons.map(b => b.text())).toEqual(['Yes', 'No', 'Maybe'])

    wrapper.unmount()
  })

  it('sends the option text when a button is clicked', async () => {
    mockMessages = [
      { role: 'user', content: 'build X' },
      { role: 'assistant', content: 'Pick one', options: ['Yes', 'No', 'Maybe'] },
    ]
    const wrapper = mountWithTask()
    await flushPromises()

    const optionButtons = wrapper.findAll('button.rounded-full')
    await optionButtons[0].trigger('click')
    await flushPromises()

    expect(sendMessageMock).toHaveBeenCalledWith('Yes')

    wrapper.unmount()
  })

  it('renders no option buttons when the last assistant message has no options', async () => {
    mockMessages = [
      { role: 'user', content: 'build X' },
      { role: 'assistant', content: 'No options here.' },
    ]
    const wrapper = mountWithTask()
    await flushPromises()

    expect(wrapper.findAll('button.rounded-full')).toHaveLength(0)

    wrapper.unmount()
  })
})
