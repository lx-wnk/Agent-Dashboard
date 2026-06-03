<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref, watch } from 'vue'

const props = withDefaults(defineProps<{
  open: boolean
  zIndex?: number
  labelledBy?: string
  /**
   * Panel sizing.
   * - `standard` (default): one fixed frame shared by all content modals —
   *   900px wide, 85vh tall, capped to the viewport. The box does NOT shrink
   *   when there is little content; the body region scrolls instead. AppModal
   *   supplies the chrome (bg/border/radius/shadow) and a flex column, so the
   *   slot only provides header / body / footer sections.
   * - `auto`: no sizing or chrome — the slot brings its own box. Used by the
   *   deliberate exceptions (command palette, settings with its own layout).
   */
  size?: 'standard' | 'auto'
}>(), {
  zIndex: 200,
  size: 'standard',
})

// Chrome + fixed frame for the standard size. `auto` is a transparent
// passthrough so exception modals keep their bespoke box.
const STANDARD_CHROME = 'bg-card border border-line rounded-xl shadow-[0_8px_40px_rgba(0,0,0,0.5)] overflow-hidden flex flex-col'
const panelClass = computed(() => (props.size === 'standard' ? STANDARD_CHROME : ''))
const panelStyle = computed(() =>
  props.size === 'standard'
    ? { width: 'min(900px, calc(100vw - 2rem))', height: 'min(85vh, 800px)' }
    : undefined,
)

const emit = defineEmits<{ close: [] }>()

const modalPanelRef = ref<HTMLElement | null>(null)
const previouslyFocused = ref<HTMLElement | null>(null)

// Lock body scroll when open to prevent background movement making modal appear to jump.
// Compensate scrollbar width so layout doesn't shift on systems with classic scrollbars.
watch(() => props.open, (isOpen) => {
  if (isOpen) {
    const scrollbarWidth = window.innerWidth - document.documentElement.clientWidth
    document.body.style.overflow = 'hidden'
    if (scrollbarWidth > 0)
      document.body.style.paddingRight = `${scrollbarWidth}px`
    // Capture currently focused element so it can be restored on close
    previouslyFocused.value = document.activeElement as HTMLElement | null
    // Focus the modal panel so keyboard events are captured immediately
    nextTick(() => { if (modalPanelRef.value) modalPanelRef.value.focus() })
  }
  else {
    document.body.style.overflow = ''
    document.body.style.paddingRight = ''
    restoreFocus()
  }
}, { immediate: true })

onUnmounted(() => {
  document.body.style.overflow = ''
  document.body.style.paddingRight = ''
  restoreFocus()
})

function restoreFocus() {
  const target = previouslyFocused.value
  previouslyFocused.value = null
  if (target && document.contains(target))
    target.focus()
}

function trapFocus(event: KeyboardEvent) {
  if (event.key !== 'Tab') return
  const FOCUSABLE_SELECTOR = [
    'a[href]',
    'button:not([disabled])',
    'textarea:not([disabled])',
    'input:not([disabled]):not([type="hidden"])',
    'select:not([disabled])',
    '[contenteditable]:not([contenteditable="false"])',
    'audio[controls]',
    'video[controls]',
    'details > summary:first-of-type',
    '[tabindex]:not([tabindex="-1"])',
  ].join(',')
  const all = Array.from(modalPanelRef.value?.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR) ?? [])
  const focusable = all.filter(el => el.offsetParent !== null || el === document.activeElement)
  if (focusable.length === 0) return
  const first = focusable[0]
  const last = focusable[focusable.length - 1]
  if (event.shiftKey) {
    if (document.activeElement === first) {
      event.preventDefault()
      last.focus()
    }
  }
  else {
    if (document.activeElement === last) {
      event.preventDefault()
      first.focus()
    }
  }
}
</script>

<template>
  <Teleport to="body">
    <Transition name="dialog">
      <div
        v-if="open"
        class="fixed inset-0 flex items-center justify-center p-4 bg-black/55"
        :style="{ zIndex }"
        role="dialog"
        aria-modal="true"
        :aria-labelledby="labelledBy || undefined"
        @click.self="emit('close')"
        @keydown.escape="emit('close')"
      >
        <div
          ref="modalPanelRef"
          class="base-modal-box outline-none"
          :class="panelClass"
          :style="panelStyle"
          tabindex="-1"
          @keydown="trapFocus"
        >
          <slot />
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style>
/* Transition styles must be global (not scoped) to target children */
.dialog-enter-active,
.dialog-leave-active {
  transition: opacity 0.2s ease;
}
.dialog-enter-active .base-modal-box,
.dialog-leave-active .base-modal-box {
  transition: transform 0.2s ease, opacity 0.2s ease;
}
.dialog-enter-from,
.dialog-leave-to {
  opacity: 0;
}
.dialog-enter-from .base-modal-box,
.dialog-leave-to .base-modal-box {
  transform: scale(0.95);
  opacity: 0;
}
</style>
