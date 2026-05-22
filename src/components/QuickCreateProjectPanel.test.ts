import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import QuickCreateProjectPanel from './QuickCreateProjectPanel.vue'

const sampleProject = {
  id: 'prj_new',
  slug: 'new-thing',
  name: 'New Thing',
  defaultSpawnerId: 'spwn_a',
  createdAt: '',
  updatedAt: '',
}
const sampleFolder = {
  id: 'fld_new',
  projectId: 'prj_new',
  path: '/home/u/new-thing',
  isDefault: true,
  createdAt: '',
}

describe('quickCreateProjectPanel', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('submits project then folder and emits created', async () => {
    fetchMock
      .mockResolvedValueOnce({ ok: true, status: 201, json: async () => sampleProject })
      .mockResolvedValueOnce({ ok: true, status: 201, json: async () => sampleFolder })

    const wrapper = mount(QuickCreateProjectPanel, {
      props: { spawners: [] },
    })
    await wrapper.find('input[name="name"]').setValue('New Thing')
    await wrapper.find('input[name="path"]').setValue('/home/u/new-thing')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/projects', expect.objectContaining({ method: 'POST' }))
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      '/api/projects/prj_new/folders',
      expect.objectContaining({ method: 'POST' }),
    )
    const emitted = wrapper.emitted('created')
    expect(emitted).toBeTruthy()
    expect(emitted![0][0]).toMatchObject({ id: 'prj_new' })
  })

  it('rolls back project create when folder create fails', async () => {
    fetchMock
      .mockResolvedValueOnce({ ok: true, status: 201, json: async () => sampleProject })
      .mockResolvedValueOnce({ ok: false, status: 500, json: async () => ({ error: 'disk full' }) })
      .mockResolvedValueOnce({ ok: true, status: 204, json: async () => ({}) })

    const wrapper = mount(QuickCreateProjectPanel, {
      props: { spawners: [] },
    })
    await wrapper.find('input[name="name"]').setValue('New Thing')
    await wrapper.find('input[name="path"]').setValue('/home/u/new-thing')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledTimes(3)
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      '/api/projects/prj_new',
      expect.objectContaining({ method: 'DELETE' }),
    )
    expect(wrapper.emitted('created')).toBeFalsy()
    expect(wrapper.text()).toContain('disk full')
  })

  it('surfaces slug conflict without firing folder request', async () => {
    fetchMock.mockResolvedValueOnce({
      ok: false,
      status: 409,
      json: async () => ({ error: 'slug already exists' }),
    })

    const wrapper = mount(QuickCreateProjectPanel, {
      props: { spawners: [] },
    })
    await wrapper.find('input[name="name"]').setValue('New Thing')
    await wrapper.find('input[name="path"]').setValue('/home/u/new-thing')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('slug already exists')
  })
})
