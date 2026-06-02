import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'

vi.mock('../composables/useProjects', () => ({
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

vi.mock('../composables/useSpawners', () => ({
  useSpawners: () => ({ spawners: ref([{ id: 's1', name: 'Claude default', slug: 'claude-default', command: 'claude', args: [], env: {}, adapterType: 'claude', adapterConfig: {}, builtIn: true, createdAt: '', updatedAt: '' }]) }),
}))

const createTaskMock = vi.fn().mockResolvedValue({ id: 't1', slug: 'demo', title: 'Demo' } as unknown)
vi.mock('../composables/useTasks', () => ({
  createTask: (input: unknown) => createTaskMock(input),
}))

vi.mock('../composables/useProjectFolders', () => ({
  suggestFolders: vi.fn().mockResolvedValue([
    { id: 'f1', projectId: 'p1', path: '/repos/web', isDefault: true, createdAt: '' },
  ]),
  createFolder: vi.fn(),
}))

import BacklogForm from './BacklogForm.vue'

describe('BacklogForm single-screen', () => {
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
    const wrapper = mount(BacklogForm)
    const select = wrapper.get('[data-testid="backlog-project-select"]').element as HTMLSelectElement
    const optionValues = Array.from(select.options).map(o => o.value)
    expect(optionValues).not.toContain('')
  })

  it('auto-derives slug from the title', async () => {
    const wrapper = mount(BacklogForm)
    await wrapper.get('[data-testid="details-title"]').setValue('My New Task')
    const slug = wrapper.get('[data-testid="details-slug"]').element as HTMLInputElement
    expect(slug.value).toBe('my-new-task')
  })

  it('auto-fills cwd from the default folder when a project is selected', async () => {
    const wrapper = mount(BacklogForm)
    await wrapper.get('[data-testid="backlog-project-select"]').setValue('p1')
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
    const wrapper = mount(BacklogForm)
    // Initially disabled — no title, no project
    expect(wrapper.get('[data-testid="details-submit-refine"]').attributes('disabled')).toBeDefined()

    // Title alone is not enough (no project, no cwd)
    await wrapper.get('[data-testid="details-title"]').setValue('Demo task')
    expect(wrapper.get('[data-testid="details-submit-refine"]').attributes('disabled')).toBeDefined()

    // Selecting a project triggers folder fetch → fills cwd → enables submit
    await wrapper.get('[data-testid="backlog-project-select"]').setValue('p1')
    await flushPromises()
    expect(wrapper.get('[data-testid="details-submit-refine"]').attributes('disabled')).toBeUndefined()
  })

  it('expands QuickCreateProjectPanel when "+ Create new project" is selected', async () => {
    const wrapper = mount(BacklogForm)
    await wrapper.get('[data-testid="backlog-project-select"]').setValue('__create__')
    expect(wrapper.findComponent({ name: 'QuickCreateProjectPanel' }).exists()).toBe(true)
  })

  it('emits createdAndRefine with the new task via Create & Refine', async () => {
    createTaskMock.mockClear()
    const wrapper = mount(BacklogForm)
    await wrapper.get('[data-testid="backlog-project-select"]').setValue('p1')
    await flushPromises()
    await wrapper.get('[data-testid="details-title"]').setValue('Demo task')
    await wrapper.get('[data-testid="details-submit-refine"]').trigger('click')
    await flushPromises()
    expect(createTaskMock).toHaveBeenCalledWith(expect.objectContaining({
      projectId: 'p1',
      title: 'Demo task',
      slug: 'demo-task',
      cwd: '/repos/web',
    }))
    expect(createTaskMock).toHaveBeenCalledWith(expect.not.objectContaining({ stage: expect.anything() }))
    expect(wrapper.emitted('createdAndRefine')).toBeTruthy()
    expect(wrapper.emitted('created')).toBeFalsy()
  })

  it('form submit also triggers createdAndRefine (form @submit.prevent calls onCreateAndRefine)', async () => {
    createTaskMock.mockClear()
    const wrapper = mount(BacklogForm)
    await wrapper.get('[data-testid="backlog-project-select"]').setValue('p1')
    await flushPromises()
    await wrapper.get('[data-testid="details-title"]').setValue('Form submit task')
    await wrapper.get('[data-testid="backlog-form"]').trigger('submit')
    await flushPromises()
    expect(wrapper.emitted('createdAndRefine')).toBeTruthy()
    expect(wrapper.emitted('created')).toBeFalsy()
  })
})
