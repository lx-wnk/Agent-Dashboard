import type { NextThing as NextThingItem } from '../composables/useNextThing'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { resolvePermissionRequest } from '@/features/pipeline'
import NextThing from '../components/NextThing.vue'

vi.mock('@/features/pipeline', () => ({ resolvePermissionRequest: vi.fn() }))

const stubs = {
  QuestionCard: { name: 'QuestionCard', props: ['detectedQuestion'], template: '<div data-testid="stub-question" />' },
  ConfirmCard: { name: 'ConfirmCard', props: ['detectedConfirm'], template: '<div data-testid="stub-confirm" />' },
}

function mountNext(next: NextThingItem | null) {
  return mount(NextThing, { props: { next }, global: { stubs } })
}

const permission: NextThingItem = {
  kind: 'permission',
  taskId: 't1',
  taskTitle: 'Fix the thing',
  projectName: 'Dashboard',
  stage: 'implementation',
  request: { id: 'r1', stageRunId: 'run', tool: 'Bash', pattern: 'task lint', requestedAt: '2026-09-18T12:00:00Z', resolvedAt: null, outcome: null } as never,
  title: 'Bash(task lint)',
  why: 'First because an agent is stopped until you answer.',
}

const question: NextThingItem = {
  kind: 'question',
  taskId: '',
  taskTitle: 'agent-dashboard',
  projectName: 'agent-dashboard',
  stage: '',
  pid: 4711,
  question: { header: 'Colour', question: 'Which colour?', multiSelect: false, options: [], typeSomethingIndex: 3, chatAboutIndex: 4 } as never,
  title: 'Which colour?',
  why: 'An agent asked you something and cannot go on until you reply.',
}

describe('nextThing', () => {
  it('offers the three decisions for a permission and no question card', () => {
    const w = mountNext(permission)
    expect(w.find('[data-testid="mission-decide-allow_once"]').exists()).toBe(true)
    expect(w.find('[data-testid="mission-decide-deny_once"]').exists()).toBe(true)
    expect(w.find('[data-testid="stub-question"]').exists()).toBe(false)
  })

  // The centre must not grow its own second way to answer an agent; it renders
  // the card the needs-you band renders.
  it('hands a question to the same card the band uses, with no decision buttons', () => {
    const w = mountNext(question)
    expect(w.find('[data-testid="stub-question"]').exists()).toBe(true)
    expect(w.find('[data-testid="mission-decide-allow_once"]').exists()).toBe(false)
  })

  it('renders the review screen when that is what the agent is holding', () => {
    const confirm = { ...question, question: undefined, confirm: { header: 'Ready to submit?', options: [] } as never }
    const w = mountNext(confirm)
    expect(w.find('[data-testid="stub-confirm"]').exists()).toBe(true)
    expect(w.find('[data-testid="stub-question"]').exists()).toBe(false)
  })

  // A question carries no stage, and a separator with nothing after it reads as
  // a truncated line rather than an absent field.
  it('leaves no dangling separator when there is no stage', () => {
    expect(mountNext(question).get('[data-testid="mission-context"]').text()).toBe('agent-dashboard')
    expect(mountNext(permission).get('[data-testid="mission-context"]').text()).toBe('Dashboard · implementation')
  })

  it('says nothing needs you in one faint line so the Kontor tile gets the height', () => {
    const w = mountNext(null)
    const calm = w.get('[data-testid="mission-calm"]')
    expect(calm.element.tagName).toBe('P')
    expect(calm.text()).toContain('Nothing needs you')
    expect(w.find('h2').exists()).toBe(false)
    expect(w.find('[data-testid="mission-next"]').exists()).toBe(false)
    w.unmount()
  })
})

// A stale click aimed at the request just resolved must not land on whatever
// the refresh puts in its place — see Security Minor 2.
describe('nextThing decision guard', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it('disables the decision buttons for 500ms after the shown request changes, then re-enables them', async () => {
    vi.useFakeTimers()
    const w = mountNext(permission)
    await vi.advanceTimersByTimeAsync(500)
    expect(w.get('[data-testid="mission-decide-allow_once"]').attributes('disabled')).toBeUndefined()

    const next: NextThingItem = { ...permission, request: { ...permission.request, id: 'r2' } as never }
    await w.setProps({ next })
    expect(w.get('[data-testid="mission-decide-allow_once"]').attributes('disabled')).toBeDefined()
    await w.get('[data-testid="mission-decide-allow_once"]').trigger('click')
    expect(resolvePermissionRequest).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(500)
    expect(w.get('[data-testid="mission-decide-allow_once"]').attributes('disabled')).toBeUndefined()
    await w.get('[data-testid="mission-decide-allow_once"]').trigger('click')
    expect(resolvePermissionRequest).toHaveBeenCalledWith('t1', 'r2', 'allow_once')
    w.unmount()
  })
})
