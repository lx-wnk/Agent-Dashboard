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

describe('BacklogForm wizard', () => {
  it('renders one radio per project in Step 1', () => {
    const wrapper = mount(BacklogForm)
    const radios = wrapper.findAll('[data-testid^="project-radio-"]')
    expect(radios).toHaveLength(2)
    expect(radios[0].text()).toContain('Web')
    expect(radios[1].text()).toContain('API')
  })

  it('disables Next until a project is selected or skipped', async () => {
    const wrapper = mount(BacklogForm)
    const next = wrapper.get('[data-testid="project-step-next"]')
    expect(next.attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="project-radio-p1"]').trigger('click')
    expect(next.attributes('disabled')).toBeUndefined()
  })

  it('advances to Step 2 when Next is clicked', async () => {
    const wrapper = mount(BacklogForm)
    await wrapper.get('[data-testid="project-radio-p1"]').trigger('click')
    await wrapper.get('[data-testid="project-step-next"]').trigger('click')
    expect(wrapper.find('[data-testid="details-step"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="project-step"]').exists()).toBe(false)
  })

  it('expands QuickCreateProjectPanel when "Create new" is clicked', async () => {
    const wrapper = mount(BacklogForm)
    expect(wrapper.find('[data-testid="quick-create-panel"]').exists()).toBe(false)
    await wrapper.get('[data-testid="project-step-create-new"]').trigger('click')
    expect(wrapper.find('[data-testid="quick-create-panel"]').exists()).toBe(true)
  })

  it('selects the newly created project after QuickCreateProjectPanel emits created', async () => {
    const wrapper = mount(BacklogForm)
    await wrapper.get('[data-testid="project-step-create-new"]').trigger('click')
    const panel = wrapper.findComponent({ name: 'QuickCreateProjectPanel' })
    panel.vm.$emit('created', {
      id: 'p3',
      slug: 'new',
      name: 'Brand New',
      folders: [{ id: 'f3', projectId: 'p3', path: '/x', isDefault: true, createdAt: '' }],
      createdAt: '',
      updatedAt: '',
    })
    await wrapper.vm.$nextTick()
    const next = wrapper.get('[data-testid="project-step-next"]')
    expect(next.attributes('disabled')).toBeUndefined()
  })

  it('skip button enables Next and clears project selection', async () => {
    const wrapper = mount(BacklogForm)
    await wrapper.get('[data-testid="project-radio-p1"]').trigger('click')
    await wrapper.get('[data-testid="project-step-skip"]').trigger('click')
    expect(wrapper.get('[data-testid="project-step-next"]').attributes('disabled')).toBeUndefined()
    // After skipping, the previously selected radio should no longer be styled as selected.
    expect(wrapper.get('[data-testid="project-radio-p1"]').classes()).not.toContain('border-blue-500')
  })

  it('prefills cwd from the project default folder when the user advances to Step 2', async () => {
    const wrapper = mount(BacklogForm)
    await wrapper.get('[data-testid="project-radio-p1"]').trigger('click')
    await wrapper.get('[data-testid="project-step-next"]').trigger('click')
    await flushPromises()
    const cwd = wrapper.get('[data-testid="details-cwd"]').element as HTMLInputElement
    expect(cwd.value).toBe('/repos/web')
  })

  it('submits createTask with projectId when project is selected', async () => {
    createTaskMock.mockClear()
    const wrapper = mount(BacklogForm)
    await wrapper.get('[data-testid="project-radio-p1"]').trigger('click')
    await wrapper.get('[data-testid="project-step-next"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="details-title"]').setValue('Demo task')
    await wrapper.get('[data-testid="details-slug"]').setValue('demo')
    await wrapper.get('[data-testid="details-submit"]').trigger('click')
    await flushPromises()
    expect(createTaskMock).toHaveBeenCalledWith(expect.objectContaining({
      projectId: 'p1',
      title: 'Demo task',
      slug: 'demo',
      cwd: '/repos/web',
    }))
  })

  it('submits createTask without projectId when Step 1 is skipped', async () => {
    createTaskMock.mockClear()
    const wrapper = mount(BacklogForm)
    await wrapper.get('[data-testid="project-step-skip"]').trigger('click')
    await wrapper.get('[data-testid="project-step-next"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="details-title"]').setValue('No project')
    await wrapper.get('[data-testid="details-slug"]').setValue('no-project')
    await wrapper.get('[data-testid="details-cwd"]').setValue('/tmp/x')
    await wrapper.get('[data-testid="details-submit"]').trigger('click')
    await flushPromises()
    expect(createTaskMock).toHaveBeenCalledWith(expect.not.objectContaining({ projectId: expect.anything() }))
  })

  it('preserves Step 2 field values after Back → Next round-trip', async () => {
    const wrapper = mount(BacklogForm)
    await wrapper.get('[data-testid="project-radio-p1"]').trigger('click')
    await wrapper.get('[data-testid="project-step-next"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="details-title"]').setValue('Persisted title')
    await wrapper.get('[data-testid="details-back"]').trigger('click')
    await wrapper.get('[data-testid="project-step-next"]').trigger('click')
    await flushPromises()
    const titleInput = wrapper.get('[data-testid="details-title"]').element as HTMLInputElement
    expect(titleInput.value).toBe('Persisted title')
  })
})
