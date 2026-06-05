<script setup lang="ts">
import { onMounted, ref } from 'vue'

const scriptPath = ref('')
const copied = ref(false)

onMounted(async () => {
  try {
    const res = await fetch('/api/config')
    if (res.ok) {
      const data = await res.json()
      scriptPath.value = data.scriptPath ?? ''
    }
  }
  catch {
    // ignore — callout simply renders nothing
  }
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
  <div v-if="scriptPath" class="mt-6 flex items-center gap-2 text-[11px] text-fg-faint">
    <span class="whitespace-nowrap">Channel command:</span>
    <code
      data-testid="channel-script-path"
      class="font-mono text-[11px] text-fg-mute bg-raised px-2 py-0.5 rounded cursor-pointer select-all transition-colors hover:text-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-card"
      tabindex="0"
      role="button"
      :title="copied ? 'Copied!' : 'Click to copy'"
      @click="copy"
      @keydown.enter="copy"
      @keydown.space.prevent="copy"
    >{{ scriptPath }}</code>
    <span v-if="copied" class="text-green-600 dark:text-green-400">Copied!</span>
  </div>
</template>
