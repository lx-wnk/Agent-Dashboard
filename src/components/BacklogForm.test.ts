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
    expect(wrapper.find('[data-testid="details-cwd"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="details-submit"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="details-submit-refine"]').exists()).toBe(true)
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
    const cwd = wrapper.get('[data-testid="details-cwd"]').element as HTMLInputElement
    expect(cwd.value).toBe('/repos/web')
  })

  it('disables both submit buttons until title, slug, and cwd are filled', async () => {
    const wrapper = mount(BacklogForm)
    expect(wrapper.get('[data-testid="details-submit"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="details-submit-refine"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="details-title"]').setValue('Demo task')
    await wrapper.get('[data-testid="details-cwd"]').setValue('/tmp/x')
    expect(wrapper.get('[data-testid="details-submit"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('[data-testid="details-submit-refine"]').attributes('disabled')).toBeUndefined()
  })

  it('expands QuickCreateProjectPanel when "+ Create new project" is selected', async () => {
    const wrapper = mount(BacklogForm)
    await wrapper.get('[data-testid="backlog-project-select"]').setValue('__create__')
    expect(wrapper.findComponent({ name: 'QuickCreateProjectPanel' }).exists()).toBe(true)
  })

  it('emits created with the new backlog task via Create', async () => {
    createTaskMock.mockClear()
    const wrapper = mount(BacklogForm)
    await wrapper.get('[data-testid="backlog-project-select"]').setValue('p1')
    await flushPromises()
    await wrapper.get('[data-testid="details-title"]').setValue('Demo task')
    await wrapper.get('[data-testid="details-submit"]').trigger('click')
    await flushPromises()
    expect(createTaskMock).toHaveBeenCalledWith(expect.objectContaining({
      projectId: 'p1',
      title: 'Demo task',
      slug: 'demo-task',
      cwd: '/repos/web',
    }))
    expect(createTaskMock).toHaveBeenCalledWith(expect.not.objectContaining({ stage: expect.anything() }))
    expect(wrapper.emitted('created')).toBeTruthy()
    expect(wrapper.emitted('createdAndRefine')).toBeFalsy()
  })

  it('emits createdAndRefine with the new task via Create & Refine', async () => {
    createTaskMock.mockClear()
    const wrapper = mount(BacklogForm)
    await wrapper.get('[data-testid="details-title"]').setValue('Refine me')
    await wrapper.get('[data-testid="details-cwd"]').setValue('/tmp/y')
    await wrapper.get('[data-testid="details-submit-refine"]').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('createdAndRefine')).toBeTruthy()
    expect(wrapper.emitted('created')).toBeFalsy()
  })

  it('omits projectId when no project is selected', async () => {
    createTaskMock.mockClear()
    const wrapper = mount(BacklogForm)
    await wrapper.get('[data-testid="details-title"]').setValue('No project')
    await wrapper.get('[data-testid="details-cwd"]').setValue('/tmp/x')
    await wrapper.get('[data-testid="details-submit"]').trigger('click')
    await flushPromises()
    expect(createTaskMock).toHaveBeenCalledWith(expect.not.objectContaining({ projectId: expect.anything() }))
  })
})
