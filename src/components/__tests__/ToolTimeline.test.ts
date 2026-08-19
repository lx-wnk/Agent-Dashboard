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

  // The count is rendered as its own element, so an agent-authored command that
  // merely ends in something cut-shaped cannot claim text was removed.
  it('reports a server-side cut beside the text, never inside it', () => {
    const w = mount(ToolTimeline, { props: { tools: [
      { name: 'Bash', detail: 'a'.repeat(120), elided: 87 },
    ] } })

    expect(w.find('[data-testid="tool-detail"]').text()).not.toContain('87')
    expect(w.find('[data-testid="tool-detail-elided"]').text()).toContain('87')
  })

  it('shows no cut marker for an uncut entry that looks cut', () => {
    const w = mount(ToolTimeline, { props: { tools: [
      { name: 'Bash', detail: 'echo done… (+400 chars)' },
    ] } })

    expect(w.find('[data-testid="tool-detail-elided"]').exists()).toBe(false)
  })

  // Nothing is hidden behind hover any more: the server already capped the
  // string, so the visible text is everything the client was given.
  it('does not hide any of the detail behind a tooltip', () => {
    const detail = 'a'.repeat(120)
    const w = mount(ToolTimeline, { props: { tools: [{ name: 'Bash', detail }] } })

    const span = w.find('[data-testid="tool-detail"]')
    expect(span.attributes('title')).toBeUndefined()
    expect(span.classes()).not.toContain('truncate')
    expect(span.text()).toBe(detail)
  })
})
