<script setup lang="ts">
import { pauseToast, resumeToast, useToast } from '../composables/useToast'

const { toasts } = useToast()

const typeClasses: Record<string, string> = {
  error: 'bg-raised border-line text-fg',
  success: 'bg-raised border-line text-fg',
  info: 'bg-raised border-line text-fg',
}
</script>

<template>
  <!-- F-UIUX-011: live region always in DOM so screen readers pick up insertions. -->
  <div role="status" aria-live="polite" aria-atomic="true" class="pointer-events-none">
    <TransitionGroup name="toast" tag="div">
      <div
        v-for="t in toasts"
        :key="t.id"
        class="pointer-events-auto fixed bottom-6 left-1/2 -translate-x-1/2 border px-5 py-2.5 rounded-lg text-[13px] z-[2000] shadow-[0_4px_16px_rgba(0,0,0,0.4)] mb-1"
        :class="typeClasses[t.type]"
        @mouseenter="pauseToast(t.id)"
        @mouseleave="resumeToast(t.id)"
      >
        {{ t.message }}
      </div>
    </TransitionGroup>
  </div>
</template>
