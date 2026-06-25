import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'

const approveMock = vi.fn().mockResolvedValue({ id: 't1', currentStage: 'implementation' })
const rejectMock = vi.fn().mockResolvedValue(undefined)
const fetchStatusMock = vi.fn().mockResolvedValue(undefined)

vi.mock('../../composables/usePlanReview', () => ({
  usePlanReview: () => ({
    gateState: ref('awaiting_user'),
    approvedPlan: ref({ steps: ['step one', 'step two'] }),
    loading: ref(false),
    error: ref(null),
    fetchStatus: fetchStatusMock,
    approve: approveMock,
    reject: rejectMock,
  }),
}))

vi.mock('../../utils/markdown', () => ({
  renderMarkdown: (text: string) => `<p>${text}</p>`,
}))

// Loaded lazily — import after mocks are in place.
let PlanReviewPanel: any

beforeEach(async () => {
  vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({ ok: true, status: 200, json: async () => ({}) })))
  const mod = await import('../PlanReviewPanel.vue')
  PlanReviewPanel = mod.default
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.clearAllMocks()
  vi.resetModules()
})

function mountPanel(overrides: Record<string, unknown> = {}) {
  return mount(PlanReviewPanel, {
    props: {
      open: true,
      task: { id: 't1', slug: 's1', title: 'My Task', currentStage: 'plan_review', createdAt: '', updatedAt: '' },
      ...overrides,
    },
    attachTo: document.body,
  })
}

describe('planReviewPanel', () => {
  it('renders nothing when open=false', async () => {
    const wrapper = mountPanel({ open: false })
    await flushPromises()
    expect(wrapper.find('[data-testid="plan-review-panel"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows the plan content when open', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    expect(wrapper.find('[data-testid="plan-review-panel"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('calls fetchStatus on mount', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    expect(fetchStatusMock).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('emits approved with updated task when Approve is clicked', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const approveBtn = wrapper.find('[data-testid="approve-plan-btn"]')
    expect(approveBtn.exists()).toBe(true)
    await approveBtn.trigger('click')
    await flushPromises()

    expect(approveMock).toHaveBeenCalled()
    expect(wrapper.emitted('approved')).toBeTruthy()
    expect(wrapper.emitted('approved')![0][0]).toMatchObject({ id: 't1', currentStage: 'implementation' })
    wrapper.unmount()
  })

  it('shows reject feedback textarea when Reject is clicked', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const rejectBtn = wrapper.find('[data-testid="reject-plan-btn"]')
    expect(rejectBtn.exists()).toBe(true)
    await rejectBtn.trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="reject-feedback-textarea"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('calls reject with feedback and emits rejected', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('[data-testid="reject-plan-btn"]').trigger('click')
    await flushPromises()

    await wrapper.find('[data-testid="reject-feedback-textarea"]').setValue('needs more detail')
    await wrapper.find('[data-testid="submit-reject-btn"]').trigger('click')
    await flushPromises()

    expect(rejectMock).toHaveBeenCalledWith('needs more detail')
    expect(wrapper.emitted('rejected')).toBeTruthy()
    wrapper.unmount()
  })

  it('emits close when the close button is clicked', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('[data-testid="plan-review-close-btn"]').trigger('click')
    expect(wrapper.emitted('close')).toBeTruthy()
    wrapper.unmount()
  })
})
