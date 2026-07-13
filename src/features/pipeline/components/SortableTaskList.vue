<script setup lang="ts">
import type { Agent, PipelineTask, Project } from '@/types'
import { useSortable } from '@vueuse/integrations/useSortable'
import { ref, watch } from 'vue'
import TaskCard from '@/features/pipeline/components/TaskCard.vue'
import { reorderTask } from '@/features/pipeline/composables/useTasks'

const props = withDefaults(defineProps<{
  tasks: PipelineTask[]
  projectById: Map<string, Project>
  workingAgentByTask?: Map<string, Agent>
  sortable?: boolean
}>(), {
  sortable: true,
})

const emit = defineEmits<{
  select: [task: PipelineTask]
  openChat: [task: PipelineTask]
  navigateAgent: [sessionId: string]
}>()

const listEl = ref<HTMLElement | null>(null)
// Local mirror that Sortable mutates on drop. Kept in sync with the parent's
// rank-sorted list; the optimistic rank update produces the same order, so the
// list resets cleanly when props flow back.
const list = ref<PipelineTask[]>([...props.tasks])
const isDragging = ref(false)
watch(() => props.tasks, (v) => {
  if (isDragging.value)
    return
  if (v.length === list.value.length && v.every((t, i) => t.id === list.value[i]?.id))
    return
  list.value = [...v]
})

function projectFor(task: PipelineTask): Project | null {
  return task.projectId ? props.projectById.get(task.projectId) ?? null : null
}

function resolveAdjacentIds(rows: PipelineTask[], index: number): { beforeId: string | null, afterId: string | null } {
  return {
    beforeId: index > 0 ? rows[index - 1].id : null,
    afterId: index < rows.length - 1 ? rows[index + 1].id : null,
  }
}

useSortable(listEl, list, {
  handle: '.task-drag-handle',
  animation: 150,
  disabled: !props.sortable,
  onStart() {
    isDragging.value = true
  },
  onEnd(evt: { oldIndex?: number, newIndex?: number }) {
    isDragging.value = false
    const { oldIndex, newIndex } = evt
    if (oldIndex == null || newIndex == null || oldIndex === newIndex)
      return
    // useSortable's internal onUpdate has already reordered `list` by now.
    const moved = list.value[newIndex]
    if (!moved)
      return
    const { beforeId, afterId } = resolveAdjacentIds(list.value, newIndex)
    void reorderTask(moved.id, beforeId, afterId)
  },
})

function moveTask(taskId: string, direction: 'up' | 'down'): void {
  const oldIndex = list.value.findIndex(t => t.id === taskId)
  if (oldIndex === -1)
    return
  const newIndex = direction === 'up' ? oldIndex - 1 : oldIndex + 1
  if (newIndex < 0 || newIndex >= list.value.length)
    return
  const reordered = [...list.value]
  const [moved] = reordered.splice(oldIndex, 1)
  reordered.splice(newIndex, 0, moved)
  list.value = reordered
  const { beforeId, afterId } = resolveAdjacentIds(reordered, newIndex)
  void reorderTask(taskId, beforeId, afterId)
}
</script>

<template>
  <div ref="listEl" class="flex flex-col gap-2">
    <TaskCard
      v-for="(task, index) in list"
      :key="task.id"
      :task="task"
      :project="projectFor(task)"
      :working-agent="workingAgentByTask?.get(task.id) ?? null"
      :sortable="props.sortable"
      :is-first="index === 0"
      :is-last="index === list.length - 1"
      @select="(t) => emit('select', t)"
      @open-chat="(t) => emit('openChat', t)"
      @navigate-agent="(sid) => emit('navigateAgent', sid)"
      @move-up="(t) => moveTask(t.id, 'up')"
      @move-down="(t) => moveTask(t.id, 'down')"
    />
  </div>
</template>
