import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import RefinementChat from '../RefinementChat.vue'

// Integration test with the REAL useRefinementChat composable. Guards the
// create-flow race where the open/id watcher fired loadHistory() on the
// null→newId transition and wiped the just-sent user message (leaving the
// empty-state "create" modal stuck open). See systematic-debugging session.

const createTaskMock = vi.fn().mockResolvedValue({
  id: 'task-1',
  slug: 'concept-123',
  title: 'New Task',
  cwd: '/home/user/project',
  currentStage: 'concept',
  createdAt: '',
  updatedAt: '',
} as unknown)

vi.mock('../../composables/useTasks', () => ({
  createTask: (input: unknown) => createTaskMock(input),
}))

vi.mock('../../utils/markdown', () => ({
  renderMarkdown: (text: string) => text,
}))

const singleFolderProject = {
  id: 'p1',
  slug: 'my-project',
  name: 'My Project',
  folderCount: 1,
  folders: [{ id: 'f1', projectId: 'p1', path: '/home/user/project', isDefault: true, createdAt: '' }],
  createdAt: '',
  updatedAt: '',
}

// A POST /turn body that streams one assistant frame then closes.
function streamingTurnResponse() {
  const chunks = [new TextEncoder().encode('data: Working on it.\n\n')]
  let i = 0
  return {
    ok: true,
    status: 200,
    body: {
      getReader: () => ({
        read: async () => i < chunks.length
          ? { done: false, value: chunks[i++] }
          : { done: true, value: undefined },
      }),
    },
  }
}

beforeEach(() => {
  // jsdom elements have no scrollTo — the messages watcher calls it.
  if (!(HTMLElement.prototype as any).scrollTo)
    (HTMLElement.prototype as any).scrollTo = () => {}

  vi.stubGlobal('fetch', vi.fn((url: string, init?: RequestInit) => {
    if (url === '/api/projects')
      return Promise.resolve({ ok: true, status: 200, json: async () => [singleFolderProject] })
    if (url === '/api/spawners')
      return Promise.resolve({ ok: true, status: 200, json: async () => [] })
    if (url.endsWith('/turns'))
      return Promise.resolve({ ok: true, status: 200, json: async () => [] })
    if (url.endsWith('/status'))
      return Promise.resolve({ ok: true, status: 200, json: async () => ({ status: 'done' }) })
    if (url.endsWith('/turn') && init?.method === 'POST')
      return Promise.resolve(streamingTurnResponse())
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

function setSelectValue(el: HTMLSelectElement, value: string) {
  el.value = value
  el.dispatchEvent(new Event('change', { bubbles: true }))
}

describe('refinementChat — create flow', () => {
  it('keeps the user message and transitions to the chat view (no loadHistory clobber)', async () => {
    const wrapper = mount(RefinementChat, { props: { open: true, task: null }, attachTo: document.body })
    await flushPromises()

    setSelectValue(wrapper.find('[data-testid="cwd-project-select"]').element as HTMLSelectElement, 'p1')
    await flushPromises()

    await wrapper.find('textarea').setValue('implement feature X')
    await wrapper.find('button[class*="bg-blue"]').trigger('click')
    await flushPromises()

    // Task created with the derived cwd.
    expect(createTaskMock).toHaveBeenCalledWith(expect.objectContaining({ cwd: '/home/user/project' }))

    // The user message survived (was NOT wiped by a loadHistory race) …
    expect(wrapper.text()).toContain('implement feature X')
    // … the streamed assistant reply rendered …
    expect(wrapper.text()).toContain('Working on it.')
    // … and the empty-state "create" prompt is gone (chat view is shown).
    expect(wrapper.text()).not.toContain('What would you like to build?')

    wrapper.unmount()
  })

  it('does not call GET /turns during the create flow', async () => {
    const wrapper = mount(RefinementChat, { props: { open: true, task: null }, attachTo: document.body })
    await flushPromises()

    setSelectValue(wrapper.find('[data-testid="cwd-project-select"]').element as HTMLSelectElement, 'p1')
    await flushPromises()
    await wrapper.find('textarea').setValue('implement feature X')
    await wrapper.find('button[class*="bg-blue"]').trigger('click')
    await flushPromises()

    const calls = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls.map(c => c[0] as string)
    expect(calls.some(u => u.endsWith('/turns'))).toBe(false)
    expect(calls).toContain('/api/refine/task-1/turn')

    wrapper.unmount()
  })
})
