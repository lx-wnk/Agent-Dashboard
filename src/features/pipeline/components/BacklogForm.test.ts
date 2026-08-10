import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'

import BacklogForm from '@/features/pipeline/components/BacklogForm.vue'
import { openListbox, optionByLabel, selectOptionsById } from '@/utils/testSelect'

vi.mock('@/composables/useProjects', () => ({
  useProjects: () => ({
    projects: ref([
      { id: 'p1', slug: 'web', name: 'Web', folders: [{ id: 'f1', projectId: 'p1', path: '/repos/web', isDefault: true, createdAt: '' }], createdAt: '', updatedAt: '' },
      { id: 'p2', slug: 'api', name: 'API', folders: [], createdAt: '', updatedAt: '' },
    ]),
    refetch: vi.fn(),
  }),
  createProject: vi.fn(),
  deleteProject: vi.fn(),
}))

vi.mock('@/composables/useSpawners', () => ({
  useSpawners: () => ({ spawners: ref([{ id: 's1', name: 'Claude default', slug: 'claude-default', command: 'claude', args: [], env: {}, adapterType: 'claude', adapterConfig: {}, builtIn: true, createdAt: '', updatedAt: '' }]) }),
}))

const createTaskMock = vi.fn().mockResolvedValue({ id: 't1', slug: 'demo', title: 'Demo' } as unknown)
vi.mock('@/features/pipeline/composables/useTasks', () => ({
  createTask: (input: unknown) => createTaskMock(input),
}))

vi.mock('@/composables/useProjectFolders', () => ({
  suggestFolders: vi.fn().mockResolvedValue([
    { id: 'f1', projectId: 'p1', path: '/repos/web', isDefault: true, createdAt: '' },
  ]),
  createFolder: vi.fn(),
}))

const fetchIssueMock = vi.fn().mockResolvedValue({
  tracker: 'github',
  key: 'owner/repo#7',
  title: 'Imported Issue Title',
  body: 'issue body',
  url: 'https://example.test/7',
  labels: [],
})
vi.mock('@/composables/useTrackerImport', () => ({
  useTrackerImport: () => ({ fetchIssue: (ref: string) => fetchIssueMock(ref) }),
}))

// AppSelect (project/autonomy dropdowns) is a custom listbox, not a native
// <select> — its panel teleports to <body> while open, so option counts and
// value changes are exercised through the trigger button + the teleported
// [role="option"] elements instead of select.setValue()/select.options,
// which only work against native <select> internals. See testSelect.ts for
// the shared openListbox()/optionByLabel() helpers.

afterEach(() => {
  document.body.innerHTML = ''
})

describe('backlogForm single-screen', () => {
  it('renders the form fields and a project dropdown on one screen', () => {
    const wrapper = mount(BacklogForm)
    expect(wrapper.find('[data-testid="backlog-project-select"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="details-title"]').exists()).toBe(true)
    // Working Directory field has been removed — cwd is auto-filled from project
    expect(wrapper.find('[data-testid="details-cwd"]').exists()).toBe(false)
    // Only one submit button: Create & Refine (primary action)
    expect(wrapper.find('[data-testid="details-submit"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="details-submit-refine"]').exists()).toBe(true)
  })

  it('has no "No project" option — project is mandatory', () => {
    // AppSelect is a multi-root (button + teleported panel) component, so a
    // full mount resolves findComponent(...).element to the parent DOM node
    // instead of the button — shallow-stub the tree instead to read props directly.
    const wrapper = mount(BacklogForm, { shallow: true })
    const options = selectOptionsById(wrapper, 'backlog-project')
    const optionValues = options.map(o => o.value)
    expect(optionValues).not.toContain('')
  })

  it('auto-derives slug from the title', async () => {
    const wrapper = mount(BacklogForm)
    await wrapper.get('[data-testid="details-title"]').setValue('My New Task')
    const slug = wrapper.get('[data-testid="details-slug"]').element as HTMLInputElement
    expect(slug.value).toBe('my-new-task')
  })

  it('stops deriving the slug once the user has edited it', async () => {
    const wrapper = mount(BacklogForm)
    await wrapper.get('[data-testid="details-title"]').setValue('My New Task')
    await wrapper.get('[data-testid="details-slug"]').setValue('my-own-slug')

    await wrapper.get('[data-testid="details-title"]').setValue('A Different Task')

    const slug = wrapper.get('[data-testid="details-slug"]').element as HTMLInputElement
    expect(slug.value).toBe('my-own-slug')
  })

  it('derives the slug again once the slug field is cleared', async () => {
    const wrapper = mount(BacklogForm)
    await wrapper.get('[data-testid="details-title"]').setValue('My New Task')
    await wrapper.get('[data-testid="details-slug"]').setValue('my-own-slug')
    await wrapper.get('[data-testid="details-slug"]').setValue('')

    await wrapper.get('[data-testid="details-title"]').setValue('A Different Task')

    const slug = wrapper.get('[data-testid="details-slug"]').element as HTMLInputElement
    expect(slug.value).toBe('a-different-task')
  })

  // Importing rewrites the title, so the slug it derives is not user-authored —
  // leaving the flag set would freeze the slug on the imported title forever.
  it('keeps deriving from the title after an issue import overwrites a typed slug', async () => {
    const wrapper = mount(BacklogForm)
    await wrapper.get('[data-testid="details-slug"]').setValue('my-own-slug')

    await wrapper.get('[data-testid="import-ref-input"]').setValue('owner/repo#7')
    await wrapper.get('[data-testid="import-ref-fetch"]').trigger('click')
    await flushPromises()

    const slug = wrapper.get('[data-testid="details-slug"]').element as HTMLInputElement
    expect(slug.value).toBe('imported-issue-title')

    await wrapper.get('[data-testid="details-title"]').setValue('Changed After Import')
    expect(slug.value).toBe('changed-after-import')
  })

  it('refills the slug the moment the field is cleared, without touching the title', async () => {
    const wrapper = mount(BacklogForm)
    await wrapper.get('[data-testid="details-title"]').setValue('My New Task')
    await wrapper.get('[data-testid="details-slug"]').setValue('my-own-slug')

    await wrapper.get('[data-testid="details-slug"]').setValue('')

    const slug = wrapper.get('[data-testid="details-slug"]').element as HTMLInputElement
    expect(slug.value).toBe('my-new-task')
  })

  it('auto-fills cwd from the default folder when a project is selected', async () => {
    const wrapper = mount(BacklogForm, { attachTo: document.body })
    const panel = await openListbox(wrapper.get('[data-testid="backlog-project-select"]'))
    optionByLabel(panel, 'Web').dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await flushPromises()
    // cwd is internal state — verify it flows through to createTask on submit
    // (no visible field, but canSubmit becomes true once project+title+slug are set)
    expect(wrapper.get('[data-testid="details-submit-refine"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="details-title"]').setValue('Demo task')
    await flushPromises()
    // After project selection fills cwd and title is set, button should enable
    expect(wrapper.get('[data-testid="details-submit-refine"]').attributes('disabled')).toBeUndefined()
  })

  it('disables submit until title, slug, and project are all provided', async () => {
    const wrapper = mount(BacklogForm, { attachTo: document.body })
    // Initially disabled — no title, no project
    expect(wrapper.get('[data-testid="details-submit-refine"]').attributes('disabled')).toBeDefined()

    // Title alone is not enough (no project, no cwd)
    await wrapper.get('[data-testid="details-title"]').setValue('Demo task')
    expect(wrapper.get('[data-testid="details-submit-refine"]').attributes('disabled')).toBeDefined()

    // Selecting a project triggers folder fetch → fills cwd → enables submit
    const panel = await openListbox(wrapper.get('[data-testid="backlog-project-select"]'))
    optionByLabel(panel, 'Web').dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await flushPromises()
    expect(wrapper.get('[data-testid="details-submit-refine"]').attributes('disabled')).toBeUndefined()
  })

  it('expands QuickCreateProjectPanel when "+ Create new project" is selected', async () => {
    const wrapper = mount(BacklogForm, { attachTo: document.body })
    const panel = await openListbox(wrapper.get('[data-testid="backlog-project-select"]'))
    optionByLabel(panel, '+ Create new project…').dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await wrapper.vm.$nextTick()
    expect(wrapper.findComponent({ name: 'QuickCreateProjectPanel' }).exists()).toBe(true)
  })

  it('renders the autonomy selector with spec_gated as default', () => {
    const wrapper = mount(BacklogForm)
    expect(wrapper.get('[data-testid="details-autonomy"]').text()).toContain('Spec-gated')
  })

  it('includes the selected autonomy in the create payload', async () => {
    createTaskMock.mockClear()
    const wrapper = mount(BacklogForm, { attachTo: document.body })
    let panel = await openListbox(wrapper.get('[data-testid="backlog-project-select"]'))
    optionByLabel(panel, 'Web').dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await flushPromises()
    await wrapper.get('[data-testid="details-title"]').setValue('Demo task')
    panel = await openListbox(wrapper.get('[data-testid="details-autonomy"]'))
    optionByLabel(panel, 'Full — fully autonomous').dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await flushPromises()
    await wrapper.get('[data-testid="details-submit-refine"]').trigger('click')
    await flushPromises()
    expect(createTaskMock).toHaveBeenCalledWith(expect.objectContaining({ autonomy: 'full' }))
  })

  it('emits createdAndRefine with the new task via Create & Refine', async () => {
    createTaskMock.mockClear()
    const wrapper = mount(BacklogForm, { attachTo: document.body })
    const panel = await openListbox(wrapper.get('[data-testid="backlog-project-select"]'))
    optionByLabel(panel, 'Web').dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await flushPromises()
    await wrapper.get('[data-testid="details-title"]').setValue('Demo task')
    await wrapper.get('[data-testid="details-submit-refine"]').trigger('click')
    await flushPromises()
    expect(createTaskMock).toHaveBeenCalledWith(expect.objectContaining({
      projectId: 'p1',
      title: 'Demo task',
      slug: 'demo-task',
      cwd: '/repos/web',
      autonomy: 'spec_gated',
    }))
    expect(createTaskMock).toHaveBeenCalledWith(expect.not.objectContaining({ stage: expect.anything() }))
    expect(wrapper.emitted('createdAndRefine')).toBeTruthy()
    expect(wrapper.emitted('created')).toBeFalsy()
  })

  it('form submit also triggers createdAndRefine (form @submit.prevent calls onCreateAndRefine)', async () => {
    createTaskMock.mockClear()
    const wrapper = mount(BacklogForm, { attachTo: document.body })
    const panel = await openListbox(wrapper.get('[data-testid="backlog-project-select"]'))
    optionByLabel(panel, 'Web').dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await flushPromises()
    await wrapper.get('[data-testid="details-title"]').setValue('Form submit task')
    await wrapper.get('[data-testid="backlog-form"]').trigger('submit')
    await flushPromises()
    expect(wrapper.emitted('createdAndRefine')).toBeTruthy()
    expect(wrapper.emitted('created')).toBeFalsy()
  })
})
