<script setup lang="ts">
import type { DashboardLayout } from '../../composables/useViewState'

defineProps<{
  layout: DashboardLayout
  hideNonClaude: boolean
}>()
defineEmits<{
  'update:layout': [value: DashboardLayout]
  'update:hideNonClaude': [value: boolean]
}>()
</script>

<template>
  <div class="flex items-center gap-1 px-1 py-2">
    <div role="group" aria-label="Layout" class="flex bg-raised rounded-lg overflow-hidden p-0.5 gap-0.5">
      <button
        type="button"
        data-testid="layout-cards"
        class="px-2.5 py-1 text-xs rounded-md transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-1 focus-visible:ring-offset-raised"
        :class="layout === 'cards' ? 'bg-card text-fg shadow-sm' : 'text-fg-mute hover:text-fg'"
        :aria-pressed="layout === 'cards'"
        @click="$emit('update:layout', 'cards')"
      >
        ⊞ Cards
      </button>
      <button
        type="button"
        data-testid="layout-list"
        class="px-2.5 py-1 text-xs rounded-md transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-1 focus-visible:ring-offset-raised"
        :class="layout === 'list' ? 'bg-card text-fg shadow-sm' : 'text-fg-mute hover:text-fg'"
        :aria-pressed="layout === 'list'"
        @click="$emit('update:layout', 'list')"
      >
        ≡ List
      </button>
    </div>
    <button
      type="button"
      data-testid="claude-only"
      class="ml-2 border border-line px-2.5 py-1 text-xs rounded-lg transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-card"
      :class="hideNonClaude ? 'bg-accent text-white border-transparent' : 'text-fg-mute hover:text-fg'"
      :aria-pressed="hideNonClaude"
      @click="$emit('update:hideNonClaude', !hideNonClaude)"
    >
      Claude only
    </button>
  </div>
</template>
