import type { Ref } from 'vue'
import type { ResourceQuery, ResourceView } from '@/features/settings/composables/useResources'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import ResourceSettings from '@/features/settings/components/ResourceSettings.vue'
import { useResources } from '@/features/settings/composables/useResources'

vi.mock('@/features/settings/composables/useResources', async () => {
  const actual = await vi.importActual<typeof import('@/features/settings/composables/useResources')>('@/features/settings/composables/useResources')
  return {
    ...actual,
    useResources: vi.fn(),
  }
})

const baseResource: ResourceView = {
  id: 'r1',
  kind: 'application',
  slug: 'obsidian',
  name: 'Obsidian',
  scopeKind: 'global',
  scopeRef: '',
  nodeId: 'local',
  state: 'enabled',
  version: '1.0.0',
  origin: 'builtin',
  originRef: 'builtin:obsidian',
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-02T00:00:00Z',
}

describe('resourceSettings', () => {
  let resources: Ref<ResourceView[]>
  let query: Ref<ResourceQuery>
  let loading: Ref<boolean>
  let error: Ref<string | null>
  let held: Ref<boolean>
  let fetchResources: ReturnType<typeof vi.fn>

  beforeEach(() => {
    resources = ref([{ ...baseResource }])
    query = ref<ResourceQuery>({ kind: 'application', scopeKind: 'global', scopeRef: '' })
    loading = ref(false)
    error = ref(null)
    held = ref(false)
    fetchResources = vi.fn(async () => {})

    vi.mocked(useResources).mockReturnValue({
      resources,
      query,
      loading,
      error,
      held,
      fetchResources,
    } as unknown as ReturnType<typeof useResources>)
  })

  it('renders a registry row with its state, origin and scope', () => {
    const wrapper = mount(ResourceSettings, { attachTo: document.body })

    const row = wrapper.get('[data-testid="resource-row-r1"]')
    expect(row.text()).toContain('obsidian')
    expect(row.text()).toContain('Obsidian')
    expect(row.text()).toContain('enabled')
    expect(row.text()).toContain('builtin')
    expect(row.text()).toContain('global')
  })

  it('reads an empty kind as "none yet", not as a failure', () => {
    resources.value = []
    query.value = { kind: 'routine', scopeKind: 'global', scopeRef: '' }
    const wrapper = mount(ResourceSettings, { attachTo: document.body })

    expect(wrapper.get('[data-testid="resource-empty"]').text()).toContain('No routines registered yet')
    expect(wrapper.find('[data-testid="resource-error"]').exists()).toBe(false)
  })

  it('surfaces a load failure as an error, distinct from an empty registry', () => {
    resources.value = []
    error.value = 'Failed to load application resources (HTTP 500)'
    const wrapper = mount(ResourceSettings, { attachTo: document.body })

    expect(wrapper.get('[data-testid="resource-error"]').text()).toContain('HTTP 500')
    expect(wrapper.find('[data-testid="resource-empty"]').exists()).toBe(false)
  })

  it('reads a held query (scope chosen, no ref typed yet) as "enter a ref", not as an empty registry', () => {
    resources.value = []
    query.value = { kind: 'application', scopeKind: 'project', scopeRef: '' }
    held.value = true
    const wrapper = mount(ResourceSettings, { attachTo: document.body })

    expect(wrapper.get('[data-testid="resource-held"]').text()).toContain('Enter a scope ref')
    expect(wrapper.find('[data-testid="resource-empty"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="resource-error"]').exists()).toBe(false)
  })

  it('shows the loading state over a stale error or empty result, and nothing else', () => {
    resources.value = []
    error.value = 'Failed to load application resources (HTTP 500)'
    loading.value = true
    const wrapper = mount(ResourceSettings, { attachTo: document.body })

    wrapper.get('[data-testid="resource-loading"]')
    expect(wrapper.find('[data-testid="resource-error"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="resource-empty"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="resource-held"]').exists()).toBe(false)
  })

  it('refetches with the new kind when the kind is switched', async () => {
    const wrapper = mount(ResourceSettings, { attachTo: document.body })

    await wrapper.get('[data-testid="resource-kind-memory_space"]').trigger('click')
    await flushPromises()

    expect(fetchResources).toHaveBeenCalledWith({ kind: 'memory_space' })
  })

  it('clears the scope ref and refetches when the scope kind goes back to global', async () => {
    query.value = { kind: 'application', scopeKind: 'project', scopeRef: '/tmp/demo' }
    const wrapper = mount(ResourceSettings, { attachTo: document.body })

    await wrapper.get('[data-testid="resource-scope-global"]').trigger('click')
    await flushPromises()

    expect(fetchResources).toHaveBeenCalledWith({ scopeKind: 'global', scopeRef: '' })
  })
})
