<script setup lang="ts">
defineProps<{
  expanded: boolean
  totalCostLabel: string
  totalTokensLabel: string
  quotaPct: number
  theme: 'dark' | 'light'
}>()
defineEmits<{
  'open-sessions': []
  'open-settings': []
  'toggle-theme': []
}>()
</script>

<template>
  <div class="mt-auto border-t border-line pt-2 px-1.5 flex flex-col gap-2">
    <div v-if="expanded" class="px-1">
      <div class="flex items-center justify-between text-[10px] text-fg-faint mb-1">
        <span>Quota</span><span>{{ quotaPct }}%</span>
      </div>
      <div
        class="h-1.5 bg-raised rounded-full overflow-hidden"
        role="progressbar"
        :aria-valuenow="quotaPct"
        aria-valuemin="0"
        aria-valuemax="100"
        :aria-label="`Monthly quota ${quotaPct}% used`"
      >
        <div
          class="h-full rounded-full transition-[width]"
          :class="quotaPct >= 90 ? 'bg-red-500' : quotaPct >= 75 ? 'bg-yellow-500' : 'bg-green-500'"
          :style="{ width: `${quotaPct}%` }"
        />
      </div>
      <div class="mt-2 text-[11px] font-mono text-fg-mute flex gap-2">
        <span>{{ totalCostLabel }}</span><span>·</span><span>{{ totalTokensLabel }} tok</span>
      </div>
    </div>
    <div class="flex items-center" :class="expanded ? 'gap-1' : 'flex-col gap-1'">
      <button
        type="button"
        data-testid="footer-sessions"
        class="flex items-center gap-2 rounded-lg px-2 min-h-[36px] text-[12px] text-fg-mute hover:text-fg hover:bg-raised transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-card"
        :class="expanded ? 'flex-1' : 'w-full justify-center'"
        :title="!expanded ? 'Sessions' : undefined"
        @click="$emit('open-sessions')"
      >
        <span aria-hidden="true">🕘</span><span v-if="expanded">Sessions</span><span v-else class="sr-only">Sessions</span>
      </button>
      <button
        type="button"
        data-testid="footer-settings"
        class="rounded-lg px-2 min-h-[36px] min-w-[36px] text-[14px] text-fg-mute hover:text-fg hover:bg-raised transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-card"
        aria-label="Settings"
        @click="$emit('open-settings')"
      >
        <span aria-hidden="true">⚙</span>
      </button>
      <button
        type="button"
        data-testid="footer-theme"
        class="rounded-lg px-2 min-h-[36px] min-w-[36px] text-[14px] text-fg-mute hover:text-fg hover:bg-raised transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-card"
        :aria-label="theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'"
        @click="$emit('toggle-theme')"
      >
        <span aria-hidden="true">{{ theme === 'dark' ? '☀' : '🌙' }}</span>
      </button>
    </div>
  </div>
</template>
