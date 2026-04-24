<script setup lang="ts">
import type { PipelineStage } from '../types'
import { computed } from 'vue'

const props = defineProps<{
  stage: PipelineStage
  output: unknown
}>()

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
  <div class="stage-output-view">
    <!-- Selbstreview -->
    <template v-if="pretty?.kind === 'selbstreview'">
      <dl class="kv">
        <div>
          <dt>Passed</dt>
          <dd>
            <code class="id-pill" :class="pretty.passed ? 'ok' : 'fail'">
              {{ pretty.passed ? '✓ PASSED' : '✗ FAILED' }}
            </code>
          </dd>
        </div>
      </dl>
      <div class="section">
        <h4>Summary</h4>
        <p class="prose">
          {{ pretty.summary }}
        </p>
      </div>
      <div v-if="pretty.findings.length > 0" class="section">
        <h4>Findings <span class="count">({{ pretty.findings.length }})</span></h4>
        <ul class="checklist">
          <li v-for="(f, i) in pretty.findings" :key="i">
            <code v-if="f.severity" class="id-pill small">{{ f.severity }}</code>
            {{ f.message ?? '' }}
            <code v-if="f.file" class="file-ref">{{ shortPath(f.file) }}</code>
          </li>
        </ul>
      </div>
    </template>

    <!-- Finalisierung -->
    <template v-else-if="pretty?.kind === 'finalisierung'">
      <div class="section">
        <h4>Summary</h4>
        <p class="prose">
          {{ pretty.summary }}
        </p>
      </div>
      <div v-if="pretty.insights.length > 0" class="section">
        <h4>Insights</h4>
        <ul class="checklist">
          <li v-for="(ins, i) in pretty.insights" :key="i">
            ★ {{ ins }}
          </li>
        </ul>
      </div>
      <div v-if="pretty.openTodos.length > 0" class="section">
        <h4>Open Todos</h4>
        <ul class="checklist">
          <li v-for="(t, i) in pretty.openTodos" :key="i">
            <span class="check">☐</span> {{ t }}
          </li>
        </ul>
      </div>
      <div v-if="pretty.testPlan.length > 0" class="section">
        <h4>Test Plan</h4>
        <ul class="checklist">
          <li v-for="(t, i) in pretty.testPlan" :key="i">
            ✓ {{ t }}
          </li>
        </ul>
      </div>
    </template>

    <!-- Fallback: raw JSON for unknown/malformed shapes -->
    <details v-else class="raw-fallback">
      <summary>Raw output</summary>
      <pre>{{ JSON.stringify(output, null, 2) }}</pre>
    </details>
  </div>
</template>

<style scoped>
.stage-output-view {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.section h4 {
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  color: var(--text-muted);
  margin-bottom: 8px;
}
.section h4 .count {
  color: var(--text-muted);
  font-weight: 400;
}
.subtask-list {
  list-style: none;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
  counter-reset: item;
}
.subtask {
  background: var(--bg-primary);
  border-radius: 4px;
  padding: 8px 10px;
  border-left: 2px solid var(--accent-blue);
}
.subtask-head {
  display: flex;
  gap: 8px;
  align-items: baseline;
  flex-wrap: wrap;
}
.id-pill {
  font-family: var(--font-mono);
  font-size: 10px;
  background: var(--bg-tertiary);
  color: var(--accent-blue);
  padding: 1px 6px;
  border-radius: 3px;
  font-weight: 600;
}
.id-pill.small {
  font-size: 9px;
}
.id-pill.ok {
  background: rgba(74, 222, 128, 0.18);
  color: var(--accent-green);
}
.id-pill.fail {
  background: rgba(248, 113, 113, 0.18);
  color: var(--accent-red);
}
.subtask-title {
  font-size: 12px;
  color: var(--text-primary);
  line-height: 1.4;
}
.file-list {
  list-style: none;
  padding: 6px 0 0 0;
  margin: 0;
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
.file-list code {
  font-family: var(--font-mono);
  font-size: 10px;
  background: var(--bg-tertiary);
  color: var(--text-muted);
  padding: 1px 5px;
  border-radius: 3px;
  display: inline-block;
}
.checklist {
  list-style: none;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 12px;
  color: var(--text-secondary);
}
.checklist li {
  padding: 4px 8px;
  background: var(--bg-primary);
  border-radius: 4px;
  line-height: 1.5;
}
.check {
  color: var(--text-muted);
  margin-right: 4px;
}
.kv {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 4px 12px;
  font-size: 12px;
  margin-bottom: 4px;
}
.kv > div { display: contents; }
.kv dt { color: var(--text-muted); text-transform: uppercase; font-size: 10px; }
.kv dd { color: var(--text-primary); }
.prose {
  font-size: 12px;
  line-height: 1.5;
  color: var(--text-secondary);
  background: var(--bg-primary);
  padding: 8px 10px;
  border-radius: 4px;
}
.tool-list {
  list-style: none;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 11px;
}
.tool-list li {
  padding: 4px 8px;
  background: var(--bg-primary);
  border-radius: 4px;
}
.tool-name {
  font-family: var(--font-mono);
  color: var(--accent-blue);
  font-weight: 600;
}
.tool-reason {
  color: var(--text-muted);
  margin-left: 4px;
}
.file-ref {
  font-family: var(--font-mono);
  font-size: 10px;
  color: var(--text-muted);
  margin-left: 4px;
}
.raw-fallback summary {
  cursor: pointer;
  color: var(--text-muted);
  font-size: 11px;
}
.raw-fallback pre {
  background: var(--bg-tertiary);
  padding: 8px;
  border-radius: 4px;
  font-size: 11px;
  max-height: 240px;
  overflow: auto;
  margin-top: 6px;
}
</style>
