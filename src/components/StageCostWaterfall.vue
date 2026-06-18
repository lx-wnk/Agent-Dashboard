<script setup lang="ts">
import type { PipelineStage } from '../types'
import { STAGE_LABELS } from '../utils/stageLabels'
import { computed } from 'vue'
import { formatCost, formatTokens } from '../utils/format'

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
  <div v-if="rows.length === 0" class="text-sm text-fg-mute italic">
    No completed stages yet.
  </div>
  <table v-else class="w-full text-[13px] border-collapse">
    <thead>
      <tr class="text-left text-fg-mute border-b border-line">
        <th class="pb-1 font-medium">
          Stage
        </th>
        <th class="pb-1 font-medium text-center">
          Iter
        </th>
        <th class="pb-1 font-medium text-right">
          Tokens
        </th>
        <th class="pb-1 font-medium text-right">
          Cost
        </th>
        <th class="pb-1 font-medium text-right">
          Duration
        </th>
      </tr>
    </thead>
    <tbody>
      <tr
        v-for="(row, i) in rows"
        :key="i"
        class="border-b border-line hover:bg-raised"
      >
        <td class="py-1 pr-2 text-fg-soft">
          {{ STAGE_LABELS[row.stage as PipelineStage] ?? row.stage }}
        </td>
        <td class="py-1 text-center text-fg-mute">
          {{ row.iteration }}
        </td>
        <td class="py-1 text-right font-mono text-fg-soft">
          {{ formatTokens(row.tokensUsed) }}
        </td>
        <td class="py-1 text-right font-mono text-fg-soft">
          {{ formatCost(centsToUsd(row.costCents)) }}
        </td>
        <td class="py-1 text-right font-mono text-fg-mute">
          {{ formatDuration(stageDurationMs(row)) }}
        </td>
      </tr>
    </tbody>
    <tfoot>
      <tr class="border-t border-line-strong font-medium">
        <td class="pt-1 text-fg-soft">
          Total
        </td>
        <td />
        <td class="pt-1 text-right font-mono text-fg">
          {{ formatTokens(totalTokens) }}
        </td>
        <td class="pt-1 text-right font-mono text-fg">
          {{ formatCost(totalCostUsd) }}
        </td>
        <td />
      </tr>
    </tfoot>
  </table>
</template>
