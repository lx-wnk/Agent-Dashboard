import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'

import ProjectSettings from '@/features/settings/components/ProjectSettings.vue'
import { SLUG_RE } from '@/utils/validation'

vi.mock('@/composables/useProjects', () => ({
  useProjects: () => ({
    projects: ref([
      { id: 'p1', slug: 'web', name: 'Web', folders: [], createdAt: '', updatedAt: '' },
    ]),
    isLoading: ref(false),
    error: ref(''),
    refetch: vi.fn(),
  }),
  createProject: vi.fn(),
  updateProject: vi.fn(),
  deleteProject: vi.fn(),
}))

vi.mock('@/composables/useSpawners', () => ({
  useSpawners: () => ({ spawners: ref([]) }),
}))

vi.mock('@/composables/useProjectFolders', () => ({
  fetchProjectFolders: vi.fn().mockResolvedValue([]),
  createFolder: vi.fn(),
  updateFolder: vi.fn(),
  deleteFolder: vi.fn(),
}))

vi.mock('@/features/pipeline', () => ({
  useProjectPipelineConfig: () => ({
    config: ref(null),
    loading: ref(false),
    error: ref(''),
    fetch: vi.fn(),
    save: vi.fn(),
  }),
}))

function slugInput(wrapper: ReturnType<typeof mount>): HTMLInputElement {
  return wrapper.get('[data-testid="proj-slug"]').element as HTMLInputElement
}

describe('projectSettings slug', () => {
  it('derives a valid slug from the name, including one the user could not type by hand', async () => {
    const wrapper = mount(ProjectSettings)
    await wrapper.get('[data-testid="proj-new"]').trigger('click')

    await wrapper.get('[data-testid="proj-name"]').setValue('DIW-ReviewApps')

    expect(slugInput(wrapper).value).toBe('diw-reviewapps')
    expect(slugInput(wrapper).value).toMatch(SLUG_RE)
  })

  it('keeps following the name while the slug has not been touched', async () => {
    const wrapper = mount(ProjectSettings)
    await wrapper.get('[data-testid="proj-new"]').trigger('click')

    await wrapper.get('[data-testid="proj-name"]').setValue('First Name')
    await wrapper.get('[data-testid="proj-name"]').setValue('Second Name')

    expect(slugInput(wrapper).value).toBe('second-name')
  })

  it('leaves a hand-edited slug alone', async () => {
    const wrapper = mount(ProjectSettings)
    await wrapper.get('[data-testid="proj-new"]').trigger('click')

    await wrapper.get('[data-testid="proj-name"]').setValue('First Name')
    await wrapper.get('[data-testid="proj-slug"]').setValue('my-own-slug')
    await wrapper.get('[data-testid="proj-name"]').setValue('Second Name')

    expect(slugInput(wrapper).value).toBe('my-own-slug')
  })

  it('never re-keys an existing project when its name is edited', async () => {
    const wrapper = mount(ProjectSettings)
    await wrapper.get('[data-testid="proj-edit"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="proj-name"]').setValue('Web Renamed')

    expect(slugInput(wrapper).value).toBe('web')
  })
})
