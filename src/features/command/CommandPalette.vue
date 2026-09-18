<script setup lang="ts">
import type { Project } from '@/types'
import { computed, nextTick, onMounted, onUnmounted, ref, useId } from 'vue'
import AppModal from '@/components/ui/AppModal.vue'
import { suggestFolders } from '@/composables/useProjectFolders'
import { useProjects } from '@/composables/useProjects'
import { ACTIVE_VIEWS, useViewState } from '@/composables/useViewState'
import { createTask } from '@/features/pipeline'
import { slugFollowingName } from '@/utils/validation'

const emit = defineEmits<{ captured: [taskId: string] }>()

const { activeView } = useViewState()
const { projects } = useProjects()

const open = ref(false)
const query = ref('')
const active = ref(0)
const busy = ref(false)
const problem = ref('')
const inputRef = ref<HTMLInputElement>()
const listId = useId()
const optionId = (i: number) => `${listId}-${i}`

let returnFocusTo: HTMLElement | null = null

interface Command {
  id: string
  label: string
  run: () => void
}

const commands = computed<Command[]>(() =>
  ACTIVE_VIEWS.map(view => ({
    id: `view:${view}`,
    label: `Go to ${view}`,
    run: () => { activeView.value = view },
  })),
)

const matches = computed(() => {
  const q = query.value.trim().toLowerCase()
  if (!q)
    return commands.value
  return commands.value.filter(c => c.label.toLowerCase().includes(q))
})

// Free text that matches no command is a capture, not a dead end: that is the
// whole point of the field -- one line in, a refinable backlog item out.
const capturing = computed(() => query.value.trim().length > 0 && matches.value.length === 0)

function close() {
  open.value = false
  query.value = ''
  active.value = 0
  problem.value = ''
  returnFocusTo?.focus()
  returnFocusTo = null
}

async function show() {
  returnFocusTo = document.activeElement as HTMLElement | null
  open.value = true
  await nextTick()
  inputRef.value?.focus()
}

// The server needs a working directory for every task. Asking for one is the
// friction this palette exists to remove, so it is derived: the first project,
// then that project's default folder.
async function deriveCwd(project: Project): Promise<string | null> {
  const folders = await suggestFolders(project.id)
  return (folders.find(f => f.isDefault) ?? folders[0])?.path ?? null
}

async function capture() {
  const title = query.value.trim()
  if (!title || busy.value)
    return
  const project = projects.value[0]
  if (!project) {
    problem.value = 'No project exists yet — create one in Settings before capturing.'
    return
  }
  busy.value = true
  problem.value = ''
  try {
    const cwd = await deriveCwd(project)
    if (!cwd) {
      problem.value = `Project "${project.name}" has no folder — add one in Settings.`
      return
    }
    const task = await createTask({
      title,
      slug: slugFollowingName(title, '', false),
      cwd,
      projectId: project.id,
    })
    close()
    emit('captured', task.id)
  }
  catch (e) {
    problem.value = e instanceof Error ? e.message : 'Could not capture that.'
  }
  finally {
    busy.value = false
  }
}

function submit() {
  if (capturing.value) {
    void capture()
    return
  }
  const command = matches.value[active.value]
  if (!command)
    return
  command.run()
  close()
}

function move(delta: number) {
  const count = matches.value.length
  if (count === 0)
    return
  active.value = (active.value + delta + count) % count
}

function onGlobalKeydown(e: KeyboardEvent) {
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
    e.preventDefault()
    if (open.value)
      close()
    else
      void show()
  }
}

onMounted(() => window.addEventListener('keydown', onGlobalKeydown))
onUnmounted(() => window.removeEventListener('keydown', onGlobalKeydown))
</script>

<template>
  <AppModal :open="open" width="560px" @close="close">
    <div class="p-3" data-testid="command-palette">
      <input
        ref="inputRef"
        v-model="query"
        type="text"
        role="combobox"
        aria-expanded="true"
        :aria-controls="listId"
        :aria-activedescendant="capturing ? undefined : optionId(active)"
        aria-label="Command or new backlog item"
        placeholder="Type a command, or a line to capture…"
        data-testid="command-palette-input"
        class="w-full bg-transparent text-fg placeholder:text-fg-mute outline-none text-[14px] px-1 py-1.5"
        @keydown.down.prevent="move(1)"
        @keydown.up.prevent="move(-1)"
        @keydown.enter.prevent="submit"
        @keydown.esc.prevent="close"
      >
      <p
        v-if="problem"
        data-testid="command-palette-problem"
        role="alert"
        class="text-[12px] rounded-md px-2 py-1.5 mt-2 bg-warning-soft text-warning-text"
      >
        {{ problem }}
      </p>
      <p
        v-if="capturing"
        data-testid="command-palette-capture"
        class="text-[12px] text-fg-mute mt-2 px-1"
      >
        {{ busy ? 'Capturing…' : 'Enter captures this as a backlog item.' }}
      </p>
      <ul v-else :id="listId" role="listbox" aria-label="Commands" class="mt-2 flex flex-col">
        <li
          v-for="(c, i) in matches"
          :id="optionId(i)"
          :key="c.id"
          role="option"
          :aria-selected="i === active"
          :data-testid="`command-palette-option-${c.id}`"
          class="text-[13px] rounded-md px-2 py-1.5 cursor-pointer"
          :class="i === active ? 'bg-accent-soft text-fg font-medium' : 'text-fg-mute'"
          @click="c.run(); close()"
          @mousemove="active = i"
        >
          <span v-if="i === active" aria-hidden="true">›&nbsp;</span>{{ c.label }}
        </li>
      </ul>
    </div>
  </AppModal>
</template>
