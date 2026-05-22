import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'

vi.mock('../composables/useProjects', () => ({
  useProjects: () => ({
    projects: ref([
      { id: 'p1', slug: 'web', name: 'Web', folders: [{ id: 'f1', projectId: 'p1', path: '/repos/web', isDefault: true, createdAt: '' }], createdAt: '', updatedAt: '' },
      { id: 'p2', slug: 'api', name: 'API', folders: [], createdAt: '', updatedAt: '' },
    ]),
  }),
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
})
