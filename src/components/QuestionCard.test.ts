import type { PendingQuestion } from '../types'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import { ANSWER_CONFIRM_MS } from '../utils/timing'
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

  it('typing a custom answer for a question enables submit and POSTs customText with empty selected', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ ok: true }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mount(QuestionCard, {
      props: { pid: 42, pendingQuestion: sampleQuestion, liveInjectable: true },
    })

    await wrapper.find('[data-testid="custom-toggle-0"]').trigger('click')
    await wrapper.find('[data-testid="custom-textarea-0"]').setValue('My own answer')
    await wrapper.findAll('input[type="checkbox"]')[0].trigger('change')

    const btn = wrapper.find('[data-testid="send-answer-btn"]')
    expect(btn.attributes('disabled')).toBeUndefined()

    await btn.trigger('click')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/agents/42/answer-question',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({
          toolUseId: 'tu-q1',
          answers: [
            { header: 'Choose framework', selected: [], customText: 'My own answer' },
            { header: 'Select features', selected: ['TypeScript'] },
          ],
        }),
      }),
    )
  })

  it('sending a chat message POSTs top-level chatText with empty answers and runs the confirm timer', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ ok: true }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mount(QuestionCard, {
      props: { pid: 42, pendingQuestion: sampleQuestion, liveInjectable: true },
    })

    await wrapper.find('[data-testid="chat-toggle"]').trigger('click')
    const chatBtn = wrapper.find('[data-testid="chat-send-btn"]')
    expect(chatBtn.attributes('disabled')).toBeDefined()

    await wrapper.find('[data-testid="chat-textarea"]').setValue('Actually, let me explain more')
    expect(chatBtn.attributes('disabled')).toBeUndefined()

    await chatBtn.trigger('click')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/agents/42/answer-question',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({
          toolUseId: 'tu-q1',
          answers: [],
          chatText: 'Actually, let me explain more',
        }),
      }),
    )

    vi.advanceTimersByTime(ANSWER_CONFIRM_MS + 1)
    await nextTick()

    expect(wrapper.find('[data-testid="confirm-failed-msg"]').exists()).toBe(true)

    wrapper.unmount()
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

  it('shows confirmFailed warning and re-enables submit after timeout with no resolution', async () => {
    const wrapper = mount(QuestionCard, {
      props: { pid: 1, pendingQuestion: sampleQuestion, liveInjectable: true },
    })

    await wrapper.findAll('input[type="radio"]')[0].trigger('change')
    await wrapper.findAll('input[type="checkbox"]')[0].trigger('change')
    await wrapper.find('[data-testid="send-answer-btn"]').trigger('click')
    await flushPromises()

    // Button enters awaiting-confirmation state
    const btn = wrapper.find('[data-testid="send-answer-btn"]')
    expect(btn.attributes('disabled')).toBeDefined()
    expect(btn.text()).toBe('Waiting…')
    expect(wrapper.find('[data-testid="confirm-failed-msg"]').exists()).toBe(false)

    // Advance past confirmation window without the card unmounting
    vi.advanceTimersByTime(ANSWER_CONFIRM_MS + 1)
    await nextTick()

    expect(wrapper.find('[data-testid="confirm-failed-msg"]').exists()).toBe(true)
    expect(btn.attributes('disabled')).toBeUndefined()

    wrapper.unmount()
  })

  it('cleans up timer on unmount without throwing (simulates resolved answer)', async () => {
    const wrapper = mount(QuestionCard, {
      props: { pid: 1, pendingQuestion: sampleQuestion, liveInjectable: true },
    })

    await wrapper.findAll('input[type="radio"]')[0].trigger('change')
    await wrapper.findAll('input[type="checkbox"]')[0].trigger('change')
    await wrapper.find('[data-testid="send-answer-btn"]').trigger('click')
    await flushPromises()

    // Unmount before timeout fires (card unmount = confirmed resolution)
    wrapper.unmount()

    // Timer must not fire any late state mutation or throw
    expect(() => vi.advanceTimersByTime(ANSWER_CONFIRM_MS + 1)).not.toThrow()
  })

  it('resets confirmation state when pendingQuestion toolUseID changes during await', async () => {
    const wrapper = mount(QuestionCard, {
      props: { pid: 1, pendingQuestion: sampleQuestion, liveInjectable: true },
    })

    await wrapper.findAll('input[type="radio"]')[0].trigger('change')
    await wrapper.findAll('input[type="checkbox"]')[0].trigger('change')
    await wrapper.find('[data-testid="send-answer-btn"]').trigger('click')
    await flushPromises()

    // New question arrives (toolUseID changes) while waiting for confirmation
    const newQuestion: PendingQuestion = { ...sampleQuestion, toolUseID: 'tu-q2' }
    await wrapper.setProps({ pendingQuestion: newQuestion })

    // Advance past original timeout
    vi.advanceTimersByTime(ANSWER_CONFIRM_MS + 1)
    await nextTick()

    // Stale timer must not surface the failure for the previous toolUseID
    expect(wrapper.find('[data-testid="confirm-failed-msg"]').exists()).toBe(false)

    wrapper.unmount()
  })

  it('does not start confirmation window when submit fails', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      json: async () => ({ error: 'Server error' }),
    }))

    const wrapper = mount(QuestionCard, {
      props: { pid: 1, pendingQuestion: sampleQuestion, liveInjectable: true },
    })

    await wrapper.findAll('input[type="radio"]')[0].trigger('change')
    await wrapper.findAll('input[type="checkbox"]')[0].trigger('change')
    await wrapper.find('[data-testid="send-answer-btn"]').trigger('click')
    await flushPromises()

    vi.advanceTimersByTime(ANSWER_CONFIRM_MS + 1)
    await nextTick()

    // No confirmation warning — only the existing sendError path applies
    expect(wrapper.find('[data-testid="confirm-failed-msg"]').exists()).toBe(false)

    wrapper.unmount()
  })
})
