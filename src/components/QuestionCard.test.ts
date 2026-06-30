import type { PendingQuestion } from '../types'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import QuestionCard from './QuestionCard.vue'

const sampleQuestion: PendingQuestion = {
  toolUseID: 'tu-q1',
  questions: [
    {
      header: 'Choose framework',
      question: 'Which frontend framework do you prefer?',
      multiSelect: false,
      options: [
        { label: 'Vue', description: 'Progressive framework' },
        { label: 'React', description: 'UI library' },
      ],
    },
    {
      header: 'Select features',
      question: 'Which features do you want?',
      multiSelect: true,
      options: [
        { label: 'TypeScript', description: 'Type safety' },
        { label: 'ESLint', description: 'Linting' },
      ],
    },
  ],
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    ok: true,
    json: async () => ({ ok: true, transport: 'tmux' }),
  }))
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('questionCard', () => {
  it('renders all question headers, prompts, and option labels', () => {
    const wrapper = mount(QuestionCard, {
      props: { pid: 1, pendingQuestion: sampleQuestion, liveInjectable: true },
    })
    expect(wrapper.text()).toContain('Choose framework')
    expect(wrapper.text()).toContain('Which frontend framework do you prefer?')
    expect(wrapper.text()).toContain('Vue')
    expect(wrapper.text()).toContain('Progressive framework')
    expect(wrapper.text()).toContain('React')
    expect(wrapper.text()).toContain('Select features')
    expect(wrapper.text()).toContain('TypeScript')
    expect(wrapper.text()).toContain('ESLint')
  })

  it('renders radio inputs for single-select and checkboxes for multi-select', () => {
    const wrapper = mount(QuestionCard, {
      props: { pid: 1, pendingQuestion: sampleQuestion, liveInjectable: true },
    })
    expect(wrapper.findAll('input[type="radio"]').length).toBe(2)
    expect(wrapper.findAll('input[type="checkbox"]').length).toBe(2)
  })

  it('send button is disabled until every question has a selection', async () => {
    const wrapper = mount(QuestionCard, {
      props: { pid: 1, pendingQuestion: sampleQuestion, liveInjectable: true },
    })
    const btn = wrapper.find('[data-testid="send-answer-btn"]')
    expect(btn.attributes('disabled')).toBeDefined()

    // Answer first question (radio — single select)
    await wrapper.findAll('input[type="radio"]')[0].trigger('change')
    expect(btn.attributes('disabled')).toBeDefined()

    // Answer second question (checkbox — multi select)
    await wrapper.findAll('input[type="checkbox"]')[0].trigger('change')
    expect(btn.attributes('disabled')).toBeUndefined()
  })

  it('clicking Send when all selections are made POSTs the correct body to fetch', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ ok: true }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mount(QuestionCard, {
      props: { pid: 42, pendingQuestion: sampleQuestion, liveInjectable: true },
    })

    await wrapper.findAll('input[type="radio"]')[0].trigger('change')
    await wrapper.findAll('input[type="checkbox"]')[0].trigger('change')
    await wrapper.find('[data-testid="send-answer-btn"]').trigger('click')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/agents/42/answer-question',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({
          toolUseId: 'tu-q1',
          answers: [
            { header: 'Choose framework', selected: ['Vue'] },
            { header: 'Select features', selected: ['TypeScript'] },
          ],
        }),
      }),
    )
  })

  it('shows terminal note and no Send button when liveInjectable is false', () => {
    const wrapper = mount(QuestionCard, {
      props: { pid: 1, pendingQuestion: sampleQuestion, liveInjectable: false },
    })
    expect(wrapper.find('[data-testid="terminal-note"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="send-answer-btn"]').exists()).toBe(false)
  })

  it('inputs are disabled when liveInjectable is false', () => {
    const wrapper = mount(QuestionCard, {
      props: { pid: 1, pendingQuestion: sampleQuestion, liveInjectable: false },
    })
    const allInputs = wrapper.findAll('input')
    expect(allInputs.length).toBeGreaterThan(0)
    allInputs.forEach(input => expect(input.attributes('disabled')).toBeDefined())
  })
})
