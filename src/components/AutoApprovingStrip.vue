<script setup lang="ts">
import { onMounted } from 'vue'
import { usePermissionPresets } from '../composables/usePermissionPresets'
import AppChip from './ui/AppChip.vue'

const { presets, load, revoke } = usePermissionPresets()

onMounted(load)

defineExpose({ load })

async function handleRevoke(cwd: string) {
  try {
    await revoke(cwd)
  }
  catch {
    // ignore — failed revokes leave the chip in place; user can retry
  }
}

function projectName(cwd: string): string {
  return cwd.split('/').filter(Boolean).at(-1) ?? cwd
}
</script>

<template>
  <div
    v-if="presets.length > 0"
    class="mb-3 flex items-center flex-wrap gap-1.5 px-3 py-2 rounded-lg border border-success-line bg-success-soft"
    aria-label="Auto-approving permissions for these projects"
  >
    <span class="text-[12px] font-medium text-success-text mr-1">Auto-approving</span>
    <AppChip
      v-for="preset in presets"
      :key="preset.projectCwd"
      tone="success"
      mono
      class="inline-flex items-center gap-1"
    >
      {{ projectName(preset.projectCwd) }}
      <button
        type="button"
        class="ml-1 text-[10px] text-success-text opacity-60 hover:opacity-100 focus-visible:outline-none focus-visible:ring-[2px] focus-visible:ring-success-text rounded"
        :aria-label="`Stop auto-approving ${projectName(preset.projectCwd)}`"
        @click="handleRevoke(preset.projectCwd)"
      >&#x2715;</button>
    </AppChip>
  </div>
</template>
