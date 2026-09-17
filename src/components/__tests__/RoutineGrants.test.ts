import type { Grant } from '@/features/settings/composables/useGrants'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import RoutineGrants from '@/components/RoutineGrants.vue'

const SCHEDULE_ID = 'r1'

function makeGrant(overrides: Partial<Grant> = {}): Grant {
  return {
    id: 'g1',
    capabilityName: 'mcp__mail__read_message',
    contextKind: 'routine',
    contextRef: SCHEDULE_ID,
    pattern: '*',
    mode: 'allow',
    limitCount: 0,
    limitWindowSeconds: 0,
    expiresAt: null,
    grantedBy: 'agent',
    grantedAt: '2026-01-01T00:00:00Z',
    revokedAt: null,
    revokedBy: '',
    reason: '',
    nodeId: '',
    ...overrides,
  }
}

let grantsResponse: { status: number, body: unknown }
let fetchMock: ReturnType<typeof vi.fn>
let deleteCalls: string[]

beforeEach(() => {
  grantsResponse = { status: 200, body: [] }
  deleteCalls = []
  fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
    if (url === '/api/capabilities')
      return { ok: true, status: 200, json: async () => [] }
    if (url === '/api/grants') {
      return {
        ok: grantsResponse.status < 400,
        status: grantsResponse.status,
        json: async () => grantsResponse.body,
      }
    }
    if (init?.method === 'DELETE' && url.startsWith('/api/grants/')) {
      deleteCalls.push(url)
      grantsResponse = { status: 200, body: [] }
      return { ok: true, status: 204, json: async () => ({}) }
    }
    throw new Error(`unexpected fetch ${url}`)
  })
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('routineGrants', () => {
  it('shows only non-revoked routine grants scoped to this schedule', async () => {
    grantsResponse.body = [
      makeGrant({ id: 'g1', capabilityName: 'mcp__mail__read_message', mode: 'allow' }),
      makeGrant({ id: 'g2', capabilityName: 'mcp__mail__delete_message', mode: 'deny' }),
      makeGrant({ id: 'g3', contextRef: 'r2', capabilityName: 'mcp__mail__other' }),
      makeGrant({ id: 'g4', revokedAt: '2026-01-02T00:00:00Z' }),
      makeGrant({ id: 'g5', contextKind: 'task', capabilityName: 'mcp__mail__task_scoped' }),
    ]
    const wrapper = mount(RoutineGrants, { props: { scheduleId: SCHEDULE_ID } })
    await flushPromises()

    const rows = wrapper.findAll('[data-testid^="routine-grant-"]').filter(el => el.attributes('data-testid')?.match(/^routine-grant-g\d$/))
    expect(rows).toHaveLength(2)

    const row1 = wrapper.get('[data-testid="routine-grant-g1"]')
    expect(row1.text()).toContain('mcp__mail__read_message')
    expect(row1.text()).toContain('Allowed')

    const row2 = wrapper.get('[data-testid="routine-grant-g2"]')
    expect(row2.text()).toContain('mcp__mail__delete_message')
    expect(row2.text()).toContain('Denied')

    wrapper.unmount()
  })

  it('revokes a grant after confirming, and sends nothing on cancel', async () => {
    grantsResponse.body = [makeGrant({ id: 'g1' })]
    const wrapper = mount(RoutineGrants, { props: { scheduleId: SCHEDULE_ID } })
    await flushPromises()

    await wrapper.get('[data-testid="routine-grant-revoke-g1"]').trigger('click')
    await wrapper.get('[data-testid="routine-grant-revoke-cancel-g1"]').trigger('click')
    await flushPromises()
    expect(deleteCalls).toHaveLength(0)

    await wrapper.get('[data-testid="routine-grant-revoke-g1"]').trigger('click')
    await wrapper.get('[data-testid="routine-grant-revoke-confirm-g1"]').trigger('click')
    await flushPromises()
    expect(deleteCalls).toEqual(['/api/grants/g1'])

    wrapper.unmount()
  })

  it('shows an empty message when no grants match this routine', async () => {
    grantsResponse.body = [makeGrant({ contextRef: 'r2' })]
    const wrapper = mount(RoutineGrants, { props: { scheduleId: SCHEDULE_ID } })
    await flushPromises()

    expect(wrapper.text()).toContain('No decisions saved for this routine')

    wrapper.unmount()
  })

  it('shows the server error message on a failed load, not the empty state', async () => {
    grantsResponse = { status: 500, body: { error: 'boom' } }
    const wrapper = mount(RoutineGrants, { props: { scheduleId: SCHEDULE_ID } })
    await flushPromises()

    const alert = wrapper.get('[role="alert"]')
    expect(alert.text()).toBe('boom')
    expect(wrapper.text()).not.toContain('No decisions saved for this routine')

    wrapper.unmount()
  })
})
