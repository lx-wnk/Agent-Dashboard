<script setup lang="ts">
import { useInjectedTaskActions, useInjectedTaskDetails } from '../../composables/taskModalContext'
import AppButton from '../ui/AppButton.vue'

const { pendingByStageRun, isActing } = useInjectedTaskDetails()
const { onResolve, onResolveAll } = useInjectedTaskActions()
</script>

<template>
  <div
    v-for="group in pendingByStageRun"
    :key="group.stageRunId"
    class="mb-3 last:mb-0"
  >
    <div
      v-if="group.requests.length > 1"
      class="flex gap-1.5 mb-2 pb-2 border-b border-yellow-200 dark:border-yellow-800/50"
    >
      <AppButton variant="primary" size="sm" :disabled="isActing" @click="onResolveAll(group.stageRunId, 'granted')">
        Grant All ({{ group.requests.length }})
      </AppButton>
      <AppButton variant="danger" size="sm" :disabled="isActing" @click="onResolveAll(group.stageRunId, 'denied')">
        Deny All ({{ group.requests.length }})
      </AppButton>
    </div>
    <div
      v-for="req in group.requests"
      :key="req.id"
      class="border-t border-yellow-200 dark:border-yellow-800/50 first:border-t-0 first:pt-0 pt-2 mt-2 first:mt-0"
    >
      <div class="flex items-baseline gap-2 flex-wrap">
        <strong class="text-sm text-fg">{{ req.tool }}</strong>
        <span
          v-if="req.pattern"
          class="font-mono text-xs text-fg-soft bg-yellow-100/60 dark:bg-yellow-900/40 px-1.5 py-px rounded"
        >{{ req.pattern }}</span>
        <span
          v-if="req.reRequestCount && req.reRequestCount > 1"
          class="ml-1.5 inline-flex items-center px-1.5 py-0.5 rounded-full text-[10px] font-medium bg-amber-100 dark:bg-amber-900/40 text-amber-700 dark:text-amber-400"
          :title="`Requested ${req.reRequestCount} times`"
        >
          {{ req.reRequestCount }}x re-requests
        </span>
        <span
          v-if="req.outsideSafeList"
          class="ml-1.5 inline-flex items-center px-1.5 py-0.5 rounded-full text-[10px] font-medium bg-amber-100 dark:bg-amber-900/40 text-amber-700 dark:text-amber-400"
          title="Granting this approves a command outside the safe allow-list (exception, this task only)."
        >
          outside safe-list
        </span>
      </div>
      <p v-if="req.reason" class="text-[11px] text-fg-mute mt-1 leading-relaxed">
        {{ req.reason }}
      </p>
      <div class="flex gap-1.5 mt-2">
        <AppButton variant="primary" size="sm" :disabled="isActing" @click="onResolve(req, 'granted')">
          Grant
        </AppButton>
        <AppButton variant="danger" size="sm" :disabled="isActing" @click="onResolve(req, 'denied')">
          Deny
        </AppButton>
      </div>
    </div>
  </div>
</template>
