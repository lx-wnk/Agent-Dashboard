import type { DetectedConfirm } from '../utils/askQuestionScreen'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ConfirmCard from './ConfirmCard.vue'

const confirmScreen: DetectedConfirm = {
  question: 'Ready to submit your answers?',
  options: [
    { index: 1, label: 'Submit answers' },
    { index: 2, label: 'Cancel' },
  ],
}

describe('confirmCard', () => {
  it('renders the prompt and both option labels', () => {
    const wrapper = mount(ConfirmCard, { props: { detectedConfirm: confirmScreen } })
    expect(wrapper.text()).toContain('Ready to submit your answers?')
    expect(wrapper.text()).toContain('Submit answers')
    expect(wrapper.text()).toContain('Cancel')
  })

  // The TUI parks its cursor on Submit, so the common case must be a single
  // click — no selection step first.
  it('preselects the first option and submits it', async () => {
    const wrapper = mount(ConfirmCard, { props: { detectedConfirm: confirmScreen } })
    const sendBtn = wrapper.find('[data-testid="detected-confirm-send-btn"]')
    expect(sendBtn.attributes('disabled')).toBeUndefined()

    await sendBtn.trigger('click')
    expect(wrapper.emitted('answer')).toEqual([[{ mode: 'single', index: 0 }]])
  })

  it('submits Cancel as the second option', async () => {
    const wrapper = mount(ConfirmCard, { props: { detectedConfirm: confirmScreen } })
    await wrapper.findAll('input[type="radio"]')[1].trigger('change')
    await wrapper.find('[data-testid="detected-confirm-send-btn"]').trigger('click')
    expect(wrapper.emitted('answer')).toEqual([[{ mode: 'single', index: 1 }]])
  })

  // Same reasoning as QuestionCard: the SSE payload is re-deserialized every
  // scan tick, so an identical screen arriving as a fresh object must not
  // reset what the user picked.
  it('keeps the selection when the same screen arrives as a new object', async () => {
    const wrapper = mount(ConfirmCard, { props: { detectedConfirm: confirmScreen } })
    await wrapper.findAll('input[type="radio"]')[1].trigger('change')

    await wrapper.setProps({ detectedConfirm: structuredClone(confirmScreen) })
    await wrapper.find('[data-testid="detected-confirm-send-btn"]').trigger('click')
    expect(wrapper.emitted('answer')).toEqual([[{ mode: 'single', index: 1 }]])
  })

  it('resets the selection when a different screen arrives', async () => {
    const wrapper = mount(ConfirmCard, { props: { detectedConfirm: confirmScreen } })
    await wrapper.findAll('input[type="radio"]')[1].trigger('change')

    await wrapper.setProps({
      detectedConfirm: {
        question: 'Ready to submit your answers?',
        options: [
          { index: 1, label: 'Submit' },
          { index: 2, label: 'Cancel' },
        ],
      },
    })
    await wrapper.find('[data-testid="detected-confirm-send-btn"]').trigger('click')
    expect(wrapper.emitted('answer')).toEqual([[{ mode: 'single', index: 0 }]])
  })
})
