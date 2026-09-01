<script setup lang="ts">
import type { CreateEntryInput } from '@/features/settings/composables/useMemory'
import type { ResourceScopeKind } from '@/features/settings/composables/useResources'
import { ref } from 'vue'
import { MEMORY_ENTRY_KINDS, MEMORY_SOURCE_KINDS, MemoryWriteDeniedError, useMemory } from '@/features/settings/composables/useMemory'
import { RESOURCE_SCOPE_KINDS } from '@/features/settings/composables/useResources'
import { errorMessage } from '@/utils/errorMessage'

const { spaces, globalSpaces, entries, scope, searchText, loading, error, searchError, denied, held, searchEntries, setScope, createSpace, createEntry, supersedeEntry, expireEntry } = useMemory()

function selectScopeKind(scopeKind: ResourceScopeKind) {
  if (scopeKind !== scope.value.scopeKind)
    void setScope({ scopeKind, scopeRef: scopeKind === 'global' ? '' : scope.value.scopeRef })
}

function onScopeRefChange(event: Event) {
  void setScope({ scopeKind: scope.value.scopeKind, scopeRef: (event.target as HTMLInputElement).value })
}

function formatConfidence(value: number): string {
  return value.toFixed(2)
}

// The spaces table only ever lists exact-scope rows (that is what the scope
// selector means) but a hit's space can be a global one the retriever
// unioned in (server/internal/memory/retrieve.go:165-192) — so label
// resolution alone falls back to the separately-fetched global list.
function spaceLabel(spaceId: string): string {
  const local = spaces.value.find(s => s.id === spaceId)
  if (local)
    return local.slug
  const global = globalSpaces.value.find(s => s.id === spaceId)
  return global ? global.slug : spaceId
}

function isOutsideScope(spaceId: string): boolean {
  return !spaces.value.some(s => s.id === spaceId) && globalSpaces.value.some(s => s.id === spaceId)
}

// Every write is gated on memory.write, which a user holding memory.read may
// not have — so the panel renders fully and refuses only at the write. That
// refusal is a configuration state with a known fix, like the read side's
// `denied`, but it is reported per action, next to the control that hit it:
// folding it into `denied` would blank a panel the read grant legitimately
// filled, and gating the controls on the read grant would hide the write from
// a user who does hold it.
const WRITE_DENIED_HINT = 'memory.write is not granted here. Open the Grants panel and add an allow grant for memory.write in this context.'

interface WriteFailure {
  message: string
  denied: boolean
}

function writeFailure(e: unknown, fallback: string): WriteFailure {
  return { message: errorMessage(e, fallback), denied: e instanceof MemoryWriteDeniedError }
}

function failureText(failure: WriteFailure): string {
  return failure.denied ? `${WRITE_DENIED_HINT} (${failure.message})` : failure.message
}

const spaceFormVisible = ref(false)
const spaceForm = ref({ slug: '', name: '' })
const spaceSaving = ref(false)
const spaceFailure = ref<WriteFailure | null>(null)

async function handleCreateSpace() {
  spaceFailure.value = null
  const slug = spaceForm.value.slug.trim()
  if (!slug) {
    spaceFailure.value = { message: 'Slug is required.', denied: false }
    return
  }
  spaceSaving.value = true
  try {
    await createSpace({ slug, name: spaceForm.value.name.trim() })
    spaceFormVisible.value = false
    spaceForm.value = { slug: '', name: '' }
  }
  catch (e) {
    spaceFailure.value = writeFailure(e, 'Failed to create space')
  }
  finally {
    spaceSaving.value = false
  }
}

function emptyEntryForm(): CreateEntryInput {
  return { spaceSlug: '', summary: '', content: '', kind: 'fact', sourceKind: 'user', sourceRef: '', confidence: 1 }
}

const entryFormVisible = ref(false)
const entryForm = ref<CreateEntryInput>(emptyEntryForm())
const entrySaving = ref(false)
const entryFailure = ref<WriteFailure | null>(null)

async function handleCreateEntry() {
  entryFailure.value = null
  // Validated and sent as one value: a padded slug that passes a trimmed
  // check but travels untrimmed misses capability.Match's exact comparison,
  // so the write 403s and the panel blames a grant the user already holds.
  const input: CreateEntryInput = {
    ...entryForm.value,
    spaceSlug: entryForm.value.spaceSlug.trim(),
    summary: entryForm.value.summary.trim(),
    content: entryForm.value.content.trim(),
  }
  if (!input.spaceSlug || !input.summary || !input.content) {
    entryFailure.value = { message: 'Space, summary and content are required.', denied: false }
    return
  }
  entrySaving.value = true
  try {
    await createEntry(input)
    entryFormVisible.value = false
    entryForm.value = emptyEntryForm()
  }
  catch (e) {
    entryFailure.value = writeFailure(e, 'Failed to create entry')
  }
  finally {
    entrySaving.value = false
  }
}

const supersedingId = ref<string | null>(null)
const supersededBy = ref('')
const confirmExpireId = ref<string | null>(null)
// Carries the entry id so the failure renders in the row it came from.
const entryActionFailure = ref<(WriteFailure & { id: string }) | null>(null)

async function handleSupersede(id: string) {
  entryActionFailure.value = null
  const replacement = supersededBy.value.trim()
  if (!replacement) {
    entryActionFailure.value = { id, message: 'The replacing entry id is required.', denied: false }
    return
  }
  try {
    await supersedeEntry(id, replacement)
    supersedingId.value = null
    supersededBy.value = ''
  }
  catch (e) {
    entryActionFailure.value = { id, ...writeFailure(e, 'Failed to supersede entry') }
  }
}

async function handleExpire(id: string) {
  entryActionFailure.value = null
  try {
    await expireEntry(id)
    confirmExpireId.value = null
  }
  catch (e) {
    entryActionFailure.value = { id, ...writeFailure(e, 'Failed to expire entry') }
  }
}
</script>

<template>
  <div class="flex flex-col gap-4">
    <div>
      <h3 class="text-[17px] font-bold text-fg mb-1">
        Memory
      </h3>
      <p class="text-xs text-fg-mute">
        What the system has learned and kept: spaces group entries, entries hold one conclusion each with a confidence and a source. Reading requires the <code>memory.read</code> capability in the selected scope; creating, superseding and expiring require <code>memory.write</code> there.
      </p>
    </div>

    <div class="flex flex-wrap items-center gap-2">
      <span class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute">Scope</span>
      <button
        v-for="s in RESOURCE_SCOPE_KINDS"
        :key="s"
        type="button"
        :data-testid="`memory-scope-${s}`"
        class="px-2 py-0.5 rounded text-[11px] border border-line cursor-pointer"
        :class="s === scope.scopeKind ? 'bg-info-soft text-info-text border-transparent font-semibold' : 'bg-transparent text-fg-mute hover:text-fg'"
        @click="selectScopeKind(s)"
      >
        {{ s }}
      </button>
      <input
        v-if="scope.scopeKind !== 'global'"
        :value="scope.scopeRef"
        data-testid="memory-scope-ref"
        type="text"
        placeholder="scope ref, e.g. /home/me/project"
        class="w-72 bg-card border border-line rounded px-2.5 py-1 text-xs text-fg font-mono focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
        @change="onScopeRefChange"
      >
    </div>

    <div class="flex items-center justify-between">
      <h4 class="text-sm font-semibold text-fg">
        Spaces
      </h4>
      <button type="button" data-testid="memory-space-new" class="px-3 py-1 rounded border border-line bg-raised text-fg text-xs cursor-pointer hover:bg-card" @click="spaceFormVisible = !spaceFormVisible">
        + New space
      </button>
    </div>
    <div v-if="spaceFormVisible" class="grid grid-cols-2 gap-3">
      <input
        v-model="spaceForm.slug"
        data-testid="memory-space-slug"
        type="text"
        placeholder="slug, e.g. project-notes"
        class="bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg font-mono focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
      >
      <input
        v-model="spaceForm.name"
        data-testid="memory-space-name"
        type="text"
        placeholder="display name"
        class="bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
      >
      <button type="button" data-testid="memory-space-submit" :disabled="spaceSaving" class="col-span-2 justify-self-start px-3 py-1.5 rounded border border-line bg-info-soft text-info-text text-sm cursor-pointer disabled:opacity-50" @click="handleCreateSpace">
        {{ spaceSaving ? 'Creating…' : 'Create space' }}
      </button>
    </div>
    <div v-if="spaceFailure" data-testid="memory-space-error" class="rounded border border-danger-line bg-danger-soft text-danger-text px-3 py-2 text-xs">
      {{ failureText(spaceFailure) }}
    </div>

    <div v-if="denied" data-testid="memory-denied" class="rounded border border-warning-line bg-warning-soft text-warning-text px-3 py-2 text-xs">
      <strong>memory.read is not granted here.</strong>
      {{ denied }}
      Open the Grants panel and add an <code>allow</code> grant for <code>memory.read</code> in this context to read memory from the dashboard.
    </div>
    <div v-else-if="error" data-testid="memory-error" class="rounded border border-danger-line bg-danger-soft text-danger-text px-3 py-2 text-xs">
      {{ error }}
    </div>
    <div v-else-if="loading" data-testid="memory-loading" class="text-center py-12 text-fg-mute text-sm">
      Loading memory spaces...
    </div>
    <div v-else-if="held" data-testid="memory-held" class="text-center py-8 text-fg-mute text-sm">
      Enter a scope ref to search.
    </div>
    <div v-else-if="!spaces.length" data-testid="memory-empty" class="text-center py-8 text-fg-mute text-sm">
      No memory spaces in this scope yet.
    </div>
    <table v-else class="w-full border-collapse text-[13px]">
      <thead>
        <tr>
          <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
            Space
          </th>
          <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
            Name
          </th>
          <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
            Scope
          </th>
          <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
            State
          </th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="s in spaces" :key="s.id" :data-testid="`memory-space-${s.id}`">
          <td class="px-3 py-2.5 border-b border-line text-fg font-mono text-xs">
            {{ s.slug }}
          </td>
          <td class="px-3 py-2.5 border-b border-line text-fg">
            {{ s.name || '—' }}
          </td>
          <td class="px-3 py-2.5 border-b border-line text-fg-mute font-mono text-xs">
            {{ s.scopeRef ? `${s.scopeKind}: ${s.scopeRef}` : s.scopeKind }}
          </td>
          <td class="px-3 py-2.5 border-b border-line text-fg-mute">
            {{ s.state }}
          </td>
        </tr>
      </tbody>
    </table>

    <div class="flex items-center justify-between pt-2">
      <h4 class="text-sm font-semibold text-fg">
        Entries
      </h4>
      <button type="button" data-testid="memory-entry-new" class="px-3 py-1 rounded border border-line bg-raised text-fg text-xs cursor-pointer hover:bg-card" @click="entryFormVisible = !entryFormVisible">
        + New entry
      </button>
    </div>
    <div v-if="entryFormVisible" class="grid grid-cols-2 gap-3">
      <input
        v-model="entryForm.spaceSlug"
        data-testid="memory-entry-space"
        type="text"
        placeholder="space slug"
        class="bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg font-mono focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
      >
      <input
        v-model="entryForm.summary"
        data-testid="memory-entry-summary"
        type="text"
        placeholder="summary — this is what gets pushed into a spawn"
        class="bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
      >
      <textarea
        v-model="entryForm.content"
        data-testid="memory-entry-content"
        rows="4"
        placeholder="content — pulled on demand"
        class="col-span-2 bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
      />
      <select v-model="entryForm.kind" data-testid="memory-entry-kind" class="bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg">
        <option v-for="k in MEMORY_ENTRY_KINDS" :key="k" :value="k">
          {{ k }}
        </option>
      </select>
      <select v-model="entryForm.sourceKind" data-testid="memory-entry-source-kind" class="bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg">
        <option v-for="s in MEMORY_SOURCE_KINDS" :key="s" :value="s">
          {{ s }}
        </option>
      </select>
      <button type="button" data-testid="memory-entry-submit" :disabled="entrySaving" class="col-span-2 justify-self-start px-3 py-1.5 rounded border border-line bg-info-soft text-info-text text-sm cursor-pointer disabled:opacity-50" @click="handleCreateEntry">
        {{ entrySaving ? 'Creating…' : 'Create entry' }}
      </button>
    </div>
    <div v-if="entryFailure" data-testid="memory-entry-error" class="rounded border border-danger-line bg-danger-soft text-danger-text px-3 py-2 text-xs">
      {{ failureText(entryFailure) }}
    </div>

    <!-- Sibling of the state chain above, not nested inside it: the spaces
         list can be empty for this exact scope while entries are still
         searchable (the retriever also unions in every global space), so
         search must stay reachable regardless of what the spaces table shows. -->
    <div class="flex items-center gap-2 pt-2 border-t border-line">
      <input
        v-model="searchText"
        data-testid="memory-search-input"
        type="text"
        placeholder="Search entries in this scope"
        class="flex-1 bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
        @keyup.enter="searchEntries()"
      >
      <button
        type="button"
        data-testid="memory-search-submit"
        class="px-3 py-1.5 rounded border border-line bg-raised text-fg text-sm cursor-pointer hover:bg-card"
        @click="searchEntries()"
      >
        Search
      </button>
    </div>
    <div v-if="searchError" data-testid="memory-search-error" class="rounded border border-danger-line bg-danger-soft text-danger-text px-3 py-2 text-xs">
      {{ searchError }}
    </div>

    <div v-for="e in entries" :key="e.id" :data-testid="`memory-entry-${e.id}`" class="px-3 py-2.5 bg-app rounded-md">
      <div class="flex items-center gap-2.5 mb-1">
        <span class="font-semibold text-xs text-fg">{{ e.summary }}</span>
        <span class="ml-auto font-mono text-[11px] text-fg-mute">{{ e.kind }} · {{ formatConfidence(e.confidence) }}</span>
      </div>
      <div class="text-[11px] text-fg-mute">
        in <code>{{ spaceLabel(e.spaceId) }}</code>
        <span v-if="isOutsideScope(e.spaceId)" :data-testid="`memory-entry-outside-scope-${e.id}`" class="italic">
          — global, outside this scope
        </span>
      </div>
      <p class="text-[11px] text-fg-mute mt-1 whitespace-pre-wrap">
        {{ e.content }}
      </p>
      <div class="flex items-center gap-2 mt-2">
        <template v-if="supersedingId === e.id">
          <input
            v-model="supersededBy"
            :data-testid="`memory-supersede-input-${e.id}`"
            type="text"
            placeholder="id of the replacing entry"
            class="w-64 bg-card border border-line rounded px-2 py-1 text-xs text-fg font-mono"
          >
          <button type="button" :data-testid="`memory-supersede-confirm-${e.id}`" class="px-2 py-1 rounded border border-line bg-info-soft text-info-text text-xs cursor-pointer" @click="handleSupersede(e.id)">
            Confirm
          </button>
          <button type="button" class="px-2 py-1 rounded border border-line bg-transparent text-fg-mute text-xs cursor-pointer" @click="supersedingId = null">
            Cancel
          </button>
        </template>
        <button v-else type="button" :data-testid="`memory-supersede-${e.id}`" class="px-2 py-1 rounded border border-line bg-transparent text-fg-mute text-xs cursor-pointer hover:text-fg" @click="supersedingId = e.id; supersededBy = ''">
          Supersede
        </button>

        <template v-if="confirmExpireId === e.id">
          <button type="button" :data-testid="`memory-expire-confirm-${e.id}`" class="px-2 py-1 rounded border border-danger-line bg-danger-soft text-danger-text text-xs cursor-pointer" @click="handleExpire(e.id)">
            Confirm expire
          </button>
          <button type="button" class="px-2 py-1 rounded border border-line bg-transparent text-fg-mute text-xs cursor-pointer" @click="confirmExpireId = null">
            Cancel
          </button>
        </template>
        <button v-else type="button" :data-testid="`memory-expire-${e.id}`" class="px-2 py-1 rounded border border-line bg-transparent text-fg-mute text-xs cursor-pointer hover:text-red-600 dark:hover:text-red-400" @click="confirmExpireId = e.id">
          Expire
        </button>
      </div>
      <p v-if="entryActionFailure?.id === e.id" :data-testid="`memory-entry-action-error-${e.id}`" class="text-[11px] text-danger-text mt-1">
        {{ failureText(entryActionFailure) }}
      </p>
    </div>
  </div>
</template>
