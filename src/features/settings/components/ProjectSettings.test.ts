import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'

import ProjectSettings from '@/features/settings/components/ProjectSettings.vue'
import { SLUG_RE } from '@/utils/validation'

vi.mock('@/composables/useProjects', () => ({
  useProjects: () => ({
    projects: ref([
      { id: 'p1', slug: 'web', name: 'Web', folders: [], createdAt: '', updatedAt: '' },
      // Slug deliberately diverges from the name: a re-derivation here is visible.
      { id: 'p2', slug: 'legacy-key', name: 'Renamed Later', folders: [], createdAt: '', updatedAt: '' },
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

function nameInput(wrapper: ReturnType<typeof mount>): HTMLInputElement {
  return wrapper.get('[data-testid="proj-name"]').element as HTMLInputElement
}

// The project table is hidden while the form is open, so the Edit button of an
// existing project is only reachable once the form is closed again.
async function closeForm(wrapper: ReturnType<typeof mount>): Promise<void> {
  const cancel = wrapper.findAll('button').find(b => b.text().trim() === 'Cancel')
  if (!cancel)
    throw new Error('form Cancel button not found')
  await cancel.trigger('click')
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
    await wrapper.get('[data-testid="proj-edit-web"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="proj-name"]').setValue('Web Renamed')

    expect(slugInput(wrapper).value).toBe('web')
  })

  it('does not re-key a project opened for edit straight out of a draft', async () => {
    // openEdit() swaps in the project's name and clears isCreating in the same
    // tick; the name watcher only runs afterwards, and must see edit mode.
    const wrapper = mount(ProjectSettings)
    await wrapper.get('[data-testid="proj-new"]').trigger('click')
    await wrapper.get('[data-testid="proj-name"]').setValue('Draft Project')
    await closeForm(wrapper)

    await wrapper.get('[data-testid="proj-edit-legacy-key"]').trigger('click')
    await flushPromises()

    expect(nameInput(wrapper).value).toBe('Renamed Later')
    expect(slugInput(wrapper).value).toBe('legacy-key')
  })

  it('empties both fields when a new project is started from an open project', async () => {
    const wrapper = mount(ProjectSettings)
    await wrapper.get('[data-testid="proj-edit-web"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="proj-new"]').trigger('click')
    await flushPromises()

    expect(nameInput(wrapper).value).toBe('')
    expect(slugInput(wrapper).value).toBe('')
  })

  it('derives again in a new draft after the previous draft had a hand-edited slug', async () => {
    const wrapper = mount(ProjectSettings)
    await wrapper.get('[data-testid="proj-new"]').trigger('click')
    await wrapper.get('[data-testid="proj-name"]').setValue('First Name')
    await wrapper.get('[data-testid="proj-slug"]').setValue('my-own-slug')

    await wrapper.get('[data-testid="proj-new"]').trigger('click')
    await wrapper.get('[data-testid="proj-name"]').setValue('Second Name')

    expect(slugInput(wrapper).value).toBe('second-name')
  })

  it('hands the slug back to the name once the slug field is cleared', async () => {
    const wrapper = mount(ProjectSettings)
    await wrapper.get('[data-testid="proj-new"]').trigger('click')
    await wrapper.get('[data-testid="proj-name"]').setValue('First Name')
    await wrapper.get('[data-testid="proj-slug"]').setValue('my-own-slug')
    await wrapper.get('[data-testid="proj-slug"]').setValue('')

    await wrapper.get('[data-testid="proj-name"]').setValue('Second Name')

    expect(slugInput(wrapper).value).toBe('second-name')
  })
})
