<script setup lang="ts">
import { computed } from 'vue'
import type { Provider } from '../sdk.generated'
import { ProviderClaude, ProviderCodex, ProviderGemini } from '../sdk.generated'

const props = defineProps<{ provider?: Provider | null }>()

interface ProviderMeta {
  emoji: string
  label: string
  // Tailwind color classes for the border + text.
  classes: string
}

// Returns a meta object for non-Claude providers, or null to suppress the
// badge entirely (Claude is the default and would just add clutter).
const meta = computed<ProviderMeta | null>(() => {
  switch (props.provider) {
    case ProviderCodex:
      return { emoji: 'O', label: 'Codex', classes: 'text-purple-600 dark:text-purple-400 border-purple-600 dark:border-purple-400' }
    case ProviderGemini:
      return { emoji: 'G', label: 'Gemini', classes: 'text-amber-600 dark:text-amber-400 border-amber-600 dark:border-amber-400' }
    case ProviderClaude:
    default:
      return null
  }
})
</script>

<template>
  <span
    v-if="meta"
    :title="`Provider: ${meta.label}`"
    class="inline-block ml-1.5 px-1 text-[9px] font-semibold border rounded align-middle tracking-wider"
    :class="meta.classes"
  >{{ meta.emoji }}</span>
</template>
