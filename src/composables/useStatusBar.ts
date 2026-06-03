import { ref, watch } from 'vue'

export type StatusSegment = 'system' | 'cost'

const collapsed = ref<boolean>(
  typeof localStorage !== 'undefined' && localStorage.getItem('agent-statusbar-collapsed') === 'true',
)
const openSegment = ref<StatusSegment | null>(null)

watch(collapsed, (v) => {
  if (typeof localStorage !== 'undefined')
    localStorage.setItem('agent-statusbar-collapsed', String(v))
}, { flush: 'sync' })

function toggleSegment(seg: StatusSegment) {
  openSegment.value = openSegment.value === seg ? null : seg
}

function toggleCollapsed() {
  collapsed.value = !collapsed.value
  if (collapsed.value)
    openSegment.value = null
}

export function useStatusBar() {
  return { collapsed, openSegment, toggleSegment, toggleCollapsed }
}
