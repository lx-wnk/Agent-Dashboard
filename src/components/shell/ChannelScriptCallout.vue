<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useServerConfig } from '../../composables/useServerConfig'

const { scriptPath, loadServerConfig } = useServerConfig()
const copied = ref(false)

onMounted(() => {
  void loadServerConfig()
})

async function copy() {
  await navigator.clipboard.writeText(scriptPath.value)
  copied.value = true
  setTimeout(() => {
    copied.value = false
  }, 2000)
}
</script>

<template>
  <div v-if="scriptPath" class="mt-auto pt-6 flex items-center gap-2 text-[11px] text-fg-faint">
    <span class="whitespace-nowrap">Channel command:</span>
    <button
      type="button"
      data-testid="channel-script-path"
      class="font-mono text-[11px] text-fg-mute bg-raised px-2 py-0.5 rounded cursor-pointer select-all transition-colors hover:text-accent focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-card"
      :title="copied ? 'Copied!' : 'Click to copy'"
      :aria-label="`Copy channel command ${scriptPath}`"
      @click="copy"
    >
      {{ scriptPath }}
    </button>
    <span v-if="copied" class="text-green-600 dark:text-green-400">Copied!</span>
  </div>
</template>
