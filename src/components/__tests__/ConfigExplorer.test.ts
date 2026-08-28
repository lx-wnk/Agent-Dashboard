import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

let ConfigExplorer: any

const memEntry = { path: '/c/CLAUDE.md', scope: 'user', size: 3, mtime: 42, editable: true }

// putStatus lets each test choose how the PUT resolves (200 vs 409).
let putStatus = 200

function makeFetch() {
  return vi.fn().mockImplementation((url: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET'
    if (url.startsWith('/api/config/skills'))
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ skills: [] }) })
    if (url.startsWith('/api/config/commands'))
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ commands: [] }) })
    if (url.startsWith('/api/config/context-files'))
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ memory: [memEntry] }) })
    if (url.startsWith('/api/config/file') && method === 'PUT')
      return Promise.resolve({ ok: putStatus === 200, status: putStatus, json: () => Promise.resolve({ path: memEntry.path, mtime: 99, size: 5 }) })
    if (url.startsWith('/api/config/file'))
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ path: memEntry.path, content: 'old', mtime: 42, editable: true, source: 'user' }) })
    throw new Error(`unexpected ${method} ${url}`)
  })
}

async function openMemoryEditor() {
  const w = mount(ConfigExplorer, { props: { spawnerId: undefined } })
  await flushPromises()
  const memTab = w.findAll('button').find(b => b.text().startsWith('Memory'))!
  await memTab.trigger('click')
  const editBtn = w.findAll('button').find(b => b.text() === 'Edit')!
  await editBtn.trigger('click')
  await flushPromises()
  return w
}

beforeEach(async () => {
  putStatus = 200
  vi.stubGlobal('fetch', makeFetch())
  vi.resetModules()
  ConfigExplorer = (await import('../ConfigExplorer.vue')).default
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('configExplorer editor', () => {
  it('opens the editor with loaded content', async () => {
    const w = await openMemoryEditor()
    const ta = w.find('textarea')
    expect(ta.exists()).toBe(true)
    expect((ta.element as HTMLTextAreaElement).value).toBe('old')
  })

  it('surfaces a reload prompt when the save conflicts (409)', async () => {
    putStatus = 409
    const w = await openMemoryEditor()
    await w.find('textarea').setValue('changed')
    const saveBtn = w.findAll('button').find(b => b.text() === 'Save')!
    await saveBtn.trigger('click')
    await flushPromises()
    expect(w.text()).toContain('changed on disk')
    expect(w.findAll('button').some(b => b.text() === 'Reload')).toBe(true)
  })

  it('dirty-guard keeps the editor open when discard is declined', async () => {
    const w = await openMemoryEditor()
    await w.find('textarea').setValue('dirty edit')
    vi.stubGlobal('confirm', vi.fn().mockReturnValue(false))
    const cancelBtn = w.findAll('button').find(b => b.text() === 'Cancel')!
    await cancelBtn.trigger('click')
    expect(w.find('textarea').exists()).toBe(true)
  })
})
