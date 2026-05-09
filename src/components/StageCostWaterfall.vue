<script setup lang="ts">
import type { PipelineStage } from '../types'
import { computed } from 'vue'
import { formatCost } from '../utils/format'

export interface StageCostRow {
  stage: PipelineStage
  iteration: number
  costCents: number
  tokensUsed: number
  startedAt: string | null
  endedAt: string | null
}

const props = defineProps<{ rows: StageCostRow[] }>()

function centsToUsd(cents: number): number {
  return cents / 100
}

const totalCostUsd = computed(() =>
  props.rows.reduce((sum, r) => sum + centsToUsd(r.costCents), 0),
)

const totalTokens = computed(() =>
  props.rows.reduce((sum, r) => sum + r.tokensUsed, 0),
)

function formatTokensCompact(n: number): string {
  if (n === 0)
    return '—'
  if (n < 1000)
    return String(n)
  if (n < 1_000_000)
    return `${(n / 1000).toFixed(1)}k`
  return `${(n / 1_000_000).toFixed(2)}M`
}

function stageDurationMs(row: StageCostRow): number | null {
  if (!row.startedAt || !row.endedAt)
    return null
  return new Date(row.endedAt).getTime() - new Date(row.startedAt).getTime()
}

function formatDuration(ms: number | null): string {
  if (ms === null)
    return '—'
  const s = Math.round(ms / 1000)
  if (s < 60)
    return `${s}s`
  return `${Math.floor(s / 60)}m ${s % 60}s`
}
</script>

<template>
  <div v-if="rows.length === 0" class="text-sm text-slate-400 dark:text-slate-600 italic">
    No completed stages yet.
  </div>
  <table v-else class="w-full text-[13px] border-collapse">
    <thead>
      <tr class="text-left text-slate-500 dark:text-slate-400 border-b border-slate-200 dark:border-slate-700">
        <th class="pb-1 font-medium">Stage</th>
        <th class="pb-1 font-medium text-center">Iter</th>
        <th class="pb-1 font-medium text-right">Tokens</th>
        <th class="pb-1 font-medium text-right">Cost</th>
        <th class="pb-1 font-medium text-right">Duration</th>
      </tr>
    </thead>
    <tbody>
      <tr
        v-for="(row, i) in rows"
        :key="i"
        class="border-b border-slate-100 dark:border-slate-800 hover:bg-slate-50 dark:hover:bg-slate-800/50"
      >
        <td class="py-1 pr-2 text-slate-700 dark:text-slate-300 capitalize">{{ row.stage.replace('_', ' ') }}</td>
        <td class="py-1 text-center text-slate-500 dark:text-slate-400">{{ row.iteration + 1 }}</td>
        <td class="py-1 text-right font-mono text-slate-700 dark:text-slate-300">{{ formatTokensCompact(row.tokensUsed) }}</td>
        <td class="py-1 text-right font-mono text-slate-700 dark:text-slate-300">{{ formatCost(centsToUsd(row.costCents)) }}</td>
        <td class="py-1 text-right font-mono text-slate-500 dark:text-slate-400">{{ formatDuration(stageDurationMs(row)) }}</td>
      </tr>
    </tbody>
    <tfoot>
      <tr class="border-t border-slate-300 dark:border-slate-600 font-medium">
        <td class="pt-1 text-slate-700 dark:text-slate-300">Total</td>
        <td />
        <td class="pt-1 text-right font-mono text-slate-900 dark:text-slate-100">{{ formatTokensCompact(totalTokens) }}</td>
        <td class="pt-1 text-right font-mono text-slate-900 dark:text-slate-100">{{ formatCost(totalCostUsd) }}</td>
        <td />
      </tr>
    </tfoot>
  </table>
</template>
