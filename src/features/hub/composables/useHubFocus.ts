import type { Ref } from 'vue'
import { ref } from 'vue'
import { useViewState } from '@/composables/useViewState'
import { pageView, pageWithWidget, useWorkspace } from '@/features/workspace'

export type HubTarget = { kind: 'note', path: string } | { kind: 'agent', pid: number }

const HUB_WIDGET = 'hub'

// Shown by callers of focusInHub when it returns false — one wording kept in one place.
export const NO_HUB_PAGE_MESSAGE = 'No page shows the Zentrale hub; add the hub tile to a page.'

export const hubFocusRequest: Ref<HubTarget | null> = ref(null)

/** Navigates to the page holding the hub and asks it to fly to the target. False when no page has one. */
export function focusInHub(target: HubTarget): boolean {
  const page = pageWithWidget(useWorkspace().layout.value, HUB_WIDGET)
  if (!page)
    return false
  useViewState().activeView.value = pageView(page.id)
  hubFocusRequest.value = target
  return true
}
