<script setup lang="ts">
import { ref } from 'vue'

const props = defineProps<{ taskId: string }>()

const COMMANDS = [
  'pnpm test',
  'pnpm lint',
  'pnpm typecheck',
  'pnpm build',
  'git log',
  'git diff',
  'git status',
]

const selectedCommand = ref(COMMANDS[0])
const output = ref('')
const exitCode = ref<number | null>(null)
const running = ref(false)
const expanded = ref(false)

async function run() {
  running.value = true
  output.value = ''
  exitCode.value = null
  try {
    const res = await fetch(`/api/tasks/${props.taskId}/run`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ command: selectedCommand.value }),
    })
    const data = await res.json() as { output: string, exitCode: number }
    output.value = data.output
    exitCode.value = data.exitCode
  }
  catch {
    output.value = 'Request failed'
    exitCode.value = 1
  }
  finally {
    running.value = false
  }
}
</script>

<template>
  <div class="border border-slate-200 dark:border-slate-700 rounded-lg overflow-hidden">
    <button
      type="button"
      class="w-full flex items-center justify-between px-4 py-2 bg-slate-50 dark:bg-slate-800 text-sm font-medium text-slate-700 dark:text-slate-300"
      @click="expanded = !expanded"
    >
      <span>Run Command in Worktree</span>
      <span class="text-xs text-slate-400">{{ expanded ? '▲' : '▼' }}</span>
    </button>
    <div v-if="expanded" class="p-4 space-y-3">
      <div class="flex gap-2">
        <select
          v-model="selectedCommand"
          class="flex-1 text-sm border border-slate-300 dark:border-slate-600 rounded px-2 py-1 bg-white dark:bg-slate-900 text-slate-800 dark:text-slate-200"
        >
          <option v-for="cmd in COMMANDS" :key="cmd" :value="cmd">
            {{ cmd }}
          </option>
        </select>
        <button
          type="button"
          class="px-3 py-1.5 text-sm rounded bg-blue-600 text-white hover:bg-blue-700 disabled:opacity-50"
          :disabled="running"
          @click="run"
        >
          {{ running ? 'Running…' : 'Run' }}
        </button>
      </div>
      <div v-if="output" class="text-[11px] font-mono bg-slate-950 text-slate-100 rounded p-3 max-h-48 overflow-y-auto whitespace-pre-wrap">
        <span
          class="block mb-1 text-[10px] font-sans"
          :class="exitCode === 0 ? 'text-green-400' : 'text-red-400'"
        >Exit {{ exitCode }}</span>{{ output }}
      </div>
    </div>
  </div>
</template>
