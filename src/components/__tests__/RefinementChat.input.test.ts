import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import RefinementChat from '../RefinementChat.vue'

// The message input must stay usable even when the run is approval-ready (the
// "Create Task" bar is showing) so the user can keep adjusting open decisions.

const sendMessageMock = vi.fn().mockResolvedValue(undefined)

vi.mock('@/features/pipeline/composables/useTasks', () => ({
  createTask: vi.fn(),
}))

vi.mock('../../utils/markdown', () => ({
  renderMarkdown: (text: string) => text,
}))

vi.mock('@/features/pipeline/composables/useRefinementChat', () => ({
  useRefinementChat: () => ({
    messages: ref([
      { role: 'user', content: 'build X' },
      { role: 'assistant', content: 'Plan ready. Confirm or adjust.' },
    ]),
    isStreaming: ref(false),
    error: ref(null),
    approvalReady: ref(true), // ← approval-ready: Create Task bar visible
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

describe('refinementChat — input availability while approval-ready', () => {
  function mountWithTask() {
    return mount(RefinementChat, {
      props: {
        open: true,
        task: { id: 't1', slug: 's', title: 'T', currentStage: 'concept', createdAt: '', updatedAt: '' } as any,
      },
      attachTo: document.body,
    })
  }

  it('keeps the textarea enabled and lets the user send while the Create Task bar shows', async () => {
    const wrapper = mountWithTask()
    await flushPromises()

    // Create Task bar is present (approval-ready).
    expect(wrapper.text()).toContain('Create Task')

    // Textarea is NOT disabled.
    const textarea = wrapper.find('textarea')
    expect(textarea.attributes('disabled')).toBeUndefined()

    // Typing + send works.
    await textarea.setValue('use the SystemConfig key for S-02')
    await wrapper.find('button[class*="bg-blue"]').trigger('click')
    await flushPromises()

    expect(sendMessageMock).toHaveBeenCalledWith('use the SystemConfig key for S-02', undefined)

    wrapper.unmount()
  })
})
