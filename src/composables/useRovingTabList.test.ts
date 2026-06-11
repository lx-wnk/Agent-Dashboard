import { defineComponent, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it } from 'vitest'
import { useRovingTabList } from './useRovingTabList'

function makeHarness(tabs: string[], idPrefix?: string, initial?: string) {
  return defineComponent({
    setup() {
      const tl = useRovingTabList(tabs, { idPrefix, initial })
      return { ...tl, tabs }
    },
    template: `
      <div>
        <button
          v-for="tab in tabs"
          :key="tab"
          v-bind="tabAttrs(tab)"
          @keydown="onKeydown"
          :data-testid="'tab-' + tab"
        >{{ tab }}</button>
        <div
          v-for="tab in tabs"
          :key="'panel-' + tab"
          v-bind="panelAttrs(tab)"
          :data-testid="'panel-' + tab"
        >panel {{ tab }}</div>
      </div>
    `,
  })
}

describe('useRovingTabList', () => {
  describe('initialization', () => {
    it('defaults activeTab to first tab when no initial provided', () => {
      const { activeTab } = useRovingTabList(['a', 'b', 'c'])
      expect(activeTab.value).toBe('a')
    })

    it('uses options.initial when valid', () => {
      const { activeTab } = useRovingTabList(['a', 'b', 'c'], { initial: 'b' })
      expect(activeTab.value).toBe('b')
    })

    it('falls back to first tab when options.initial is not in list', () => {
      const { activeTab } = useRovingTabList(['a', 'b'], { initial: 'z' })
      expect(activeTab.value).toBe('a')
    })
  })

  describe('id generation', () => {
    it('tab id uses default prefix when none given', () => {
      const { tabAttrs } = useRovingTabList(['foo'])
      expect(tabAttrs('foo').id).toBe('tablist-tab-foo')
    })

    it('panel id uses default prefix when none given', () => {
      const { panelAttrs } = useRovingTabList(['foo'])
      expect(panelAttrs('foo').id).toBe('tablist-panel-foo')
    })

    it('tab id respects idPrefix option', () => {
      const { tabAttrs } = useRovingTabList(['foo'], { idPrefix: 'my' })
      expect(tabAttrs('foo').id).toBe('my-tab-foo')
    })

    it('panel id respects idPrefix option', () => {
      const { panelAttrs } = useRovingTabList(['foo'], { idPrefix: 'my' })
      expect(panelAttrs('foo').id).toBe('my-panel-foo')
    })

    it('tabAttrs aria-controls equals panel id', () => {
      const { tabAttrs, panelAttrs } = useRovingTabList(['foo'], { idPrefix: 'x' })
      expect(tabAttrs('foo')['aria-controls']).toBe(panelAttrs('foo').id)
    })

    it('panelAttrs aria-labelledby equals tab id', () => {
      const { tabAttrs, panelAttrs } = useRovingTabList(['foo'], { idPrefix: 'x' })
      expect(panelAttrs('foo')['aria-labelledby']).toBe(tabAttrs('foo').id)
    })
  })

  describe('aria attributes', () => {
    it('active tab has role=tab, aria-selected=true, tabindex=0', () => {
      const { tabAttrs } = useRovingTabList(['a', 'b'], { initial: 'a' })
      const attrs = tabAttrs('a')
      expect(attrs.role).toBe('tab')
      expect(attrs['aria-selected']).toBe('true')
      expect(attrs.tabindex).toBe(0)
    })

    it('inactive tab has aria-selected=false, tabindex=-1', () => {
      const { tabAttrs } = useRovingTabList(['a', 'b'], { initial: 'a' })
      const attrs = tabAttrs('b')
      expect(attrs['aria-selected']).toBe('false')
      expect(attrs.tabindex).toBe(-1)
    })

    it('panel always has role=tabpanel and tabindex=0', () => {
      const { panelAttrs } = useRovingTabList(['a', 'b'])
      const attrs = panelAttrs('a')
      expect(attrs.role).toBe('tabpanel')
      expect(attrs.tabindex).toBe(0)
    })
  })

  describe('select()', () => {
    it('changes activeTab to the selected tab', () => {
      const { activeTab, select } = useRovingTabList(['a', 'b', 'c'])
      select('b')
      expect(activeTab.value).toBe('b')
    })

    it('ignores unknown tab', () => {
      const { activeTab, select } = useRovingTabList(['a', 'b'])
      select('unknown')
      expect(activeTab.value).toBe('a')
    })
  })

  describe('reactivity: tabs as ref', () => {
    it('updates when a ref-wrapped tabs changes', async () => {
      const tabsRef = ref(['a', 'b', 'c'])
      const { activeTab } = useRovingTabList(tabsRef, { initial: 'c' })
      expect(activeTab.value).toBe('c')

      tabsRef.value = ['x', 'y']
      await nextTick()
      expect(activeTab.value).toBe('x')
    })

    it('falls back to first tab when active tab is removed from list', async () => {
      const tabsRef = ref(['a', 'b', 'c'])
      const { activeTab, select } = useRovingTabList(tabsRef)
      select('c')
      expect(activeTab.value).toBe('c')

      tabsRef.value = ['a', 'b']
      await nextTick()
      expect(activeTab.value).toBe('a')
    })

    it('tabs as getter function', () => {
      const list = ['a', 'b']
      const { activeTab } = useRovingTabList(() => list, { initial: 'b' })
      expect(activeTab.value).toBe('b')
    })
  })

  describe('keyboard navigation', () => {
    beforeEach(() => {
      while (document.body.firstChild)
        document.body.removeChild(document.body.firstChild)
    })

    function pressKey(key: string, wrapper: ReturnType<typeof mount>) {
      const activeButton = wrapper.find('[tabindex="0"]')
      activeButton.trigger('keydown', { key })
    }

    it('ArrowRight moves to next tab', async () => {
      const wrapper = mount(makeHarness(['a', 'b', 'c']))
      pressKey('ArrowRight', wrapper)
      await nextTick()
      expect(wrapper.find('[data-testid="tab-b"]').attributes('aria-selected')).toBe('true')
    })

    it('ArrowRight wraps from last to first', async () => {
      const wrapper = mount(makeHarness(['a', 'b', 'c'], undefined, 'c'))
      pressKey('ArrowRight', wrapper)
      await nextTick()
      expect(wrapper.find('[data-testid="tab-a"]').attributes('aria-selected')).toBe('true')
    })

    it('ArrowLeft moves to previous tab', async () => {
      const wrapper = mount(makeHarness(['a', 'b', 'c'], undefined, 'b'))
      pressKey('ArrowLeft', wrapper)
      await nextTick()
      expect(wrapper.find('[data-testid="tab-a"]').attributes('aria-selected')).toBe('true')
    })

    it('ArrowLeft wraps from first to last', async () => {
      const wrapper = mount(makeHarness(['a', 'b', 'c']))
      pressKey('ArrowLeft', wrapper)
      await nextTick()
      expect(wrapper.find('[data-testid="tab-c"]').attributes('aria-selected')).toBe('true')
    })

    it('Home moves to first tab', async () => {
      const wrapper = mount(makeHarness(['a', 'b', 'c'], undefined, 'c'))
      pressKey('Home', wrapper)
      await nextTick()
      expect(wrapper.find('[data-testid="tab-a"]').attributes('aria-selected')).toBe('true')
    })

    it('End moves to last tab', async () => {
      const wrapper = mount(makeHarness(['a', 'b', 'c']))
      pressKey('End', wrapper)
      await nextTick()
      expect(wrapper.find('[data-testid="tab-c"]').attributes('aria-selected')).toBe('true')
    })

    it('ArrowRight calls preventDefault', async () => {
      const { onKeydown } = useRovingTabList(['a', 'b'])
      const event = new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true })
      const prevented: boolean[] = []
      event.preventDefault = () => prevented.push(true)
      onKeydown(event)
      expect(prevented).toHaveLength(1)
    })

    it('other keys do not call preventDefault', () => {
      const { onKeydown } = useRovingTabList(['a', 'b'])
      const event = new KeyboardEvent('keydown', { key: 'Tab', bubbles: true })
      const prevented: boolean[] = []
      event.preventDefault = () => prevented.push(true)
      onKeydown(event)
      expect(prevented).toHaveLength(0)
    })
  })
})
