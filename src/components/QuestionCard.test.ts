import type { DetectedQuestion } from '../utils/askQuestionScreen'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import QuestionCard from './QuestionCard.vue'

const singleQuestion: DetectedQuestion = {
  header: 'Choose framework',
  question: 'Which frontend framework do you prefer?',
  multiSelect: false,
  options: [
    { index: 1, label: 'Vue', description: 'Progressive framework' },
    { index: 2, label: 'React', description: 'UI library' },
  ],
  typeSomethingIndex: 3,
  chatAboutIndex: 4,
}

const multiQuestion: DetectedQuestion = {
  header: 'Select features',
  question: 'Which features do you want?',
  multiSelect: true,
  options: [
    { index: 1, label: 'TypeScript', description: 'Type safety' },
    { index: 2, label: 'ESLint', description: 'Linting' },
  ],
  typeSomethingIndex: 3,
  chatAboutIndex: 4,
}

describe('questionCard', () => {
  it('renders the question header, prompt, and option labels', () => {
    const wrapper = mount(QuestionCard, {
      props: { detectedQuestion: singleQuestion },
    })
    expect(wrapper.text()).toContain('Choose framework')
    expect(wrapper.text()).toContain('Which frontend framework do you prefer?')
    expect(wrapper.text()).toContain('Vue')
    expect(wrapper.text()).toContain('Progressive framework')
    expect(wrapper.text()).toContain('React')
  })

  it('renders radio inputs for single-select and checkboxes for multi-select', () => {
    const single = mount(QuestionCard, { props: { detectedQuestion: singleQuestion } })
    expect(single.findAll('input[type="radio"]').length).toBe(2)
    expect(single.findAll('input[type="checkbox"]').length).toBe(0)

    const multi = mount(QuestionCard, { props: { detectedQuestion: multiQuestion } })
    expect(multi.findAll('input[type="checkbox"]').length).toBe(2)
    expect(multi.findAll('input[type="radio"]').length).toBe(0)
  })

  it('send button is disabled until an option is selected', async () => {
    const wrapper = mount(QuestionCard, {
      props: { detectedQuestion: singleQuestion },
    })
    const btn = wrapper.find('[data-testid="detected-send-btn"]')
    expect(btn.attributes('disabled')).toBeDefined()

    await wrapper.findAll('input[type="radio"]')[0].trigger('change')
    expect(btn.attributes('disabled')).toBeUndefined()
  })

  it('emits an answer intent with a 0-based index when Send is clicked', async () => {
    const wrapper = mount(QuestionCard, {
      props: { detectedQuestion: singleQuestion },
    })
    await wrapper.findAll('input[type="radio"]')[1].trigger('change')
    await wrapper.find('[data-testid="detected-send-btn"]').trigger('click')

    expect(wrapper.emitted('answer')).toEqual([[{ mode: 'single', index: 1 }]])
  })

  it('typing a custom answer clears any selection and emits a custom intent', async () => {
    const wrapper = mount(QuestionCard, {
      props: { detectedQuestion: singleQuestion },
    })
    await wrapper.findAll('input[type="radio"]')[0].trigger('change')
    await wrapper.find('[data-testid="detected-custom-toggle"]').trigger('click')
    await wrapper.find('[data-testid="detected-custom-textarea"]').setValue('My own answer')
    await wrapper.find('[data-testid="detected-send-btn"]').trigger('click')

    expect(wrapper.emitted('answer')).toEqual([[{ mode: 'custom', optionCount: 2, text: 'My own answer' }]])
  })

  it('sending a chat message emits a chat intent', async () => {
    const wrapper = mount(QuestionCard, {
      props: { detectedQuestion: singleQuestion },
    })
    await wrapper.find('[data-testid="detected-chat-toggle"]').trigger('click')
    const chatBtn = wrapper.find('[data-testid="detected-chat-send-btn"]')
    expect(chatBtn.attributes('disabled')).toBeDefined()

    await wrapper.find('[data-testid="detected-chat-textarea"]').setValue('Actually, let me explain more')
    expect(chatBtn.attributes('disabled')).toBeUndefined()

    await chatBtn.trigger('click')
    expect(wrapper.emitted('answer')).toEqual([[{ mode: 'chat', optionCount: 2, text: 'Actually, let me explain more' }]])
  })

  it('resets local selection state when a new detected question arrives', async () => {
    const wrapper = mount(QuestionCard, {
      props: { detectedQuestion: singleQuestion },
    })
    await wrapper.findAll('input[type="radio"]')[0].trigger('change')
    expect(wrapper.find('[data-testid="detected-send-btn"]').attributes('disabled')).toBeUndefined()

    await wrapper.setProps({ detectedQuestion: multiQuestion })
    expect(wrapper.find('[data-testid="detected-send-btn"]').attributes('disabled')).toBeDefined()
  })
})
