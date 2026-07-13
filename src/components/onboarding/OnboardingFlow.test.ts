import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { axe } from '../../utils/testA11y'
import OnboardingFlow from './OnboardingFlow.vue'

vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({
  ok: true,
  json: () => Promise.resolve({}),
} as Response)))

describe('onboardingFlow — AppModal migration', () => {
  afterEach(() => {
    document.body.innerHTML = ''
    document.body.style.overflow = ''
    document.body.style.paddingRight = ''
  })

  it('renders through AppModal with a single dialog root and no axe violations', async () => {
    const wrapper = mount(OnboardingFlow, {
      props: { open: true },
      attachTo: document.body,
    })
    await flushPromises()

    const dialogs = document.querySelectorAll('[role="dialog"]')
    expect(dialogs).toHaveLength(1)
    expect(dialogs[0].getAttribute('aria-labelledby')).toBe('onboarding-title')

    const results = await axe(dialogs[0] as HTMLElement)
    expect(results).toHaveNoViolations()

    wrapper.unmount()
  })

  it('moves focus into the modal panel on open (AppModal focus lifecycle)', async () => {
    const wrapper = mount(OnboardingFlow, {
      props: { open: true },
      attachTo: document.body,
    })
    await flushPromises()

    const panel = document.querySelector('.base-modal-box')
    expect(panel).not.toBeNull()
    expect(document.activeElement).toBe(panel)

    wrapper.unmount()
  })

  it('closing via AppModal (Escape) invokes skip and emits close', async () => {
    const wrapper = mount(OnboardingFlow, {
      props: { open: true },
      attachTo: document.body,
    })
    await flushPromises()

    const dialog = document.querySelector('[role="dialog"]') as HTMLElement
    dialog.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()

    expect(wrapper.emitted('close')).toHaveLength(1)

    wrapper.unmount()
  })
})
