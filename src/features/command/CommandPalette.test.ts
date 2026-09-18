import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'

const createTask = vi.fn()
const suggestFolders = vi.fn()
const projects = ref<Array<{ id: string, name: string }>>([])
const activeView = ref('dashboard')

vi.mock('@/features/pipeline', () => ({
  createTask: (...args: unknown[]) => createTask(...args),
}))
vi.mock('@/composables/useProjectFolders', () => ({
  suggestFolders: (...args: unknown[]) => suggestFolders(...args),
}))
vi.mock('@/composables/useProjects', () => ({
  useProjects: () => ({ projects }),
}))
vi.mock('@/composables/useViewState', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/composables/useViewState')>()
  return { ...actual, useViewState: () => ({ activeView }) }
})

const { default: CommandPalette } = await import('./CommandPalette.vue')

function press(key: string, init: KeyboardEventInit = {}) {
  window.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, ...init }))
}

async function openPalette() {
  const wrapper = mount(CommandPalette, { attachTo: document.body })
  press('k', { metaKey: true })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  createTask.mockReset()
  suggestFolders.mockReset()
  projects.value = [{ id: 'p1', name: 'Dashboard' }]
  suggestFolders.mockResolvedValue([{ path: '/repo', isDefault: true }])
  createTask.mockResolvedValue({ id: 't1' })
  activeView.value = 'dashboard'
})

afterEach(() => {
  document.body.innerHTML = ''
})

describe('commandPalette', () => {
  it('opens on Cmd+K and closes on a second Cmd+K', async () => {
    const wrapper = await openPalette()
    expect(document.querySelector('[data-testid="command-palette"]')).not.toBeNull()
    press('k', { metaKey: true })
    await flushPromises()
    expect(document.querySelector('[data-testid="command-palette"]')).toBeNull()
    wrapper.unmount()
  })

  // One line of text, no project picked, no slug typed: that is the whole
  // point of the field, so it is the test that must never quietly pass.
  it('captures free text as a backlog task without asking for a project', async () => {
    const wrapper = await openPalette()
    const input = document.querySelector<HTMLInputElement>('[data-testid="command-palette-input"]')!
    input.value = 'Teach the palette to capture'
    input.dispatchEvent(new Event('input'))
    await flushPromises()
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await flushPromises()

    expect(createTask).toHaveBeenCalledWith(expect.objectContaining({
      title: 'Teach the palette to capture',
      cwd: '/repo',
      projectId: 'p1',
    }))
    expect(createTask.mock.calls[0][0].slug).toMatch(/^[a-z0-9-]+$/)
    expect(wrapper.emitted('captured')).toEqual([['t1']])
    wrapper.unmount()
  })

  it('says so instead of failing silently when no project exists', async () => {
    projects.value = []
    const wrapper = await openPalette()
    const input = document.querySelector<HTMLInputElement>('[data-testid="command-palette-input"]')!
    input.value = 'Something'
    input.dispatchEvent(new Event('input'))
    await flushPromises()
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await flushPromises()

    expect(createTask).not.toHaveBeenCalled()
    expect(document.querySelector('[data-testid="command-palette-problem"]')?.textContent)
      .toContain('No project exists yet')
    wrapper.unmount()
  })

  it('filters the commands and runs the selected one', async () => {
    const wrapper = await openPalette()
    const input = document.querySelector<HTMLInputElement>('[data-testid="command-palette-input"]')!
    input.value = 'pipeline'
    input.dispatchEvent(new Event('input'))
    await flushPromises()

    const options = document.querySelectorAll('[role="option"]')
    expect(options).toHaveLength(1)
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await flushPromises()
    expect(activeView.value).toBe('pipeline')
    wrapper.unmount()
  })

  it('returns focus to where it came from on Escape', async () => {
    const before = document.createElement('button')
    document.body.appendChild(before)
    before.focus()

    const wrapper = await openPalette()
    const input = document.querySelector<HTMLInputElement>('[data-testid="command-palette-input"]')!
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(document.activeElement).toBe(before)
    wrapper.unmount()
  })
})
