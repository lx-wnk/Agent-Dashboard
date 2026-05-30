import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import RefinementChat from '../RefinementChat.vue'

// ── Mocks ──────────────────────────────────────────────────────────────────

const createTaskMock = vi.fn().mockResolvedValue({
  id: 'task-1',
  slug: 'concept-123',
  title: 'New Task',
  cwd: '/home/user/project',
  currentStage: 'concept',
  createdAt: new Date().toISOString(),
  updatedAt: new Date().toISOString(),
} as unknown)

vi.mock('../../composables/useTasks', () => ({
  createTask: (input: unknown) => createTaskMock(input),
}))

// sendMessage spy so we can confirm it's eventually called without real network
const sendMessageMock = vi.fn().mockResolvedValue(undefined)
const loadHistoryMock = vi.fn().mockResolvedValue(undefined)
const confirmMock = vi.fn().mockResolvedValue(null)

vi.mock('../../composables/useRefinementChat', () => ({
  useRefinementChat: () => ({
    messages: ref([]),
    isStreaming: ref(false),
    error: ref(null),
    approvalReady: ref(false),
    loadHistory: loadHistoryMock,
    sendMessage: sendMessageMock,
    confirm: confirmMock,
    phaseLabel: (p: string) => p,
  }),
}))

vi.mock('../../utils/markdown', () => ({
  renderMarkdown: (text: string) => text,
}))

// ── Global fetch stub ──────────────────────────────────────────────────────
// The component fetches /api/projects on mount. Return a project with one folder
// so the datalist gets populated.

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    ok: true,
    status: 200,
    json: async () => [
      {
        id: 'p1',
        slug: 'my-project',
        name: 'My Project',
        folders: [
          { id: 'f1', projectId: 'p1', path: '/home/user/project', isDefault: true, createdAt: '' },
        ],
        createdAt: '',
        updatedAt: '',
      },
    ],
  }))
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

// ── Helpers ────────────────────────────────────────────────────────────────

function mountChat() {
  return mount(RefinementChat, {
    props: { open: true, task: null },
    global: { stubs: {} },
  })
}

// ── Tests ──────────────────────────────────────────────────────────────────

describe('refinementChat — working-directory selector', () => {
  it('renders the cwd input in the empty state', async () => {
    const wrapper = mountChat()
    await flushPromises()
    const input = wrapper.find('[data-testid="cwd-input"]')
    expect(input.exists()).toBe(true)
  })

  it('populates the datalist options from the projects API', async () => {
    const wrapper = mountChat()
    await flushPromises()
    const options = wrapper.findAll('#refine-cwd-list option')
    expect(options.length).toBeGreaterThan(0)
    expect(options[0].attributes('value')).toBe('/home/user/project')
  })

  it('shows an error and does NOT call createTask when cwd is empty', async () => {
    const wrapper = mountChat()
    await flushPromises()

    // Leave cwd blank — type a message and click send
    const textarea = wrapper.find('textarea')
    await textarea.setValue('build something cool')
    await wrapper.find('button[class*="bg-blue"]').trigger('click')
    await flushPromises()

    expect(createTaskMock).not.toHaveBeenCalled()
    // Error message visible
    expect(wrapper.text()).toContain('Please choose a working directory first')
  })

  it('calls createTask with the chosen cwd (not "/")', async () => {
    const wrapper = mountChat()
    await flushPromises()

    // Set working directory
    const cwdInput = wrapper.find('[data-testid="cwd-input"]')
    await cwdInput.setValue('/home/user/project')

    // Type a message
    const textarea = wrapper.find('textarea')
    await textarea.setValue('implement feature X')

    // Click send (→ button)
    await wrapper.find('button[class*="bg-blue"]').trigger('click')
    await flushPromises()

    expect(createTaskMock).toHaveBeenCalledOnce()
    expect(createTaskMock).toHaveBeenCalledWith(
      expect.objectContaining({ cwd: '/home/user/project' }),
    )
    // Explicitly confirm the hardcoded slash is gone
    expect(createTaskMock).not.toHaveBeenCalledWith(
      expect.objectContaining({ cwd: '/' }),
    )
  })

  it('also passes cwd when a suggestion chip is clicked then sent', async () => {
    const wrapper = mountChat()
    await flushPromises()

    // Set cwd
    await wrapper.find('[data-testid="cwd-input"]').setValue('/repos/myapp')

    // Use a suggestion chip to populate the textarea
    const chips = wrapper.findAll('button.rounded-full')
    expect(chips.length).toBeGreaterThan(0)
    await chips[0].trigger('click')

    // Send
    await wrapper.find('button[class*="bg-blue"]').trigger('click')
    await flushPromises()

    expect(createTaskMock).toHaveBeenCalledWith(
      expect.objectContaining({ cwd: '/repos/myapp' }),
    )
  })

  it('clears the cwd error when the user starts typing in the cwd field', async () => {
    const wrapper = mountChat()
    await flushPromises()

    // Trigger error first
    await wrapper.find('textarea').setValue('hello')
    await wrapper.find('button[class*="bg-blue"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Please choose a working directory first')

    // Start editing the cwd input
    const cwdInput = wrapper.find('[data-testid="cwd-input"]')
    await cwdInput.trigger('input')
    await flushPromises()

    expect(wrapper.text()).not.toContain('Please choose a working directory first')
  })
})
