<script setup lang="ts">
import { useServerReconnect } from '../composables/useServerReconnect'

const { isReconnecting } = useServerReconnect()
</script>

<template>
  <div
    v-if="isReconnecting"
    class="fixed inset-0 z-[9999] flex items-center justify-center bg-black/60"
    role="alertdialog"
    aria-live="assertive"
  >
    <div class="flex flex-col items-center gap-3 rounded-xl bg-card p-8 shadow-modal">
      <div class="server-reconnect-spinner" aria-hidden="true" />
      <p class="text-base font-semibold text-fg">
        Server is restarting…
      </p>
      <p class="text-sm text-fg-mute">
        Reconnecting automatically.
      </p>
    </div>
  </div>
</template>

<style scoped>
.server-reconnect-spinner {
  width: 2rem;
  height: 2rem;
  border: 3px solid rgb(255 255 255 / 20%);
  border-top-color: currentColor;
  border-radius: 50%;
  animation: server-reconnect-spin 0.8s linear infinite;
}

@keyframes server-reconnect-spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
