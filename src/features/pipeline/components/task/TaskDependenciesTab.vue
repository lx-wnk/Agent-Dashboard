<script setup lang="ts">
import DependencyGraph from '@/components/DependencyGraph.vue'
import AppInput from '@/components/ui/AppInput.vue'
import AppSelect from '@/components/ui/AppSelect.vue'
import { useInjectedTask } from '@/features/pipeline/composables/taskModalContext'
import { useTaskDependencies } from '@/features/pipeline/composables/useTaskDependencies'

const emit = defineEmits<{ navigateTask: [taskId: string] }>()

const task = useInjectedTask()
const {
  dependencies,
  dependents,
  newDepId,
  newDepStage,
  newDepCancelAction,
  depError,
  isAddingDep,
  handleAddDependency,
  handleRemoveDependency,
} = useTaskDependencies(task)

const requiredStageOptions: Array<{ value: string, label: string }> = [
  { value: 'done', label: 'Done' },
  { value: 'cancelled', label: 'Cancelled' },
]

const onCancelActionOptions: Array<{ value: string, label: string }> = [
  { value: 'on_hold', label: 'On Hold (on cancel)' },
  { value: 'cancel', label: 'Cancel (on cancel)' },
  { value: 'start', label: 'Start (on cancel)' },
]
</script>

<template>
  <section v-if="task" class="p-5 flex flex-col gap-4">
    <div>
      <h4 class="text-[11px] font-semibold uppercase tracking-[0.5px] text-fg-mute mb-2">
        Dependencies
      </h4>

      <div v-if="dependencies.length > 0" class="mb-2.5">
        <p class="text-[11px] text-fg-mute font-semibold uppercase tracking-[0.3px] mb-1">
          Waiting for:
        </p>
        <div v-for="dep in dependencies" :key="dep.id" class="flex items-center gap-2 py-1 text-xs">
          <span class="flex-1 text-fg">{{ dep.dependsOnTitle }}</span>
          <span
            class="px-1.5 py-px rounded text-[10px] font-mono"
            :class="dep.dependsOnStage === dep.requiredStage
              ? 'bg-green-50 dark:bg-green-950/50 text-green-600 dark:text-green-400 border border-green-300 dark:border-green-700'
              : 'bg-red-50 dark:bg-red-950/50 text-danger-text border border-red-300 dark:border-red-700/50'"
          >{{ dep.dependsOnStage }}</span>
          <span class="text-[10px] text-fg-mute font-mono">on cancel: {{ dep.onCancelAction }}</span>
          <button type="button" class="bg-transparent border-none cursor-pointer text-fg-mute px-1 py-px text-[10px] rounded hover:bg-red-50 dark:hover:bg-red-950/30 hover:text-red-600 dark:hover:text-red-400" title="Remove dependency" @click="handleRemoveDependency(dep.id)">
            ✕
          </button>
        </div>
      </div>

      <div v-if="dependents.length > 0" class="mb-2.5">
        <p class="text-[11px] text-fg-mute font-semibold uppercase tracking-[0.3px] mb-1">
          Needed by:
        </p>
        <div v-for="dep in dependents" :key="dep.id" class="flex items-center gap-2 py-1 text-xs">
          <span class="flex-1 text-fg">{{ dep.taskTitle || dep.taskId }}</span>
        </div>
      </div>

      <form class="flex gap-1.5 items-center flex-wrap mt-2" @submit.prevent="handleAddDependency">
        <AppInput v-model="newDepId" class="flex-1 min-w-0" placeholder="Predecessor Task ID" :disabled="isAddingDep" />
        <AppSelect
          :model-value="newDepStage"
          :options="requiredStageOptions"
          aria-label="Required stage"
          size="compact"
          class="text-[11px]"
          @update:model-value="newDepStage = $event as 'done' | 'cancelled'"
        />
        <AppSelect
          :model-value="newDepCancelAction"
          :options="onCancelActionOptions"
          aria-label="On cancel action"
          size="compact"
          class="text-[11px]"
          @update:model-value="newDepCancelAction = $event as 'cancel' | 'start' | 'on_hold'"
        />
        <button type="submit" class="px-2.5 py-1 bg-blue-600 text-white border-none rounded text-xs cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed hover:brightness-110" :disabled="isAddingDep || !newDepId.trim()">
          Add
        </button>
      </form>
      <p v-if="depError" class="text-[11px] text-danger-text mt-1">
        {{ depError }}
      </p>
    </div>

    <div class="border-t border-line pt-4">
      <DependencyGraph :task-id="task.id" @navigate="id => emit('navigateTask', id)" />
    </div>
  </section>
</template>
