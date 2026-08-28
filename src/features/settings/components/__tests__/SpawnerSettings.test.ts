import type { VueWrapper } from '@vue/test-utils'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { selectByLabel } from '@/utils/testSelect'

vi.mock('@/composables/useSpawners', () => ({
  useSpawners: () => ({
    spawners: ref([
      {
        id: 's-claude',
        name: 'Claude Row',
        slug: 'claude-row',
        command: 'claude',
        args: [],
        env: {},
        adapterType: 'claude',
        adapterConfig: {},
        builtIn: false,
        isDefault: false,
        createdAt: '',
        updatedAt: '',
      },
      {
        id: 's-ollama',
        name: 'Ollama Row',
        slug: 'ollama-row',
        command: '',
        args: [],
        env: {},
        adapterType: 'ollama',
        adapterConfig: { host: '' },
        builtIn: false,
        isDefault: false,
        createdAt: '',
        updatedAt: '',
      },
      {
        id: 's-legacy',
        name: 'Legacy Effort Row',
        slug: 'legacy-effort-row',
        command: 'claude',
        args: [],
        env: {},
        adapterType: 'claude',
        adapterConfig: { effort: 'ultra' },
        builtIn: false,
        isDefault: false,
        createdAt: '',
        updatedAt: '',
      },
    ]),
    isLoading: ref(false),
    error: ref(null),
    refetch: vi.fn(),
  }),
  createSpawner: vi.fn(),
  updateSpawner: vi.fn().mockResolvedValue({}),
  deleteSpawner: vi.fn(),
  setDefaultSpawner: vi.fn(),
}))

vi.mock('@/composables/useAdapterCatalog', () => ({
  useAdapterCatalog: () => {
    const catalog = [
      {
        name: 'claude',
        description: 'Claude CLI adapter',
        configKeys: [
          { key: 'effort', type: 'string', required: false, note: 'Reasoning effort' },
        ],
      },
      {
        name: 'ollama',
        description: 'Ollama HTTP adapter',
        configKeys: [
          { key: 'host', type: 'string', required: false },
        ],
      },
    ]
    return {
      catalog: ref(catalog),
      isLoading: ref(false),
      error: ref(null),
      reload: vi.fn(),
      getByType: (type: string) => catalog.find(a => a.name === type),
    }
  },
}))

vi.mock('@/composables/useToast', () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}))

let SpawnerSettings: typeof import('@/features/settings/components/SpawnerSettings.vue').default
let updateSpawnerMock: ReturnType<typeof vi.fn>

beforeEach(async () => {
  vi.resetModules()
  const spawnersMod = await import('@/composables/useSpawners')
  updateSpawnerMock = vi.mocked(spawnersMod.updateSpawner)
  SpawnerSettings = (await import('@/features/settings/components/SpawnerSettings.vue')).default
})

afterEach(() => {
  // AppSelect teleports its open panel to <body> — an open panel left over
  // from a test that doesn't close it would corrupt document.querySelector
  // lookups in the next test.
  document.body.innerHTML = ''
})

function rowByName(wrapper: VueWrapper, name: string) {
  const row = wrapper.findAll('tr').find(tr => tr.text().includes(name))
  if (!row)
    throw new Error(`no row for "${name}"`)
  return row
}

async function clickEdit(wrapper: VueWrapper, rowName: string): Promise<void> {
  const row = rowByName(wrapper, rowName)
  const editBtn = row.findAll('button').find(b => b.text() === 'Edit')
  if (!editBtn)
    throw new Error(`no Edit button for "${rowName}"`)
  await editBtn.trigger('click')
  await flushPromises()
}

function effortTrigger(wrapper: VueWrapper): HTMLButtonElement {
  return wrapper.get('#sp-effort').element as HTMLButtonElement
}

describe('spawnerSettings — effort control', () => {
  it('renders enabled for the claude adapter and disabled with a reason for an adapter that does not support it', async () => {
    const wrapper = mount(SpawnerSettings, { attachTo: document.body })
    await flushPromises()

    await clickEdit(wrapper, 'Claude Row')
    expect(effortTrigger(wrapper).disabled).toBe(false)
    expect(wrapper.text()).not.toContain('Not supported by the claude adapter')

    await clickEdit(wrapper, 'Ollama Row')
    expect(effortTrigger(wrapper).disabled).toBe(true)
    expect(wrapper.text()).toContain('Not supported by the ollama adapter')

    wrapper.unmount()
  })

  it('selecting a value updates the form and is sent as adapter_config.effort on save', async () => {
    const wrapper = mount(SpawnerSettings, { attachTo: document.body })
    await flushPromises()

    await clickEdit(wrapper, 'Claude Row')
    await selectByLabel(effortTrigger(wrapper), 'High')

    const saveBtn = wrapper.findAll('button').find(b => b.text() === 'Save Changes')
    if (!saveBtn)
      throw new Error('no Save Changes button')
    await saveBtn.trigger('click')
    await flushPromises()

    expect(updateSpawnerMock).toHaveBeenCalledWith(
      's-claude',
      expect.objectContaining({ adapterConfig: expect.objectContaining({ effort: 'high' }) }),
    )

    wrapper.unmount()
  })

  it('does not render an unrecognized stored value as a silently chosen level', async () => {
    const wrapper = mount(SpawnerSettings, { attachTo: document.body })
    await flushPromises()

    await clickEdit(wrapper, 'Legacy Effort Row')

    const trigger = effortTrigger(wrapper)
    expect(trigger.querySelector('span')?.textContent?.trim()).toBe('')
    expect(wrapper.text()).toContain('"ultra"')
    expect(wrapper.text()).toContain('not one of the known levels')

    wrapper.unmount()
  })
})
