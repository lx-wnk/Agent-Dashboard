<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { formatTokens } from '../utils/format'

interface TrendPoint {
  t: number
  cost: number
  tokens: number
}

const trend = ref<TrendPoint[]>([])

async function fetchTrend() {
  try {
    const res = await fetch('/api/trends')
    if (res.ok)
      trend.value = await res.json()
  }
  catch { /* ignore */ }
}

// Fetch historical trend data once on mount (no polling — server records trends via SSE broadcast)
onMounted(fetchTrend)

// Take last 60 points (3 minutes of data) for the sparkline
const sparkData = computed(() => {
  const points = trend.value.slice(-60)
  if (points.length < 2)
    return []
  return points
})

const maxCost = computed(() => {
  const costs = sparkData.value.map(p => p.cost)
  return Math.max(...costs, 0.01) // min 0.01 to avoid division by zero
})

const costDelta = computed(() => {
  const pts = trend.value
  if (pts.length < 2)
    return null
  const recent = pts[pts.length - 1].cost
  const older = pts[Math.max(0, pts.length - 61)].cost
  return recent - older
})

const tokenDelta = computed(() => {
  const pts = trend.value
  if (pts.length < 2)
    return null
  const recent = pts[pts.length - 1].tokens
  const older = pts[Math.max(0, pts.length - 61)].tokens
  return recent - older
})
</script>

<template>
  <div v-if="sparkData.length >= 2" class="cost-trend">
    <div class="trend-header">
      <span class="trend-label">Cost trend (3min)</span>
      <span v-if="costDelta !== null" class="trend-delta" :class="costDelta > 0 ? 'up' : 'flat'">
        {{ costDelta > 0 ? '+' : '' }}${{ costDelta.toFixed(2) }}
      </span>
      <span v-if="tokenDelta !== null && tokenDelta > 0" class="trend-delta tokens">
        +{{ formatTokens(tokenDelta) }} tok
      </span>
    </div>
    <div class="sparkline">
      <div
        v-for="(point, i) in sparkData"
        :key="i"
        class="spark-bar"
        :style="{ height: `${Math.max(2, (point.cost / maxCost) * 100)}%` }"
        :title="`$${point.cost.toFixed(2)}`"
      />
    </div>
  </div>
</template>

<style scoped>
.cost-trend {
  padding: 6px 24px;
  background: var(--bg-secondary);
  border-bottom: 1px solid var(--border);
}

.trend-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 4px;
}

.trend-label {
  font-size: 11px;
  color: var(--text-muted);
}

.trend-delta {
  font-size: 11px;
  font-family: var(--font-mono);
}

.trend-delta.up {
  color: var(--accent-red);
}

.trend-delta.flat {
  color: var(--text-muted);
}

.trend-delta.tokens {
  color: var(--accent-blue);
}

.sparkline {
  display: flex;
  align-items: flex-end;
  gap: 1px;
  height: 24px;
}

.spark-bar {
  flex: 1;
  min-width: 2px;
  background: var(--accent-green);
  border-radius: 1px 1px 0 0;
  opacity: 0.7;
  transition: opacity 0.15s;
}

.spark-bar:hover {
  opacity: 1;
}

.spark-bar:last-child {
  opacity: 1;
}
</style>
