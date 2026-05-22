<script setup lang="ts">
import { onUnmounted, ref } from 'vue'
import { useVisibilityPolling } from '../composables/useVisibilityPolling'

interface SystemInfo {
  cpu: { usage: number, cores: number, model: string }
  memory: { total: number, used: number, available: number, usagePercent: number }
  disk: { total: number, used: number, available: number, usagePercent: number, mount: string }
  loadAvg: number[]
  uptime: number
}

const info = ref<SystemInfo | null>(null)

function fmtBytes(bytes: number): string {
  if (bytes >= 1e12)
    return `${(bytes / 1e12).toFixed(1)} TB`
  if (bytes >= 1e9)
    return `${(bytes / 1e9).toFixed(1)} GB`
  return `${(bytes / 1e6).toFixed(0)} MB`
}

const abortCtrl = new AbortController()
onUnmounted(() => abortCtrl.abort())

async function poll() {
  try {
    const res = await fetch('/api/system', { signal: abortCtrl.signal })
    if (res.ok && !abortCtrl.signal.aborted)
      info.value = await res.json()
  }
  catch { /* ignore */ }
}

useVisibilityPolling(poll, 15000)
</script>

<template>
  <div v-if="info" class="flex items-center gap-4 px-6 py-1.5 bg-card border-b border-line">
    <div class="flex items-center gap-1.5" :title="`CPU: ${info.cpu.usage}% (${info.cpu.cores} cores)`">
      <span class="text-[10px] font-semibold tracking-wider text-fg-mute w-8">CPU</span>
      <div class="w-15 h-1.5 bg-app rounded-full overflow-hidden">
        <div
          class="h-full rounded-full transition-[width] duration-500"
          :class="info.cpu.usage > 85 ? 'bg-red-600 dark:bg-red-400' : info.cpu.usage > 60 ? 'bg-yellow-600 dark:bg-yellow-400' : 'bg-green-600 dark:bg-green-400'"
          :style="{ width: `${info.cpu.usage}%` }"
        />
      </div>
      <span class="text-[11px] font-mono text-fg-mute w-7 text-right">{{ Math.round(info.cpu.usage) }}%</span>
    </div>
    <div class="flex items-center gap-1.5" :title="`Memory: ${fmtBytes(info.memory.used)} / ${fmtBytes(info.memory.total)}`">
      <span class="text-[10px] font-semibold tracking-wider text-fg-mute w-8">MEM</span>
      <div class="w-15 h-1.5 bg-app rounded-full overflow-hidden">
        <div
          class="h-full rounded-full transition-[width] duration-500"
          :class="info.memory.usagePercent > 85 ? 'bg-red-600 dark:bg-red-400' : info.memory.usagePercent > 60 ? 'bg-yellow-600 dark:bg-yellow-400' : 'bg-green-600 dark:bg-green-400'"
          :style="{ width: `${info.memory.usagePercent}%` }"
        />
      </div>
      <span class="text-[11px] font-mono text-fg-mute w-7 text-right">{{ Math.round(info.memory.usagePercent) }}%</span>
    </div>
    <div class="flex items-center gap-1.5" :title="`Disk: ${fmtBytes(info.disk.used)} / ${fmtBytes(info.disk.total)}`">
      <span class="text-[10px] font-semibold tracking-wider text-fg-mute w-8">DISK</span>
      <div class="w-15 h-1.5 bg-app rounded-full overflow-hidden">
        <div
          class="h-full rounded-full transition-[width] duration-500"
          :class="info.disk.usagePercent > 85 ? 'bg-red-600 dark:bg-red-400' : info.disk.usagePercent > 60 ? 'bg-yellow-600 dark:bg-yellow-400' : 'bg-green-600 dark:bg-green-400'"
          :style="{ width: `${info.disk.usagePercent}%` }"
        />
      </div>
      <span class="text-[11px] font-mono text-fg-mute w-7 text-right">{{ Math.round(info.disk.usagePercent) }}%</span>
    </div>
    <div class="flex items-center gap-1.5" :title="`Load: ${info.loadAvg.map(l => l.toFixed(2)).join(', ')}`">
      <span class="text-[10px] font-semibold tracking-wider text-fg-mute w-8">LOAD</span>
      <span class="text-[11px] font-mono text-fg-mute">{{ info.loadAvg[0].toFixed(1) }}</span>
    </div>
  </div>
</template>
