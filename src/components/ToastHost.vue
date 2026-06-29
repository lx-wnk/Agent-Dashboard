<script setup lang="ts">
import { dismiss, pauseToast, resumeToast, useToast } from '../composables/useToast'

const { toasts } = useToast()

const typeClasses: Record<string, string> = {
  error: 'bg-danger-soft border-danger-line text-danger-text',
  success: 'bg-success-soft border-success-line text-success-text',
  info: 'bg-raised border-line text-fg',
}
</script>

<template>
  <!-- F-UIUX-011: live region always in DOM so screen readers pick up insertions. -->
  <div role="status" aria-live="polite" aria-atomic="true" class="pointer-events-none">
    <TransitionGroup
      name="toast"
      tag="div"
      class="fixed bottom-6 left-1/2 -translate-x-1/2 z-[2000] flex flex-col items-center gap-2"
    >
      <div
        v-for="t in toasts"
        :key="t.id"
        class="pointer-events-auto flex items-center gap-3 border px-5 py-2.5 rounded-lg text-[13px] shadow-[0_4px_16px_rgba(0,0,0,0.4)]"
        :class="typeClasses[t.type]"
        @mouseenter="pauseToast(t.id)"
        @mouseleave="resumeToast(t.id)"
      >
        <span>{{ t.message }}</span>
        <button
          type="button"
          aria-label="Dismiss"
          class="-mr-1.5 shrink-0 leading-none text-base opacity-60 hover:opacity-100"
          @click="dismiss(t.id)"
        >
          &times;
        </button>
      </div>
    </TransitionGroup>
  </div>
</template>
