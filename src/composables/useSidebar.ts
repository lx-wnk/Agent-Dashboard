import { computed, ref, watch } from 'vue'

const storedPinned = typeof localStorage !== 'undefined'
  ? localStorage.getItem('agent-sidebar-pinned') === 'true'
  : false

const pinned = ref<boolean>(storedPinned)
const hovering = ref(false)
const expanded = computed(() => pinned.value || hovering.value)

watch(pinned, (v) => {
  if (typeof localStorage !== 'undefined')
    localStorage.setItem('agent-sidebar-pinned', String(v))
}, { flush: 'sync' })

function togglePinned() {
  pinned.value = !pinned.value
}

function setHovering(v: boolean) {
  hovering.value = v
}

function handleShortcut(e: KeyboardEvent) {
  if (e.key === 'b' && (e.ctrlKey || e.metaKey) && !e.shiftKey && !e.altKey) {
    e.preventDefault()
    togglePinned()
  }
}

export function useSidebar() {
  return { pinned, hovering, expanded, togglePinned, setHovering, handleShortcut }
}
