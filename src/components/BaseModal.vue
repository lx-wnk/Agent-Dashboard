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
        class="base-modal-backdrop"
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
.base-modal-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.55);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 16px;
}

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
