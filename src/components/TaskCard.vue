<script setup lang="ts">
import type { PipelineTask } from '../types'

defineProps<{ task: PipelineTask }>()
defineEmits<{ select: [task: PipelineTask] }>()

function shortDate(iso: string): string {
  return new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}
</script>

<template>
  <div
    class="task-card"
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
    <div class="task-meta">
      <span v-if="task.worktreePath" class="meta-chip" title="Has worktree">WT</span>
      <span v-if="task.sourceBranch" class="meta-chip">{{ task.sourceBranch }}</span>
      <span v-if="task.parentTaskId" class="meta-chip" title="Follow-up task">↳</span>
      <span v-if="task.currentStage === 'umsetzung'" class="meta-chip iter">
        iter {{ task.maxIterations }}
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
</style>
