<script setup lang="ts">
import type { PipelineStage } from '../types'
import { computed } from 'vue'

const props = defineProps<{
  stage: PipelineStage
  output: unknown
}>()

interface PlanningSubtask { id?: string, title?: string, files?: string[] }
interface UmsetzungskonzeptStep { id?: string, description?: string, files?: string[] }
interface UmsetzungskonzeptToolRequest { tool?: string, pattern?: string, reason?: string }
interface SelbstreviewFinding { severity?: string, message?: string, file?: string }

function asRecord(o: unknown): Record<string, unknown> | null {
  return (o && typeof o === 'object' && !Array.isArray(o)) ? o as Record<string, unknown> : null
}

const record = computed(() => asRecord(props.output))

const pretty = computed(() => {
  const r = record.value
  if (!r)
    return null

  switch (props.stage) {
    case 'planning':
      return {
        kind: 'planning' as const,
        subtasks: (Array.isArray(r.subtasks) ? r.subtasks : []) as PlanningSubtask[],
        acceptanceCriteria: (Array.isArray(r.acceptanceCriteria) ? r.acceptanceCriteria : []) as string[],
      }
    case 'umsetzungskonzept':
      return {
        kind: 'umsetzungskonzept' as const,
        steps: (Array.isArray(r.steps) ? r.steps : []) as UmsetzungskonzeptStep[],
        toolRequests: (Array.isArray(r.toolRequests) ? r.toolRequests : []) as UmsetzungskonzeptToolRequest[],
      }
    case 'pruefung':
      return {
        kind: 'pruefung' as const,
        wellDefined: Boolean(r.wellDefined),
        complexity: String(r.complexity ?? ''),
        recommendation: String(r.recommendation ?? ''),
        risks: (Array.isArray(r.risks) ? r.risks : []) as string[],
        blockers: (Array.isArray(r.blockers) ? r.blockers : []) as string[],
      }
    case 'refinement':
      return {
        kind: 'refinement' as const,
        refinedTitle: String(r.refinedTitle ?? ''),
        refinedDescription: String(r.refinedDescription ?? ''),
        successCriteria: (Array.isArray(r.successCriteria) ? r.successCriteria : []) as string[],
      }
    case 'selbstreview':
      return {
        kind: 'selbstreview' as const,
        passed: Boolean(r.passed),
        summary: String(r.summary ?? ''),
        findings: (Array.isArray(r.findings) ? r.findings : []) as SelbstreviewFinding[],
      }
    case 'finalisierung':
      return {
        kind: 'finalisierung' as const,
        summary: String(r.summary ?? ''),
        insights: (Array.isArray(r.insights) ? r.insights : []) as string[],
        openTodos: (Array.isArray(r.openTodos) ? r.openTodos : []) as string[],
        testPlan: (Array.isArray(r.testPlan) ? r.testPlan : []) as string[],
      }
    default:
      return null
  }
})

function shortPath(full: string): string {
  const parts = full.split('/')
  if (parts.length <= 3)
    return full
  return `…/${parts.slice(-3).join('/')}`
}
</script>

<template>
  <div class="flex flex-col gap-3.5">
    <!-- Planning: subtasks + acceptance criteria -->
    <template v-if="pretty?.kind === 'planning'">
      <div v-if="pretty.subtasks.length > 0" class="mb-4 last:mb-0">
        <h4 class="text-[11px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-2">
          Subtasks <span class="font-normal">({{ pretty.subtasks.length }})</span>
        </h4>
        <ol class="list-none p-0 m-0 flex flex-col gap-2.5">
          <li v-for="(st, i) in pretty.subtasks" :key="st.id ?? i" class="bg-slate-50 dark:bg-slate-950 rounded p-2 px-2.5 border-l-2 border-blue-500 dark:border-blue-400">
            <div class="flex gap-2 items-baseline flex-wrap">
              <code v-if="st.id" class="font-mono text-[10px] bg-slate-100 dark:bg-slate-800 text-blue-600 dark:text-blue-400 px-1.5 py-px rounded font-semibold">{{ st.id }}</code>
              <span class="text-xs text-slate-900 dark:text-slate-100 leading-snug">{{ st.title ?? '(no title)' }}</span>
            </div>
            <ul v-if="st.files && st.files.length > 0" class="list-none p-0 pt-1.5 m-0 flex flex-wrap gap-1">
              <li v-for="f in st.files" :key="f" :title="f">
                <code class="font-mono text-[10px] bg-slate-100 dark:bg-slate-800 text-slate-400 dark:text-slate-600 px-1 py-px rounded-sm inline-block">{{ shortPath(f) }}</code>
              </li>
            </ul>
          </li>
        </ol>
      </div>
      <div v-if="pretty.acceptanceCriteria.length > 0" class="mb-4 last:mb-0">
        <h4 class="text-[11px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-2">
          Acceptance Criteria <span class="font-normal">({{ pretty.acceptanceCriteria.length }})</span>
        </h4>
        <ul class="list-none p-0 m-0 flex flex-col gap-1 text-xs text-slate-500 dark:text-slate-400">
          <li v-for="(ac, i) in pretty.acceptanceCriteria" :key="i" class="py-1 px-2 bg-slate-50 dark:bg-slate-950 rounded leading-relaxed">
            <span class="text-slate-400 dark:text-slate-600 mr-1">☐</span> {{ ac }}
          </li>
        </ul>
      </div>
    </template>

    <!-- Umsetzungskonzept: steps + toolRequests -->
    <template v-else-if="pretty?.kind === 'umsetzungskonzept'">
      <div v-if="pretty.steps.length > 0" class="mb-4 last:mb-0">
        <h4 class="text-[11px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-2">
          Steps <span class="font-normal">({{ pretty.steps.length }})</span>
        </h4>
        <ol class="list-none p-0 m-0 flex flex-col gap-2.5">
          <li v-for="(step, i) in pretty.steps" :key="step.id ?? i" class="bg-slate-50 dark:bg-slate-950 rounded p-2 px-2.5 border-l-2 border-blue-500 dark:border-blue-400">
            <div class="flex gap-2 items-baseline flex-wrap">
              <code v-if="step.id" class="font-mono text-[10px] bg-slate-100 dark:bg-slate-800 text-blue-600 dark:text-blue-400 px-1.5 py-px rounded font-semibold">{{ step.id }}</code>
              <span class="text-xs text-slate-900 dark:text-slate-100 leading-snug">{{ step.description ?? '(no description)' }}</span>
            </div>
            <ul v-if="step.files && step.files.length > 0" class="list-none p-0 pt-1.5 m-0 flex flex-wrap gap-1">
              <li v-for="f in step.files" :key="f" :title="f">
                <code class="font-mono text-[10px] bg-slate-100 dark:bg-slate-800 text-slate-400 dark:text-slate-600 px-1 py-px rounded-sm inline-block">{{ shortPath(f) }}</code>
              </li>
            </ul>
          </li>
        </ol>
      </div>
      <div v-if="pretty.toolRequests.length > 0" class="mb-4 last:mb-0">
        <h4 class="text-[11px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-2">
          Tool Requests <span class="font-normal">({{ pretty.toolRequests.length }})</span>
        </h4>
        <ul class="list-none p-0 m-0 flex flex-col gap-1 text-[11px]">
          <li v-for="(tr, i) in pretty.toolRequests" :key="i" class="py-1 px-2 bg-slate-50 dark:bg-slate-950 rounded">
            <code class="font-mono text-blue-600 dark:text-blue-400 font-semibold">{{ tr.tool }}<template v-if="tr.pattern">({{ tr.pattern }})</template></code>
            <span v-if="tr.reason" class="text-slate-400 dark:text-slate-600 ml-1">— {{ tr.reason }}</span>
          </li>
        </ul>
      </div>
    </template>

    <!-- Pruefung -->
    <template v-else-if="pretty?.kind === 'pruefung'">
      <dl class="grid gap-1 text-xs mb-1" style="grid-template-columns: auto 1fr; gap: 4px 12px">
        <div class="contents">
          <dt class="text-slate-400 dark:text-slate-600 uppercase text-[10px]">
            Well-defined
          </dt>
          <dd class="text-slate-900 dark:text-slate-100">
            {{ pretty.wellDefined ? '✓ yes' : '✗ no' }}
          </dd>
        </div>
        <div class="contents">
          <dt class="text-slate-400 dark:text-slate-600 uppercase text-[10px]">
            Complexity
          </dt>
          <dd class="text-slate-900 dark:text-slate-100">
            <code class="font-mono text-[10px] bg-slate-100 dark:bg-slate-800 text-blue-600 dark:text-blue-400 px-1.5 py-px rounded font-semibold">{{ pretty.complexity }}</code>
          </dd>
        </div>
        <div class="contents">
          <dt class="text-slate-400 dark:text-slate-600 uppercase text-[10px]">
            Recommendation
          </dt>
          <dd class="text-slate-900 dark:text-slate-100">
            <code class="font-mono text-[10px] bg-slate-100 dark:bg-slate-800 text-blue-600 dark:text-blue-400 px-1.5 py-px rounded font-semibold">{{ pretty.recommendation }}</code>
          </dd>
        </div>
      </dl>
      <div v-if="pretty.risks.length > 0" class="mb-4 last:mb-0">
        <h4 class="text-[11px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-2">
          Risks
        </h4>
        <ul class="list-none p-0 m-0 flex flex-col gap-1 text-xs text-slate-500 dark:text-slate-400">
          <li v-for="(r, i) in pretty.risks" :key="i" class="py-1 px-2 bg-slate-50 dark:bg-slate-950 rounded leading-relaxed">
            ⚠ {{ r }}
          </li>
        </ul>
      </div>
      <div v-if="pretty.blockers.length > 0" class="mb-4 last:mb-0">
        <h4 class="text-[11px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-2">
          Blockers
        </h4>
        <ul class="list-none p-0 m-0 flex flex-col gap-1 text-xs text-slate-500 dark:text-slate-400">
          <li v-for="(b, i) in pretty.blockers" :key="i" class="py-1 px-2 bg-slate-50 dark:bg-slate-950 rounded leading-relaxed">
            ⛔ {{ b }}
          </li>
        </ul>
      </div>
    </template>

    <!-- Refinement -->
    <template v-else-if="pretty?.kind === 'refinement'">
      <div class="mb-4 last:mb-0">
        <h4 class="text-[11px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-2">
          Refined Title
        </h4>
        <p class="text-xs leading-relaxed text-slate-500 dark:text-slate-400 bg-slate-50 dark:bg-slate-950 px-2.5 py-2 rounded">
          {{ pretty.refinedTitle }}
        </p>
      </div>
      <div class="mb-4 last:mb-0">
        <h4 class="text-[11px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-2">
          Refined Description
        </h4>
        <p class="text-xs leading-relaxed text-slate-500 dark:text-slate-400 bg-slate-50 dark:bg-slate-950 px-2.5 py-2 rounded">
          {{ pretty.refinedDescription }}
        </p>
      </div>
      <div v-if="pretty.successCriteria.length > 0" class="mb-4 last:mb-0">
        <h4 class="text-[11px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-2">
          Success Criteria <span class="font-normal">({{ pretty.successCriteria.length }})</span>
        </h4>
        <ul class="list-none p-0 m-0 flex flex-col gap-1 text-xs text-slate-500 dark:text-slate-400">
          <li v-for="(sc, i) in pretty.successCriteria" :key="i" class="py-1 px-2 bg-slate-50 dark:bg-slate-950 rounded leading-relaxed">
            <span class="text-slate-400 dark:text-slate-600 mr-1">☐</span> {{ sc }}
          </li>
        </ul>
      </div>
    </template>

    <!-- Selbstreview -->
    <template v-else-if="pretty?.kind === 'selbstreview'">
      <dl class="grid gap-1 text-xs mb-1" style="grid-template-columns: auto 1fr; gap: 4px 12px">
        <div class="contents">
          <dt class="text-slate-400 dark:text-slate-600 uppercase text-[10px]">
            Passed
          </dt>
          <dd class="text-slate-900 dark:text-slate-100">
            <code
              class="font-mono text-[10px] px-1.5 py-px rounded font-semibold"
              :class="pretty.passed
                ? 'bg-green-50 dark:bg-green-950/50 text-green-600 dark:text-green-400'
                : 'bg-red-50 dark:bg-red-950/50 text-red-600 dark:text-red-400'"
            >
              {{ pretty.passed ? '✓ PASSED' : '✗ FAILED' }}
            </code>
          </dd>
        </div>
      </dl>
      <div class="mb-4 last:mb-0">
        <h4 class="text-[11px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-2">
          Summary
        </h4>
        <p class="text-xs leading-relaxed text-slate-500 dark:text-slate-400 bg-slate-50 dark:bg-slate-950 px-2.5 py-2 rounded">
          {{ pretty.summary }}
        </p>
      </div>
      <div v-if="pretty.findings.length > 0" class="mb-4 last:mb-0">
        <h4 class="text-[11px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-2">
          Findings <span class="font-normal">({{ pretty.findings.length }})</span>
        </h4>
        <ul class="list-none p-0 m-0 flex flex-col gap-1 text-xs text-slate-500 dark:text-slate-400">
          <li v-for="(f, i) in pretty.findings" :key="i" class="py-1 px-2 bg-slate-50 dark:bg-slate-950 rounded leading-relaxed">
            <code v-if="f.severity" class="font-mono text-[10px] bg-slate-100 dark:bg-slate-800 text-blue-600 dark:text-blue-400 px-1.5 py-px rounded font-semibold">{{ f.severity }}</code>
            {{ f.message ?? '' }}
            <code v-if="f.file" class="font-mono text-[10px] text-slate-400 dark:text-slate-600 ml-1">{{ shortPath(f.file) }}</code>
          </li>
        </ul>
      </div>
    </template>

    <!-- Finalisierung -->
    <template v-else-if="pretty?.kind === 'finalisierung'">
      <div class="mb-4 last:mb-0">
        <h4 class="text-[11px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-2">
          Summary
        </h4>
        <p class="text-xs leading-relaxed text-slate-500 dark:text-slate-400 bg-slate-50 dark:bg-slate-950 px-2.5 py-2 rounded">
          {{ pretty.summary }}
        </p>
      </div>
      <div v-if="pretty.insights.length > 0" class="mb-4 last:mb-0">
        <h4 class="text-[11px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-2">
          Insights
        </h4>
        <ul class="list-none p-0 m-0 flex flex-col gap-1 text-xs text-slate-500 dark:text-slate-400">
          <li v-for="(ins, i) in pretty.insights" :key="i" class="py-1 px-2 bg-slate-50 dark:bg-slate-950 rounded leading-relaxed">
            ★ {{ ins }}
          </li>
        </ul>
      </div>
      <div v-if="pretty.openTodos.length > 0" class="mb-4 last:mb-0">
        <h4 class="text-[11px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-2">
          Open Todos
        </h4>
        <ul class="list-none p-0 m-0 flex flex-col gap-1 text-xs text-slate-500 dark:text-slate-400">
          <li v-for="(t, i) in pretty.openTodos" :key="i" class="py-1 px-2 bg-slate-50 dark:bg-slate-950 rounded leading-relaxed">
            <span class="text-slate-400 dark:text-slate-600 mr-1">☐</span> {{ t }}
          </li>
        </ul>
      </div>
      <div v-if="pretty.testPlan.length > 0" class="mb-4 last:mb-0">
        <h4 class="text-[11px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-2">
          Test Plan
        </h4>
        <ul class="list-none p-0 m-0 flex flex-col gap-1 text-xs text-slate-500 dark:text-slate-400">
          <li v-for="(t, i) in pretty.testPlan" :key="i" class="py-1 px-2 bg-slate-50 dark:bg-slate-950 rounded leading-relaxed">
            ✓ {{ t }}
          </li>
        </ul>
      </div>
    </template>

    <!-- Fallback: raw JSON for unknown/malformed shapes -->
    <details v-else>
      <summary class="cursor-pointer text-slate-400 dark:text-slate-600 text-[11px]">
        Raw output
      </summary>
      <pre class="bg-slate-100 dark:bg-slate-800 p-2 rounded text-[11px] max-h-60 overflow-auto mt-1.5">{{ JSON.stringify(output, null, 2) }}</pre>
    </details>
  </div>
</template>
