<script setup lang="ts">
import { ref } from 'vue'
import { useInjectedTaskActions, useInjectedTaskDetails } from '../../composables/taskModalContext'
import TaskSlashCommandMenu from '../TaskSlashCommandMenu.vue'
import AppButton from '../ui/AppButton.vue'

const { isActing, actionError, actionSuccess, latestStageRun, isFailedRun, isTerminal, isOnHoldStage, isResumableAwaitingUser } = useInjectedTaskDetails()
const { additionalPrompt, analysisInfo, cancelConfirm, slashCommands, onCancelClick, onAnalyze, onResume, onRetry, onProgress, onSlashSelect } = useInjectedTaskActions()

const slashMenuRef = ref<InstanceType<typeof TaskSlashCommandMenu> | null>(null)
</script>

<template>
  <footer class="px-5 py-3 border-t border-line flex-shrink-0">
    <p v-if="actionError" class="text-danger-text text-xs mb-2">
      {{ actionError }}
    </p>
    <p v-if="actionSuccess" class="text-info-text text-xs mb-2">
      {{ actionSuccess }}
    </p>
    <p v-if="analysisInfo" class="text-green-600 dark:text-green-400 text-xs mb-2">
      Analysis agent spawned · PID <code>{{ analysisInfo.pid }}</code> · look for it in the agents list.
    </p>
    <div v-if="isFailedRun || isResumableAwaitingUser" class="mb-2">
      <div class="relative">
        <TaskSlashCommandMenu
          ref="slashMenuRef"
          v-model="additionalPrompt"
          :commands="slashCommands"
          @select="onSlashSelect"
        />
        <textarea
          v-model="additionalPrompt"
          rows="2"
          class="w-full bg-raised border border-line rounded px-2.5 py-1.5 text-fg text-xs resize-none focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent placeholder:text-fg-faint"
          placeholder="Optional instruction for Resume / Retry (e.g. logic change or hint)…"
          @keydown="slashMenuRef?.onKeydown($event)"
        />
      </div>
    </div>
    <div class="flex gap-2 justify-end">
      <AppButton
        v-if="(isFailedRun && latestStageRun?.sessionId) || isResumableAwaitingUser"
        variant="secondary"
        :disabled="isActing"
        :title="isResumableAwaitingUser
          ? 'Re-run this stage — the agent stopped without a passing result'
          : 'Continue the agent\'s last session from where it stopped'"
        @click="onResume"
      >
        {{ isResumableAwaitingUser && !latestStageRun?.sessionId ? 'Resume Stage' : 'Resume Session' }}
      </AppButton>
      <AppButton
        v-if="isFailedRun"
        variant="info"
        :disabled="isActing"
        title="Start a fresh iteration of this stage"
        @click="onRetry"
      >
        Retry Stage
      </AppButton>
      <AppButton
        v-if="isFailedRun || isResumableAwaitingUser"
        variant="secondary"
        :disabled="isActing"
        title="Spawn a standalone Claude session with the failure context attached"
        @click="onAnalyze"
      >
        Analyze Failure
      </AppButton>
      <AppButton
        v-if="!isTerminal && !isOnHoldStage && !isFailedRun"
        variant="secondary"
        :disabled="isActing"
        title="Manually advance to the next stage (skips approval gates)"
        @click="onProgress"
      >
        Progress →
      </AppButton>
      <AppButton
        v-if="!isTerminal"
        variant="danger"
        :disabled="isActing"
        :title="cancelConfirm ? 'Click again to confirm — this stops the task and marks it cancelled' : 'Stop this task and mark it as cancelled'"
        @click="onCancelClick"
      >
        {{ cancelConfirm ? 'Confirm Cancel?' : 'Cancel Task' }}
      </AppButton>
    </div>
  </footer>
</template>
