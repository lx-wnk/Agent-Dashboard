import { describe, expect, it } from 'vitest'
import { NAV_GROUPS, NAV_ITEMS, viewTitle } from './navConfig'

describe('navConfig', () => {
  it('has one item per ActiveView', () => {
    const views = NAV_ITEMS.map(i => i.view).sort()
    expect(views).toEqual(['cockpit', 'cost', 'dashboard', 'eval', 'pipeline', 'schedules', 'workflows'])
  })

  it('cockpit is the first Monitor item and has a title', () => {
    expect(NAV_ITEMS[0].view).toBe('cockpit')
    expect(viewTitle('cockpit')).toBe('Cockpit')
  })

  it('groups are Monitor, Build and Insights', () => {
    expect(NAV_GROUPS).toEqual(['Monitor', 'Build', 'Insights'])
  })

  it('groups Workflows, Cost and Eval under Insights', () => {
    const insights = NAV_ITEMS.filter(i => i.group === 'Insights').map(i => i.view)
    expect(insights).toEqual(['workflows', 'cost', 'eval'])
  })

  it('every item belongs to a known group', () => {
    for (const item of NAV_ITEMS)
      expect(NAV_GROUPS).toContain(item.group)
  })

  it('viewTitle returns the label for a view', () => {
    expect(viewTitle('dashboard')).toBe('Dashboard')
    expect(viewTitle('cost')).toBe('Cost')
  })
})
