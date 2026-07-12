<script setup lang="ts">
import { ref } from 'vue'
import { useInjectedTaskActions, useInjectedTaskDetails } from '../../composables/taskModalContext'
import AppButton from '../ui/AppButton.vue'
import TaskPendingRequests from './TaskPendingRequests.vue'

const { permissions, pendingRequests } = useInjectedTaskDetails()
const { onGrantPermission, isGranting, permError } = useInjectedTaskActions()

const newPermTool = ref('')
const newPermPattern = ref('')

async function submitGrant(): Promise<void> {
  const ok = await onGrantPermission(newPermTool.value, newPermPattern.value)
  if (ok) {
    newPermTool.value = ''
    newPermPattern.value = ''
  }
}
</script>

<template>
  <section class="p-5">
    <div v-if="pendingRequests.length > 0" class="mb-4">
      <h3 class="text-[11px] uppercase text-fg-mute mb-2 tracking-[0.5px]">
        Pending runtime requests ({{ pendingRequests.length }})
      </h3>
      <TaskPendingRequests />
    </div>

    <div class="border border-line rounded-md px-3.5 py-3 mb-3 bg-app">
      <h3 class="text-[11px] uppercase text-fg-mute mb-1 tracking-[0.5px]">
        Grant a tool permission
      </h3>
      <p class="text-[11px] text-fg-mute mb-2.5 leading-relaxed">
        Pre-approve a tool before Retry — useful when the agent hit a permission wall.
        Examples: <code class="bg-raised px-[3px] rounded text-[11px]">Write</code>, <code class="bg-raised px-[3px] rounded text-[11px]">Bash</code> with pattern <code class="bg-raised px-[3px] rounded text-[11px]">npm run *</code>
      </p>
      <div class="flex gap-2 items-center">
        <label for="perm-tool" class="sr-only">Tool</label>
        <input
          id="perm-tool"
          v-model="newPermTool"
          class="flex-1 min-w-0 bg-raised border border-line rounded px-2 py-1.5 text-fg text-xs focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
          placeholder="Tool (e.g. Bash, Write)"
          @keydown.enter="submitGrant"
        >
        <label for="perm-pattern" class="sr-only">Pattern</label>
        <input
          id="perm-pattern"
          v-model="newPermPattern"
          class="flex-1 min-w-0 bg-raised border border-line rounded px-2 py-1.5 text-fg text-xs focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
          placeholder="Pattern (optional, e.g. npm run *)"
          @keydown.enter="submitGrant"
        >
        <AppButton variant="primary" size="sm" :disabled="isGranting || !newPermTool.trim()" @click="submitGrant">
          Grant
        </AppButton>
      </div>
      <p v-if="permError" class="text-[11px] text-danger-text mt-1.5">
        {{ permError }}
      </p>
    </div>

    <div v-if="permissions.length > 0">
      <h3 class="text-[11px] uppercase text-fg-mute mb-2 tracking-[0.5px]">
        Granted permissions
      </h3>
      <div v-for="p in permissions" :key="p.id" class="flex gap-2.5 px-2.5 py-1.5 text-xs border-b border-line">
        <span class="font-semibold text-fg min-w-[80px]">{{ p.tool }}</span>
        <span v-if="p.pattern" class="font-mono text-fg-mute flex-1">{{ p.pattern }}</span>
        <span class="text-[10px] text-fg-mute uppercase">{{ p.preApproved ? 'pre-approved' : 'runtime' }}</span>
        <span class="text-[10px] text-fg-mute">{{ p.decidedBy }}</span>
      </div>
    </div>
    <div v-if="permissions.length === 0 && pendingRequests.length === 0" class="text-fg-mute text-xs text-center py-8">
      No permissions granted yet.
    </div>
  </section>
</template>
