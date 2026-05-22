import { mount } from '@vue/test-utils'
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
})
