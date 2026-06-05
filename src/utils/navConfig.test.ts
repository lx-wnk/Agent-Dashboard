import { describe, expect, it } from 'vitest'
import { NAV_GROUPS, NAV_ITEMS, viewTitle } from './navConfig'

describe('navConfig', () => {
  it('has one item per ActiveView', () => {
    const views = NAV_ITEMS.map(i => i.view).sort()
    expect(views).toEqual(['cost', 'dashboard', 'pipeline', 'workflows'])
  })

  it('groups are Monitor, Build and Insights', () => {
    expect(NAV_GROUPS).toEqual(['Monitor', 'Build', 'Insights'])
  })

  it('groups Workflows and Cost under Insights', () => {
    const insights = NAV_ITEMS.filter(i => i.group === 'Insights').map(i => i.view)
    expect(insights).toEqual(['workflows', 'cost'])
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
