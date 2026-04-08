<template>
  <div class="resource-bar" v-if="info">
    <div class="res-item" :title="`CPU: ${info.cpu.usage}% (${info.cpu.cores} cores)`">
      <span class="res-label">CPU</span>
      <div class="res-gauge">
        <div class="res-fill" :class="levelClass(info.cpu.usage)" :style="{ width: info.cpu.usage + '%' }"></div>
      </div>
      <span class="res-pct">{{ Math.round(info.cpu.usage) }}%</span>
    </div>
    <div class="res-item" :title="`Memory: ${fmtBytes(info.memory.used)} / ${fmtBytes(info.memory.total)}`">
      <span class="res-label">MEM</span>
      <div class="res-gauge">
        <div class="res-fill" :class="levelClass(info.memory.usagePercent)" :style="{ width: info.memory.usagePercent + '%' }"></div>
      </div>
      <span class="res-pct">{{ Math.round(info.memory.usagePercent) }}%</span>
    </div>
    <div class="res-item" :title="`Disk: ${fmtBytes(info.disk.used)} / ${fmtBytes(info.disk.total)}`">
      <span class="res-label">DISK</span>
      <div class="res-gauge">
        <div class="res-fill" :class="levelClass(info.disk.usagePercent)" :style="{ width: info.disk.usagePercent + '%' }"></div>
      </div>
      <span class="res-pct">{{ Math.round(info.disk.usagePercent) }}%</span>
    </div>
    <div class="res-item load" :title="`Load: ${info.loadAvg.map(l => l.toFixed(2)).join(', ')}`">
      <span class="res-label">LOAD</span>
      <span class="res-value">{{ info.loadAvg[0].toFixed(1) }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'

interface SystemInfo {
  cpu: { usage: number; cores: number; model: string }
  memory: { total: number; used: number; available: number; usagePercent: number }
  disk: { total: number; used: number; available: number; usagePercent: number; mount: string }
  loadAvg: number[]
  uptime: number
}

const info = ref<SystemInfo | null>(null)
let timer: ReturnType<typeof setInterval> | null = null

function levelClass(pct: number): string {
  if (pct > 85) return 'critical'
  if (pct > 60) return 'warn'
  return 'ok'
}

function fmtBytes(bytes: number): string {
  if (bytes >= 1e12) return (bytes / 1e12).toFixed(1) + ' TB'
  if (bytes >= 1e9) return (bytes / 1e9).toFixed(1) + ' GB'
  return (bytes / 1e6).toFixed(0) + ' MB'
}

async function poll() {
  try {
    const res = await fetch('/api/system')
    if (res.ok) info.value = await res.json()
  } catch { /* ignore */ }
}

onMounted(() => {
  poll()
  timer = setInterval(poll, 5000)
})

onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>

<style scoped>
.resource-bar {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 6px 24px;
  background: var(--bg-secondary);
  border-bottom: 1px solid var(--border);
}

.res-item {
  display: flex;
  align-items: center;
  gap: 6px;
}

.res-label {
  font-size: 10px;
  font-weight: 600;
  color: var(--text-muted);
  letter-spacing: 0.5px;
  width: 32px;
}

.res-gauge {
  width: 60px;
  height: 6px;
  background: var(--bg-primary);
  border-radius: 3px;
  overflow: hidden;
}

.res-fill {
  height: 100%;
  border-radius: 3px;
  transition: width 0.5s ease;
}

.res-fill.ok { background: var(--accent-green); }
.res-fill.warn { background: var(--accent-yellow); }
.res-fill.critical { background: #f87171; }

.res-pct {
  font-size: 11px;
  font-family: monospace;
  color: var(--text-secondary);
  width: 28px;
  text-align: right;
}

.res-value {
  font-size: 11px;
  font-family: monospace;
  color: var(--text-secondary);
}
</style>
