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
  // The detector's `header` is scrollback above the modal, not a title, so the
  // card deliberately does not render it — the question is the heading.
  it('renders the question prompt and option labels, but not the detected header', () => {
    const wrapper = mount(QuestionCard, {
      props: { detectedQuestion: singleQuestion },
    })
    expect(wrapper.text()).not.toContain('Choose framework')
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

  // The card is fed from the SSE agent payload, which is re-deserialized on
  // every scan tick — a fresh object with identical content must NOT count as a
  // new question, or the user's selection is wiped every few seconds mid-answer.
  it('keeps local selection state when the same question arrives as a new object', async () => {
    const wrapper = mount(QuestionCard, {
      props: { detectedQuestion: singleQuestion },
    })
    await wrapper.findAll('input[type="radio"]')[0].trigger('change')

    await wrapper.setProps({ detectedQuestion: structuredClone(singleQuestion) })
    expect(wrapper.find('[data-testid="detected-send-btn"]').attributes('disabled')).toBeUndefined()

    await wrapper.find('[data-testid="detected-send-btn"]').trigger('click')
    expect(wrapper.emitted('answer')).toEqual([[{ mode: 'single', index: 0 }]])
  })

  it('keeps typed custom text when the same question arrives as a new object', async () => {
    const wrapper = mount(QuestionCard, {
      props: { detectedQuestion: singleQuestion },
    })
    await wrapper.find('[data-testid="detected-custom-toggle"]').trigger('click')
    await wrapper.find('[data-testid="detected-custom-textarea"]').setValue('Svelte, actually')

    await wrapper.setProps({ detectedQuestion: structuredClone(singleQuestion) })
    expect((wrapper.find('[data-testid="detected-custom-textarea"]').element as HTMLTextAreaElement).value)
      .toBe('Svelte, actually')
  })

  // A radio `name` is a document-wide group. Two cards are routinely mounted at
  // once — two agents with a question in the triage band, or a band card plus
  // the terminal overlay — and a shared name makes the browser uncheck the
  // other card's radio. Vue does not repair that (its bound `:checked` value is
  // unchanged, so it patches nothing), leaving the selection visually gone
  // while the component still holds it.
  it('does not share a radio group with a second card in the same document', async () => {
    const hostA = document.createElement('div')
    const hostB = document.createElement('div')
    document.body.append(hostA, hostB)

    const cardA = mount(QuestionCard, { props: { detectedQuestion: singleQuestion }, attachTo: hostA })
    const cardB = mount(QuestionCard, {
      props: { detectedQuestion: { ...singleQuestion, header: 'Second card', question: 'Another question?' } },
      attachTo: hostB,
    })

    const radioA = cardA.findAll('input[type="radio"]')[0].element as HTMLInputElement
    const radioB = cardB.findAll('input[type="radio"]')[0].element as HTMLInputElement
    expect(radioA.name).not.toBe(radioB.name)

    radioA.click()
    await cardA.vm.$nextTick()
    expect(radioA.checked).toBe(true)

    radioB.click()
    await cardB.vm.$nextTick()
    expect(radioA.checked).toBe(true)

    cardA.unmount()
    cardB.unmount()
    hostA.remove()
    hostB.remove()
  })

  it('keeps Send disabled while nothing is selected or typed', async () => {
    const wrapper = mount(QuestionCard, { props: { detectedQuestion: singleQuestion } })
    const sendBtn = () => wrapper.find('[data-testid="detected-send-btn"]')
    expect(sendBtn().attributes('disabled')).toBeDefined()

    await wrapper.find('[data-testid="detected-custom-toggle"]').trigger('click')
    await wrapper.find('[data-testid="detected-custom-textarea"]').setValue('   ')
    expect(sendBtn().attributes('disabled')).toBeDefined()

    await wrapper.find('[data-testid="detected-custom-textarea"]').setValue('Svelte')
    expect(sendBtn().attributes('disabled')).toBeUndefined()
  })
})
