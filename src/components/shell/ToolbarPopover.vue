<script setup lang="ts">
import { onBeforeUnmount, ref, useId, watch } from 'vue'

const props = withDefaults(defineProps<{
  label: string
  ariaLabel?: string
  icon?: string
  badge?: number | null
  align?: 'start' | 'end'
  active?: boolean
  showCaret?: boolean
}>(), {
  align: 'start',
  badge: null,
  active: false,
  showCaret: true,
})

const panelId = useId()
const rootRef = ref<HTMLElement | null>(null)
const triggerRef = ref<HTMLButtonElement | null>(null)
const isOpen = ref(false)

function toggle(): void {
  isOpen.value = !isOpen.value
}

function close(restoreFocus = false): void {
  isOpen.value = false
  if (restoreFocus)
    triggerRef.value?.focus()
}

// AppSelect teleports its listbox to <body>, so a click on one of its options
// lands outside this popover's subtree. Treating that as an outside click would
// close the popover the moment a filter is picked inside it.
function isInsideOpenListbox(target: Node): boolean {
  const el = target instanceof Element ? target : target.parentElement
  return !!el?.closest('[role="listbox"]')
}

function onDocumentMouseDown(event: MouseEvent): void {
  const target = event.target as Node | null
  if (!target)
    return
  if (rootRef.value?.contains(target) || isInsideOpenListbox(target))
    return
  close()
}

function onDocumentKeyDown(event: KeyboardEvent): void {
  if (event.key !== 'Escape')
    return
  // A nested select owns Escape while its own panel is open.
  if (document.querySelector('[role="listbox"]'))
    return
  event.stopPropagation()
  close(true)
}

watch(isOpen, (open) => {
  if (open) {
    document.addEventListener('mousedown', onDocumentMouseDown, true)
    document.addEventListener('keydown', onDocumentKeyDown)
  }
  else {
    document.removeEventListener('mousedown', onDocumentMouseDown, true)
    document.removeEventListener('keydown', onDocumentKeyDown)
  }
})

onBeforeUnmount(() => {
  document.removeEventListener('mousedown', onDocumentMouseDown, true)
  document.removeEventListener('keydown', onDocumentKeyDown)
})

defineExpose({ close })
</script>

<template>
  <div ref="rootRef" class="relative">
    <button
      ref="triggerRef"
      type="button"
      class="flex items-center gap-1.5 rounded-lg border px-2.5 py-1 text-xs transition-colors focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent"
      :class="props.active || isOpen
        ? 'border-accent bg-accent-soft text-accent font-semibold'
        : 'border-line bg-card text-fg hover:border-line-strong'"
      :aria-label="props.ariaLabel ?? props.label"
      :aria-expanded="isOpen"
      :aria-controls="panelId"
      aria-haspopup="true"
      @click="toggle"
    >
      <span v-if="props.icon" aria-hidden="true">{{ props.icon }}</span>
      <span>{{ props.label }}</span>
      <span
        v-if="props.badge"
        class="rounded-full bg-accent px-1.5 text-[10px] font-bold leading-4 text-accent-contrast"
      >{{ props.badge }}</span>
      <span v-if="props.showCaret" aria-hidden="true" class="text-[9px] opacity-70">▾</span>
    </button>

    <div
      v-show="isOpen"
      :id="panelId"
      class="absolute top-full z-20 mt-1 min-w-[220px] rounded-lg border border-line bg-card p-3 shadow-[0_8px_24px_rgba(0,0,0,0.18)]"
      :class="props.align === 'end' ? 'right-0' : 'left-0'"
    >
      <slot :close="close" />
    </div>
  </div>
</template>
