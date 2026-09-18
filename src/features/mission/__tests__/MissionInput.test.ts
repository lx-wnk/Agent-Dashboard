import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'

const captureTask = vi.fn()
const projects = ref<Array<{ id: string, name: string }>>([{ id: 'p1', name: 'Dashboard' }])
const activeView = ref('mission')

vi.mock('@/composables/useCapture', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/composables/useCapture')>()
  return { ...actual, captureTask: (...a: unknown[]) => captureTask(...a) }
})
vi.mock('@/composables/useProjects', () => ({ useProjects: () => ({ projects }) }))
vi.mock('@/composables/useViewState', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/composables/useViewState')>()
  return { ...actual, useViewState: () => ({ activeView }) }
})

const { default: MissionInput } = await import('../components/MissionInput.vue')

async function typeInto(wrapper: ReturnType<typeof mount>, text: string) {
  await wrapper.get('[data-testid="mission-input"]').setValue(text)
  await flushPromises()
}

beforeEach(() => {
  captureTask.mockReset().mockResolvedValue('t1')
  activeView.value = 'mission'
  projects.value = [{ id: 'p1', name: 'Dashboard' }]
})
afterEach(() => {
  document.body.innerHTML = ''
})

describe('missionInput', () => {
  // The promise of the field: you see what Enter does before you press it.
  it('shows no reading until something is typed', () => {
    const wrapper = mount(MissionInput)
    expect(wrapper.find('[data-testid="mission-reading"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('reads a view name as navigation and switches without creating anything', async () => {
    const wrapper = mount(MissionInput)
    await typeInto(wrapper, 'go to pipeline')

    expect(wrapper.get('[data-testid="mission-reading-label"]').text()).toBe('GO TO')
    await wrapper.get('[data-testid="mission-input-submit"]').trigger('click')
    await flushPromises()

    expect(activeView.value).toBe('pipeline')
    expect(captureTask).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('reads anything else as a capture and says so before Enter', async () => {
    const wrapper = mount(MissionInput)
    await typeInto(wrapper, 'build a real diff view')

    expect(wrapper.get('[data-testid="mission-reading-label"]').text()).toBe('CAPTURE')
    expect(wrapper.get('[data-testid="mission-reading"]').text()).toContain('Nothing runs yet')

    // The badge and the sentence must stay apart in the TEXT layer, not only
    // on screen: flex gap separates them visually, so copying the line or
    // reading textContent gave "CAPTUREBecomes a backlog item…".
    expect(wrapper.get('[data-testid="mission-reading"]').text()).toMatch(/CAPTURE:\s\S/)

    await wrapper.get('[data-testid="mission-input-submit"]').trigger('click')
    await flushPromises()

    expect(captureTask).toHaveBeenCalledWith('build a real diff view', projects.value)
    expect(wrapper.emitted('captured')).toEqual([['t1']])
    wrapper.unmount()
  })

  // A reading this screen cannot carry out must say so rather than run the
  // work somewhere the user cannot see it.
  it('refuses a slash command instead of running it out of sight', async () => {
    const wrapper = mount(MissionInput)
    await typeInto(wrapper, '/grant Bash')

    expect(wrapper.get('[data-testid="mission-reading-label"]').text()).toBe('COMMAND')
    await wrapper.get('[data-testid="mission-input-submit"]').trigger('click')
    await flushPromises()

    expect(captureTask).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="mission-input-problem"]').text()).toContain('own prompt')
    wrapper.unmount()
  })

  it('surfaces a capture that cannot happen at all', async () => {
    const { CaptureUnavailable } = await import('@/composables/useCapture')
    captureTask.mockRejectedValue(new CaptureUnavailable('No project exists yet — create one in Settings before capturing.'))

    const wrapper = mount(MissionInput)
    await typeInto(wrapper, 'something')
    await wrapper.get('[data-testid="mission-input-submit"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="mission-input-problem"]').text()).toContain('No project exists yet')
    wrapper.unmount()
  })
})
