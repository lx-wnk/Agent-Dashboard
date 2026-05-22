<script setup lang="ts">
import { ref, useId } from 'vue'

const props = defineProps<{
  taskId: string
  cwd?: string
}>()

const emit = defineEmits<{
  (e: 'abort'): void
}>()

const COMMANDS = [
  'pnpm test',
  'pnpm lint',
  'pnpm typecheck',
  'pnpm build',
  'git log',
  'git diff',
  'git status',
]

const panelId = useId()
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

function abort() {
  emit('abort')
}
</script>

<template>
  <div class="border border-line rounded-lg overflow-hidden">
    <button
      type="button"
      class="w-full flex items-center justify-between px-4 py-2 bg-raised text-sm font-medium text-fg-soft"
      :aria-expanded="expanded"
      :aria-controls="panelId"
      @click="expanded = !expanded"
    >
      <span>Run Command in Worktree</span>
      <span class="text-xs text-slate-400">{{ expanded ? '▲' : '▼' }}</span>
    </button>
    <div v-show="expanded" :id="panelId" :inert="!expanded" class="p-4 space-y-3">
      <span v-if="cwd" class="block text-xs text-fg-mute font-mono truncate">{{ cwd }}</span>
      <div class="flex gap-2">
        <select
          v-model="selectedCommand"
          class="flex-1 text-sm border border-line-strong rounded px-2 py-1 bg-card text-fg"
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
        <button
          v-if="running"
          type="button"
          class="px-3 py-1.5 text-sm rounded bg-raised text-fg-soft hover:bg-slate-300 dark:hover:bg-slate-600"
          @click="abort"
        >
          Abort
        </button>
      </div>
      <div v-if="output" role="log" aria-live="polite" class="text-[11px] font-mono bg-slate-950 text-slate-100 rounded p-3 max-h-96 overflow-y-auto resize-y whitespace-pre-wrap">
        <span
          class="block mb-1 text-[10px] font-sans"
          :class="exitCode === 0 ? 'text-green-400' : 'text-red-400'"
        >Exit {{ exitCode }}</span>{{ output }}
      </div>
    </div>
  </div>
</template>
