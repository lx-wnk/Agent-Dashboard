import type { MaybeRefOrGetter, Ref } from 'vue'
import { computed, ref, toValue, watch } from 'vue'

export interface RovingTabAttrs {
  'role': 'tab'
  'id': string
  'aria-selected': 'true' | 'false'
  'aria-controls': string
  'tabindex': 0 | -1
}

export interface RovingPanelAttrs {
  'role': 'tabpanel'
  'id': string
  'aria-labelledby': string
  'tabindex': 0
}

export function useRovingTabList(
  tabs: MaybeRefOrGetter<readonly string[]>,
  options?: { idPrefix?: string, initial?: string },
): {
  activeTab: Ref<string>
  select: (tab: string) => void
  tabAttrs: (tab: string) => RovingTabAttrs
  panelAttrs: (tab: string) => RovingPanelAttrs
  onKeydown: (e: KeyboardEvent) => void
} {
  const prefix = options?.idPrefix ?? 'tablist'

  const tabList = computed(() => toValue(tabs))

  function resolveInitial() {
    const list = tabList.value
    if (options?.initial && list.includes(options.initial))
      return options.initial
    return list[0] ?? ''
  }

  const activeTab = ref(resolveInitial())

  watch(tabList, (list) => {
    if (!list.includes(activeTab.value))
      activeTab.value = list[0] ?? ''
  })

  function select(tab: string) {
    if (tabList.value.includes(tab))
      activeTab.value = tab
  }

  function tabId(tab: string) {
    return `${prefix}-tab-${tab}`
  }

  function panelId(tab: string) {
    return `${prefix}-panel-${tab}`
  }

  function tabAttrs(tab: string): RovingTabAttrs {
    const isActive = activeTab.value === tab
    return {
      'role': 'tab',
      'id': tabId(tab),
      'aria-selected': isActive ? 'true' : 'false',
      'aria-controls': panelId(tab),
      'tabindex': isActive ? 0 : -1,
    }
  }

  function panelAttrs(tab: string): RovingPanelAttrs {
    return {
      'role': 'tabpanel',
      'id': panelId(tab),
      'aria-labelledby': tabId(tab),
      'tabindex': 0,
    }
  }

  function onKeydown(e: KeyboardEvent) {
    const list = tabList.value
    const idx = list.indexOf(activeTab.value)
    let next: string | undefined

    if (e.key === 'ArrowRight') {
      next = list[(idx + 1) % list.length]
    }
    else if (e.key === 'ArrowLeft') {
      next = list[(idx - 1 + list.length) % list.length]
    }
    else if (e.key === 'Home') {
      next = list[0]
    }
    else if (e.key === 'End') {
      next = list[list.length - 1]
    }

    if (next !== undefined) {
      e.preventDefault()
      activeTab.value = next
      document.getElementById(tabId(next))?.focus()
    }
  }

  return { activeTab, select, tabAttrs, panelAttrs, onKeydown }
}
