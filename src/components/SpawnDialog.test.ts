import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { openListboxDom as openListbox, selectByLabel, selectOptionsById } from '@/utils/testSelect'
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
//   4. The project/folder/spawner/permission-mode fields are AppSelect, a
//      custom listbox, not a native <select> — its panel teleports to
//      <body> while open, so selections go through the trigger button +
//      the teleported [role="option"] elements via the shared
//      openListboxDom()/optionByLabel()/selectByLabel() helpers (see
//      testSelect.ts) rather than select.setValue(). Raw option `value`s
//      (not just visible labels) are inspected via selectOptionsById()
//      where a test doesn't drive a selection.

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
afterEach(() => {
  vi.unstubAllGlobals()
  // AppSelect teleports its open panel to <body>; a test that throws before
  // wrapper.unmount() (or leaves a panel open) would otherwise leak stale
  // nodes that corrupt every document.querySelector() in later tests.
  document.body.innerHTML = ''
})

describe('spawnDialog', () => {
  it('does not offer a "None (manual)" option — project is required', async () => {
    const wrapper = mount(SpawnDialog, { props: { open: true }, attachTo: document.body })
    await flushPromises()

    const optionValues = selectOptionsById(wrapper, 'spawn-project').map(o => o.value)
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
    const projectTrigger = document.querySelector('#spawn-project') as HTMLElement
    await selectByLabel(projectTrigger, 'Alpha')
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

    const spawnerTrigger = document.querySelector('[data-testid="spawn-spawner"]') as HTMLElement
    expect(spawnerTrigger).not.toBeNull()

    // Before project selection, the trigger shows the empty-value option's label.
    expect(spawnerTrigger.textContent).toContain('Claude default')
    const panelBefore = await openListbox(spawnerTrigger)
    expect(panelBefore.querySelectorAll('[role="option"]')[0].textContent?.trim()).toContain('Claude default')
    // Close it — leaving it open would make the next trigger click toggle it closed instead of open.
    spawnerTrigger.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await flushPromises()

    // Select a project — spawner should be hydrated from project.defaultSpawnerId
    const projectTrigger = document.querySelector('#spawn-project') as HTMLElement
    await selectByLabel(projectTrigger, 'Alpha')
    await flushPromises()

    expect(spawnerTrigger.textContent).toContain('Claude (Opus)')
    // First option label changes to "Project default" when a project is chosen
    const panelAfter = await openListbox(spawnerTrigger)
    expect(panelAfter.querySelectorAll('[role="option"]')[0].textContent?.trim()).toContain('Project default')

    wrapper.unmount()
  })

  it('spawner select lists loaded spawners with built-in indicator', async () => {
    const wrapper = mount(SpawnDialog, { props: { open: true }, attachTo: document.body })
    await flushPromises()

    const spawnerTrigger = document.querySelector('[data-testid="spawn-spawner"]') as HTMLElement
    expect(spawnerTrigger).not.toBeNull()
    const panel = await openListbox(spawnerTrigger)
    const optionTexts = Array.from(panel.querySelectorAll('[role="option"]')).map(el => el.textContent?.trim())
    // sampleSpawner.builtIn = false, so no "(built-in)" suffix
    expect(optionTexts.some(t => t?.includes('Claude (Opus)'))).toBe(true)
    expect(optionTexts.every(t => !t?.includes('(built-in)'))).toBe(true)

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

  it('permission-mode select offers all claude CLI modes', async () => {
    const wrapper = mount(SpawnDialog, { props: { open: true }, attachTo: document.body })
    await flushPromises()

    const permTrigger = document.querySelector('[data-testid="spawn-permission-mode"]') as HTMLElement
    expect(permTrigger).not.toBeNull()

    const values = selectOptionsById(wrapper, 'spawn-permission-mode').map(o => o.value)
    for (const mode of ['default', 'plan', 'acceptEdits', 'auto', 'bypassPermissions', 'dontAsk'])
      expect(values).toContain(mode)
    expect(permTrigger.textContent).toContain('Ask for permission (default)')

    wrapper.unmount()
  })

  it('dontAsk also shows the dangerous-mode warning banner', async () => {
    const wrapper = mount(SpawnDialog, { props: { open: true }, attachTo: document.body })
    await flushPromises()

    const permTrigger = document.querySelector('[data-testid="spawn-permission-mode"]') as HTMLElement
    await selectByLabel(permTrigger, 'Never ask (dangerous)')
    expect(document.querySelector('[data-testid="bypass-warning"]')).not.toBeNull()

    // A non-dangerous mode (auto) must NOT show the warning.
    await selectByLabel(permTrigger, 'Auto (smart approvals)')
    expect(document.querySelector('[data-testid="bypass-warning"]')).toBeNull()

    wrapper.unmount()
  })

  it('bypass warning banner is shown only when bypassPermissions is selected', async () => {
    const wrapper = mount(SpawnDialog, { props: { open: true }, attachTo: document.body })
    await flushPromises()

    // Initially no warning
    const warningBefore = document.querySelector('.bg-yellow-50\\/50, .dark\\:bg-yellow-950\\/20')
    expect(warningBefore).toBeNull()

    const permTrigger = document.querySelector('[data-testid="spawn-permission-mode"]') as HTMLElement
    await selectByLabel(permTrigger, 'Bypass all permissions (dangerous)')

    // Warning banner should appear
    const warning = document.querySelector('[data-testid="bypass-warning"]')
    expect(warning).not.toBeNull()

    wrapper.unmount()
  })

  it('bypass confirm gate: first click sets confirmed flag, second proceeds', async () => {
    const wrapper = mount(SpawnDialog, { props: { open: true }, attachTo: document.body })
    await flushPromises()

    // Select a project to enable spawn
    const projectTrigger = document.querySelector('#spawn-project') as HTMLElement
    await selectByLabel(projectTrigger, 'Alpha')
    await flushPromises()

    const promptInput = document.querySelector('[data-testid="spawn-prompt-wrap"]') as HTMLTextAreaElement
    setInputValue(promptInput, 'do something dangerous')
    await flushPromises()

    // Select bypassPermissions mode
    const permTrigger = document.querySelector('[data-testid="spawn-permission-mode"]') as HTMLElement
    await selectByLabel(permTrigger, 'Bypass all permissions (dangerous)')

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

    const projectTrigger = document.querySelector('#spawn-project') as HTMLElement
    await selectByLabel(projectTrigger, 'Alpha')
    await flushPromises()

    const promptInput = document.querySelector('[data-testid="spawn-prompt-wrap"]') as HTMLTextAreaElement
    setInputValue(promptInput, 'risky task')
    await flushPromises()

    const permTrigger = document.querySelector('[data-testid="spawn-permission-mode"]') as HTMLElement
    await selectByLabel(permTrigger, 'Bypass all permissions (dangerous)')

    // First click — triggers confirm
    const spawnBtn = document.querySelector('[data-testid="spawn-btn"]') as HTMLButtonElement
    spawnBtn.click()
    await flushPromises()

    const confirmMsgBefore = document.querySelector('[data-testid="bypass-confirm-msg"]')
    expect(confirmMsgBefore).not.toBeNull()

    // Change mode — confirm state should reset
    await selectByLabel(permTrigger, 'Ask for permission (default)')

    const confirmMsgAfter = document.querySelector('[data-testid="bypass-confirm-msg"]')
    expect(confirmMsgAfter).toBeNull()

    wrapper.unmount()
  })

  it('sends spawnerId, projectId, cwd, enableChannel:true, permissionMode, and NO model key in the spawn payload', async () => {
    const wrapper = mount(SpawnDialog, { props: { open: true }, attachTo: document.body })
    await flushPromises()

    const projectTrigger = document.querySelector('#spawn-project') as HTMLElement
    await selectByLabel(projectTrigger, 'Alpha')
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

  it('does not show an error when an interactive session exits with a null exit code', async () => {
    vi.stubGlobal('fetch', vi.fn((url: string) => {
      if (url === '/api/projects')
        return Promise.resolve({ ok: true, json: async () => [sampleProject] })
      if (url === '/api/spawners')
        return Promise.resolve({ ok: true, json: async () => [sampleSpawner] })
      if (url === '/api/agents/spawn')
        return Promise.resolve({ ok: true, json: async () => ({ ok: true, pid: 12345 }) })
      if (url.startsWith('/api/agents/spawn/12345/status'))
        return Promise.resolve({ ok: true, json: async () => ({ pid: 12345, status: 'exited', exitCode: null }) })
      return Promise.resolve({ ok: true, json: async () => [] })
    }))

    const wrapper = mount(SpawnDialog, { props: { open: true }, attachTo: document.body })
    await flushPromises()

    const projectTrigger = document.querySelector('#spawn-project') as HTMLElement
    await selectByLabel(projectTrigger, 'Alpha')
    await flushPromises()

    const promptInput = document.querySelector('[data-testid="spawn-prompt-wrap"]') as HTMLTextAreaElement
    setInputValue(promptInput, 'do a thing')
    await flushPromises()

    const spawnBtn = document.querySelector('[data-testid="spawn-btn"]') as HTMLButtonElement
    spawnBtn.click()
    await flushPromises()
    await flushPromises()

    // A clean interactive-session end (null exitCode) must not surface an error.
    expect(document.body.textContent).not.toContain('exited with code')

    wrapper.unmount()
  })

  it('sends permissionMode:acceptEdits without confirm gate', async () => {
    const wrapper = mount(SpawnDialog, { props: { open: true }, attachTo: document.body })
    await flushPromises()

    const projectTrigger = document.querySelector('#spawn-project') as HTMLElement
    await selectByLabel(projectTrigger, 'Alpha')
    await flushPromises()

    const promptInput = document.querySelector('[data-testid="spawn-prompt-wrap"]') as HTMLTextAreaElement
    setInputValue(promptInput, 'edit files')
    await flushPromises()

    const permTrigger = document.querySelector('[data-testid="spawn-permission-mode"]') as HTMLElement
    await selectByLabel(permTrigger, 'Auto-accept edits')

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
