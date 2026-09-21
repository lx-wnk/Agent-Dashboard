import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'

vi.mock('../widgetRegistry', () => ({
  WIDGETS: {
    agents: { id: 'agents', title: 'Agents', component: defineComponent({ render: () => h('p', 'agents body') }) },
  },
}))

const { default: WorkspaceGrid } = await import('./WorkspaceGrid.vue')

const page = {
  id: 'zentrale',
  title: 'Zentrale',
  tiles: [
    { widget: 'obsidian__recent', col: 5, row: 2, colSpan: 4, rowSpan: 1 },
    { widget: 'agents', col: 1, row: 1, colSpan: 4, rowSpan: 2 },
  ],
}

describe('workspaceGrid', () => {
  it('anchors each tile where the layout says, in reading order', () => {
    const w = mount(WorkspaceGrid, { props: { page, editing: false } })
    const tiles = w.findAll('[data-testid^="workspace-tile-"]')
    expect(tiles.map(t => t.attributes('data-testid'))).toEqual(['workspace-tile-agents', 'workspace-tile-obsidian__recent'])
    const style = tiles[0].attributes('style')
    expect(style).toContain('--col: 1')
    expect(style).toContain('--col-span: 4')
    expect(style).toContain('--row: 1')
    expect(style).toContain('--row-span: 2')
    expect(w.get('[data-testid="workspace-grid"]').attributes('style')).toContain('--rows: 2')
    w.unmount()
  })

  // A module that is deactivated must not cost the operator their layout.
  it('renders an unknown widget as a placeholder naming it', () => {
    const w = mount(WorkspaceGrid, { props: { page, editing: false } })
    expect(w.get('[data-testid="workspace-unknown"]').text()).toContain('obsidian__recent')
    expect(w.text()).toContain('agents body')
    w.unmount()
  })
})
