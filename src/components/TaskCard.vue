<script setup lang="ts">
import type { PipelineStage, PipelineTask, StageRunStatus } from '../types'

defineProps<{ task: PipelineTask }>()
const emit = defineEmits<{ select: [task: PipelineTask], openChat: [task: PipelineTask] }>()

function shortDate(iso: string): string {
  return new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}

const RUN_STATUS_LABELS: Record<StageRunStatus, string> = {
  pending: 'Pending',
  running: 'Running',
  awaiting_user: 'Waiting',
  on_hold: 'On Hold',
  done: 'Done',
  failed: 'Failed',
}

function runStatusLabel(status: StageRunStatus): string {
  return RUN_STATUS_LABELS[status] ?? status
}

const STAGE_LABELS: Record<PipelineStage, string> = {
  konzept: 'Konzept',
  backlog: 'Backlog',
  umsetzung: 'Umsetzung',
  selbstreview: 'Selbstreview',
  finalisierung: 'Finalisierung',
  done: 'Done',
  on_hold: 'Permission',
  cancelled: 'Cancelled',
}

function stageLabel(stage: PipelineStage): string {
  return STAGE_LABELS[stage] || stage
}
</script>

<template>
  <div
    class="task-card"
    :class="{ 'is-blocked': task.isBlocked }"
    tabindex="0"
    role="button"
    :aria-label="`Open task ${task.title}`"
    @click="$emit('select', task)"
    @keydown.enter="$emit('select', task)"
    @keydown.space.prevent="$emit('select', task)"
  >
    <div class="task-card-head">
      <span class="task-slug">{{ task.slug }}</span>
      <span class="task-date">{{ shortDate(task.createdAt) }}</span>
    </div>
    <div class="task-title">
      {{ task.title }}
    </div>
    <div v-if="task.description" class="task-desc">
      {{ task.description }}
    </div>
    <button
      v-if="task.currentStage === 'konzept'"
      class="btn-resume-chat"
      @click.stop="emit('openChat', task)"
    >
      Chat fortsetzen →
    </button>
    <div class="task-meta">
      <span class="meta-chip stage" :class="`stage-${task.currentStage}`">
        {{ stageLabel(task.currentStage) }}
      </span>
      <span
        v-if="task.latestStageRunStatus"
        class="meta-chip run-status"
        :class="`run-${task.latestStageRunStatus}`"
        :title="`Latest stage run: ${runStatusLabel(task.latestStageRunStatus)}`"
      >
        {{ runStatusLabel(task.latestStageRunStatus) }}
      </span>
      <span v-if="task.worktreePath" class="meta-chip" title="Has worktree">WT</span>
      <span v-if="task.sourceBranch" class="meta-chip">{{ task.sourceBranch }}</span>
      <span v-if="task.parentTaskId" class="meta-chip" title="Follow-up task">↳</span>
      <span v-if="task.isUnsatisfiable" class="meta-chip unsatisfiable" title="Dependency can never be fulfilled — prerequisite reached wrong terminal stage. Remove dependency manually.">⚠ Unsatisfiable dep</span>
      <span v-else-if="task.isBlocked" class="meta-chip blocked" title="Waiting for prerequisite tasks">🔒 Blocked</span>
      <span v-if="task.currentStage === 'umsetzung'" class="meta-chip iter">
        max iter {{ task.maxIterations }}
      </span>
    </div>
  </div>
</template>

<style scoped>
.task-card {
  background: var(--bg-primary);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 10px 12px;
  cursor: pointer;
  transition: border-color 0.15s, transform 0.15s;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.task-card:hover {
  border-color: var(--accent-blue);
  transform: translateY(-1px);
}
.task-card:focus-visible {
  outline: 2px solid var(--accent-blue);
  outline-offset: 2px;
}
.task-card-head {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  gap: 8px;
}
.task-slug {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--accent-blue);
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.task-date {
  font-size: 10px;
  color: var(--text-muted);
}
.task-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary);
  line-height: 1.35;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.task-desc {
  font-size: 11px;
  color: var(--text-muted);
  line-height: 1.4;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.task-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
.meta-chip {
  font-size: 10px;
  color: var(--text-muted);
  background: var(--bg-tertiary);
  padding: 1px 6px;
  border-radius: 4px;
  font-family: var(--font-mono);
}
.meta-chip.iter {
  color: var(--accent-yellow);
}
.meta-chip.stage {
  background: var(--bg-secondary);
  color: var(--text-secondary);
  border: 1px solid var(--border);
}
.meta-chip.stage.stage-on_hold,
.meta-chip.stage.stage-approval1,
.meta-chip.stage.stage-approval2 {
  background: rgba(234, 179, 8, 0.15);
  color: rgb(234, 179, 8);
  border-color: rgba(234, 179, 8, 0.4);
}
.meta-chip.stage.stage-umsetzung {
  background: rgba(59, 130, 246, 0.15);
  color: var(--accent-blue);
  border-color: var(--accent-blue);
}
.meta-chip.stage.stage-done {
  background: rgba(74, 222, 128, 0.15);
  color: var(--accent-green);
  border-color: var(--accent-green);
}
.meta-chip.stage.stage-cancelled {
  background: rgba(248, 113, 113, 0.15);
  color: var(--accent-red);
  border-color: var(--accent-red);
}
.meta-chip.run-status {
  text-transform: uppercase;
  letter-spacing: 0.3px;
  font-weight: 700;
  border: 1px solid var(--border);
}
.meta-chip.run-status.run-running {
  background: rgba(59, 130, 246, 0.18);
  color: var(--accent-blue);
  border-color: rgba(59, 130, 246, 0.5);
}
.meta-chip.run-status.run-pending {
  background: var(--bg-tertiary);
  color: var(--text-muted);
}
.meta-chip.run-status.run-awaiting_user,
.meta-chip.run-status.run-on_hold {
  background: rgba(234, 179, 8, 0.18);
  color: rgb(234, 179, 8);
  border-color: rgba(234, 179, 8, 0.5);
}
.meta-chip.run-status.run-done {
  background: rgba(74, 222, 128, 0.15);
  color: var(--accent-green);
  border-color: rgba(74, 222, 128, 0.5);
}
.meta-chip.run-status.run-failed {
  background: rgba(248, 113, 113, 0.18);
  color: var(--accent-red);
  border-color: rgba(248, 113, 113, 0.5);
}
.meta-chip.blocked {
  background: rgba(148, 163, 184, 0.15);
  color: var(--text-muted);
  border: 1px solid rgba(148, 163, 184, 0.3);
}
.meta-chip.unsatisfiable {
  background: rgba(251, 191, 36, 0.12);
  color: var(--accent-yellow);
  border: 1px solid rgba(251, 191, 36, 0.35);
}
.task-card.is-blocked {
  opacity: 0.6;
}
.task-card.is-blocked:hover {
  opacity: 0.85;
  border-color: var(--border);
  transform: none;
}
.btn-resume-chat {
  align-self: flex-start;
  background: rgba(59, 130, 246, 0.12);
  color: var(--accent-blue);
  border: 1px solid rgba(59, 130, 246, 0.35);
  border-radius: 4px;
  padding: 3px 8px;
  font-size: 11px;
  font-weight: 600;
  cursor: pointer;
  font-family: inherit;
  transition: background 0.15s, border-color 0.15s;
}
.btn-resume-chat:hover {
  background: rgba(59, 130, 246, 0.2);
  border-color: var(--accent-blue);
}
.btn-resume-chat:focus-visible {
  outline: 2px solid var(--accent-blue);
  outline-offset: 2px;
}
</style>
