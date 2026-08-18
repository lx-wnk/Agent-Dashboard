import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ToolTimeline from '../ToolTimeline.vue'

describe('toolTimeline', () => {
  it('shows the detail beside the tool name so repeated tools are distinguishable', () => {
    const w = mount(ToolTimeline, { props: { tools: [
      { name: 'Bash', detail: 'go test ./...' },
      { name: 'Bash', detail: 'pnpm lint' },
    ] } })

    const details = w.findAll('[data-testid="tool-detail"]').map(d => d.text())
    expect(details).toEqual(['go test ./...', 'pnpm lint'])
  })

  it('renders the bare name when an entry carries no detail', () => {
    const w = mount(ToolTimeline, { props: { tools: [{ name: 'TodoWrite' }] } })

    expect(w.text()).toContain('TodoWrite')
    expect(w.find('[data-testid="tool-detail"]').exists()).toBe(false)
  })

  it('keeps the full detail reachable on hover rather than only the clipped text', () => {
    const detail = 'a'.repeat(200)
    const w = mount(ToolTimeline, { props: { tools: [{ name: 'Bash', detail }] } })

    expect(w.find('[data-testid="tool-detail"]').attributes('title')).toBe(detail)
  })
})
