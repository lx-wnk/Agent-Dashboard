import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { WIDGET_SPECS } from '../widgetSpecs'

// Real panels would fetch from jsdom; the swap/add candidates are the four
// stub widgets below so the assertions on which options are offered hold.
vi.mock('../widgetRegistry', () => {
  const ids = ['agents', 'github', 'pipeline', 'live-work']
  const stub = (id: string) => defineComponent({ name: id, render: () => h('p', `${id} body`) })
  return {
    WIDGETS: Object.fromEntries(ids.map(id => [id, { ...WIDGET_SPECS[id], component: stub(id) }])),
    widgetIds: () => ids,
  }
})

const { default: WorkspaceGrid } = await import('./WorkspaceGrid.vue')
const { default: WorkspaceEditBar } = await import('./WorkspaceEditBar.vue')

const page = {
  id: 'zentrale',
  title: 'Zentrale',
  tiles: [
    { widget: 'agents', col: 1, row: 1, colSpan: 3, rowSpan: 3 },
    { widget: 'github', col: 4, row: 1, colSpan: 3, rowSpan: 3 },
  ],
}

describe('edit mode', () => {
  it('moves a focused tile with the arrow keys', async () => {
    const w = mount(WorkspaceGrid, { props: { page, editing: true } })
    await w.get('[data-testid="workspace-tile-agents"]').trigger('keydown', { key: 'ArrowDown' })
    expect(w.emitted('change')?.[0]?.[0]).toMatchObject({ tiles: [{ widget: 'agents', row: 2 }, { widget: 'github' }] })
    w.unmount()
  })

  // Refuse, never displace: the model is unchanged and the reason is said.
  it('refuses a move onto another tile and reports why', async () => {
    const w = mount(WorkspaceGrid, { props: { page, editing: true } })
    for (const _ of [1, 2, 3])
      await w.get('[data-testid="workspace-tile-agents"]').trigger('keydown', { key: 'ArrowRight' })
    expect(w.emitted('change')?.length ?? 0).toBeLessThan(3)
    expect(w.emitted('refuse')?.[0]?.[0]).toMatch(/overlap/i)
    w.unmount()
  })

  it('swaps a tile in place from its chrome, keeping anchor and span', async () => {
    const w = mount(WorkspaceGrid, { props: { page, editing: true } })
    await w.get('[data-testid="workspace-swap-agents"]').setValue('pipeline')
    expect(w.emitted('change')?.[0]?.[0]).toMatchObject({ tiles: [{ widget: 'pipeline', col: 1, row: 1, colSpan: 3, rowSpan: 3 }, { widget: 'github' }] })
    w.unmount()
  })

  it('removes a tile from its chrome', async () => {
    const w = mount(WorkspaceGrid, { props: { page, editing: true } })
    await w.get('[data-testid="workspace-remove-agents"]').trigger('click')
    expect(w.emitted('change')?.[0]?.[0]).toMatchObject({ tiles: [{ widget: 'github' }] })
    w.unmount()
  })

  it('shows no chrome and ignores keys outside edit mode', async () => {
    const w = mount(WorkspaceGrid, { props: { page, editing: false } })
    expect(w.find('[data-testid="workspace-remove-agents"]').exists()).toBe(false)
    await w.get('[data-testid="workspace-tile-agents"]').trigger('keydown', { key: 'ArrowDown' })
    expect(w.emitted('change')).toBeUndefined()
    w.unmount()
  })
})

describe('workspace edit bar', () => {
  it('adds a tile, excluding widgets already on the page', async () => {
    const w = mount(WorkspaceEditBar, { props: { page, refusal: null, locked: false } })
    const options = w.findAll('option').map(o => o.attributes('value')).filter(v => v)
    expect(options).not.toContain('agents')
    expect(options).not.toContain('github')
    await w.get('[data-testid="workspace-add"]').setValue('pipeline')
    await w.get('[data-testid="workspace-add-submit"]').trigger('click')
    expect(w.emitted('change')?.[0]?.[0]).toMatchObject({ tiles: [{ widget: 'agents' }, { widget: 'github' }, { widget: 'pipeline' }] })
    w.unmount()
  })
})
