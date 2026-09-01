import type { ResourceView } from '@/features/settings/composables/useResources'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import ResourceSettings from '@/features/settings/components/ResourceSettings.vue'

// useResources is deliberately NOT mocked. Mocking it wholesale made every
// assertion here a statement about hand-written refs, so the panel could
// display one thing while the composable had requested another — which is
// exactly the defect these specs exist to catch. Stubbing `fetch` instead keeps
// what is rendered and what went on the wire in the same test.

function okResponse(body: unknown) {
  return { ok: true, status: 200, json: async () => body }
}

function errorResponse(status: number) {
  return { ok: false, status, json: async () => ({}) }
}

function deniedResponse(message?: string) {
  return {
    ok: false,
    status: 403,
    json: () => message ? Promise.resolve({ error: message }) : Promise.reject(new Error('no body')),
  }
}

let handler: (url: string) => unknown

function calls(): string[] {
  return vi.mocked(globalThis.fetch).mock.calls.map(c => String(c[0]))
}

function lastUrl(): string {
  const all = calls()
  return all[all.length - 1]
}

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

beforeEach(() => {
  handler = () => okResponse([])
  vi.stubGlobal('fetch', vi.fn((url: string) => Promise.resolve(handler(url))))
})

afterEach(() => {
  vi.unstubAllGlobals()
})

// VTU's setValue fires `input` AND `change`, which is precisely the difference
// under test — so typing is simulated by hand and committed separately.
async function typeScopeRef(wrapper: ReturnType<typeof mount>, value: string) {
  const input = wrapper.get('[data-testid="resource-scope-ref"]')
  ;(input.element as HTMLInputElement).value = value
  await input.trigger('input')
  await flushPromises()
  return input
}

async function mountPanel() {
  const wrapper = mount(ResourceSettings, { attachTo: document.body })
  await flushPromises()
  return wrapper
}

describe('resourceSettings', () => {
  it('renders a registry row and keeps the scope ref, so same-slug rows stay distinguishable', async () => {
    handler = () => okResponse([
      baseResource,
      { ...baseResource, id: 'r2', scopeKind: 'project', scopeRef: '/tmp/a' },
      { ...baseResource, id: 'r3', scopeKind: 'project', scopeRef: '/tmp/b' },
    ])
    const wrapper = await mountPanel()

    const row = wrapper.get('[data-testid="resource-row-r1"]')
    expect(row.text()).toContain('obsidian')
    expect(row.text()).toContain('Obsidian')
    expect(row.text()).toContain('enabled')
    expect(row.text()).toContain('builtin')
    expect(row.text()).toContain('global')
    expect(wrapper.get('[data-testid="resource-row-r2"]').text()).toContain('project: /tmp/a')
    expect(wrapper.get('[data-testid="resource-row-r3"]').text()).toContain('project: /tmp/b')
  })

  it('asks for the selected kind and scope, and reads an empty answer as "none yet"', async () => {
    const wrapper = await mountPanel()
    expect(lastUrl()).toBe('/api/resources?kind=application&scope=global')

    await wrapper.get('[data-testid="resource-kind-routine"]').trigger('click')
    await flushPromises()

    expect(lastUrl()).toBe('/api/resources?kind=routine&scope=global')
    expect(wrapper.get('[data-testid="resource-empty"]').text()).toContain('No routines registered yet')
    expect(wrapper.find('[data-testid="resource-error"]').exists()).toBe(false)
  })

  it('surfaces a non-ok response as an error, distinct from an empty registry', async () => {
    handler = () => errorResponse(500)
    const wrapper = await mountPanel()

    expect(wrapper.get('[data-testid="resource-error"]').text()).toContain('HTTP 500')
    expect(wrapper.find('[data-testid="resource-empty"]').exists()).toBe(false)
  })

  it('offers a retry that re-issues the failed query and clears the banner', async () => {
    handler = () => errorResponse(500)
    const wrapper = await mountPanel()
    const failedUrl = lastUrl()

    handler = () => okResponse([baseResource])
    await wrapper.get('[data-testid="resource-retry"]').trigger('click')
    await flushPromises()

    expect(lastUrl()).toBe(failedUrl)
    expect(wrapper.find('[data-testid="resource-error"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="resource-row-r1"]').exists()).toBe(true)
  })

  // Regression guard for the round-3 blocking finding. Typing used to write
  // query.scopeRef on every keystroke while only `change` fired the request, so
  // `held` went false against a scope that had never been asked about and the
  // panel reported the previous query's empty result as this scope's answer.
  it('never calls a scope empty before a query for it has been issued', async () => {
    const wrapper = await mountPanel()
    await wrapper.get('[data-testid="resource-scope-project"]').trigger('click')
    await flushPromises()
    const before = calls().length

    const input = await typeScopeRef(wrapper, '/tmp/demo')

    expect(calls().length).toBe(before)
    expect(wrapper.find('[data-testid="resource-empty"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="resource-held"]').text()).toContain('Enter a scope ref')

    handler = () => okResponse([baseResource])
    await input.trigger('change')
    await flushPromises()

    expect(lastUrl()).toBe('/api/resources?kind=application&scope=project&scopeRef=%2Ftmp%2Fdemo')
    expect(wrapper.find('[data-testid="resource-row-r1"]').exists()).toBe(true)
  })

  it('treats a whitespace-only ref as no ref, not as an answered scope', async () => {
    const wrapper = await mountPanel()
    await wrapper.get('[data-testid="resource-scope-project"]').trigger('click')
    await flushPromises()
    const before = calls().length

    const input = await typeScopeRef(wrapper, '   ')
    await input.trigger('change')
    await flushPromises()

    expect(calls().length).toBe(before)
    expect(wrapper.find('[data-testid="resource-held"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="resource-empty"]').exists()).toBe(false)
  })

  it('clears the scope ref and refetches when the scope kind goes back to global', async () => {
    const wrapper = await mountPanel()
    await wrapper.get('[data-testid="resource-scope-project"]').trigger('click')
    const input = await typeScopeRef(wrapper, '/tmp/demo')
    await input.trigger('change')
    await flushPromises()

    await wrapper.get('[data-testid="resource-scope-global"]').trigger('click')
    await flushPromises()

    expect(lastUrl()).toBe('/api/resources?kind=application&scope=global')
    expect(wrapper.find('[data-testid="resource-scope-ref"]').exists()).toBe(false)
  })

  it('does not refire when the already-active kind or scope chip is clicked', async () => {
    const wrapper = await mountPanel()
    const before = calls().length

    await wrapper.get('[data-testid="resource-kind-application"]').trigger('click')
    await wrapper.get('[data-testid="resource-scope-global"]').trigger('click')
    await flushPromises()

    expect(calls().length).toBe(before)
  })

  it('shows the loading state, and nothing else, on first paint', async () => {
    handler = () => new Promise(() => {})
    const wrapper = mount(ResourceSettings, { attachTo: document.body })
    await nextTick()

    expect(wrapper.find('[data-testid="resource-loading"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="resource-empty"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="resource-error"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="resource-held"]').exists()).toBe(false)
  })

  it('keeps the live region mounted across states so a change is announced', async () => {
    handler = () => new Promise(() => {})
    const wrapper = mount(ResourceSettings, { attachTo: document.body })
    await nextTick()
    const region = wrapper.get('[data-testid="resource-status"]')
    expect(region.attributes('role')).toBe('status')
    expect(region.attributes('aria-live')).toBe('polite')

    handler = () => okResponse([baseResource])
    await wrapper.get('[data-testid="resource-kind-routine"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="resource-status"]').attributes('role')).toBe('status')
    expect(wrapper.find('[data-testid="resource-loading"]').exists()).toBe(false)
  })

  // `kind=memory_space` is gated on memory.read, so a fresh install meets this
  // before it meets any row. It must not read as a broken registry.
  it('reads a refused memory_space read as a denial, not as a failure', async () => {
    const wrapper = await mountPanel()

    handler = () => deniedResponse('memory.read is not granted for scope global')
    await wrapper.get('[data-testid="resource-kind-memory_space"]').trigger('click')
    await flushPromises()

    const notice = wrapper.get('[data-testid="resource-denied"]')
    expect(notice.text()).toContain('memory.read is not granted for scope global')
    expect(notice.text()).toContain('Most likely cause')
    expect(wrapper.find('[data-testid="resource-error"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="resource-empty"]').exists()).toBe(false)
  })

  it('still reads a 500 on the gated kind as a failure', async () => {
    const wrapper = await mountPanel()

    handler = () => errorResponse(500)
    await wrapper.get('[data-testid="resource-kind-memory_space"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="resource-error"]').text()).toContain('HTTP 500')
    expect(wrapper.find('[data-testid="resource-denied"]').exists()).toBe(false)
  })

  it('clears the denial when an ungated kind is selected', async () => {
    const wrapper = await mountPanel()
    handler = () => deniedResponse('memory.read is not granted for scope global')
    await wrapper.get('[data-testid="resource-kind-memory_space"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="resource-denied"]').exists()).toBe(true)

    handler = () => okResponse([baseResource])
    await wrapper.get('[data-testid="resource-kind-application"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="resource-denied"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="resource-row-r1"]').exists()).toBe(true)
  })

  it('marks the active kind and scope chips as pressed', async () => {
    const wrapper = await mountPanel()

    expect(wrapper.get('[data-testid="resource-kind-application"]').attributes('aria-pressed')).toBe('true')
    expect(wrapper.get('[data-testid="resource-kind-routine"]').attributes('aria-pressed')).toBe('false')
    expect(wrapper.get('[data-testid="resource-scope-global"]').attributes('aria-pressed')).toBe('true')
    expect(wrapper.get('[data-testid="resource-scope-project"]').attributes('aria-pressed')).toBe('false')
  })

  it('gives the scope ref field an accessible name', async () => {
    const wrapper = await mountPanel()
    await wrapper.get('[data-testid="resource-scope-project"]').trigger('click')
    await flushPromises()

    const input = wrapper.get('[data-testid="resource-scope-ref"]')
    const label = wrapper.get(`label[for="${input.attributes('id')}"]`)
    expect(label.text()).toBe('Scope ref')
  })
})
