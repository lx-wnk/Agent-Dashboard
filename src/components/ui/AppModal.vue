<script setup lang="ts">
withDefaults(defineProps<{
  open: boolean
  zIndex?: number
}>(), {
  zIndex: 200,
})

const emit = defineEmits<{ close: [] }>()
</script>

<template>
  <Teleport to="body">
    <Transition name="dialog">
      <div
        v-if="open"
        class="fixed inset-0 flex items-center justify-center p-4 bg-black/55"
        :style="{ zIndex }"
        @click.self="emit('close')"
      >
        <div class="base-modal-box">
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
