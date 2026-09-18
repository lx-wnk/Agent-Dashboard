<script setup lang="ts">
import { computed, ref } from 'vue'
import { captureTask, CaptureUnavailable } from '@/composables/useCapture'
import { useProjects } from '@/composables/useProjects'
import { useViewState } from '@/composables/useViewState'
import { errorMessage } from '@/utils/errorMessage'
import { readInput } from '../composables/useReading'

const emit = defineEmits<{ captured: [taskId: string] }>()

const { projects } = useProjects()
const { activeView } = useViewState()

const text = ref('')
const busy = ref(false)
const problem = ref('')

const reading = computed(() => readInput(text.value))

async function submit() {
  const r = reading.value
  if (r.kind === 'empty' || busy.value)
    return
  problem.value = ''

  if (r.kind === 'navigate' && r.view) {
    activeView.value = r.view
    text.value = ''
    return
  }
  if (r.kind === 'command') {
    // Slash commands belong to an agent's prompt box, which this view does
    // not own. Saying so beats running it somewhere the user cannot see.
    problem.value = 'Slash commands run in an agent’s own prompt. Open the agent and type it there.'
    return
  }

  busy.value = true
  try {
    const id = await captureTask(text.value, projects.value)
    text.value = ''
    emit('captured', id)
  }
  catch (e) {
    problem.value = e instanceof CaptureUnavailable ? e.message : errorMessage(e, 'Could not capture that.')
  }
  finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="flex flex-col gap-2">
    <label for="mission-input" class="text-[12.5px] text-fg-mute">
      Or ask for anything — tasks, this interface, the system itself
    </label>

    <div class="rounded-xl border border-line-strong bg-app overflow-hidden">
      <div class="flex items-center gap-2.5 px-3.5 h-11">
        <span aria-hidden="true" class="font-mono text-[13px] text-accent">›</span>
        <input
          id="mission-input"
          v-model="text"
          data-testid="mission-input"
          type="text"
          placeholder="Type a task, or go to pipeline"
          class="flex-grow bg-transparent text-[14.5px] text-fg outline-none"
          @keydown.enter.prevent="submit"
        >
        <button
          type="button"
          data-testid="mission-input-submit"
          :disabled="reading.kind === 'empty' || busy"
          class="h-7 rounded-md border border-line-strong px-2.5 text-[12px] text-fg-soft disabled:opacity-50"
          @click="submit"
        >
          {{ busy ? 'Working…' : 'Enter' }}
        </button>
      </div>

      <div
        v-if="reading.kind !== 'empty'"
        data-testid="mission-reading"
        class="border-t border-line px-3.5 py-2.5 flex items-center gap-2.5"
      >
        <span
          data-testid="mission-reading-label"
          class="font-mono text-[10px] rounded px-1.5 py-0.5 border border-line-strong text-fg-soft shrink-0"
        >{{ reading.label }}</span>
        <!--
          The badge and the sentence are a term and its description. Flex gap
          separates them on screen but not in the text layer, so copying the
          line or reading textContent produced "GO TOSwitches to the pipeline
          view". This separator exists only there.
        -->
        <span class="sr-only">: </span>
        <span data-testid="mission-reading-will" class="text-[12.5px] text-fg-mute leading-snug">{{ reading.will }}</span>
      </div>
    </div>

    <p v-if="problem" data-testid="mission-input-problem" role="alert" class="text-[12.5px] text-warning-text">
      {{ problem }}
    </p>
    <p v-else class="text-[12px] text-fg-faint">
      The reading above is what Enter does. Only readings this screen can carry out are offered.
    </p>
  </div>
</template>
