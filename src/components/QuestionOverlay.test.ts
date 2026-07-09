import type { DetectedQuestion } from '../utils/askQuestionScreen'
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import QuestionOverlay from './QuestionOverlay.vue'

const singleQuestion: DetectedQuestion = {
  header: 'Pick a colour',
  question: 'What is your favourite colour?',
  multiSelect: false,
  options: [
    { index: 1, label: 'Red', description: 'A warm colour' },
    { index: 2, label: 'Green' },
    { index: 3, label: 'Blue' },
  ],
  typeSomethingIndex: 4,
  chatAboutIndex: 5,
}

const multiQuestion: DetectedQuestion = {
  header: 'Pick fruits',
  question: 'Which fruits?',
  multiSelect: true,
  options: [
    { index: 1, label: 'Apples' },
    { index: 2, label: 'Bananas' },
    { index: 3, label: 'Cherries' },
  ],
  typeSomethingIndex: 4,
  chatAboutIndex: 5,
}

function encodedCalls(sendMock: ReturnType<typeof vi.fn>): string[] {
  const decoder = new TextDecoder()
  return sendMock.mock.calls.map(([bytes]) => decoder.decode(bytes as Uint8Array))
}

describe('questionOverlay', () => {
  it('renders the QuestionCard with the option labels when a question is bound', () => {
    const wrapper = mount(QuestionOverlay, {
      props: { question: singleQuestion, send: vi.fn() },
    })

    expect(wrapper.find('[data-testid="question-overlay"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('Pick a colour')
    expect(wrapper.text()).toContain('Red')
    expect(wrapper.text()).toContain('Green')
    expect(wrapper.text()).toContain('Blue')
  })

  it('does not render when question is null', () => {
    const wrapper = mount(QuestionOverlay, {
      props: { question: null, send: vi.fn() },
    })

    expect(wrapper.find('[data-testid="question-overlay"]').exists()).toBe(false)
  })

  it('marks the overlay container as an accessible modal dialog', () => {
    const wrapper = mount(QuestionOverlay, {
      props: { question: singleQuestion, send: vi.fn() },
    })

    const dialog = wrapper.find('[data-testid="question-overlay"]')
    expect(dialog.attributes('role')).toBe('dialog')
    expect(dialog.attributes('aria-modal')).toBe('true')
    expect(dialog.attributes('aria-label')).toBe(singleQuestion.header)
  })

  it('moves focus into the overlay (first control) once a question appears', async () => {
    const wrapper = mount(QuestionOverlay, {
      attachTo: document.body,
      props: { question: null, send: vi.fn() },
    })

    await wrapper.setProps({ question: singleQuestion })
    await wrapper.vm.$nextTick()

    const firstRadio = wrapper.findAll('input[type="radio"]')[0].element
    expect(document.activeElement).toBe(firstRadio)

    wrapper.unmount()
  })

  it('traps Tab at the last control, wrapping focus back to the first', async () => {
    const wrapper = mount(QuestionOverlay, {
      attachTo: document.body,
      props: { question: singleQuestion, send: vi.fn() },
    })
    await wrapper.vm.$nextTick()

    const dialog = wrapper.find('[data-testid="question-overlay"]')
    const focusables = dialog.element.querySelectorAll<HTMLElement>('input, textarea, button')
    const first = focusables[0]
    const last = focusables[focusables.length - 1]

    last.focus()
    expect(document.activeElement).toBe(last)
    await dialog.trigger('keydown', { key: 'Tab' })

    expect(document.activeElement).toBe(first)
    wrapper.unmount()
  })

  it('traps Shift+Tab at the first control, wrapping focus to the last', async () => {
    const wrapper = mount(QuestionOverlay, {
      attachTo: document.body,
      props: { question: singleQuestion, send: vi.fn() },
    })
    await wrapper.vm.$nextTick()

    const dialog = wrapper.find('[data-testid="question-overlay"]')
    const focusables = dialog.element.querySelectorAll<HTMLElement>('input, textarea, button')
    const first = focusables[0]
    const last = focusables[focusables.length - 1]

    first.focus()
    expect(document.activeElement).toBe(first)
    await dialog.trigger('keydown', { key: 'Tab', shiftKey: true })

    expect(document.activeElement).toBe(last)
    wrapper.unmount()
  })

  it('hides once the bound question becomes null (success confirmation)', async () => {
    const wrapper = mount(QuestionOverlay, {
      props: { question: singleQuestion, send: vi.fn() },
    })
    expect(wrapper.find('[data-testid="question-overlay"]').exists()).toBe(true)

    await wrapper.setProps({ question: null })

    expect(wrapper.find('[data-testid="question-overlay"]').exists()).toBe(false)
  })

  it('single-select: choosing option index 1 sends the "2" keystroke', async () => {
    const sendMock = vi.fn()
    const wrapper = mount(QuestionOverlay, {
      props: { question: singleQuestion, send: sendMock },
    })

    await wrapper.findAll('input[type="radio"]')[1].trigger('change')
    await wrapper.find('[data-testid="detected-send-btn"]').trigger('click')

    expect(encodedCalls(sendMock)).toEqual(['2'])
  })

  it('multi-select: choosing options 0 and 2 sends digits, then Tab, then Enter', async () => {
    const sendMock = vi.fn()
    const wrapper = mount(QuestionOverlay, {
      props: { question: multiQuestion, send: sendMock },
    })

    const checkboxes = wrapper.findAll('input[type="checkbox"]')
    await checkboxes[0].trigger('change')
    await checkboxes[2].trigger('change')
    await wrapper.find('[data-testid="detected-send-btn"]').trigger('click')

    expect(encodedCalls(sendMock)).toEqual(['1', '3', '\t', '\r'])
  })

  it('custom answer: sends digit(len+1), the typed text, then Enter', async () => {
    const sendMock = vi.fn()
    const wrapper = mount(QuestionOverlay, {
      props: { question: singleQuestion, send: sendMock },
    })

    await wrapper.find('[data-testid="detected-custom-toggle"]').trigger('click')
    await wrapper.find('[data-testid="detected-custom-textarea"]').setValue('Something else')
    await wrapper.find('[data-testid="detected-send-btn"]').trigger('click')

    expect(encodedCalls(sendMock)).toEqual(['4', 'Something else', '\r'])
  })

  it('chat: sends digit(len+2), the typed text, then Enter', async () => {
    const sendMock = vi.fn()
    const wrapper = mount(QuestionOverlay, {
      props: { question: singleQuestion, send: sendMock },
    })

    await wrapper.find('[data-testid="detected-chat-toggle"]').trigger('click')
    await wrapper.find('[data-testid="detected-chat-textarea"]').setValue('Actually, let me explain')
    await wrapper.find('[data-testid="detected-chat-send-btn"]').trigger('click')

    expect(encodedCalls(sendMock)).toEqual(['5', 'Actually, let me explain', '\r'])
  })
})
