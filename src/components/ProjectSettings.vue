<script setup lang="ts">
import type { Project, ProjectFolder } from '../types'
import { ref, watch } from 'vue'
import {
  createFolder,
  deleteFolder,
  fetchProjectFolders,
  updateFolder,
} from '../composables/useProjectFolders'
import {
  createProject,
  deleteProject,
  updateProject,
  useProjects,
} from '../composables/useProjects'
import { isAbsolutePath } from '../utils/validation'
import AppButton from './ui/AppButton.vue'

// Projects composable — auto-loads on mount
const { projects, isLoading, error, refetch } = useProjects()

// ── Edit / create project form ──────────────────────────────────────────────
interface ProjectFormState {
  slug: string
  name: string
  description: string
  color: string
  defaultSpawnerId: string
}

const editingProject = ref<Project | null>(null)
const isCreating = ref(false)
const formVisible = ref(false)
const formSaving = ref(false)
const formError = ref<string | null>(null)
const form = ref<ProjectFormState>({ slug: '', name: '', description: '', color: '#3b82f6', defaultSpawnerId: '' })

function openCreate() {
  editingProject.value = null
  form.value = { slug: '', name: '', description: '', color: '#3b82f6', defaultSpawnerId: '' }
  formError.value = null
  formVisible.value = true
  isCreating.value = true
}

function openEdit(project: Project) {
  editingProject.value = project
  form.value = {
    slug: project.slug,
    name: project.name,
    description: project.description ?? '',
    color: project.color ?? '#3b82f6',
    defaultSpawnerId: project.defaultSpawnerId ?? '',
  }
  formError.value = null
  formVisible.value = true
  isCreating.value = false
  void loadFolders(project.id)
}

function closeForm() {
  formVisible.value = false
  editingProject.value = null
  folderRows.value = []
  folderError.value = null
}

async function handleSave() {
  formError.value = null
  if (!form.value.name.trim() || !form.value.slug.trim()) {
    formError.value = 'Name and slug are required.'
    return
  }
  formSaving.value = true
  try {
    if (isCreating.value) {
      const project = await createProject({
        slug: form.value.slug.trim(),
        name: form.value.name.trim(),
        description: form.value.description.trim() || undefined,
        color: form.value.color || undefined,
        defaultSpawnerId: form.value.defaultSpawnerId || null,
      })
      // Switch to edit mode so folders can be managed
      editingProject.value = project
      isCreating.value = false
      await refetch()
    }
    else if (editingProject.value) {
      await updateProject(editingProject.value.id, {
        slug: form.value.slug.trim(),
        name: form.value.name.trim(),
        description: form.value.description.trim() || undefined,
        color: form.value.color || undefined,
        defaultSpawnerId: form.value.defaultSpawnerId || null,
      })
      await refetch()
    }
  }
  catch (e) {
    formError.value = (e as Error).message
  }
  finally {
    formSaving.value = false
  }
}

// ── Delete confirmation ─────────────────────────────────────────────────────
const confirmDeleteId = ref<string | null>(null)
const deleteError = ref<string | null>(null)

async function handleDelete(id: string) {
  deleteError.value = null
  try {
    await deleteProject(id)
    confirmDeleteId.value = null
    await refetch()
    if (editingProject.value?.id === id)
      closeForm()
  }
  catch (e) {
    deleteError.value = (e as Error).message
    confirmDeleteId.value = null
  }
}

// ── Folder management ───────────────────────────────────────────────────────
interface FolderRow extends Partial<ProjectFolder> {
  _key: string
  path: string
  label: string
  isDefault: boolean
  isDirty: boolean
  isNew: boolean
  saving: boolean
  saveError: string | null
}

const folderRows = ref<FolderRow[]>([])
const folderError = ref<string | null>(null)
const folderLoading = ref(false)
let _rowKey = 0

async function loadFolders(projectId: string) {
  folderLoading.value = true
  folderError.value = null
  try {
    const folders = await fetchProjectFolders(projectId)
    folderRows.value = folders.map(f => ({
      ...f,
      _key: String(_rowKey++),
      label: f.label ?? '',
      isDirty: false,
      isNew: false,
      saving: false,
      saveError: null,
    }))
  }
  catch (e) {
    folderError.value = (e as Error).message
  }
  finally {
    folderLoading.value = false
  }
}

function addFolderRow() {
  folderRows.value.push({
    _key: String(_rowKey++),
    path: '',
    label: '',
    isDefault: folderRows.value.length === 0,
    isDirty: true,
    isNew: true,
    saving: false,
    saveError: null,
  })
}

async function saveFolderRow(row: FolderRow) {
  if (!editingProject.value)
    return
  if (!row.path.trim()) {
    row.saveError = 'Path is required.'
    return
  }
  if (!isAbsolutePath(row.path.trim())) {
    row.saveError = 'Path must be absolute (starts with /) and must not contain ".." segments.'
    return
  }
  row.saveError = null
  row.saving = true
  try {
    const input = { path: row.path.trim(), label: row.label.trim() || undefined, isDefault: row.isDefault }
    if (row.isNew) {
      const created = await createFolder(editingProject.value.id, input)
      Object.assign(row, created, { _key: row._key, label: created.label ?? '', isDirty: false, isNew: false, saving: false, saveError: null })
    }
    else if (row.id) {
      const updated = await updateFolder(editingProject.value.id, row.id, input)
      Object.assign(row, updated, { _key: row._key, label: updated.label ?? '', isDirty: false, isNew: false, saving: false, saveError: null })
    }
  }
  catch (e) {
    row.saveError = (e as Error).message
    row.saving = false
  }
}

async function removeFolderRow(row: FolderRow) {
  if (!editingProject.value)
    return
  if (row.isNew) {
    folderRows.value = folderRows.value.filter(r => r._key !== row._key)
    return
  }
  if (!row.id)
    return
  row.saving = true
  row.saveError = null
  try {
    await deleteFolder(editingProject.value.id, row.id)
    folderRows.value = folderRows.value.filter(r => r._key !== row._key)
  }
  catch (e) {
    row.saveError = (e as Error).message
    row.saving = false
  }
}

function setDefault(targetRow: FolderRow) {
  for (const r of folderRows.value)
    r.isDefault = r._key === targetRow._key
}

// Sync isDefault when changed
watch(folderRows, () => {}, { deep: true })
</script>

<template>
  <div class="flex flex-col gap-4">
    <!-- Header -->
    <div class="flex items-start justify-between gap-3">
      <div>
        <h3 class="text-[17px] font-bold text-slate-900 dark:text-slate-100 mb-1">
          Projects
        </h3>
        <p class="text-xs text-slate-400 dark:text-slate-600">
          Group tasks under named projects. Each project can have default folders (working directories) and a spawner override.
        </p>
      </div>
      <AppButton variant="info" @click="openCreate">
        + New Project
      </AppButton>
    </div>

    <!-- Global error -->
    <p v-if="error" class="text-xs text-red-600 dark:text-red-400">
      {{ error }}
    </p>
    <p v-if="deleteError" class="text-xs text-red-600 dark:text-red-400">
      {{ deleteError }}
    </p>

    <!-- Loading -->
    <div v-if="isLoading" class="text-center py-12 text-slate-400 dark:text-slate-600 text-sm">
      Loading projects...
    </div>

    <!-- Empty -->
    <div v-else-if="!projects.length && !formVisible" class="text-center py-8 text-slate-400 dark:text-slate-600 text-sm">
      No projects yet. Create one to group tasks and set default working directories.
    </div>

    <!-- Project list -->
    <table v-else-if="!formVisible" class="w-full border-collapse text-[13px]">
      <thead>
        <tr>
          <th class="text-left text-[10px] uppercase tracking-wide text-slate-400 dark:text-slate-600 px-3 py-2 border-b border-slate-200 dark:border-slate-700">
            Name
          </th>
          <th class="text-left text-[10px] uppercase tracking-wide text-slate-400 dark:text-slate-600 px-3 py-2 border-b border-slate-200 dark:border-slate-700">
            Slug
          </th>
          <th class="text-left text-[10px] uppercase tracking-wide text-slate-400 dark:text-slate-600 px-3 py-2 border-b border-slate-200 dark:border-slate-700">
            Folders
          </th>
          <th class="text-left text-[10px] uppercase tracking-wide text-slate-400 dark:text-slate-600 px-3 py-2 border-b border-slate-200 dark:border-slate-700">
            Actions
          </th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="project in projects" :key="project.id">
          <td class="px-3 py-2.5 border-b border-slate-200 dark:border-slate-700">
            <div class="flex items-center gap-2">
              <span
                v-if="project.color"
                class="inline-block w-2.5 h-2.5 rounded-full flex-shrink-0"
                :style="{ backgroundColor: project.color }"
                aria-hidden="true"
              />
              <span class="font-semibold text-slate-900 dark:text-slate-100">{{ project.name }}</span>
            </div>
            <p v-if="project.description" class="text-[11px] text-slate-400 dark:text-slate-600 mt-0.5 line-clamp-1">
              {{ project.description }}
            </p>
          </td>
          <td class="px-3 py-2.5 border-b border-slate-200 dark:border-slate-700 font-mono text-xs text-slate-500 dark:text-slate-400">
            {{ project.slug }}
          </td>
          <td class="px-3 py-2.5 border-b border-slate-200 dark:border-slate-700 text-slate-500 dark:text-slate-400">
            {{ project.folderCount ?? 0 }}
          </td>
          <td class="px-3 py-2.5 border-b border-slate-200 dark:border-slate-700 whitespace-nowrap">
            <template v-if="confirmDeleteId === project.id">
              <AppButton variant="danger" size="sm" class="mr-1" @click="handleDelete(project.id)">
                Confirm Delete
              </AppButton>
              <AppButton variant="secondary" size="sm" @click="confirmDeleteId = null">
                Cancel
              </AppButton>
            </template>
            <template v-else>
              <button
                type="button"
                class="bg-transparent border-none text-slate-400 dark:text-slate-600 cursor-pointer text-sm px-2 py-1 rounded hover:bg-blue-50 dark:hover:bg-blue-950/30 hover:text-blue-600 dark:hover:text-blue-400 mr-1"
                @click="openEdit(project)"
              >
                Edit
              </button>
              <button
                type="button"
                class="bg-transparent border-none text-slate-400 dark:text-slate-600 cursor-pointer text-sm px-2 py-1 rounded hover:bg-red-50 dark:hover:bg-red-950/30 hover:text-red-600 dark:hover:text-red-400"
                @click="confirmDeleteId = project.id"
              >
                Delete
              </button>
            </template>
          </td>
        </tr>
      </tbody>
    </table>

    <!-- Project create/edit form -->
    <div v-if="formVisible" class="flex flex-col gap-4">
      <div class="flex items-center justify-between">
        <h4 class="text-sm font-semibold text-slate-900 dark:text-slate-100">
          {{ isCreating ? 'New Project' : `Edit: ${editingProject?.name}` }}
        </h4>
        <button type="button" class="bg-transparent border-none text-slate-400 dark:text-slate-600 text-lg cursor-pointer px-1 leading-none hover:text-slate-900 dark:hover:text-slate-100" @click="closeForm">
          &times;
        </button>
      </div>

      <div class="grid grid-cols-2 gap-3">
        <div>
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-1" for="proj-name">Name</label>
          <input
            id="proj-name"
            v-model="form.name"
            type="text"
            required
            class="w-full bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded px-2.5 py-1.5 text-sm text-slate-900 dark:text-slate-100 focus:outline-none focus:border-blue-500"
            placeholder="My Project"
          >
        </div>
        <div>
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-1" for="proj-slug">Slug</label>
          <input
            id="proj-slug"
            v-model="form.slug"
            type="text"
            required
            class="w-full bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded px-2.5 py-1.5 text-sm text-slate-900 dark:text-slate-100 font-mono focus:outline-none focus:border-blue-500"
            placeholder="my-project"
          >
        </div>
        <div class="col-span-2">
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-1" for="proj-desc">Description (optional)</label>
          <input
            id="proj-desc"
            v-model="form.description"
            type="text"
            class="w-full bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded px-2.5 py-1.5 text-sm text-slate-900 dark:text-slate-100 focus:outline-none focus:border-blue-500"
            placeholder="Short description"
          >
        </div>
        <div>
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-1" for="proj-color">Color</label>
          <div class="flex items-center gap-2">
            <input
              id="proj-color"
              v-model="form.color"
              type="color"
              class="h-8 w-12 rounded border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 cursor-pointer p-0.5"
            >
            <span class="text-xs font-mono text-slate-500 dark:text-slate-400">{{ form.color }}</span>
          </div>
        </div>
        <div>
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-1" for="proj-spawner">Default Spawner ID (optional)</label>
          <input
            id="proj-spawner"
            v-model="form.defaultSpawnerId"
            type="text"
            class="w-full bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded px-2.5 py-1.5 text-sm text-slate-900 dark:text-slate-100 font-mono focus:outline-none focus:border-blue-500"
            placeholder="spawner-id"
          >
        </div>
      </div>

      <p v-if="formError" class="text-xs text-red-600 dark:text-red-400">
        {{ formError }}
      </p>

      <div class="flex gap-2">
        <AppButton variant="info" :disabled="formSaving" @click="handleSave">
          {{ formSaving ? 'Saving…' : (isCreating ? 'Create Project' : 'Save Changes') }}
        </AppButton>
        <AppButton variant="secondary" @click="closeForm">
          Cancel
        </AppButton>
      </div>

      <!-- Folder management (only shown when editing an existing project) -->
      <template v-if="!isCreating && editingProject">
        <div class="border-t border-slate-200 dark:border-slate-700 pt-4 mt-1">
          <div class="flex items-center justify-between mb-3">
            <h5 class="text-xs font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-400">
              Folders (Working Directories)
            </h5>
            <AppButton variant="secondary" size="sm" @click="addFolderRow">
              + Add Folder
            </AppButton>
          </div>
          <p v-if="folderError" class="text-xs text-red-600 dark:text-red-400 mb-2">
            {{ folderError }}
          </p>
          <div v-if="folderLoading" class="text-xs text-slate-400 dark:text-slate-600 py-3">
            Loading folders...
          </div>
          <div v-else-if="folderRows.length === 0" class="text-xs text-slate-400 dark:text-slate-600 py-3">
            No folders yet. Add one to provide working-directory suggestions when creating tasks.
          </div>
          <table v-else class="w-full border-collapse text-[12px]">
            <thead>
              <tr>
                <th class="text-left text-[10px] uppercase tracking-wide text-slate-400 dark:text-slate-600 px-2 py-1.5 border-b border-slate-200 dark:border-slate-700">
                  Path
                </th>
                <th class="text-left text-[10px] uppercase tracking-wide text-slate-400 dark:text-slate-600 px-2 py-1.5 border-b border-slate-200 dark:border-slate-700">
                  Label
                </th>
                <th class="text-center text-[10px] uppercase tracking-wide text-slate-400 dark:text-slate-600 px-2 py-1.5 border-b border-slate-200 dark:border-slate-700">
                  Default
                </th>
                <th class="text-left text-[10px] uppercase tracking-wide text-slate-400 dark:text-slate-600 px-2 py-1.5 border-b border-slate-200 dark:border-slate-700">
                  Actions
                </th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in folderRows" :key="row._key">
                <td class="px-2 py-1.5 border-b border-slate-200 dark:border-slate-700">
                  <input
                    v-model="row.path"
                    type="text"
                    class="w-full bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded px-2 py-1 text-xs font-mono text-slate-900 dark:text-slate-100 focus:outline-none focus:border-blue-500"
                    placeholder="/absolute/path"
                    @input="row.isDirty = true"
                  >
                </td>
                <td class="px-2 py-1.5 border-b border-slate-200 dark:border-slate-700">
                  <input
                    v-model="row.label"
                    type="text"
                    class="w-full bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded px-2 py-1 text-xs text-slate-900 dark:text-slate-100 focus:outline-none focus:border-blue-500"
                    placeholder="Optional label"
                    @input="row.isDirty = true"
                  >
                </td>
                <td class="px-2 py-1.5 border-b border-slate-200 dark:border-slate-700 text-center">
                  <input
                    type="radio"
                    :name="`folder-default-${editingProject.id}`"
                    :checked="row.isDefault"
                    class="accent-blue-600"
                    @change="setDefault(row)"
                  >
                </td>
                <td class="px-2 py-1.5 border-b border-slate-200 dark:border-slate-700 whitespace-nowrap">
                  <div class="flex items-center gap-1">
                    <button
                      v-if="row.isDirty"
                      type="button"
                      :disabled="row.saving"
                      class="text-[11px] px-2 py-0.5 rounded bg-blue-600 text-white disabled:opacity-50 hover:bg-blue-700"
                      @click="saveFolderRow(row)"
                    >
                      {{ row.saving ? '…' : 'Save' }}
                    </button>
                    <button
                      type="button"
                      :disabled="row.saving"
                      class="text-[11px] px-2 py-0.5 rounded bg-transparent border-none text-red-400 hover:text-red-600 disabled:opacity-50 cursor-pointer"
                      @click="removeFolderRow(row)"
                    >
                      Remove
                    </button>
                  </div>
                  <p v-if="row.saveError" class="text-[10px] text-red-600 dark:text-red-400 mt-0.5">
                    {{ row.saveError }}
                  </p>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </template>
    </div>
  </div>
</template>
