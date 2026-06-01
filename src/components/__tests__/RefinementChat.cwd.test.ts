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
    runStatus: ref(null),
    syncStatus: vi.fn().mockResolvedValue(undefined),
    stop: vi.fn(),
    loadHistory: loadHistoryMock,
    sendMessage: sendMessageMock,
    confirm: confirmMock,
    phaseLabel: (p: string) => p,
  }),
}))

vi.mock('../../utils/markdown', () => ({
  renderMarkdown: (text: string) => text,
}))

// ── Sample data ─────────────────────────────────────────────────────────────
// `useProjects()` fetches /api/projects on mount; folders are embedded so the
// picker derives cwd without a second round-trip. `useSpawners()` fetches
// /api/spawners. Both open SSE on mount → EventSource is stubbed below.

const singleFolderProject = {
  id: 'p1',
  slug: 'my-project',
  name: 'My Project',
  folderCount: 1,
  folders: [
    { id: 'f1', projectId: 'p1', path: '/home/user/project', isDefault: true, createdAt: '' },
  ],
  createdAt: '',
  updatedAt: '',
}

const multiFolderProject = {
  id: 'p2',
  slug: 'multi',
  name: 'Multi Project',
  folderCount: 2,
  folders: [
    { id: 'f2a', projectId: 'p2', path: '/repos/alpha', isDefault: true, createdAt: '' },
    { id: 'f2b', projectId: 'p2', path: '/repos/beta', isDefault: false, createdAt: '' },
  ],
  createdAt: '',
  updatedAt: '',
}

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn((url: string) => {
    if (url === '/api/projects')
      return Promise.resolve({ ok: true, status: 200, json: async () => [singleFolderProject, multiFolderProject] })
    if (url === '/api/spawners')
      return Promise.resolve({ ok: true, status: 200, json: async () => [] })
    return Promise.resolve({ ok: true, status: 200, json: async () => [] })
  }))
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

// ── Helpers ────────────────────────────────────────────────────────────────

function mountChat() {
  return mount(RefinementChat, {
    props: { open: true, task: null },
    attachTo: document.body,
  })
}

function setSelectValue(el: HTMLSelectElement, value: string) {
  el.value = value
  el.dispatchEvent(new Event('change', { bubbles: true }))
}

// ── Tests ──────────────────────────────────────────────────────────────────

describe('refinementChat — project picker', () => {
  it('renders the project select in the empty state', async () => {
    const wrapper = mountChat()
    await flushPromises()
    const select = wrapper.find('[data-testid="cwd-project-select"]')
    expect(select.exists()).toBe(true)
    wrapper.unmount()
  })

  it('populates the project options from the projects API', async () => {
    const wrapper = mountChat()
    await flushPromises()
    // placeholder + 2 projects + create-new
    const options = wrapper.findAll('[data-testid="cwd-project-select"] option')
    const labels = options.map(o => o.text())
    expect(labels.some(l => l.includes('My Project'))).toBe(true)
    expect(labels.some(l => l.includes('Multi Project'))).toBe(true)
    wrapper.unmount()
  })

  it('shows an error and does NOT call createTask when no project is chosen', async () => {
    const wrapper = mountChat()
    await flushPromises()

    const textarea = wrapper.find('textarea')
    await textarea.setValue('build something cool')
    await wrapper.find('button[class*="bg-blue"]').trigger('click')
    await flushPromises()

    expect(createTaskMock).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('Please choose a working directory first')
    wrapper.unmount()
  })

  it('derives cwd from the chosen project and passes it to createTask', async () => {
    const wrapper = mountChat()
    await flushPromises()

    const select = wrapper.find('[data-testid="cwd-project-select"]').element as HTMLSelectElement
    setSelectValue(select, 'p1')
    await flushPromises()
    await flushPromises()

    // derived cwd shown read-only
    expect(wrapper.find('[data-testid="cwd-derived"]').text()).toContain('/home/user/project')

    const textarea = wrapper.find('textarea')
    await textarea.setValue('implement feature X')
    await wrapper.find('button[class*="bg-blue"]').trigger('click')
    await flushPromises()

    expect(createTaskMock).toHaveBeenCalledOnce()
    expect(createTaskMock).toHaveBeenCalledWith(
      expect.objectContaining({ cwd: '/home/user/project' }),
    )
    expect(createTaskMock).not.toHaveBeenCalledWith(
      expect.objectContaining({ cwd: '/' }),
    )
    wrapper.unmount()
  })

  it('shows a folder picker for multi-folder projects and updates cwd on folder change', async () => {
    const wrapper = mountChat()
    await flushPromises()

    const projectSelect = wrapper.find('[data-testid="cwd-project-select"]').element as HTMLSelectElement
    setSelectValue(projectSelect, 'p2')
    await flushPromises()
    await flushPromises()

    // default folder (isDefault) wins initially
    expect(wrapper.find('[data-testid="cwd-derived"]').text()).toContain('/repos/alpha')

    const folderSelect = wrapper.find('[data-testid="cwd-folder-select"]')
    expect(folderSelect.exists()).toBe(true)
    setSelectValue(folderSelect.element as HTMLSelectElement, 'f2b')
    await flushPromises()

    expect(wrapper.find('[data-testid="cwd-derived"]').text()).toContain('/repos/beta')
    wrapper.unmount()
  })

  it('clears the cwd error once a project is chosen', async () => {
    const wrapper = mountChat()
    await flushPromises()

    // trigger error first
    await wrapper.find('textarea').setValue('hello')
    await wrapper.find('button[class*="bg-blue"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Please choose a working directory first')

    const select = wrapper.find('[data-testid="cwd-project-select"]').element as HTMLSelectElement
    setSelectValue(select, 'p1')
    await flushPromises()
    await flushPromises()

    expect(wrapper.text()).not.toContain('Please choose a working directory first')
    wrapper.unmount()
  })
})
