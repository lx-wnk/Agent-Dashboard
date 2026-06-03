import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import SpawnDialog from './SpawnDialog.vue'

// jsdom quirks worked around in this file:
//   1. AppModal teleports to <body>, so tests use document.querySelector
//      rather than wrapper.find for elements inside the modal.
//   2. AppInput uses useId() for the real <input>/<textarea> id; the
//      `id` attribute passed in falls through to the wrapper <div>.
//      Tests target [data-testid="…-wrap"] input/textarea to reach the
//      real form element.
//   3. EventSource is stubbed because useProjects()/useSpawners() open
//      SSE on mount and jsdom has no built-in EventSource.

const sampleProject = {
  id: 'prj_a',
  slug: 'alpha',
  name: 'Alpha',
  defaultSpawnerId: 'spwn_a',
  folders: [{ id: 'fld_a', projectId: 'prj_a', path: '/home/u/alpha', isDefault: true, createdAt: '' }],
  createdAt: '',
  updatedAt: '',
}

const sampleSpawner = {
  id: 'spwn_a',
  name: 'Claude (Opus)',
  slug: 'claude-opus',
  command: 'claude',
  args: [],
  env: {},
  adapterType: 'claude' as const,
  adapterConfig: {},
  modelOverride: 'claude-opus-4-6',
  builtIn: false,
  createdAt: '',
  updatedAt: '',
}

function setSelectValue(el: HTMLSelectElement, value: string) {
  el.value = value
  el.dispatchEvent(new Event('change', { bubbles: true }))
}

function setInputValue(el: HTMLInputElement | HTMLTextAreaElement, value: string) {
  el.value = value
  el.dispatchEvent(new Event('input', { bubbles: true }))
}

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn((url: string) => {
    if (url === '/api/projects')
      return Promise.resolve({ ok: true, json: async () => [sampleProject] })
    if (url === '/api/spawners')
      return Promise.resolve({ ok: true, json: async () => [sampleSpawner] })
    if (url === '/api/agents/spawn') {
      return Promise.resolve({
        ok: true,
        json: async () => ({ ok: true, pid: 12345 }),
      })
    }
    if (url.startsWith('/api/agents/spawn/12345/status'))
      return Promise.resolve({ ok: true, json: async () => ({ pid: 12345, status: 'running' }) })
    return Promise.resolve({ ok: true, json: async () => [] })
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
afterEach(() => vi.unstubAllGlobals())

describe('spawnDialog', () => {
  it('does not offer a "None (manual)" option — project is required', async () => {
    const wrapper = mount(SpawnDialog, { props: { open: true }, attachTo: document.body })
    await flushPromises()

    const projectSelect = document.querySelector('#spawn-project') as HTMLSelectElement
    expect(projectSelect).not.toBeNull()

    const optionValues = Array.from(projectSelect.options).map(o => o.value)
    expect(optionValues).not.toContain('')

    wrapper.unmount()
  })

  it('spawn button is disabled until a project with a folder is selected', async () => {
    const wrapper = mount(SpawnDialog, { props: { open: true }, attachTo: document.body })
    await flushPromises()

    const promptInput = document.querySelector('[data-testid="spawn-prompt-wrap"]') as HTMLTextAreaElement
    expect(promptInput).not.toBeNull()
    setInputValue(promptInput, 'do a thing')
    await flushPromises()

    // Button must remain disabled — no project selected yet, so cwd is empty.
    const spawnBtn = document.querySelector('[data-testid="spawn-btn"]') as HTMLButtonElement
    expect(spawnBtn).not.toBeNull()
    expect(spawnBtn.disabled).toBe(true)

    // Select a project — cwd should now be filled from the default folder.
    const projectSelect = document.querySelector('#spawn-project') as HTMLSelectElement
    setSelectValue(projectSelect, 'prj_a')
    await flushPromises()
    await flushPromises()

    expect(spawnBtn.disabled).toBe(false)

    wrapper.unmount()
  })

  it('there is no free-text working directory input', async () => {
    const wrapper = mount(SpawnDialog, { props: { open: true }, attachTo: document.body })
    await flushPromises()

    const cwdWrap = document.querySelector('[data-testid="spawn-cwd-wrap"]')
    expect(cwdWrap).toBeNull()

    wrapper.unmount()
  })

  it('spawner select is present and hydrates from project default', async () => {
    const wrapper = mount(SpawnDialog, { props: { open: true }, attachTo: document.body })
    await flushPromises()

    const spawnerSelect = document.querySelector('[data-testid="spawn-spawner"]') as HTMLSelectElement
    expect(spawnerSelect).not.toBeNull()

    // Before project selection, first option should be "Claude default"
    expect(spawnerSelect.options[0].text).toContain('Claude default')

    // Select a project — spawner should be hydrated from project.defaultSpawnerId
    const projectSelect = document.querySelector('#spawn-project') as HTMLSelectElement
    setSelectValue(projectSelect, 'prj_a')
    await flushPromises()
    await flushPromises()

    expect(spawnerSelect.value).toBe('spwn_a')
    // First option label changes to "Project default" when a project is chosen
    expect(spawnerSelect.options[0].text).toContain('Project default')

    wrapper.unmount()
  })

  it('spawner select lists loaded spawners with built-in indicator', async () => {
    const wrapper = mount(SpawnDialog, { props: { open: true }, attachTo: document.body })
    await flushPromises()

    const spawnerSelect = document.querySelector('[data-testid="spawn-spawner"]') as HTMLSelectElement
    expect(spawnerSelect).not.toBeNull()
    const optionTexts = Array.from(spawnerSelect.options).map(o => o.text)
    // sampleSpawner.builtIn = false, so no "(built-in)" suffix
    expect(optionTexts.some(t => t.includes('Claude (Opus)'))).toBe(true)
    expect(optionTexts.every(t => !t.includes('(built-in)'))).toBe(true)

    wrapper.unmount()
  })

  it('there is no model select', async () => {
    const wrapper = mount(SpawnDialog, { props: { open: true }, attachTo: document.body })
    await flushPromises()

    const modelSelect = document.querySelector('#spawn-model')
    expect(modelSelect).toBeNull()

    wrapper.unmount()
  })

  it('there is no channel checkbox', async () => {
    const wrapper = mount(SpawnDialog, { props: { open: true }, attachTo: document.body })
    await flushPromises()

    const channelCheckbox = document.querySelector('#spawn-channel')
    expect(channelCheckbox).toBeNull()

    wrapper.unmount()
  })

  it('permission-mode select is present with three options', async () => {
    const wrapper = mount(SpawnDialog, { props: { open: true }, attachTo: document.body })
    await flushPromises()

    const permSelect = document.querySelector('[data-testid="spawn-permission-mode"]') as HTMLSelectElement
    expect(permSelect).not.toBeNull()

    const values = Array.from(permSelect.options).map(o => o.value)
    expect(values).toContain('default')
    expect(values).toContain('acceptEdits')
    expect(values).toContain('bypassPermissions')
    expect(permSelect.value).toBe('default')

    wrapper.unmount()
  })

  it('bypass warning banner is shown only when bypassPermissions is selected', async () => {
    const wrapper = mount(SpawnDialog, { props: { open: true }, attachTo: document.body })
    await flushPromises()

    // Initially no warning
    const warningBefore = document.querySelector('.bg-yellow-50\\/50, .dark\\:bg-yellow-950\\/20')
    expect(warningBefore).toBeNull()

    const permSelect = document.querySelector('[data-testid="spawn-permission-mode"]') as HTMLSelectElement
    setSelectValue(permSelect, 'bypassPermissions')
    await flushPromises()

    // Warning banner should appear
    const warning = document.querySelector('[data-testid="bypass-warning"]')
    expect(warning).not.toBeNull()

    wrapper.unmount()
  })

  it('bypass confirm gate: first click sets confirmed flag, second proceeds', async () => {
    const wrapper = mount(SpawnDialog, { props: { open: true }, attachTo: document.body })
    await flushPromises()

    // Select a project to enable spawn
    const projectSelect = document.querySelector('#spawn-project') as HTMLSelectElement
    setSelectValue(projectSelect, 'prj_a')
    await flushPromises()
    await flushPromises()

    const promptInput = document.querySelector('[data-testid="spawn-prompt-wrap"]') as HTMLTextAreaElement
    setInputValue(promptInput, 'do something dangerous')
    await flushPromises()

    // Select bypassPermissions mode
    const permSelect = document.querySelector('[data-testid="spawn-permission-mode"]') as HTMLSelectElement
    setSelectValue(permSelect, 'bypassPermissions')
    await flushPromises()

    const spawnBtn = document.querySelector('[data-testid="spawn-btn"]') as HTMLButtonElement

    // First click — should NOT call spawn, should show confirm message
    spawnBtn.click()
    await flushPromises()

    const fetchMock = globalThis.fetch as ReturnType<typeof vi.fn>
    const spawnCallsBefore = fetchMock.mock.calls.filter(c => c[0] === '/api/agents/spawn')
    expect(spawnCallsBefore).toHaveLength(0)

    const confirmMsg = document.querySelector('[data-testid="bypass-confirm-msg"]')
    expect(confirmMsg).not.toBeNull()

    // Second click — should proceed
    spawnBtn.click()
    await flushPromises()

    const spawnCallsAfter = fetchMock.mock.calls.filter(c => c[0] === '/api/agents/spawn')
    expect(spawnCallsAfter).toHaveLength(1)

    wrapper.unmount()
  })

  it('bypass confirm gate resets when permission mode changes', async () => {
    const wrapper = mount(SpawnDialog, { props: { open: true }, attachTo: document.body })
    await flushPromises()

    const projectSelect = document.querySelector('#spawn-project') as HTMLSelectElement
    setSelectValue(projectSelect, 'prj_a')
    await flushPromises()
    await flushPromises()

    const promptInput = document.querySelector('[data-testid="spawn-prompt-wrap"]') as HTMLTextAreaElement
    setInputValue(promptInput, 'risky task')
    await flushPromises()

    const permSelect = document.querySelector('[data-testid="spawn-permission-mode"]') as HTMLSelectElement
    setSelectValue(permSelect, 'bypassPermissions')
    await flushPromises()

    // First click — triggers confirm
    const spawnBtn = document.querySelector('[data-testid="spawn-btn"]') as HTMLButtonElement
    spawnBtn.click()
    await flushPromises()

    const confirmMsgBefore = document.querySelector('[data-testid="bypass-confirm-msg"]')
    expect(confirmMsgBefore).not.toBeNull()

    // Change mode — confirm state should reset
    setSelectValue(permSelect, 'default')
    await flushPromises()

    const confirmMsgAfter = document.querySelector('[data-testid="bypass-confirm-msg"]')
    expect(confirmMsgAfter).toBeNull()

    wrapper.unmount()
  })

  it('sends spawnerId, projectId, cwd, enableChannel:true, permissionMode, and NO model key in the spawn payload', async () => {
    const wrapper = mount(SpawnDialog, { props: { open: true }, attachTo: document.body })
    await flushPromises()

    const projectSelect = document.querySelector('#spawn-project') as HTMLSelectElement
    setSelectValue(projectSelect, 'prj_a')
    await flushPromises()
    await flushPromises()

    const promptInput = document.querySelector('[data-testid="spawn-prompt-wrap"]') as HTMLTextAreaElement
    expect(promptInput).not.toBeNull()
    setInputValue(promptInput, 'do a thing')
    await flushPromises()

    const spawnBtn = document.querySelector('[data-testid="spawn-btn"]') as HTMLButtonElement
    expect(spawnBtn).not.toBeNull()
    spawnBtn.click()
    await flushPromises()

    const calls = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls
    const spawnCall = calls.find(c => c[0] === '/api/agents/spawn')
    expect(spawnCall).toBeTruthy()
    const body = JSON.parse(spawnCall![1].body as string)
    expect(body).toMatchObject({
      prompt: 'do a thing',
      cwd: '/home/u/alpha',
      spawnerId: 'spwn_a',
      projectId: 'prj_a',
      enableChannel: true,
      permissionMode: 'default',
    })
    // model key must NOT be present
    expect(Object.keys(body)).not.toContain('model')

    wrapper.unmount()
  })

  it('sends permissionMode:acceptEdits without confirm gate', async () => {
    const wrapper = mount(SpawnDialog, { props: { open: true }, attachTo: document.body })
    await flushPromises()

    const projectSelect = document.querySelector('#spawn-project') as HTMLSelectElement
    setSelectValue(projectSelect, 'prj_a')
    await flushPromises()
    await flushPromises()

    const promptInput = document.querySelector('[data-testid="spawn-prompt-wrap"]') as HTMLTextAreaElement
    setInputValue(promptInput, 'edit files')
    await flushPromises()

    const permSelect = document.querySelector('[data-testid="spawn-permission-mode"]') as HTMLSelectElement
    setSelectValue(permSelect, 'acceptEdits')
    await flushPromises()

    const spawnBtn = document.querySelector('[data-testid="spawn-btn"]') as HTMLButtonElement
    spawnBtn.click()
    await flushPromises()

    const calls = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls
    const spawnCall = calls.find(c => c[0] === '/api/agents/spawn')
    expect(spawnCall).toBeTruthy()
    const body = JSON.parse(spawnCall![1].body as string)
    expect(body.permissionMode).toBe('acceptEdits')
    expect(Object.keys(body)).not.toContain('model')

    wrapper.unmount()
  })
})
