import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import SpawnDialog from './SpawnDialog.vue'

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
  modelOverride: 'claude-opus-4-7',
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
    onmessage: ((e: MessageEvent) => void) | null = null
    onerror: ((e: Event) => void) | null = null
    readyState = 0
    close() {}
  })
})
afterEach(() => vi.unstubAllGlobals())

describe('spawnDialog', () => {
  it('hydrates cwd and model when a project is selected', async () => {
    const wrapper = mount(SpawnDialog, { props: { open: true }, attachTo: document.body })
    await flushPromises()

    const projectSelect = document.querySelector('#spawn-project') as HTMLSelectElement
    expect(projectSelect).not.toBeNull()
    setSelectValue(projectSelect, 'prj_a')
    await flushPromises()
    await flushPromises()

    const cwdInput = document.querySelector('[data-testid="spawn-cwd-wrap"] input') as HTMLInputElement
    expect(cwdInput).not.toBeNull()
    expect(cwdInput.value).toBe('/home/u/alpha')

    // Model hydration is verified via the spawn payload in the next test —
    // the <select> reports '' when the bound value has no matching <option>
    // (AVAILABLE_MODELS does not include the override), but v-model carries
    // it to the spawn body regardless.
    const modelSelect = document.querySelector('#spawn-model') as HTMLSelectElement
    expect(modelSelect).not.toBeNull()
    wrapper.unmount()
  })

  it('sends spawnerId and projectId in the spawn payload', async () => {
    const wrapper = mount(SpawnDialog, { props: { open: true }, attachTo: document.body })
    await flushPromises()

    const projectSelect = document.querySelector('#spawn-project') as HTMLSelectElement
    setSelectValue(projectSelect, 'prj_a')
    await flushPromises()
    await flushPromises()

    const promptInput = document.querySelector('[data-testid="spawn-prompt-wrap"] textarea') as HTMLTextAreaElement
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
      model: 'claude-opus-4-7',
      spawnerId: 'spwn_a',
      projectId: 'prj_a',
    })
    wrapper.unmount()
  })
})
