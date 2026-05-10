import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import ExecutionWaterfall from './ExecutionWaterfall.vue'

const resolvedFetch = vi.fn().mockResolvedValue({
  ok: true,
  json: async () => ({
    toolCalls: [
      { role: 'tool_call', toolName: 'Read', timestamp: '2024-01-01T00:00:00.000Z', content: '' },
      { role: 'tool_call', toolName: 'Write', timestamp: '2024-01-01T00:00:01.000Z', content: '' },
    ],
  }),
})

describe('executionWaterfall', () => {
  it('fetches timeline on mount', async () => {
    vi.stubGlobal('fetch', resolvedFetch)
    const wrapper = mount(ExecutionWaterfall, { props: { sessionId: '00000000-0000-0000-0000-000000000001' } })
    await wrapper.vm.$nextTick()
    expect(fetch).toHaveBeenCalledWith('/api/sessions/00000000-0000-0000-0000-000000000001/timeline')
  })

  it('shows loading state initially', () => {
    // Use a never-resolving fetch so the component stays in loading state
    vi.stubGlobal('fetch', vi.fn().mockReturnValue(new Promise(() => {})))
    const wrapper = mount(ExecutionWaterfall, { props: { sessionId: '00000000-0000-0000-0000-000000000001' } })
    expect(wrapper.text()).toContain('Loading')
  })
})
