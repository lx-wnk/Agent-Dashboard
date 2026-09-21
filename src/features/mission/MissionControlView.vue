<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { usePendingPermissions } from '@/composables/usePendingPermissions'
import { useAgents } from '@/features/agents'
import { GitHubPanel, MemoryPanel } from '@/features/cockpit'
import { useTasks } from '@/features/pipeline'
import KontorTile from './components/KontorTile.vue'
import LiveWorkWidget from './components/LiveWorkWidget.vue'
import NextThing from './components/NextThing.vue'
import { rankNextThings } from './composables/useNextThing'

const emit = defineEmits<{ openTask: [taskId: string] }>()

const { tasks, refetch } = useTasks()
const { items: pending, refresh: refreshPending } = usePendingPermissions(tasks)
// autoStart: false — App.vue owns the stream. This view reads the same shared
// refs without touching the subscriber count it never raised.
const { agents } = useAgents({ autoStart: false })

onMounted(refetch)

const ranked = computed(() => rankNextThings(pending.value, tasks.value, agents.value))
const next = computed(() => ranked.value[0] ?? null)
const remaining = computed(() => Math.max(0, ranked.value.length - 1))
</script>

<template>
  <div class="flex min-h-0 flex-grow" data-testid="mission-control">
    <div class="w-[360px] shrink-0 border-r border-line">
      <LiveWorkWidget />
    </div>

    <div class="flex-grow min-w-0 flex flex-col gap-6 px-10 py-8">
      <NextThing
        :next="next"
        :remaining="remaining"
        @resolved="refreshPending"
        @open="(id) => emit('openTask', id)"
      />
      <KontorTile class="flex-1" />
    </div>

    <div class="w-[360px] shrink-0 border-l border-line p-4 flex flex-col gap-4 overflow-y-auto">
      <GitHubPanel />
      <MemoryPanel />
    </div>
  </div>
</template>
