import { describe, expect, it } from 'vitest'
import { widgetIds, WIDGETS } from './widgetRegistry'
import { WIDGET_SPECS } from './widgetSpecs'

describe('widget registry', () => {
  // Both sides are read: a spec without a component, or a component without a
  // spec, is the drift this registry exists to rule out.
  it('has a component for every spec and a spec for every component', () => {
    expect(widgetIds().sort()).toEqual(Object.keys(WIDGET_SPECS).sort())
  })

  it('carries the cockpit panels, live work and today\'s cost', () => {
    for (const id of ['agents', 'pipeline', 'routines', 'memory', 'github', 'live-work', 'cost-today', 'kontor', 'hub'])
      expect(widgetIds()).toContain(id)
  })

  it('gives every widget a title and spans that fit twelve columns', () => {
    for (const id of widgetIds()) {
      const w = WIDGETS[id]
      expect(w.title.length).toBeGreaterThan(0)
      expect(w.minColSpan).toBeGreaterThanOrEqual(1)
      expect(w.minColSpan).toBeLessThanOrEqual(w.defaultColSpan)
      expect(w.defaultColSpan).toBeLessThanOrEqual(12)
      expect(w.minRowSpan).toBeGreaterThanOrEqual(1)
      expect(w.minRowSpan).toBeLessThanOrEqual(w.defaultRowSpan)
    }
  })
})
