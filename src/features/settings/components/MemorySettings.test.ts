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
  let searchError: Ref<string | null>
  let denied: Ref<string | null>
  let held: Ref<boolean>
  let globalSpaces: Ref<MemorySpace[]>
  let searchEntries: ReturnType<typeof vi.fn>
  let setScope: ReturnType<typeof vi.fn>

  beforeEach(() => {
    spaces = ref([{ ...baseSpace }])
    entries = ref([])
    scope = ref<MemoryScope>({ scopeKind: 'global', scopeRef: '' })
    searchText = ref('')
    loading = ref(false)
    error = ref(null)
    searchError = ref(null)
    denied = ref(null)
    held = ref(false)
    globalSpaces = ref([])
    searchEntries = vi.fn(async () => {
      entries.value = [{ ...baseHit }]
    })
    setScope = vi.fn(async () => {})

    vi.mocked(useMemory).mockReturnValue({
      spaces,
      globalSpaces,
      entries,
      scope,
      searchText,
      loading,
      error,
      searchError,
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
    // scope is deliberately left at the default global value: held is the
    // ONLY signal driving this branch. If the template ever re-derived the
    // predicate from `scope` instead of reading `held`, this would still
    // pass with `scope.scopeKind === 'global'` set explicitly here — so it
    // is left untouched, matching the composable's mocked defaults.
    held.value = true
    spaces.value = []
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    expect(wrapper.find('[data-testid="memory-held"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="memory-empty"]').exists()).toBe(false)
  })

  // F5.3: the loading branch was the only one of the five states with no
  // testid and no coverage.
  it('shows the loading state with its own testid', () => {
    loading.value = true
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    expect(wrapper.find('[data-testid="memory-loading"]').exists()).toBe(true)
  })

  // F2: the search box used to be gated on spaces.length, but a scope can
  // have zero exact-scope spaces and still have searchable entries (the
  // retriever unions in every global space). Search must stay reachable
  // even when the spaces table renders its empty state.
  it('keeps the search box reachable when the spaces list is empty', () => {
    spaces.value = []
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    expect(wrapper.find('[data-testid="memory-empty"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="memory-search-input"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="memory-search-submit"]').exists()).toBe(true)
  })

  // F3: a search failure must render inline, next to the search box, not
  // replace the already-loaded spaces table the way the shared error ref did.
  it('renders a search failure inline, next to the results, leaving the spaces table in place', () => {
    searchError.value = 'search boom'
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    expect(wrapper.get('[data-testid="memory-search-error"]').text()).toContain('search boom')
    expect(wrapper.find('[data-testid="memory-space-s1"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="memory-search-input"]').exists()).toBe(true)
  })

  // F7: a hit from a global space has no row in `spaces` (exact-scope only)
  // — label resolution must fall back to the separately-fetched global list
  // and mark the result as coming from outside the selected scope.
  it('resolves a hit from a global space via globalSpaces and marks it outside this scope', () => {
    globalSpaces.value = [{ ...baseSpace, id: 'g1', slug: 'shared-notes', scopeKind: 'global' }]
    entries.value = [{ ...baseHit, spaceId: 'g1' }]
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    const hit = wrapper.get('[data-testid="memory-entry-e1"]')
    expect(hit.text()).toContain('shared-notes')
    expect(wrapper.find('[data-testid="memory-entry-outside-scope-e1"]').exists()).toBe(true)
  })

  it('falls back to the raw id when a hit resolves in neither spaces nor globalSpaces', () => {
    entries.value = [{ ...baseHit, spaceId: 'unknown-space' }]
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    const hit = wrapper.get('[data-testid="memory-entry-e1"]')
    expect(hit.text()).toContain('unknown-space')
    expect(wrapper.find('[data-testid="memory-entry-outside-scope-e1"]').exists()).toBe(false)
  })
})
