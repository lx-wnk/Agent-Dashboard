import type { Ref } from 'vue'
import type { MemoryEntryHit, MemoryScope, MemorySpace } from '@/features/settings/composables/useMemory'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import MemorySettings from '@/features/settings/components/MemorySettings.vue'
import { MemoryWriteDeniedError, useMemory } from '@/features/settings/composables/useMemory'
import { selectByLabel } from '@/utils/testSelect'

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
  let searchDenied: Ref<string | null>
  let searching: Ref<boolean>
  let searched: Ref<boolean>
  let held: Ref<boolean>
  let globalSpaces: Ref<MemorySpace[]>
  let fetchSpaces: ReturnType<typeof vi.fn>
  let searchEntries: ReturnType<typeof vi.fn>
  let setScope: ReturnType<typeof vi.fn>
  let createSpace: ReturnType<typeof vi.fn>
  let createEntry: ReturnType<typeof vi.fn>
  let supersedeEntry: ReturnType<typeof vi.fn>
  let expireEntry: ReturnType<typeof vi.fn>

  beforeEach(() => {
    spaces = ref([{ ...baseSpace }])
    entries = ref([])
    scope = ref<MemoryScope>({ scopeKind: 'global', scopeRef: '' })
    searchText = ref('')
    loading = ref(false)
    error = ref(null)
    searchError = ref(null)
    denied = ref(null)
    searchDenied = ref(null)
    searching = ref(false)
    searched = ref(false)
    held = ref(false)
    globalSpaces = ref([])
    fetchSpaces = vi.fn()
    searchEntries = vi.fn(async () => {
      entries.value = [{ ...baseHit }]
      searched.value = true
    })
    setScope = vi.fn(async () => {})
    createSpace = vi.fn(async () => {})
    createEntry = vi.fn(async () => {})
    supersedeEntry = vi.fn(async () => {})
    expireEntry = vi.fn(async () => {})

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
      searchDenied,
      searching,
      searched,
      held,
      fetchSpaces,
      searchEntries,
      setScope,
      createSpace,
      createEntry,
      supersedeEntry,
      expireEntry,
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
  // The panel sends only what the form owns. The scope is added inside the
  // composable, from the single `scope` ref — see the wire-body assertions in
  // composables/__tests__/useMemory.test.ts.
  it('creates a space with the slug and name from the form', async () => {
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    await wrapper.get('[data-testid="memory-space-new"]').trigger('click')
    await wrapper.get('[data-testid="memory-space-slug"]').setValue('  new-space  ')
    await wrapper.get('[data-testid="memory-space-name"]').setValue('  New space  ')
    await wrapper.get('[data-testid="memory-space-submit"]').trigger('click')
    await flushPromises()

    expect(createSpace).toHaveBeenCalledWith({ slug: 'new-space', name: 'New space' })
    expect(wrapper.find('[data-testid="memory-space-slug"]').exists()).toBe(false)
  })

  it('refuses to create a space without a slug and says so next to the form', async () => {
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    await wrapper.get('[data-testid="memory-space-new"]').trigger('click')
    await wrapper.get('[data-testid="memory-space-name"]').setValue('New space')
    await wrapper.get('[data-testid="memory-space-submit"]').trigger('click')
    await flushPromises()

    expect(createSpace).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="memory-space-error"]').text()).toContain('Slug')
  })

  it('creates an entry with its kind, source kind and confidence', async () => {
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    await wrapper.get('[data-testid="memory-entry-new"]').trigger('click')
    await wrapper.get('[data-testid="memory-entry-space"]').setValue('project-notes')
    await wrapper.get('[data-testid="memory-entry-summary"]').setValue('Binds to loopback')
    await wrapper.get('[data-testid="memory-entry-content"]').setValue('The server binds 127.0.0.1 only.')
    await wrapper.get('[data-testid="memory-entry-submit"]').trigger('click')
    await flushPromises()

    expect(createEntry).toHaveBeenCalledWith({
      spaceSlug: 'project-notes',
      summary: 'Binds to loopback',
      content: 'The server binds 127.0.0.1 only.',
      kind: 'fact',
      sourceKind: 'user',
      sourceRef: '',
      confidence: 1,
    })
    expect(wrapper.find('[data-testid="memory-entry-space"]').exists()).toBe(false)
  })

  // The form validates the trimmed value; sending the raw one 403s on
  // capability.Match's exact string comparison, and the panel then blames a
  // grant the user already holds. One trim, used for both.
  it('sends the trimmed values it validated, not the raw ones', async () => {
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    await wrapper.get('[data-testid="memory-entry-new"]').trigger('click')
    await wrapper.get('[data-testid="memory-entry-space"]').setValue('  project-notes  ')
    await wrapper.get('[data-testid="memory-entry-summary"]').setValue('  Binds to loopback  ')
    await wrapper.get('[data-testid="memory-entry-content"]').setValue('  The server binds 127.0.0.1 only.  ')
    await wrapper.get('[data-testid="memory-entry-submit"]').trigger('click')
    await flushPromises()

    expect(createEntry).toHaveBeenCalledWith({
      spaceSlug: 'project-notes',
      summary: 'Binds to loopback',
      content: 'The server binds 127.0.0.1 only.',
      kind: 'fact',
      sourceKind: 'user',
      sourceRef: '',
      confidence: 1,
    })
  })

  // Neither select was exercised: re-binding the kind select to sourceKind
  // shipped green.
  it('sends the kind and source kind chosen in the selects', async () => {
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    await wrapper.get('[data-testid="memory-entry-new"]').trigger('click')
    await wrapper.get('[data-testid="memory-entry-space"]').setValue('project-notes')
    await wrapper.get('[data-testid="memory-entry-summary"]').setValue('Binds to loopback')
    await wrapper.get('[data-testid="memory-entry-content"]').setValue('The server binds 127.0.0.1 only.')
    await selectByLabel(wrapper.get('[data-testid="memory-entry-kind"]').element, 'lesson')
    await selectByLabel(wrapper.get('[data-testid="memory-entry-source-kind"]').element, 'agent')
    await wrapper.get('[data-testid="memory-entry-submit"]').trigger('click')
    await flushPromises()

    expect(createEntry).toHaveBeenCalledWith(expect.objectContaining({ kind: 'lesson', sourceKind: 'agent' }))
  })

  it('supersedes an entry with the replacement id from the inline form', async () => {
    entries.value = [{ ...baseHit }]
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    await wrapper.get('[data-testid="memory-supersede-e1"]').trigger('click')
    await wrapper.get('[data-testid="memory-supersede-input-e1"]').setValue('e2')
    await wrapper.get('[data-testid="memory-supersede-confirm-e1"]').trigger('click')
    await flushPromises()

    expect(supersedeEntry).toHaveBeenCalledWith('e1', 'e2')
  })

  it('expires an entry only after the confirm step', async () => {
    entries.value = [{ ...baseHit }]
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    await wrapper.get('[data-testid="memory-expire-e1"]').trigger('click')
    expect(expireEntry).not.toHaveBeenCalled()

    await wrapper.get('[data-testid="memory-expire-confirm-e1"]').trigger('click')
    await flushPromises()

    expect(expireEntry).toHaveBeenCalledWith('e1')
  })

  // memory.write is a separate grant from memory.read: a user who can read
  // sees a fully populated panel and is refused only at the write. That
  // refusal belongs next to the form that hit it — not in the panel-level
  // `denied` box the read side owns, and not by blanking the spaces table
  // the read grant legitimately filled.
  it('renders a write denial next to the form, without disturbing the read side', async () => {
    createSpace.mockRejectedValue(new MemoryWriteDeniedError('capability memory.write denied in scope global'))
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    await wrapper.get('[data-testid="memory-space-new"]').trigger('click')
    await wrapper.get('[data-testid="memory-space-slug"]').setValue('new-space')
    await wrapper.get('[data-testid="memory-space-submit"]').trigger('click')
    await flushPromises()

    const notice = wrapper.get('[data-testid="memory-space-error"]')
    expect(notice.text()).toContain('memory.write')
    expect(notice.text()).toContain('Grants')
    expect(wrapper.find('[data-testid="memory-denied"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="memory-space-s1"]').exists()).toBe(true)
    // The form closes on success only. Closing it before the await would
    // read as a success and throw away what the user typed.
    const slug = wrapper.find('[data-testid="memory-space-slug"]')
    expect(slug.exists()).toBe(true)
    expect((slug.element as HTMLInputElement).value).toBe('new-space')
  })

  // The counterpart to the test above: the capability hint is attached to a
  // denial, not to every write failure. Appending it unconditionally would
  // satisfy that test and tell a user with the grant to go fix a grant.
  it('renders an ordinary write failure without the capability hint', async () => {
    createSpace.mockRejectedValue(new Error('boom'))
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    await wrapper.get('[data-testid="memory-space-new"]').trigger('click')
    await wrapper.get('[data-testid="memory-space-slug"]').setValue('new-space')
    await wrapper.get('[data-testid="memory-space-submit"]').trigger('click')
    await flushPromises()

    const notice = wrapper.get('[data-testid="memory-space-error"]')
    expect(notice.text()).toContain('boom')
    expect(notice.text()).not.toContain('Grants')
  })

  // Two rows on purpose: the id on entryActionFailure exists for exactly one
  // requirement — render the failure where it happened — and with a single
  // row a template that ignores the id looks identical.
  it('renders a denied expire in the row it was triggered from, and nowhere else', async () => {
    entries.value = [{ ...baseHit }, { ...baseHit, id: 'e2', summary: 'Another entry' }]
    expireEntry.mockRejectedValue(new MemoryWriteDeniedError('capability memory.write denied in scope global'))
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    await wrapper.get('[data-testid="memory-expire-e1"]').trigger('click')
    await wrapper.get('[data-testid="memory-expire-confirm-e1"]').trigger('click')
    await flushPromises()

    const notice = wrapper.get('[data-testid="memory-entry-action-error-e1"]')
    expect(notice.text()).toContain('memory.write')
    expect(notice.text()).toContain('Grants')
    expect(wrapper.find('[data-testid="memory-entry-action-error-e2"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="memory-denied"]').exists()).toBe(false)
  })

  // The write controls are gated on nothing the read side reports: a read
  // denial says nothing about memory.write, and hiding them would leave a
  // user who holds write with no way to use it.
  it('keeps the write controls reachable while the read side is denied', () => {
    denied.value = 'capability memory.read denied in scope global'
    spaces.value = []
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    expect(wrapper.find('[data-testid="memory-space-new"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="memory-entry-new"]').exists()).toBe(true)
  })

  // The search's denial is not the panel's denial: the spaces table above it
  // answered the same grant check successfully, and blanking it loses both
  // the rows and the retry path.
  it('renders a denied search inline, leaving the loaded spaces table in place', () => {
    searchDenied.value = 'capability memory.read denied in scope global'
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    const notice = wrapper.get('[data-testid="memory-search-denied"]')
    expect(notice.text()).toContain('memory.read denied in scope global')
    expect(wrapper.find('[data-testid="memory-space-s1"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="memory-denied"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="memory-empty"]').exists()).toBe(false)
  })

  // "Found nothing", "never asked" and "still asking" were one identical
  // blank: the hits were a bare v-for with no empty branch at all.
  it('tells a search that found nothing apart from one that never ran', async () => {
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    expect(wrapper.get('[data-testid="memory-entries-unsearched"]').text()).toContain('No search')
    expect(wrapper.find('[data-testid="memory-entries-empty"]').exists()).toBe(false)

    searchEntries.mockImplementation(async () => {
      entries.value = []
      searched.value = true
    })
    await wrapper.get('[data-testid="memory-search-submit"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="memory-entries-empty"]').text()).toContain('No entries matched')
    expect(wrapper.find('[data-testid="memory-entries-unsearched"]').exists()).toBe(false)
  })

  it('tells an in-flight search and a held one apart from both', async () => {
    searching.value = true
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    expect(wrapper.find('[data-testid="memory-entries-searching"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="memory-entries-unsearched"]').exists()).toBe(false)

    searching.value = false
    held.value = true
    await flushPromises()

    expect(wrapper.find('[data-testid="memory-entries-held"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="memory-entries-empty"]').exists()).toBe(false)
  })

  // A refused search says nothing about what matches, so the four-state
  // region stays silent and lets the denial notice speak.
  it('leaves the hit-state region silent while a denial or a failure explains it', async () => {
    searchDenied.value = 'capability memory.read denied in scope global'
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    expect(wrapper.get('[data-testid="memory-entries-status"]').text()).toBe('')

    searchDenied.value = null
    searchError.value = 'search boom'
    await flushPromises()

    expect(wrapper.get('[data-testid="memory-entries-status"]').text()).toBe('')
  })

  // handler.go maps every Gate.Authorize failure to 403 — a rate limit and a
  // failed grant-store read included — so the panel may not name a cause the
  // server never distinguished. The server's own message leads.
  it('leads a read denial with the server message and demotes the grant advice to a likely cause', () => {
    denied.value = 'grant g1 allows 1 use per 60s; 1 already used'
    spaces.value = []
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    const text = wrapper.get('[data-testid="memory-denied"]').text()
    expect(text.startsWith('grant g1 allows 1 use per 60s; 1 already used')).toBe(true)
    expect(text).toContain('Most likely cause')
  })

  it('leads a write denial with the server message and demotes the grant advice the same way', async () => {
    createSpace.mockRejectedValue(new MemoryWriteDeniedError('grant g2 allows 1 use per 60s; 1 already used'))
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    await wrapper.get('[data-testid="memory-space-new"]').trigger('click')
    await wrapper.get('[data-testid="memory-space-slug"]').setValue('new-space')
    await wrapper.get('[data-testid="memory-space-submit"]').trigger('click')
    await flushPromises()

    const text = wrapper.get('[data-testid="memory-space-error"]').text()
    expect(text.startsWith('grant g2 allows 1 use per 60s; 1 already used')).toBe(true)
    expect(text).toContain('Most likely cause')
  })

  // Ten sibling panels label their controls; these two selects had no
  // accessible name at all — a screen reader announced "combo box" twice.
  it('gives every control in the panel a label bound to its id', async () => {
    scope.value = { scopeKind: 'project', scopeRef: '/tmp/demo' }
    entries.value = [{ ...baseHit }]
    const wrapper = mount(MemorySettings, { attachTo: document.body })
    await wrapper.get('[data-testid="memory-space-new"]').trigger('click')
    await wrapper.get('[data-testid="memory-entry-new"]').trigger('click')
    await wrapper.get('[data-testid="memory-supersede-e1"]').trigger('click')

    const root = wrapper.element as HTMLElement
    const controls = Array.from(root.querySelectorAll('input, textarea, [role="combobox"]'))
    expect(controls).toHaveLength(10)
    for (const control of controls) {
      const id = control.getAttribute('id')
      expect(id, `no id on ${control.outerHTML}`).toBeTruthy()
      expect(root.querySelector(`label[for="${id}"]`), `no label for #${id}`).not.toBeNull()
    }
  })

  // A toggle whose label never changes ("+ New space" either way) is the
  // only cue that the form can be dismissed again.
  it('reports on each form toggle whether its form is open', async () => {
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    for (const testid of ['memory-space-new', 'memory-entry-new']) {
      const toggle = wrapper.get(`[data-testid="${testid}"]`)
      expect(toggle.attributes('aria-expanded')).toBe('false')
      await toggle.trigger('click')
      expect(toggle.attributes('aria-expanded')).toBe('true')
    }
  })

  // Closing the form unmounts the button that has focus, which drops it to
  // <body> and loses the keyboard user's place in a long panel.
  it('returns focus to the toggle that opened a form when the form closes', async () => {
    const wrapper = mount(MemorySettings, { attachTo: document.body })
    const toggle = wrapper.get('[data-testid="memory-space-new"]')
    await toggle.trigger('click')
    ;(wrapper.get('[data-testid="memory-space-submit"]').element as HTMLElement).focus()

    await wrapper.get('[data-testid="memory-space-slug"]').setValue('new-space')
    await wrapper.get('[data-testid="memory-space-submit"]').trigger('click')
    await flushPromises()

    expect(document.activeElement).toBe(toggle.element)
  })

  it('returns focus to the row control that opened an inline confirm', async () => {
    entries.value = [{ ...baseHit }]
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    await wrapper.get('[data-testid="memory-expire-e1"]').trigger('click')
    await wrapper.get('[data-testid="memory-expire-confirm-e1"]').trigger('click')
    await flushPromises()

    expect(document.activeElement).toBe(wrapper.get('[data-testid="memory-expire-e1"]').element)
  })

  // A form that just closed leaves nothing on screen to read: the outcome of
  // a write reaches a screen reader only through a live region.
  it('announces a successful write through an always-mounted live region', async () => {
    const wrapper = mount(MemorySettings, { attachTo: document.body })
    const live = wrapper.get('[data-testid="memory-announcement"]')
    expect(live.attributes('role')).toBe('status')
    expect(live.attributes('aria-live')).toBe('polite')
    expect(live.text()).toBe('')

    await wrapper.get('[data-testid="memory-space-new"]').trigger('click')
    await wrapper.get('[data-testid="memory-space-slug"]').setValue('new-space')
    await wrapper.get('[data-testid="memory-space-submit"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="memory-announcement"]').text()).toContain('new-space')
  })

  it('announces a superseded and an expired entry too', async () => {
    entries.value = [{ ...baseHit }]
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    await wrapper.get('[data-testid="memory-supersede-e1"]').trigger('click')
    await wrapper.get('[data-testid="memory-supersede-input-e1"]').setValue('e2')
    await wrapper.get('[data-testid="memory-supersede-confirm-e1"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="memory-announcement"]').text()).toContain('superseded')

    await wrapper.get('[data-testid="memory-expire-e1"]').trigger('click')
    await wrapper.get('[data-testid="memory-expire-confirm-e1"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="memory-announcement"]').text()).toContain('expired')
  })

  // A live region mounted together with its text is not announced: it has to
  // already be in the DOM when the content changes.
  it('keeps the loading/held live region mounted while it has nothing to say', async () => {
    const wrapper = mount(MemorySettings, { attachTo: document.body })
    const live = wrapper.get('[data-testid="memory-status"]')
    expect(live.attributes('role')).toBe('status')
    expect(live.attributes('aria-live')).toBe('polite')
    expect(live.text()).toBe('')

    loading.value = true
    await flushPromises()

    expect(wrapper.get('[data-testid="memory-status"]').element).toBe(live.element)
    expect(wrapper.get('[data-testid="memory-loading"]').text()).toContain('Loading')
  })

  // Without this the only way out of a failed load is a two-click scope
  // detour that nothing on screen signposts.
  it('offers a retry on the error state', async () => {
    error.value = 'HTTP 500'
    spaces.value = []
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    await wrapper.get('[data-testid="memory-retry"]').trigger('click')

    expect(fetchSpaces).toHaveBeenCalled()
  })

  it('refuses to create an entry without a space, a summary and content', async () => {
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    await wrapper.get('[data-testid="memory-entry-new"]').trigger('click')
    await wrapper.get('[data-testid="memory-entry-summary"]').setValue('Binds to loopback')
    await wrapper.get('[data-testid="memory-entry-submit"]').trigger('click')
    await flushPromises()

    expect(createEntry).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="memory-entry-error"]').text()).toContain('required')
  })

  it('refuses to supersede without a replacement id, and says so in that row', async () => {
    entries.value = [{ ...baseHit }]
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    await wrapper.get('[data-testid="memory-supersede-e1"]').trigger('click')
    await wrapper.get('[data-testid="memory-supersede-input-e1"]').setValue('   ')
    await wrapper.get('[data-testid="memory-supersede-confirm-e1"]').trigger('click')
    await flushPromises()

    expect(supersedeEntry).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="memory-entry-action-error-e1"]').text()).toContain('required')
  })

  // Same reason the create form trims: capability.Match compares exactly, and
  // a padded id is also simply not an id.
  it('sends the trimmed replacement id', async () => {
    entries.value = [{ ...baseHit }]
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    await wrapper.get('[data-testid="memory-supersede-e1"]').trigger('click')
    await wrapper.get('[data-testid="memory-supersede-input-e1"]').setValue('  e2  ')
    await wrapper.get('[data-testid="memory-supersede-confirm-e1"]').trigger('click')
    await flushPromises()

    expect(supersedeEntry).toHaveBeenCalledWith('e1', 'e2')
  })

  // setScope clears the hits and refetches; re-running it for the scope that
  // is already selected throws away a search the user just ran.
  it('ignores a click on the scope that is already selected', async () => {
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    await wrapper.get('[data-testid="memory-scope-global"]').trigger('click')
    await flushPromises()

    expect(setScope).not.toHaveBeenCalled()
  })

  // The server refuses a ref on global, so the form never offers it one —
  // the mirror image of ResourceSettings.selectScopeKind.
  it('drops the scope ref when switching to global', async () => {
    scope.value = { scopeKind: 'project', scopeRef: '/tmp/demo' }
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    await wrapper.get('[data-testid="memory-scope-global"]').trigger('click')
    await flushPromises()

    expect(setScope).toHaveBeenCalledWith({ scopeKind: 'global', scopeRef: '' })
  })

  it('carries the current ref over when switching between two ref-bearing scopes', async () => {
    scope.value = { scopeKind: 'project', scopeRef: '/tmp/demo' }
    const wrapper = mount(MemorySettings, { attachTo: document.body })

    await wrapper.get('[data-testid="memory-scope-application"]').trigger('click')
    await flushPromises()

    expect(setScope).toHaveBeenCalledWith({ scopeKind: 'application', scopeRef: '/tmp/demo' })
  })
})
