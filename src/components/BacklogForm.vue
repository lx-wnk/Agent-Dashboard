<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { createTask } from '../composables/useTasks'
import AppButton from './ui/AppButton.vue'
import AppInput from './ui/AppInput.vue'
import AppModal from './ui/AppModal.vue'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: [] }>()

const slug = ref('')
const title = ref('')
const description = ref('')
const cwd = ref('')
const sourceBranch = ref('')
const targetBranch = ref('')
const useWorktree = ref(true)
const maxIterations = ref(20)
const tokenBudget = ref<number | null>(null)
const silverBullet = ref(false)
const priority = ref<'high' | 'medium' | 'low'>('medium')
const errorMsg = ref('')
const isCreating = ref(false)

watch(() => props.open, (val) => {
  if (val)
    errorMsg.value = ''
})

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && props.open)
    emit('close')
}
onMounted(() => window.addEventListener('keydown', onKeydown))
onUnmounted(() => window.removeEventListener('keydown', onKeydown))

function resetForm() {
  slug.value = ''
  title.value = ''
  description.value = ''
  cwd.value = ''
  sourceBranch.value = ''
  targetBranch.value = ''
  useWorktree.value = true
  maxIterations.value = 20
  tokenBudget.value = null
  silverBullet.value = false
  priority.value = 'medium'
  errorMsg.value = ''
}

function slugifyTitle() {
  if (slug.value)
    return
  slug.value = title.value
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 60)
}

async function handleCreate() {
  if (isCreating.value)
    return
  errorMsg.value = ''

  if (!title.value.trim() || !cwd.value.trim()) {
    errorMsg.value = 'Title and working directory are required'
    return
  }
  if (!slug.value.trim())
    slugifyTitle()

  isCreating.value = true
  try {
    await createTask({
      slug: slug.value.trim(),
      title: title.value.trim(),
      description: description.value.trim() || undefined,
      cwd: cwd.value.trim(),
      sourceBranch: sourceBranch.value.trim() || undefined,
      targetBranch: targetBranch.value.trim() || undefined,
      useWorktree: useWorktree.value,
      maxIterations: maxIterations.value,
      tokenBudget: tokenBudget.value ?? undefined,
      silverBullet: silverBullet.value,
      priority: priority.value,
    })
    resetForm()
    emit('close')
  }
  catch (err) {
    errorMsg.value = (err as Error).message
  }
  finally {
    isCreating.value = false
  }
}
</script>

<template>
  <AppModal :open="open" @close="emit('close')">
    <div class="bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 shadow-[0_8px_40px_rgba(0,0,0,0.5)] w-full max-w-xl">
      <header class="flex justify-between items-center px-5 py-4 border-b border-slate-200 dark:border-slate-700">
        <h2 class="text-lg font-semibold text-slate-900 dark:text-slate-100">
          New Task
        </h2>
        <button type="button" class="bg-transparent border-none text-slate-400 dark:text-slate-600 text-2xl cursor-pointer px-1 leading-none hover:text-slate-900 dark:hover:text-slate-100" @click="emit('close')">
          &times;
        </button>
      </header>

      <form class="p-5" @submit.prevent="handleCreate">
        <div class="mb-4 flex flex-col gap-1.5">
          <label for="task-title" class="text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600">Title</label>
          <AppInput
            id="task-title"
            v-model="title"
            required
            placeholder="Fix login bug in auth middleware"
            @blur="slugifyTitle"
          />
        </div>

        <div class="mb-4 flex flex-col gap-1.5">
          <label for="task-slug" class="text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600">Slug (url-safe id)</label>
          <AppInput
            id="task-slug"
            v-model="slug"
            pattern="[a-z0-9][a-z0-9-]{0,63}"
            placeholder="auto-generated from title"
          />
        </div>

        <div class="mb-4 flex flex-col gap-1.5">
          <label for="task-desc" class="text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600">Description</label>
          <AppInput
            id="task-desc"
            v-model="description"
            type="textarea"
            :rows="3"
            placeholder="What should the agent do? Include screenshots or references as needed."
          />
        </div>

        <div class="mb-4 flex flex-col gap-1.5">
          <label for="task-cwd" class="text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600">Working Directory</label>
          <AppInput
            id="task-cwd"
            v-model="cwd"
            required
            placeholder="/path/to/project"
          />
        </div>

        <div class="flex gap-3 mb-4">
          <div class="flex-1 flex flex-col gap-1.5">
            <label for="task-src" class="text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600">Source Branch</label>
            <AppInput
              id="task-src"
              v-model="sourceBranch"
              placeholder="main"
            />
          </div>
          <div class="flex-1 flex flex-col gap-1.5">
            <label for="task-tgt" class="text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600">Target Branch</label>
            <AppInput
              id="task-tgt"
              v-model="targetBranch"
              placeholder="feat/my-branch"
            />
          </div>
        </div>

        <div class="flex items-center gap-2 mb-4">
          <input
            id="task-worktree"
            v-model="useWorktree"
            type="checkbox"
          >
          <label for="task-worktree">Use isolated git worktree (recommended)</label>
        </div>

        <div class="flex gap-3 mb-4">
          <div class="flex-1 flex flex-col gap-1.5">
            <label for="task-priority" class="text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600">Priority</label>
            <select
              id="task-priority"
              v-model="priority"
              class="w-full bg-slate-50 dark:bg-slate-950 border border-slate-200 dark:border-slate-700 rounded text-slate-900 dark:text-slate-100 text-[13px] px-2.5 py-2 focus:outline-none focus:border-blue-500"
            >
              <option value="high">
                High
              </option>
              <option value="medium">
                Medium
              </option>
              <option value="low">
                Low
              </option>
            </select>
          </div>
          <div class="flex-1 flex items-center gap-2 self-center mt-4">
            <input
              id="task-silver"
              v-model="silverBullet"
              type="checkbox"
            >
            <label for="task-silver">Silver bullet (jump the queue)</label>
          </div>
        </div>

        <div class="flex gap-3 mb-4">
          <div class="flex-1 flex flex-col gap-1.5">
            <label for="task-iter" class="text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600">Max Iterations</label>
            <input
              id="task-iter"
              v-model.number="maxIterations"
              class="w-full bg-slate-50 dark:bg-slate-950 border border-slate-200 dark:border-slate-700 rounded text-slate-900 dark:text-slate-100 text-[13px] px-2.5 py-2 focus:outline-none focus:border-blue-500"
              type="number"
              min="1"
              max="50"
            >
          </div>
          <div class="flex-1 flex flex-col gap-1.5">
            <label for="task-tokens" class="text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600">Token Budget (optional)</label>
            <input
              id="task-tokens"
              v-model.number="tokenBudget"
              class="w-full bg-slate-50 dark:bg-slate-950 border border-slate-200 dark:border-slate-700 rounded text-slate-900 dark:text-slate-100 text-[13px] px-2.5 py-2 focus:outline-none focus:border-blue-500"
              type="number"
              min="0"
              placeholder="∞"
            >
          </div>
        </div>

        <p v-if="errorMsg" class="text-xs text-red-600 dark:text-red-400 mt-1 leading-snug">
          {{ errorMsg }}
        </p>
      </form>

      <footer class="flex justify-end gap-2 px-5 py-3 border-t border-slate-200 dark:border-slate-700">
        <AppButton variant="secondary" @click="emit('close')">
          Cancel
        </AppButton>
        <AppButton
          variant="primary"
          :disabled="isCreating || !title.trim() || !cwd.trim()"
          @click="handleCreate"
        >
          {{ isCreating ? 'Creating...' : 'Create Task' }}
        </AppButton>
      </footer>
    </div>
  </AppModal>
</template>
