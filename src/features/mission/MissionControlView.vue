<script setup lang="ts">
import { GitHubPanel, MemoryPanel } from '@/features/cockpit'
import KontorTile from './components/KontorTile.vue'
import LiveWorkWidget from './components/LiveWorkWidget.vue'
import NeedsYouQueue from './components/NeedsYouQueue.vue'
</script>

<template>
  <div class="flex min-h-0 flex-grow" data-testid="mission-control">
    <div class="w-[360px] shrink-0 border-r border-line">
      <LiveWorkWidget />
    </div>

    <div class="flex-grow min-w-0 flex flex-col gap-6 px-10 py-8">
      <NeedsYouQueue variant="docked" />
      <KontorTile class="flex-1" />
    </div>

    <div class="w-[360px] shrink-0 border-l border-line p-4 flex flex-col gap-4 overflow-y-auto">
      <GitHubPanel />
      <MemoryPanel />
    </div>
  </div>
</template>
