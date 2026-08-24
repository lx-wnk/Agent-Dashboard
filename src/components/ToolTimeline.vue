<script setup lang="ts">
import type { RecentTool } from '@/sdk.generated'

defineProps<{ tools: RecentTool[] }>()
</script>

<template>
  <div v-if="tools.length > 0">
    <h4 class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-2">
      Recent Tools
    </h4>
    <ul class="flex flex-col gap-1">
      <li
        v-for="(tool, i) in tools"
        :key="`${i}-${tool.name}`"
        class="flex items-baseline gap-2 min-w-0 text-[11px] font-mono"
      >
        <span class="px-2 py-0.5 rounded bg-raised text-fg-soft flex-shrink-0">{{ tool.name }}</span>
        <span
          v-if="tool.detail"
          class="text-fg-mute break-all min-w-0"
          data-testid="tool-detail"
        >{{ tool.detail }}</span>
        <span
          v-if="tool.elided"
          class="text-fg-mute/70 flex-shrink-0"
          data-testid="tool-detail-elided"
        >+{{ tool.elided }} more</span>
      </li>
    </ul>
  </div>
</template>
