import type { Ref } from 'vue'
import type { MemoryEntryHit, MemoryScope, MemorySpace } from '@/features/settings/composables/useMemory'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import MemorySettings from '@/features/settings/components/MemorySettings.vue'
import { useMemory } from '@/features/settings/composables/useMemory'

vi.mock('@/features/settings/composables/useMemory', async () => {
  const actual = await vi.importActual<typeof import('@/features/settings/composables/useMemory')>('@/features/settings/composables/useMemory')
  return {
    ...actual,
    useMemory: vi.fn(),
  }
})

const baseSpace: MemorySpace = {
  id: 's1',
  kind: 'memory_space',
  slug: 'project-notes',
  name: 'Project notes',
  scopeKind: 'global',
  scopeRef: '',
  nodeId: 'local',
  state: 'enabled',
  version: '',
  origin: 'local',
  originRef: '',
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}

const baseHit: MemoryEntryHit = {
  id: 'e1',
  spaceId: 's1',
  summary: 'The dashboard binds to 127.0.0.1',
  content: 'Long form content.',
  kind: 'fact',
  confidence: 0.9,
  createdAt: '2026-01-01T00:00:00Z',
}

describe('memorySettings', () => {
  let spaces: Ref<MemorySpace[]>
  let entries: Ref<MemoryEntryHit[]>
  let scope: Ref<MemoryScope>
  let searchText: Ref<string>
  let loading: Ref<boolean>
  let error: Ref<string | null>
  let denied: Ref<string | null>
  let held: Ref<boolean>
  let searchEntries: ReturnType<typeof vi.fn>
  let setScope: ReturnType<typeof vi.fn>

  beforeEach(() => {
    spaces = ref([{ ...baseSpace }])
    entries = ref([])
    scope = ref<MemoryScope>({ scopeKind: 'global', scopeRef: '' })
    searchText = ref('')
    loading = ref(false)
    error = ref(null)
    denied = ref(null)
    held = ref(false)
    searchEntries = vi.fn(async () => {
      entries.value = [{ ...baseHit }]
    })
    setScope = vi.fn(async () => {})

    vi.mocked(useMemory).mockReturnValue({
      spaces,
      entries,
      scope,
      searchText,
      loading,
      error,
      denied,
      held,
      fetchSpaces: vi.fn(),
      searchEntries,
      setScope,
    } as unknown as ReturnType<typeof useMemory>)
  })

  it('lists the memory spaces in scope', () => {
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    const row = wrapper.get('[data-testid="memory-space-s1"]')
    expect(row.text()).toContain('project-notes')
    expect(row.text()).toContain('Project notes')
  })

  it('renders a capability denial as an explanation, not as an error', () => {
    denied.value = 'capability memory.read denied in scope global'
    spaces.value = []
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    const notice = wrapper.get('[data-testid="memory-denied"]')
    expect(notice.text()).toContain('memory.read')
    expect(notice.text()).toContain('Grants')
    expect(wrapper.find('[data-testid="memory-error"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="memory-empty"]').exists()).toBe(false)
  })

  it('renders a transport failure as an error, distinct from a denial', () => {
    error.value = 'HTTP 500'
    spaces.value = []
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    expect(wrapper.get('[data-testid="memory-error"]').text()).toContain('HTTP 500')
    expect(wrapper.find('[data-testid="memory-denied"]').exists()).toBe(false)
  })

  it('searches entries and renders the hits with their confidence', async () => {
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    await wrapper.get('[data-testid="memory-search-input"]').setValue('binds')
    await wrapper.get('[data-testid="memory-search-submit"]').trigger('click')
    await flushPromises()

    expect(searchEntries).toHaveBeenCalled()
    const hit = wrapper.get('[data-testid="memory-entry-e1"]')
    expect(hit.text()).toContain('The dashboard binds to 127.0.0.1')
    expect(hit.text()).toContain('fact')
    expect(hit.text()).toContain('0.90')
  })

  it('switches scope through setScope rather than mutating the ref directly', async () => {
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    await wrapper.get('[data-testid="memory-scope-project"]').trigger('click')
    await flushPromises()

    expect(setScope).toHaveBeenCalledWith({ scopeKind: 'project', scopeRef: '' })
  })

  // Task 3's fourth-state lesson: a query never sent must not render as
  // "confirmed empty" — the empty-state message asserts nothing was found,
  // which is not knowable before a request with a ref has actually fired.
  it('renders the not-yet-asked state distinctly from confirmed-empty when a scope ref is required but missing', () => {
    scope.value = { scopeKind: 'project', scopeRef: '' }
    held.value = true
    spaces.value = []
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    expect(wrapper.find('[data-testid="memory-held"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="memory-empty"]').exists()).toBe(false)
  })
})
